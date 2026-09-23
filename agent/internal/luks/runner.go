// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package luks implements the DiskEncryption policy type on the agent:
// LUKS2 inventory, recovery-key escrow and rotation, TPM2 and Tang/Clevis
// protectors, and initramfs verification/management
// .
package luks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Command deadlines.
const (
	cryptTimeout     = 60 * time.Second
	dracutTimeout    = 10 * time.Minute
	defaultWaitDelay = 10 * time.Second
)

// runResult carries a finished command's output.
type runResult struct {
	Stdout string
	Stderr string
}

// runRequest describes one command invocation. This is the only place in the
// agent that passes secrets to child processes: they travel exclusively via
// stdin and one inherited pipe (fd 3), never through argv, files or the
// environment. Argv values are fixed flags plus validated identifiers.
type runRequest struct {
	// Name is the binary (resolved via PATH or an absolute path from config).
	Name string
	// Args are the fixed/validated arguments.
	Args []string
	// Stdin is written to the child's stdin and zeroed by run.
	Stdin []byte
	// FD3 is exposed to the child as /proc/self/fd/3 and zeroed by run.
	FD3 []byte
	// Timeout bounds the call (default cryptTimeout).
	Timeout time.Duration
	// Env replaces the default environment when non-nil.
	Env []string
}

// runCommand executes a runRequest. Package var so tests can stub every CLI.
var runCommand = execRun

// lookPath reports whether an executable is on PATH. Package var for tests.
var lookPath = func(file string) error {
	_, err := exec.LookPath(file)
	return err
}

// execRun is the real implementation of runCommand.
func execRun(ctx context.Context, req *runRequest) (*runResult, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = cryptTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, req.Name, req.Args...) //nolint:gosec // G204: fixed binary + constant/validated args, see runRequest doc
	cmd.Env = defaultEnv(req.Env)
	cmd.WaitDelay = defaultWaitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(req.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}

	var fd3Files []*os.File
	if req.FD3 != nil {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, fmt.Errorf("luks: fd3 pipe: %w", err)
		}
		cmd.ExtraFiles = []*os.File{r} // child sees the read end as fd 3
		fd3Files = []*os.File{r, w}
		// Write the secret from a goroutine so a large payload cannot
		// deadlock against the child's own output.
		go func() {
			_, _ = w.Write(req.FD3)
			_ = w.Close()
		}()
	}

	err := cmd.Run()

	for _, f := range fd3Files {
		_ = f.Close()
	}
	Zero(req.Stdin)
	Zero(req.FD3)

	res := &runResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		if runCtx.Err() != nil {
			return res, fmt.Errorf("%s timed out: %w", req.Name, runCtx.Err())
		}
		return res, fmt.Errorf("%s: %w (%s)", req.Name, err, LastLine(res.Stderr+"\n"+res.Stdout))
	}
	return res, nil
}

// defaultEnv builds a minimal, explicit child environment: C locale so output
// is parseable, and the parent's PATH. Secrets never enter the environment.
func defaultEnv(extra []string) []string {
	env := []string{"LC_ALL=C", "LANG=C"}
	if p := os.Getenv("PATH"); p != "" {
		env = append(env, "PATH="+p)
	} else {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return append(env, extra...)
}

// RunForOutput runs a fixed binary and returns its stdout. For callers
// outside the package that need the same runner discipline (deadline,
// WaitDelay, C locale), e.g. systemd-ask-password in the adopt CLI.
func RunForOutput(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	res, err := runCommand(ctx, &runRequest{Name: name, Args: args, Timeout: timeout})
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// LastLine returns the last non-empty line of command output, trimmed and
// truncated for compliance messages.
func LastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		l = strings.TrimPrefix(l, "error: ")
		l = strings.TrimPrefix(l, "Error: ")
		if len(l) > 300 {
			l = l[:300] + "…"
		}
		return l
	}
	return ""
}

// Zero overwrites a secret byte slice.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// IsTimeout reports whether err is a command deadline expiry.
func IsTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
