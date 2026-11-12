// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"log"
	"sync"
	"time"

	"github.com/VuteTech/Bor/agent/internal/policyclient"
)

// renewRetryInterval is the least time between two certificate renewals the
// server asks for, so a server that keeps asking cannot make the agent
// renew in a loop.
const renewRetryInterval = time.Hour

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
