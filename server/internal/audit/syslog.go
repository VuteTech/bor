// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package audit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

// SyslogFormat selects the message body format.
type SyslogFormat string

// Supported syslog message body formats.
const (
	FormatCEFSyslog  SyslogFormat = "cef"
	FormatOCSFSyslog SyslogFormat = "ocsf"
)

// SyslogConfig holds all configuration for the syslog sink.
type SyslogConfig struct {
	Enabled  bool
	Network  string       // "udp" | "tcp" | "tcp+tls"
	Addr     string       // "host:port"
	Format   SyslogFormat // "cef" | "ocsf"
	Facility int          // syslog facility (default 16 = local0)
	// Optional: PEM-encoded CA certificate file for tcp+tls server verification.
	// If empty, the system cert pool is used.
	TLSCAFile string
}

// SyslogSink implements Sink and forwards AuditEvents to a remote syslog
// receiver over UDP, TCP, or TLS-over-TCP using RFC 5424 framing.
//
// Events are dispatched asynchronously via a buffered channel so that a slow
// or unreachable SIEM never blocks the request path. When the channel is full,
// events are dropped and a warning is logged.
type SyslogSink struct {
	cfg      *SyslogConfig
	queue    chan *auditpb.AuditEvent
	mu       sync.Mutex
	conn     net.Conn
	stopOnce sync.Once
	stop     chan struct{}
}

const syslogQueueSize = 512

// NewSyslogSink creates and starts a SyslogSink.
// Call Close() to drain the queue and shut down the sender goroutine.
func NewSyslogSink(cfg *SyslogConfig) *SyslogSink {
	s := &SyslogSink{
		cfg:   cfg,
		queue: make(chan *auditpb.AuditEvent, syslogQueueSize),
		stop:  make(chan struct{}),
	}
	go s.sender()
	return s
}

// Emit enqueues an event for asynchronous delivery. Non-blocking.
func (s *SyslogSink) Emit(_ context.Context, event *auditpb.AuditEvent) {
	select {
	case s.queue <- event:
	default:
		log.Printf("audit SyslogSink: queue full, dropping event action=%s", event.GetAction())
	}
}

// Close stops the sender and waits for the queue to drain (up to 5 s).
func (s *SyslogSink) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// sender is the background goroutine that dequeues and sends events.
func (s *SyslogSink) sender() {
	for {
		select {
		case event := <-s.queue:
			s.send(event)
		case <-s.stop:
			// Drain remaining events before exit.
			for {
				select {
				case event := <-s.queue:
					s.send(event)
				default:
					s.closeConn()
					return
				}
			}
		}
	}
}

// send formats and writes one event, reconnecting if necessary.
func (s *SyslogSink) send(event *auditpb.AuditEvent) {
	msg, err := s.format(event)
	if err != nil {
		log.Printf("audit SyslogSink: format error: %v", err)
		return
	}

	frame := s.frame(msg)

	for attempt := 0; attempt < 2; attempt++ {
		conn, err := s.getConn()
		if err != nil {
			log.Printf("audit SyslogSink: connect %s: %v", s.cfg.Addr, err)
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, werr := fmt.Fprint(conn, frame); werr != nil {
			// Connection broken — reset and retry once.
			s.closeConn()
			continue
		}
		return
	}
	log.Printf("audit SyslogSink: failed to deliver event after reconnect, dropping")
}

// format renders the AuditEvent as a RFC 5424 syslog message string.
// The syslog header uses priority derived from facility + severity.
func (s *SyslogSink) format(event *auditpb.AuditEvent) (string, error) {
	facility := s.cfg.Facility
	if facility == 0 {
		facility = 16 // local0
	}

	severity := syslogSeverity(event.GetAction())
	priority := facility*8 + severity

	ts := time.Now().UTC()
	if event.GetOccurredAt() != nil {
		ts = event.GetOccurredAt().AsTime().UTC()
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "-"
	}

	var body string
	switch s.cfg.Format {
	case FormatOCSFSyslog:
		j, err := FormatOCSF(event)
		if err != nil {
			return "", fmt.Errorf("ocsf format: %w", err)
		}
		body = j
	default: // CEF
		body = FormatCEF(event)
	}

	// RFC 5424: <priority>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
	return fmt.Sprintf("<%d>1 %s %s Bor - - - %s",
		priority,
		ts.Format(time.RFC3339),
		hostname,
		body,
	), nil
}

// frame wraps a syslog message for the configured transport.
// UDP: send as-is. TCP: append newline (octet-stuffing is sufficient for most SIEMs).
func (s *SyslogSink) frame(msg string) string {
	if strings.HasPrefix(s.cfg.Network, "tcp") {
		return msg + "\n"
	}
	return msg
}

// getConn returns the existing connection or dials a new one.
func (s *SyslogSink) getConn() (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return s.conn, nil
	}
	conn, err := s.dial()
	if err != nil {
		return nil, err
	}
	s.conn = conn
	return conn, nil
}

func (s *SyslogSink) closeConn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *SyslogSink) dial() (net.Conn, error) {
	network := s.cfg.Network
	switch network {
	case "tcp+tls":
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if s.cfg.TLSCAFile != "" {
			pem, err := os.ReadFile(s.cfg.TLSCAFile) //nolint:gosec // admin-controlled path
			if err != nil {
				return nil, fmt.Errorf("read TLS CA: %w", err)
			}
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(pem)
			tlsCfg.RootCAs = pool
		}
		return tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", s.cfg.Addr, tlsCfg)
	case "tcp":
		return net.DialTimeout("tcp", s.cfg.Addr, 10*time.Second)
	default: // udp
		return net.DialTimeout("udp", s.cfg.Addr, 10*time.Second)
	}
}

// syslogSeverity maps Bor action names to RFC 5424 severity values.
// 0=Emergency 1=Alert 2=Critical 3=Error 4=Warning 5=Notice 6=Info 7=Debug
func syslogSeverity(action string) int {
	switch action {
	case "tamper_detected":
		return 4 // Warning
	case "delete":
		return 5 // Notice
	case "create", "update":
		return 6 // Info
	default:
		return 6 // Info
	}
}
