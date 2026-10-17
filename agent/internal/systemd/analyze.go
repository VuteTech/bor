// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Analyzer wraps systemd-analyze for the checks the policy runs before it
// activates a change. Every invocation uses a constant argv; the only
// variable argument is placed after "--" and was validated against a
// strict character set by the server and again by the agent.
type Analyzer struct {
	// Binary is the executable name or path (default "systemd-analyze").
	Binary string
	// Run executes the command; injected so tests can stub it.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultAnalyzeBinary is the executable name resolved through PATH.
const DefaultAnalyzeBinary = "systemd-analyze"

// NewAnalyzer returns an Analyzer that executes the binary.
func NewAnalyzer(binary string) *Analyzer {
	if binary == "" {
		binary = DefaultAnalyzeBinary
	}
	return &Analyzer{
		Binary: binary,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // G204: binary from agent config, constant subcommand, validated value after "--"
		},
	}
}

// Available reports whether the binary can be found.
func (a *Analyzer) Available() bool {
	if a == nil {
		return false
	}
	_, err := exec.LookPath(a.Binary)
	return err == nil
}

// ValidateCalendar runs "systemd-analyze calendar -- <expr>" and returns
// the parser's message on failure.
func (a *Analyzer) ValidateCalendar(ctx context.Context, expr string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := a.Run(ctx, a.Binary, "calendar", "--", expr)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("invalid calendar expression %q: %s", expr, firstLine(msg))
	}
	return nil
}

// VerifyUnit runs "systemd-analyze verify <unit>" so a drop-in that
// systemd would reject is caught before the daemon reload.
func (a *Analyzer) VerifyUnit(ctx context.Context, unit string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := a.Run(ctx, a.Binary, "verify", "--", unit)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", firstLine(msg))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
