// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package apparmor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Parser runs apparmor_parser with constant argv; the only variable
// argument is a profile file path under the profile directory.
type Parser struct {
	// Binary is the executable name or path (default "apparmor_parser",
	// also searched in /usr/sbin and /sbin).
	Binary string
	// Dir is the profile directory passed as the include base.
	Dir string
	// Run executes the command; injected so tests can stub it.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultParserBinary is the executable name.
const DefaultParserBinary = "apparmor_parser"

// NewParser returns a Parser that executes the binary.
func NewParser(binary, dir string) *Parser {
	if binary == "" {
		binary = DefaultParserBinary
	}
	return &Parser{
		Binary: binary,
		Dir:    dir,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // G204: binary from agent config, constant flags, validated file path
		},
	}
}

// path resolves the binary, looking in the sbin directories too because
// Debian keeps apparmor_parser out of the user PATH.
func (p *Parser) path() string {
	if filepath.IsAbs(p.Binary) {
		return p.Binary
	}
	if path, err := exec.LookPath(p.Binary); err == nil {
		return path
	}
	for _, dir := range []string{"/usr/sbin", "/sbin"} {
		candidate := filepath.Join(dir, p.Binary)
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// Available reports whether the binary can be found.
func (p *Parser) Available() bool {
	return p != nil && p.path() != ""
}

// Version returns the parser version ("4.1.0"), "" when unknown.
func (p *Parser) Version(ctx context.Context) string {
	bin := p.path()
	if bin == "" {
		return ""
	}
	out, err := p.Run(ctx, bin, "--version")
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func (p *Parser) run(ctx context.Context, what string, args ...string) error {
	bin := p.path()
	if bin == "" {
		return fmt.Errorf("apparmor_parser is not installed")
	}
	out, err := p.Run(ctx, bin, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if len(msg) > 400 {
			msg = msg[:400]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: %s", what, msg)
	}
	return nil
}

// Check compiles a profile file without loading it (-Q -K): the syntax
// check the agent runs before it writes a profile into place.
func (p *Parser) Check(ctx context.Context, file string) error {
	return p.run(ctx, "profile check", "-Q", "-K", "-b", p.Dir, file)
}

// Replace loads or reloads a profile file, in complain mode when asked.
// A force-complain link forces complain mode on its own; -C makes the
// intent explicit.
func (p *Parser) Replace(ctx context.Context, file string, complain bool) error {
	args := []string{"-r"}
	if complain {
		args = append(args, "-C")
	}
	args = append(args, "-b", p.Dir, file)
	return p.run(ctx, "profile load", args...)
}

// Remove unloads a profile file's profiles.
func (p *Parser) Remove(ctx context.Context, file string) error {
	return p.run(ctx, "profile unload", "-R", "-b", p.Dir, file)
}

// ReplaceDir reloads every profile of the directory (after a tunables
// change).
func (p *Parser) ReplaceDir(ctx context.Context) error {
	return p.run(ctx, "profile reload", "-r", "-b", p.Dir, p.Dir)
}
