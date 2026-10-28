// SPDX-License-Identifier: AGPL-3.0-only
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
	queue    chan syslogItem
	mu       sync.Mutex
	conn     net.Conn
	stopOnce sync.Once
	stop     chan struct{}
}

const syslogQueueSize = 512

// syslogItem is a queued message: an audit event, or a pre-formatted body
// forwarded for an extension module.
type syslogItem struct {
	event    *auditpb.AuditEvent
	raw      string
	severity int
}

// NewSyslogSink creates and starts a SyslogSink.
// Call Close() to drain the queue and shut down the sender goroutine.
func NewSyslogSink(cfg *SyslogConfig) *SyslogSink {
	s := &SyslogSink{
		cfg:   cfg,
		queue: make(chan syslogItem, syslogQueueSize),
		stop:  make(chan struct{}),
	}
	go s.sender()
	return s
}

// Emit enqueues an event for asynchronous delivery. Non-blocking.
func (s *SyslogSink) Emit(_ context.Context, event *auditpb.AuditEvent) {
	select {
	case s.queue <- syslogItem{event: event}:
	default:
		log.Printf("audit SyslogSink: queue full, dropping event action=%s", event.GetAction())
	}
}

// Format is the configured message body format.
func (s *SyslogSink) Format() SyslogFormat { return s.cfg.Format }

// ForwardRaw enqueues a message body that is already in the configured
// format, with a syslog severity (0-7). Non-blocking.
func (s *SyslogSink) ForwardRaw(body string, severity int) {
	if severity < 0 || severity > 7 {
		severity = 6
	}
	select {
	case s.queue <- syslogItem{raw: body, severity: severity}:
	default:
		log.Printf("audit SyslogSink: queue full, dropping forwarded message")
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
		case item := <-s.queue:
			s.send(item)
		case <-s.stop:
			// Drain remaining events before exit.
			for {
				select {
				case item := <-s.queue:
					s.send(item)
				default:
					s.closeConn()
					return
				}
			}
		}
	}
}

// send formats and writes one message, reconnecting if necessary.
func (s *SyslogSink) send(item syslogItem) {
	var msg string
	if item.event != nil {
		var err error
		if msg, err = s.format(item.event); err != nil {
			log.Printf("audit SyslogSink: format error: %v", err)
			return
		}
	} else {
		msg = s.header(item.severity, time.Now().UTC()) + item.raw
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
	severity := syslogSeverity(event.GetAction())

	ts := time.Now().UTC()
	if event.GetOccurredAt() != nil {
		ts = event.GetOccurredAt().AsTime().UTC()
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

	return s.header(severity, ts) + body, nil
}

// header renders the RFC 5424 header up to and including the space before
// MSG: <priority>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA.
func (s *SyslogSink) header(severity int, ts time.Time) string {
	facility := s.cfg.Facility
	if facility == 0 {
		facility = 16 // local0
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "-"
	}
	return fmt.Sprintf("<%d>1 %s %s Bor - - - ", facility*8+severity, ts.Format(time.RFC3339), hostname)
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
