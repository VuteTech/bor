// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Managed file suffixes under the Flatpak state directory.
const (
	flatpakRepoFileExt   = ".flatpakrepo"
	flatpakFilterFileExt = ".filter"
	flatpakGPGFileExt    = ".gpg"
)

// ── state file ──────────────────────────────────────────────────────────────

// flatpakState records what Bor did to each remote so that removal restores
// the node to its previous configuration.
type flatpakState struct {
	Remotes map[string]flatpakRemoteRecord `json:"remotes"`
}

// flatpakRemoteRecord describes one managed remote.
type flatpakRemoteRecord struct {
	// Created is true when Bor added the remote (removal deletes it).
	Created bool `json:"created"`
	// Original holds the pre-Bor configuration of an adopted remote.
	Original *flatpakRemoteOriginal `json:"original,omitempty"`
	// GPGKeySHA256 is the digest of the last imported key (avoid re-imports).
	GPGKeySHA256 string `json:"gpg_key_sha256,omitempty"`
	// NoDepsApplied records that --no-use-for-deps was applied (not observable).
	NoDepsApplied bool `json:"nodeps_applied,omitempty"`
}

// flatpakRemoteOriginal is the observable configuration of a remote before
// Bor touched it.
type flatpakRemoteOriginal struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Subset    string `json:"subset"`
	Filter    string `json:"filter"`
	Priority  int    `json:"priority"`
	Enabled   bool   `json:"enabled"`
	GPGVerify bool   `json:"gpg_verify"`
}

func loadFlatpakState(path string) flatpakState {
	st := flatpakState{Remotes: map[string]flatpakRemoteRecord{}}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from agent configuration
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("flatpak: ignoring unreadable state file %s: %v", path, err)
		return flatpakState{Remotes: map[string]flatpakRemoteRecord{}}
	}
	if st.Remotes == nil {
		st.Remotes = map[string]flatpakRemoteRecord{}
	}
	return st
}

func saveFlatpakState(path string, st flatpakState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode flatpak state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	return atomicWrite(path, data, 0o600)
}

// ── file rendering ──────────────────────────────────────────────────────────

// flatpakRemoteFiles returns the managed file paths for a remote name.
func flatpakRemoteFiles(stateDir, name string) (repo, filter, gpg string) {
	return filepath.Join(stateDir, name+flatpakRepoFileExt),
		filepath.Join(stateDir, name+flatpakFilterFileExt),
		filepath.Join(stateDir, name+flatpakGPGFileExt)
}

// flatpakINIValue strips characters that would break a single-line INI value.
func flatpakINIValue(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// renderFlatpakRepoFile renders the .flatpakrepo definition consumed by
// `flatpak remote-add --from`. filterPath is included only when the remote
// has a filter. Priority is not a repofile key; it is applied with
// `remote-modify --prio` during reconciliation.
func renderFlatpakRepoFile(r *pb.FlatpakRemoteEntry, filterPath string) []byte {
	var b strings.Builder
	b.WriteString("# Managed by Bor. Do not edit manually.\n")
	b.WriteString("[Flatpak Repo]\n")
	b.WriteString("Version=1\n")
	b.WriteString("Url=" + flatpakINIValue(r.GetUrl()) + "\n")
	if t := flatpakINIValue(r.GetTitle()); t != "" {
		b.WriteString("Title=" + t + "\n")
	}
	if c := flatpakINIValue(r.GetComment()); c != "" {
		b.WriteString("Comment=" + c + "\n")
	}
	if h := flatpakINIValue(r.GetHomepage()); h != "" {
		b.WriteString("Homepage=" + h + "\n")
	}
	if db := flatpakINIValue(r.GetDefaultBranch()); db != "" {
		b.WriteString("DefaultBranch=" + db + "\n")
	}
	if s := flatpakINIValue(r.GetSubset()); s != "" {
		b.WriteString("Subset=" + s + "\n")
	}
	if c := flatpakINIValue(r.GetCollectionId()); c != "" {
		b.WriteString("CollectionID=" + c + "\n")
	}
	if r.GetNoUseForDeps() {
		b.WriteString("NoDeps=true\n")
	}
	if filterPath != "" && r.GetFilterMode() != pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_NONE {
		b.WriteString("Filter=" + filterPath + "\n")
	}
	if r.GetGpgVerify() && len(r.GetGpgKeyData()) > 0 {
		b.WriteString("GPGKey=" + base64.StdEncoding.EncodeToString(r.GetGpgKeyData()) + "\n")
	}
	return []byte(b.String())
}

// renderFlatpakFilter renders the local filter file for a remote. In
// allowlist mode everything is denied first, runtimes are allowed (nothing
// installs without them) and every desired PRESENT/LATEST app of this remote
// is allowed automatically so Bor never blocks its own installs. Returns nil
// when the remote has no filter.
func renderFlatpakFilter(r *pb.FlatpakRemoteEntry, apps []*pb.FlatpakAppEntry) []byte {
	var lines []string
	switch r.GetFilterMode() {
	case pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_ALLOWLIST:
		lines = append(lines, "deny *", "allow runtime/*")
		for _, ref := range r.GetFilterRefs() {
			if ValidateFlatpakFilterRef(ref) == nil {
				lines = append(lines, "allow "+ref)
			}
		}
		for _, a := range apps {
			if a.GetState() == pb.FlatpakAppState_FLATPAK_APP_STATE_ABSENT {
				continue
			}
			if a.GetRemote() != "" && a.GetRemote() != r.GetName() {
				continue
			}
			if ValidateFlatpakAppID(a.GetAppId()) == nil {
				lines = append(lines, "allow app/"+a.GetAppId()+"/*/*")
			}
		}
	case pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_DENYLIST:
		for _, ref := range r.GetFilterRefs() {
			if ValidateFlatpakFilterRef(ref) == nil {
				lines = append(lines, "deny "+ref)
			}
		}
	default:
		return nil
	}
	var b strings.Builder
	b.WriteString("# Managed by Bor. Do not edit manually.\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return []byte(b.String())
}

// FlatpakDesiredFiles returns the managed file paths that a sync of desired
// will write, so the caller can pre-suppress the file watcher.
func FlatpakDesiredFiles(opts *FlatpakOptions, desired *FlatpakDesired) []string {
	var paths []string
	for _, r := range desired.Remotes {
		if ValidateFlatpakRemoteName(r.GetName()) != nil {
			continue
		}
		repo, filter, gpg := flatpakRemoteFiles(opts.stateDir(), r.GetName())
		paths = append(paths, repo)
		if r.GetFilterMode() != pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_NONE {
			paths = append(paths, filter)
		}
		if r.GetGpgVerify() && len(r.GetGpgKeyData()) > 0 {
			paths = append(paths, gpg)
		}
	}
	return paths
}

// ListBorManagedFlatpakFiles returns every managed remote file currently
// present in stateDir (*.flatpakrepo, *.filter, *.gpg).
func ListBorManagedFlatpakFiles(stateDir string) ([]string, error) {
	if stateDir == "" {
		stateDir = FlatpakStateDir
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", stateDir, err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, flatpakRepoFileExt) ||
			strings.HasSuffix(name, flatpakFilterFileExt) ||
			strings.HasSuffix(name, flatpakGPGFileExt) {
			paths = append(paths, filepath.Join(stateDir, name))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// ── reconciliation ──────────────────────────────────────────────────────────

// listFlatpakRemotes returns the configured remotes of the selected
// installation.
func listFlatpakRemotes(ctx context.Context, opts *FlatpakOptions) ([]flatpakRemoteState, error) {
	inst, err := opts.instFlag()
	if err != nil {
		return nil, err
	}
	out, err := opts.run(ctx, flatpakListTimeout, "remotes", inst, "--columns="+flatpakRemotesColumns)
	if err != nil {
		kind, msg := classifyFlatpakFailure(err, out)
		if kind == flatpakFailureUnknownInstallation {
			return nil, &FlatpakInapplicableError{Reason: "flatpak installation " + strconv.Quote(opts.Installation) + " is not defined on this node: " + msg}
		}
		return nil, fmt.Errorf("flatpak remotes: %s", msg)
	}
	return parseFlatpakRemotes(out), nil
}

// remoteModifyArgs computes the minimal `flatpak remote-modify` flag set that
// turns current into desired. gpgPath is the managed keyring file;
// importKey requests --gpg-import (key changed or verification was off).
func remoteModifyArgs(current *flatpakRemoteState, desired *pb.FlatpakRemoteEntry, filterPath, gpgPath string, importKey bool) []string {
	var args []string
	if u := flatpakINIValue(desired.GetUrl()); u != "" && u != current.URL {
		args = append(args, "--url="+u)
	}
	if t := flatpakINIValue(desired.GetTitle()); t != "" && t != current.Title {
		args = append(args, "--title="+t)
	}
	if s := flatpakINIValue(desired.GetSubset()); s != current.Subset {
		args = append(args, "--subset="+s)
	}
	if desired.GetFilterMode() != pb.FlatpakFilterMode_FLATPAK_FILTER_MODE_NONE {
		if current.Filter != filterPath {
			args = append(args, "--filter="+filterPath)
		}
	} else if current.Filter != "" {
		args = append(args, "--no-filter")
	}
	wantPrio := int(desired.GetPriority())
	if wantPrio <= 0 {
		wantPrio = 1
	}
	if current.Priority != wantPrio {
		args = append(args, "--prio="+strconv.Itoa(wantPrio))
	}
	if desired.GetEnabled() && current.Disabled {
		args = append(args, "--enable")
	}
	if !desired.GetEnabled() && !current.Disabled {
		args = append(args, "--disable")
	}
	if desired.GetNoEnumerate() && !current.NoEnumerate {
		args = append(args, "--no-enumerate")
	}
	if desired.GetGpgVerify() && len(desired.GetGpgKeyData()) > 0 {
		if current.NoGPGVerify {
			args = append(args, "--gpg-verify")
		}
		if importKey || current.NoGPGVerify {
			args = append(args, "--gpg-import="+gpgPath)
		}
	} else if !desired.GetGpgVerify() && !current.NoGPGVerify {
		args = append(args, "--no-gpg-verify")
	}
	return args
}

// restoreModifyArgs computes the remote-modify flags that return an adopted
// remote to its recorded original configuration.
func restoreModifyArgs(current *flatpakRemoteState, orig *flatpakRemoteOriginal) []string {
	var args []string
	if orig.URL != "" && orig.URL != current.URL {
		args = append(args, "--url="+orig.URL)
	}
	if orig.Title != "" && orig.Title != current.Title {
		args = append(args, "--title="+orig.Title)
	}
	if orig.Subset != current.Subset {
		args = append(args, "--subset="+orig.Subset)
	}
	if orig.Filter != current.Filter {
		if orig.Filter == "" {
			args = append(args, "--no-filter")
		} else {
			args = append(args, "--filter="+orig.Filter)
		}
	}
	if orig.Priority > 0 && orig.Priority != current.Priority {
		args = append(args, "--prio="+strconv.Itoa(orig.Priority))
	}
	if orig.Enabled && current.Disabled {
		args = append(args, "--enable")
	}
	if !orig.Enabled && !current.Disabled {
		args = append(args, "--disable")
	}
	if orig.GPGVerify && current.NoGPGVerify {
		args = append(args, "--gpg-verify")
	}
	if !orig.GPGVerify && !current.NoGPGVerify {
		args = append(args, "--no-gpg-verify")
	}
	return args
}

func snapshotOriginal(cur *flatpakRemoteState) *flatpakRemoteOriginal {
	return &flatpakRemoteOriginal{
		URL:       cur.URL,
		Title:     cur.Title,
		Subset:    cur.Subset,
		Filter:    cur.Filter,
		Priority:  cur.Priority,
		Enabled:   !cur.Disabled,
		GPGVerify: !cur.NoGPGVerify,
	}
}

func gpgDigest(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

// SyncFlatpakRemotes reconciles the node's remotes with desired: it renders
// the managed files, adds missing remotes from their .flatpakrepo, applies the
// minimal remote-modify flag set to existing ones (adopting them), removes
// remotes Bor previously managed that are no longer desired, and persists the
// state file. It returns one ComplianceItem per remote. A returned error is
// fatal for the whole sync (flatpak unusable, unknown installation).
func SyncFlatpakRemotes(ctx context.Context, opts *FlatpakOptions, desired *FlatpakDesired) ([]ComplianceItem, error) {
	inst, err := opts.instFlag()
	if err != nil {
		return nil, &FlatpakInapplicableError{Reason: err.Error()}
	}
	current, err := listFlatpakRemotes(ctx, opts)
	if err != nil {
		return nil, err
	}
	currentByName := make(map[string]flatpakRemoteState, len(current))
	for _, r := range current {
		currentByName[r.Name] = r
	}

	stateDir := opts.stateDir()
	state := loadFlatpakState(opts.stateFile())
	desiredNames := make(map[string]bool, len(desired.Remotes))
	items := make([]ComplianceItem, 0, len(desired.Remotes))

	for _, r := range desired.Remotes {
		name := r.GetName()
		key := flatpakItemRemote + name
		if verr := ValidateFlatpakRemoteName(name); verr != nil {
			items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: verr.Error()})
			continue
		}
		desiredNames[name] = true
		repoPath, filterPath, gpgPath := flatpakRemoteFiles(stateDir, name)

		// Render managed files.
		filterData := renderFlatpakFilter(r, desired.Apps)
		if filterData != nil {
			if werr := WriteFileAtomically(filterPath, filterData); werr != nil {
				items = append(items, errItem(key, werr))
				continue
			}
		} else {
			_ = os.Remove(filterPath)
		}
		hasKey := r.GetGpgVerify() && len(r.GetGpgKeyData()) > 0
		if hasKey {
			if werr := WriteFileAtomically(gpgPath, r.GetGpgKeyData()); werr != nil {
				items = append(items, errItem(key, werr))
				continue
			}
		} else {
			_ = os.Remove(gpgPath)
		}
		if werr := WriteFileAtomically(repoPath, renderFlatpakRepoFile(r, filterPath)); werr != nil {
			items = append(items, errItem(key, werr))
			continue
		}

		rec := state.Remotes[name]
		cur, exists := currentByName[name]
		if !exists {
			out, aerr := opts.run(ctx, opts.opTimeout(), "remote-add", inst, "--if-not-exists", "--from", name, repoPath)
			if aerr != nil {
				_, msg := classifyFlatpakFailure(aerr, out)
				items = append(items, ComplianceItem{Key: key, Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "remote-add failed: " + msg})
				continue
			}
			rec.Created = true
			rec.Original = nil
			rec.GPGKeySHA256 = gpgDigest(r.GetGpgKeyData())
			rec.NoDepsApplied = r.GetNoUseForDeps()
			// Re-read so the diff below applies priority/flags on the fresh remote.
			cur = flatpakRemoteState{Name: name, URL: flatpakINIValue(r.GetUrl()), Title: flatpakINIValue(r.GetTitle()),
				Subset: flatpakINIValue(r.GetSubset()), Priority: 1, NoGPGVerify: !hasKey}
			if filterData != nil {
				cur.Filter = filterPath
			}
		} else if !rec.Created && rec.Original == nil {
			rec.Original = snapshotOriginal(&cur)
		}

		importKey := hasKey && rec.GPGKeySHA256 != gpgDigest(r.GetGpgKeyData())
		modArgs := remoteModifyArgs(&cur, r, filterPath, gpgPath, importKey)
		if r.GetNoUseForDeps() && !rec.NoDepsApplied {
			modArgs = append(modArgs, "--no-use-for-deps")
		}
		status := pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT
		msg := "configured"
		if len(modArgs) > 0 {
			args := append([]string{"remote-modify", inst}, modArgs...)
			args = append(args, name)
			if out, merr := opts.run(ctx, opts.opTimeout(), args...); merr != nil {
				_, m := classifyFlatpakFailure(merr, out)
				status = pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR
				msg = "remote-modify failed: " + m
			} else {
				msg = "configured (" + strings.Join(modArgs, " ") + ")"
				if hasKey {
					rec.GPGKeySHA256 = gpgDigest(r.GetGpgKeyData())
				}
				if r.GetNoUseForDeps() {
					rec.NoDepsApplied = true
				}
			}
		}
		if status == pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
			switch {
			case !r.GetNoEnumerate() && cur.NoEnumerate:
				status = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
				msg = "remote is marked no-enumerate on this node and flatpak cannot clear it; remove and re-add the remote"
			case !r.GetGpgVerify():
				status = pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT
				msg = "GPG verification disabled"
			}
		}
		state.Remotes[name] = rec
		items = append(items, ComplianceItem{Key: key, Status: status, Message: msg})
	}

	// Remotes Bor managed before but that are no longer desired.
	for name, rec := range state.Remotes {
		if desiredNames[name] {
			continue
		}
		undoFlatpakRemote(ctx, opts, inst, name, rec, currentByName)
		delete(state.Remotes, name)
	}
	removeStaleFlatpakFiles(stateDir, desiredNames)

	if serr := saveFlatpakState(opts.stateFile(), state); serr != nil {
		log.Printf("flatpak: failed to save state file: %v", serr)
	}
	return items, nil
}

// undoFlatpakRemote deletes a Bor-created remote or restores an adopted one.
func undoFlatpakRemote(ctx context.Context, opts *FlatpakOptions, inst, name string, rec flatpakRemoteRecord, current map[string]flatpakRemoteState) {
	if ValidateFlatpakRemoteName(name) != nil {
		return
	}
	cur, exists := current[name]
	if !exists {
		return
	}
	if rec.Created {
		if out, err := opts.run(ctx, opts.opTimeout(), "remote-delete", inst, name); err != nil {
			_, msg := classifyFlatpakFailure(err, out)
			log.Printf("flatpak: could not remove remote %q (apps may still use it): %s", name, msg)
		}
		return
	}
	if rec.Original == nil {
		return
	}
	args := restoreModifyArgs(&cur, rec.Original)
	if len(args) == 0 {
		return
	}
	full := append([]string{"remote-modify", inst}, args...)
	full = append(full, name)
	if out, err := opts.run(ctx, opts.opTimeout(), full...); err != nil {
		_, msg := classifyFlatpakFailure(err, out)
		log.Printf("flatpak: could not restore remote %q: %s", name, msg)
	}
}

// removeStaleFlatpakFiles deletes managed files for remotes not in keep.
func removeStaleFlatpakFiles(stateDir string, keep map[string]bool) {
	files, err := ListBorManagedFlatpakFiles(stateDir)
	if err != nil {
		return
	}
	for _, p := range files {
		base := filepath.Base(p)
		name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(base, flatpakRepoFileExt), flatpakFilterFileExt), flatpakGPGFileExt)
		if !keep[name] {
			_ = os.Remove(p)
		}
	}
}

// CleanupFlatpak undoes every managed remote (used when the last Flatpak
// policy is unbound) and removes the managed files and the state file. When
// flatpak itself is missing only the files are removed.
func CleanupFlatpak(ctx context.Context, opts *FlatpakOptions) {
	state := loadFlatpakState(opts.stateFile())
	if len(state.Remotes) > 0 && FlatpakAvailable(opts.binary()) {
		if inst, err := opts.instFlag(); err == nil {
			current, lerr := listFlatpakRemotes(ctx, opts)
			if lerr == nil {
				byName := make(map[string]flatpakRemoteState, len(current))
				for _, r := range current {
					byName[r.Name] = r
				}
				for name, rec := range state.Remotes {
					undoFlatpakRemote(ctx, opts, inst, name, rec, byName)
				}
			} else if !errors.Is(lerr, context.Canceled) {
				log.Printf("flatpak: cleanup could not list remotes: %v", lerr)
			}
		}
	}
	removeStaleFlatpakFiles(opts.stateDir(), map[string]bool{})
	_ = os.Remove(opts.stateFile())
}
