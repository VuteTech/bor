// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package selinux

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Tools runs the SELinux command line tools with constant argv. Every
// variable value (boolean names, module names, file paths) was validated
// against the shared rules before it reaches a command.
type Tools struct {
	SetseboolBin       string
	SemoduleBin        string
	RestoreconBin      string
	SemanageBin        string
	Audit2allowBin     string
	CheckmoduleBin     string
	SemodulePackageBin string
	// HLLPP is the pp-to-CIL converter libsemanage uses
	// (/usr/libexec/selinux/hll/pp).
	HLLPPBin string
	// LockRetries is how often a commit is retried when the store is busy.
	LockRetries int
	// RetryBase is the first back-off; it doubles per attempt.
	RetryBase time.Duration
	// Run executes a command; injected so tests can stub it.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// RunInput executes a command with data on stdin.
	RunInput func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error)
	// Sleep waits between retries; injected for tests.
	Sleep func(time.Duration)
	// LookPath resolves a binary; injected for tests.
	LookPath func(string) (string, error)
}

// NewTools returns Tools that execute the real binaries.
func NewTools() *Tools {
	return &Tools{
		SetseboolBin:       "setsebool",
		SemoduleBin:        "semodule",
		RestoreconBin:      "restorecon",
		SemanageBin:        "semanage",
		Audit2allowBin:     "audit2allow",
		CheckmoduleBin:     "checkmodule",
		SemodulePackageBin: "semodule_package",
		HLLPPBin:           "/usr/libexec/selinux/hll/pp",
		LockRetries:        4,
		RetryBase:          2 * time.Second,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // G204: constant binaries, validated arguments
		},
		RunInput: func(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: constant binaries, validated arguments
			cmd.Stdin = bytes.NewReader(input)
			return cmd.CombinedOutput()
		},
		Sleep:    time.Sleep,
		LookPath: exec.LookPath,
	}
}

// Present reports whether a tool can be found.
func (t *Tools) Present(name string) bool {
	if name == "" {
		return false
	}
	if filepath.IsAbs(name) {
		_, err := os.Stat(name)
		return err == nil
	}
	look := t.LookPath
	if look == nil {
		look = exec.LookPath
	}
	_, err := look(name)
	return err == nil
}

// Module is one entry of semodule --list-modules=full.
type Module struct {
	Name     string
	Priority int
	Enabled  bool
	Lang     string
}

// ParseModuleList parses "<priority> <name> <lang> [disabled]" lines.
func ParseModuleList(out []byte) []Module {
	var mods []Module
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		prio, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		m := Module{Name: fields[1], Priority: prio, Enabled: true}
		if len(fields) > 2 {
			m.Lang = fields[2]
		}
		for _, f := range fields[2:] {
			if f == "disabled" {
				m.Enabled = false
			}
		}
		mods = append(mods, m)
	}
	return mods
}

// ListModules returns every module of the store with its priority.
func (t *Tools) ListModules(ctx context.Context) ([]Module, error) {
	out, err := t.Run(ctx, t.SemoduleBin, "--list-modules=full")
	if err != nil {
		return nil, fmt.Errorf("semodule --list-modules=full: %w: %s", err, lastLine(out))
	}
	return ParseModuleList(out), nil
}

// isBusy reports whether a failed commit should be retried: the store
// lock was held (another semodule or a package install) or the tool
// failed without a message, which is how setsebool reports it.
func isBusy(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(string(out))
	return strings.TrimSpace(msg) == "" || strings.Contains(msg, "lock") || strings.Contains(msg, "busy") || strings.Contains(msg, "try again")
}

// retry runs a commit until it succeeds or the retries are used up.
func (t *Tools) retry(ctx context.Context, what string, run func() ([]byte, error)) error {
	delay := t.RetryBase
	for attempt := 0; ; attempt++ {
		out, err := run()
		if err == nil {
			return nil
		}
		if attempt >= t.LockRetries || !isBusy(out, err) || ctx.Err() != nil {
			return fmt.Errorf("%s: %w: %s", what, err, lastLine(out))
		}
		if t.Sleep != nil {
			t.Sleep(delay)
		}
		delay *= 2
	}
}

// SetBooleans sets booleans persistently and at runtime in one commit
// (setsebool -P name=on name2=off ...).
func (t *Tools) SetBooleans(ctx context.Context, values map[string]bool) error {
	if len(values) == 0 {
		return nil
	}
	args := []string{"-P"}
	for _, name := range SortedNames(values) {
		v := "off"
		if values[name] {
			v = "on"
		}
		args = append(args, name+"="+v)
	}
	return t.retry(ctx, "setsebool -P", func() ([]byte, error) { return t.Run(ctx, t.SetseboolBin, args...) })
}

// ModuleOps are the module store changes of one semodule run.
type ModuleOps struct {
	// Priority is the -X priority Bor modules live at.
	Priority int
	// Install are CIL source files to install at Priority.
	Install []string
	// Remove are Bor module names to remove at Priority.
	Remove []string
	// Enable and Disable are modules of any priority to switch.
	Enable  []string
	Disable []string
}

// Empty reports whether there is nothing to do.
func (o *ModuleOps) Empty() bool {
	return len(o.Install) == 0 && len(o.Remove) == 0 && len(o.Enable) == 0 && len(o.Disable) == 0
}

// ApplyModules runs one semodule invocation for every operation, so the
// policy is rebuilt once.
func (t *Tools) ApplyModules(ctx context.Context, ops *ModuleOps) error {
	if ops.Empty() {
		return nil
	}
	args := []string{"-X", strconv.Itoa(ops.Priority)}
	for _, f := range ops.Install {
		args = append(args, "-i", f)
	}
	for _, n := range ops.Remove {
		args = append(args, "-r", n)
	}
	for _, n := range ops.Enable {
		args = append(args, "-e", n)
	}
	for _, n := range ops.Disable {
		args = append(args, "-d", n)
	}
	return t.retry(ctx, "semodule", func() ([]byte, error) { return t.Run(ctx, t.SemoduleBin, args...) })
}

// Restorecon relabels the given paths recursively.
func (t *Tools) Restorecon(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"-R"}, paths...)
	out, err := t.Run(ctx, t.RestoreconBin, args...)
	if err != nil {
		return fmt.Errorf("restorecon: %w: %s", err, lastLine(out))
	}
	return nil
}

// Login is one login mapping of the store.
type Login struct {
	Seuser string
	Range  string
}

// ParseLoginList parses "semanage login -l -n" output: login, SELinux
// user, range, service.
func ParseLoginList(out []byte) map[string]Login {
	logins := make(map[string]Login)
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] == "Login" {
			continue
		}
		l := Login{Seuser: fields[1]}
		if len(fields) > 2 {
			l.Range = fields[2]
		}
		logins[fields[0]] = l
	}
	return logins
}

// ListLogins returns the login mappings (needs semanage).
func (t *Tools) ListLogins(ctx context.Context) (map[string]Login, error) {
	out, err := t.Run(ctx, t.SemanageBin, "login", "-l", "-n")
	if err != nil {
		return nil, fmt.Errorf("semanage login -l: %w: %s", err, lastLine(out))
	}
	return ParseLoginList(out), nil
}

// SetLogin adds or modifies a login mapping.
func (t *Tools) SetLogin(ctx context.Context, login, seuser, rng string, exists bool) error {
	verb := "-a"
	if exists {
		verb = "-m"
	}
	args := []string{"login", verb, "-s", seuser}
	if rng != "" {
		args = append(args, "-r", rng)
	}
	args = append(args, login)
	return t.retry(ctx, "semanage login", func() ([]byte, error) { return t.Run(ctx, t.SemanageBin, args...) })
}

// DeleteLogin removes a login mapping.
func (t *Tools) DeleteLogin(ctx context.Context, login string) error {
	return t.retry(ctx, "semanage login -d", func() ([]byte, error) { return t.Run(ctx, t.SemanageBin, "login", "-d", login) })
}

// Audit2allow turns audit records into a Type Enforcement module source
// named like the given module.
func (t *Tools) Audit2allow(ctx context.Context, records []byte, module string) (string, error) {
	tmp, err := os.CreateTemp("", "bor-audit-*.log")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, werr := tmp.Write(records); werr != nil {
		_ = tmp.Close()
		return "", werr
	}
	if cerr := tmp.Close(); cerr != nil {
		return "", cerr
	}
	out, err := t.Run(ctx, t.Audit2allowBin, "-i", tmp.Name(), "-m", module)
	if err != nil {
		return "", fmt.Errorf("audit2allow: %w: %s", err, lastLine(out))
	}
	return string(out), nil
}

// TEToCIL compiles a Type Enforcement module source to CIL through
// checkmodule, semodule_package and the pp converter, the same path
// semodule takes for .pp modules. module is the name declared in the
// source: checkmodule insists that the output file carries it. Any
// missing tool yields an error.
func (t *Tools) TEToCIL(ctx context.Context, te, module string) (string, error) {
	if module == "" || strings.ContainsAny(module, "/\\ ") {
		return "", fmt.Errorf("invalid module name %q", module)
	}
	for _, bin := range []string{t.CheckmoduleBin, t.SemodulePackageBin, t.HLLPPBin} {
		if !t.Present(bin) {
			return "", fmt.Errorf("%s is not installed", bin)
		}
	}
	dir, err := os.MkdirTemp("", "bor-te-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tePath := filepath.Join(dir, module+".te")
	modPath := filepath.Join(dir, module+".mod")
	ppPath := filepath.Join(dir, module+".pp")
	if werr := os.WriteFile(tePath, []byte(te), 0o600); werr != nil {
		return "", werr
	}
	if out, cerr := t.Run(ctx, t.CheckmoduleBin, "-M", "-m", "-o", modPath, tePath); cerr != nil {
		return "", fmt.Errorf("checkmodule: %w: %s", cerr, lastLine(out))
	}
	if out, perr := t.Run(ctx, t.SemodulePackageBin, "-o", ppPath, "-m", modPath); perr != nil {
		return "", fmt.Errorf("semodule_package: %w: %s", perr, lastLine(out))
	}
	pp, err := os.ReadFile(ppPath) //nolint:gosec // G304: path created above
	if err != nil {
		return "", err
	}
	out, err := t.RunInput(ctx, pp, t.HLLPPBin)
	if err != nil {
		return "", fmt.Errorf("pp to CIL: %w: %s", err, lastLine(out))
	}
	return string(out), nil
}

func lastLine(out []byte) string {
	s := strings.TrimSpace(string(out))
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
