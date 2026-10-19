// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	// RunEnv executes the command with extra environment variables; nil
	// falls back to Run (tests stub Run only).
	RunEnv func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
}

// userVerifyEnv lets "systemd-analyze --user verify" initialise its
// offline manager as root: it looks up the runtime directory from
// XDG_RUNTIME_DIR and fails with ENXIO when root has none; any existing
// directory satisfies it, nothing is written there.
var userVerifyEnv = []string{"XDG_RUNTIME_DIR=/run"}

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
		RunEnv: func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: binary from agent config, constant subcommand, validated value after "--"
			cmd.Env = append(os.Environ(), env...)
			return cmd.CombinedOutput()
		},
	}
}

// run executes with extra environment variables when RunEnv is set.
func (a *Analyzer) run(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	if a.RunEnv != nil {
		return a.RunEnv(ctx, env, name, args...)
	}
	return a.Run(ctx, name, args...)
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

// VerifyUserUnit runs "systemd-analyze --user verify <unit>" for a unit
// file of the user-manager scope.
func (a *Analyzer) VerifyUserUnit(ctx context.Context, unit string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := a.run(ctx, userVerifyEnv, a.Binary, "--user", "verify", "--", unit)
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

// SecurityScore is the sandboxing exposure systemd-analyze security
// computes for a service: a level from 0.0 (fully sandboxed) to 10.0 and
// its predicate (SAFE, OK, MEDIUM, EXPOSED or UNSAFE).
type SecurityScore struct {
	Exposure  string
	Predicate string
}

var (
	securityExposureRE  = regexp.MustCompile(`^\d{1,2}(\.\d)?$`)
	securityPredicateRE = regexp.MustCompile(`^[A-Z]{1,12}$`)
)

// securityTimeout bounds the analysis of every loaded service at once.
const securityTimeout = 90 * time.Second

// SecurityScores runs "systemd-analyze security --json=short" once for
// every loaded service of the system manager and returns the scores by
// unit name. An analyzer that lacks JSON output (systemd before 246)
// yields an error and no scores.
func (a *Analyzer) SecurityScores(ctx context.Context) (map[string]SecurityScore, error) {
	if a == nil {
		return nil, fmt.Errorf("no analyzer")
	}
	ctx, cancel := context.WithTimeout(ctx, securityTimeout)
	defer cancel()
	out, err := a.Run(ctx, a.Binary, "security", "--json=short", "--no-pager")
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("systemd-analyze security: %s", firstLine(msg))
	}
	return ParseSecurityScores(out)
}

// ParseSecurityScores parses the JSON list systemd-analyze security prints
// for several units ({"unit","exposure","predicate","happy"} objects).
func ParseSecurityScores(out []byte) (map[string]SecurityScore, error) {
	var rows []struct {
		Unit      string `json:"unit"`
		Exposure  string `json:"exposure"`
		Predicate string `json:"predicate"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("systemd-analyze security: unexpected output: %w", err)
	}
	scores := make(map[string]SecurityScore, len(rows))
	for _, r := range rows {
		if r.Unit == "" || !securityExposureRE.MatchString(r.Exposure) || !securityPredicateRE.MatchString(r.Predicate) {
			continue
		}
		scores[r.Unit] = SecurityScore{Exposure: r.Exposure, Predicate: r.Predicate}
	}
	return scores, nil
}
