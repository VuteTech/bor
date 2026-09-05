// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// flatpakRun executes the flatpak binary and returns its combined output. It
// is a package var so tests can stub the CLI. Every call site passes a fixed
// binary name from the agent configuration plus constant flags; the only
// server-supplied values on the command line are identifiers validated by
// ValidateFlatpak* (remote name, app id, branch, installation name). All other
// policy content (GPG keys, filter rules) goes through files.
var flatpakRun = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: fixed binary + constant/validated args, see doc comment
	cmd.Env = env
	cmd.WaitDelay = 10 * time.Second
	return cmd.CombinedOutput()
}

// flatpakLookPath reports whether an executable is found on PATH. It is a
// package var so tests can stub binary availability.
var flatpakLookPath = func(file string) error {
	_, err := exec.LookPath(file)
	return err
}

// FlatpakOptions carries node-local settings for Flatpak enforcement.
type FlatpakOptions struct {
	// Binary is the flatpak executable name or path (default "flatpak").
	Binary string
	// StateDir holds the managed .flatpakrepo/.filter/.gpg files.
	StateDir string
	// StateFile records created/adopted remotes for clean removal.
	StateFile string
	// ProxyURL, when set, is exported as http_proxy/https_proxy to flatpak.
	ProxyURL string
	// Installation selects "--system" (empty) or "--installation=<name>".
	Installation string
	// OperationTimeout bounds each install/update/uninstall invocation.
	OperationTimeout time.Duration
}

func (o *FlatpakOptions) binary() string {
	if o.Binary == "" {
		return "flatpak"
	}
	return o.Binary
}

func (o *FlatpakOptions) stateDir() string {
	if o.StateDir == "" {
		return FlatpakStateDir
	}
	return o.StateDir
}

func (o *FlatpakOptions) stateFile() string {
	if o.StateFile == "" {
		return FlatpakStateFile
	}
	return o.StateFile
}

func (o *FlatpakOptions) opTimeout() time.Duration {
	if o.OperationTimeout <= 0 {
		return flatpakDefaultOperationTimeout
	}
	return o.OperationTimeout
}

// instFlag returns the installation selector flag. The installation name is
// validated before use so it is safe on the command line.
func (o *FlatpakOptions) instFlag() (string, error) {
	if o.Installation == "" {
		return "--system", nil
	}
	if err := ValidateFlatpakRemoteName(o.Installation); err != nil {
		return "", fmt.Errorf("installation: %w", err)
	}
	return "--installation=" + o.Installation, nil
}

// env builds the child environment: C locale (so output is parseable and
// sizes are not localised), a HOME for flatpak's per-user config, the
// parent's PATH, and optional proxy variables.
func (o *FlatpakOptions) env() []string {
	env := []string{"LC_ALL=C", "LANG=C", "HOME=/root"}
	if p := os.Getenv("PATH"); p != "" {
		env = append(env, "PATH="+p)
	} else {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	if o.ProxyURL != "" {
		env = append(env,
			"http_proxy="+o.ProxyURL, "https_proxy="+o.ProxyURL,
			"HTTP_PROXY="+o.ProxyURL, "HTTPS_PROXY="+o.ProxyURL,
		)
	}
	return env
}

// run invokes flatpak with the given deadline and returns trimmed combined
// output. The returned error wraps the exit error; callers classify it with
// classifyFlatpakFailure.
func (o *FlatpakOptions) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := flatpakRun(runCtx, o.env(), o.binary(), args...)
	text := strings.TrimSpace(string(out))
	if err != nil {
		if runCtx.Err() != nil {
			return text, fmt.Errorf("%w: %v", runCtx.Err(), err)
		}
		return text, err
	}
	return text, nil
}

// FlatpakAvailable reports whether the flatpak binary is present.
func FlatpakAvailable(binary string) bool {
	if binary == "" {
		binary = "flatpak"
	}
	return flatpakLookPath(binary) == nil
}

// FlatpakVersion returns the installed flatpak version string ("1.18.0").
func FlatpakVersion(ctx context.Context, opts *FlatpakOptions) (string, error) {
	out, err := opts.run(ctx, flatpakListTimeout, "--version")
	if err != nil {
		return "", fmt.Errorf("flatpak --version: %w (%s)", err, lastFlatpakLine(out))
	}
	return strings.TrimSpace(strings.TrimPrefix(out, "Flatpak ")), nil
}

// ── output parsing ───────────────────────────────────────────────────────────

// flatpakInstalledApp is one row of `flatpak list --app --columns=...`.
type flatpakInstalledApp struct {
	App     string
	Origin  string
	Branch  string
	Arch    string
	Version string
	Active  string
	Ref     string
}

// flatpakListColumns is the column set requested from `flatpak list`.
const flatpakListColumns = "application,origin,branch,arch,version,active,ref"

// parseFlatpakList parses tab-separated rows produced by
// `flatpak list --app --columns=application,origin,branch,arch,version,active,ref`
// (no header when stdout is not a TTY).
func parseFlatpakList(out string) []flatpakInstalledApp {
	var apps []flatpakInstalledApp
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		get := func(i int) string {
			if i < len(f) {
				return strings.TrimSpace(f[i])
			}
			return ""
		}
		app := flatpakInstalledApp{
			App:     get(0),
			Origin:  get(1),
			Branch:  get(2),
			Arch:    get(3),
			Version: get(4),
			Active:  get(5),
			Ref:     get(6),
		}
		if app.App == "" {
			continue
		}
		apps = append(apps, app)
	}
	return apps
}

// flatpakRemoteState is one row of `flatpak remotes --columns=...` plus the
// flags encoded in its options column.
type flatpakRemoteState struct {
	Name        string
	URL         string
	Title       string
	Subset      string
	Filter      string
	Priority    int
	Collection  string
	Disabled    bool
	NoEnumerate bool
	NoGPGVerify bool
	OCI         bool
}

// flatpakRemotesColumns is the column set requested from `flatpak remotes`.
const flatpakRemotesColumns = "name,url,title,subset,filter,priority,options,collection"

// parseFlatpakRemotes parses tab-separated rows produced by
// `flatpak remotes --columns=name,url,title,subset,filter,priority,options,collection`.
// Empty values are printed as "-". The options column is a comma-separated
// list that may contain the installation id plus "disabled", "oci",
// "no-enumerate", "no-gpg-verify" and "filtered".
func parseFlatpakRemotes(out string) []flatpakRemoteState {
	var remotes []flatpakRemoteState
	dash := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "-" {
			return ""
		}
		return s
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		get := func(i int) string {
			if i < len(f) {
				return dash(f[i])
			}
			return ""
		}
		r := flatpakRemoteState{
			Name:       get(0),
			URL:        get(1),
			Title:      get(2),
			Subset:     get(3),
			Filter:     get(4),
			Collection: get(7),
		}
		if r.Name == "" {
			continue
		}
		if p, err := strconv.Atoi(get(5)); err == nil {
			r.Priority = p
		}
		for _, opt := range strings.Split(get(6), ",") {
			switch strings.TrimSpace(opt) {
			case "disabled":
				r.Disabled = true
			case "no-enumerate":
				r.NoEnumerate = true
			case "no-gpg-verify":
				r.NoGPGVerify = true
			case "oci":
				r.OCI = true
			}
		}
		remotes = append(remotes, r)
	}
	return remotes
}

// ── failure classification ──────────────────────────────────────────────────

// flatpakFailureKind classifies why a flatpak invocation failed.
type flatpakFailureKind int

const (
	flatpakFailureNone flatpakFailureKind = iota
	// flatpakFailureNotFound: the ref does not exist in the remote.
	flatpakFailureNotFound
	// flatpakFailureTimeout: the per-operation deadline expired.
	flatpakFailureTimeout
	// flatpakFailureUnknownInstallation: --installation=NAME is not defined.
	flatpakFailureUnknownInstallation
	// flatpakFailureOther: anything else (network, GPG, lock, ...).
	flatpakFailureOther
)

// flatpakNotFoundMarkers are substrings (C locale) that flatpak prints when a
// ref cannot be found in the selected remote.
var flatpakNotFoundMarkers = []string{
	"Nothing matches",
	"not found in remote",
	"No remote refs found",
	"No entry for",
	"is not installed",
	"Nothing unused to uninstall",
}

// flatpakUnknownInstallationMarkers are substrings printed for an unknown
// --installation=NAME.
var flatpakUnknownInstallationMarkers = []string{
	"Unknown installation",
	"No installation named",
	"not a known installation",
}

// classifyFlatpakFailure maps a flatpak error and its output to a failure kind
// and a short human-readable message (the last non-empty output line).
func classifyFlatpakFailure(err error, out string) (kind flatpakFailureKind, message string) {
	if err == nil {
		return flatpakFailureNone, ""
	}
	last := lastFlatpakLine(out)
	if errors.Is(err, context.DeadlineExceeded) {
		return flatpakFailureTimeout, "timed out"
	}
	if errors.Is(err, context.Canceled) {
		return flatpakFailureOther, "cancelled"
	}
	for _, m := range flatpakUnknownInstallationMarkers {
		if strings.Contains(out, m) {
			return flatpakFailureUnknownInstallation, last
		}
	}
	for _, m := range flatpakNotFoundMarkers {
		if strings.Contains(out, m) {
			return flatpakFailureNotFound, last
		}
	}
	if last == "" {
		last = err.Error()
	}
	return flatpakFailureOther, last
}

// lastFlatpakLine returns the last non-empty output line with the "error: "
// prefix removed, truncated to a reasonable length for compliance messages.
func lastFlatpakLine(out string) string {
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
