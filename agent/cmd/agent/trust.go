// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/policyclient"
)

// renewRetryInterval is the least time between two certificate renewals the
// server asks for, so a server that keeps asking cannot make the agent
// renew in a loop.
const renewRetryInterval = time.Hour

// certRenewThreshold is how long before expiry the agent renews its
// certificate, and certCheckInterval how often a running agent checks.
// The server issues certificates for 90 days.
const (
	certRenewThreshold = 30 * 24 * time.Hour
	certCheckInterval  = 12 * time.Hour
)

// agentCredentials is where the enrolled agent keeps its CA certificates and
// its own certificate, and how it reacts when the server replaces its CA.
type agentCredentials struct {
	serverAddr string
	paths      policyclient.EnrollmentPaths

	mu          sync.Mutex
	lastRenewal time.Time
}

// agentCreds is set once the agent is enrolled, before the first
// heartbeat. Nil while enrolling.
var agentCreds *agentCredentials

// trustFingerprint is what a heartbeat reports about the CA certificate
// file ("" before enrollment).
func (c *agentCredentials) trustFingerprint() string {
	if c == nil {
		return ""
	}
	return policyclient.TrustBundleSHA256(c.paths.CACert)
}

// apply acts on a heartbeat answer: it installs the CA certificates the
// server sent and renews the agent certificate when asked to. Both take
// effect on the next connection; the current one stays up. It reports
// whether new CA certificates were installed, which the caller confirms to
// the server with another heartbeat.
func (c *agentCredentials) apply(res *policyclient.HeartbeatResult) (trustChanged bool) {
	if c == nil || res == nil {
		return false
	}
	if len(res.TrustBundlePEM) > 0 {
		if err := policyclient.WriteTrustBundle(c.paths.CACert, res.TrustBundlePEM); err != nil {
			log.Printf("Server sent new CA certificates, but they were not installed: %v", err)
		} else {
			log.Printf("Installed new CA certificates from the server in %s", c.paths.CACert)
			trustChanged = true
		}
	}
	if !res.RenewCertificate {
		return trustChanged
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lastRenewal.IsZero() && time.Since(c.lastRenewal) < renewRetryInterval {
		return trustChanged
	}
	c.lastRenewal = time.Now()
	log.Println("Server asked for a certificate renewal (its CA is being replaced), renewing...")
	if err := policyclient.RenewCertificate(c.serverAddr, c.paths.CACert, c.paths.CertFile, c.paths.KeyFile); err != nil {
		log.Printf("Warning: certificate renewal failed: %v - will retry later", err)
		return trustChanged
	}
	log.Println("Certificate renewed")
	return trustChanged
}

// renewIfExpiring renews the agent certificate when it expires within
// certRenewThreshold. The renewed certificate is used from the next
// connection on; the server closes connections daily.
func (c *agentCredentials) renewIfExpiring() {
	if c == nil {
		return
	}
	expiring, err := policyclient.CertExpiringSoon(c.paths.CertFile, certRenewThreshold)
	if err != nil {
		log.Printf("Warning: could not check certificate expiry: %v", err)
		return
	}
	if !expiring {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lastRenewal.IsZero() && time.Since(c.lastRenewal) < renewRetryInterval {
		return
	}
	c.lastRenewal = time.Now()
	log.Println("Certificate expires within 30 days, renewing...")
	if err := policyclient.RenewCertificate(c.serverAddr, c.paths.CACert, c.paths.CertFile, c.paths.KeyFile); err != nil {
		log.Printf("Warning: certificate renewal failed: %v - will retry in %s", err, certCheckInterval)
		return
	}
	log.Println("Certificate renewed")
}

// watchCertExpiry checks the agent certificate every certCheckInterval
// until ctx ends, so an agent that runs for months renews in time.
func (c *agentCredentials) watchCertExpiry(ctx context.Context) {
	ticker := time.NewTicker(certCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.renewIfExpiring()
		}
	}
}
