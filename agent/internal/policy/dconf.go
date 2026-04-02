// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// dconfManagedHeader is prepended to every dconf keyfile written by Bor.
const dconfManagedHeader = "# This file is managed by Bor. Do not edit manually.\n# Changes will be overwritten by policy enforcement.\n\n"

// DConfItemResult is the compliance result for a single DConf entry.
type DConfItemResult struct {
	SchemaID string
	Key      string
	Status   pb.ComplianceStatus
	Message  string
}

// MergeDConfPolicies merges multiple DConfPolicy values into one.
// Later entries for the same (schema_id, path, key) override earlier ones.
func MergeDConfPolicies(policies []*pb.DConfPolicy) *pb.DConfPolicy {
	type entryKey struct{ schemaID, path, key string }
	seen := make(map[entryKey]int) // maps key → index in result
	var merged []*pb.DConfEntry
	dbName := "local"

	for _, pol := range policies {
		if pol == nil {
			continue
		}
		if pol.GetDbName() != "" {
			dbName = pol.GetDbName()
		}
		for _, e := range pol.GetEntries() {
			k := entryKey{e.GetSchemaId(), e.GetPath(), e.GetKey()}
			if idx, ok := seen[k]; ok {
				merged[idx] = e
			} else {
				seen[k] = len(merged)
				merged = append(merged, e)
			}
		}
	}

	return &pb.DConfPolicy{Entries: merged, DbName: dbName}
}

// DConfPolicyToFiles converts a DConfPolicy to the raw bytes of the keyfile
// and the locks file that should be written under /etc/dconf/db/<db>.d/.
//
// keyfile content uses dconf INI path syntax: [org/gnome/desktop/screensaver]
// locksfile content: one absolute dconf path per locked key.
func DConfPolicyToFiles(pol *pb.DConfPolicy) (keyfile []byte, locksfile []byte) {
	// Group entries by section path.
	type section struct {
		path    string
		entries []*pb.DConfEntry
	}
	pathIndex := make(map[string]int)
	var sections []section

	for _, e := range pol.GetEntries() {
		sectionPath := schemaToPath(e.GetSchemaId(), e.GetPath())
		idx, ok := pathIndex[sectionPath]
		if !ok {
			idx = len(sections)
			sections = append(sections, section{path: sectionPath})
			pathIndex[sectionPath] = idx
		}
		sections[idx].entries = append(sections[idx].entries, e)
	}

	var kf bytes.Buffer
	var lf bytes.Buffer

	for _, sec := range sections {
		fmt.Fprintf(&kf, "[%s]\n", sec.path)
		for _, e := range sec.entries {
			fmt.Fprintf(&kf, "%s=%s\n", e.GetKey(), e.GetValue())
			if e.GetLock() {
				fmt.Fprintf(&lf, "/%s/%s\n", sec.path, e.GetKey())
			}
		}
		kf.WriteByte('\n')
	}

	return kf.Bytes(), lf.Bytes()
}

// schemaToPath converts a schema ID and optional path override to a dconf
// INI section path (dots replaced with slashes, leading/trailing slashes
// removed from the schema-derived form).
//
//   "org.gnome.desktop.screensaver"  → "org/gnome/desktop/screensaver"
//   path="/org/gnome/foo/"           → "org/gnome/foo"  (override wins)
func schemaToPath(schemaID, pathOverride string) string {
	if pathOverride != "" {
		p := strings.Trim(pathOverride, "/")
		return p
	}
	return strings.ReplaceAll(schemaID, ".", "/")
}

// SyncDConfFiles writes the keyfile and locks file under
// /etc/dconf/db/<dbName>.d/ and runs dconf update.
//
// It creates the necessary directories, backs up pre-existing unmanaged files,
// and ensures /etc/dconf/profile/user contains "system-db:<dbName>".
func SyncDConfFiles(dbName string, keyfile []byte, locksfile []byte) error {
	if dbName == "" {
		dbName = "local"
	}

	dbDir := filepath.Join("/etc/dconf/db", dbName+".d")
	locksDir := filepath.Join(dbDir, "locks")

	for _, dir := range []string{dbDir, locksDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("dconf: create dir %s: %w", dir, err)
		}
	}

	keyfilePath := filepath.Join(dbDir, "00-bor")
	locksPath := filepath.Join(locksDir, "bor")

	if err := BackupOriginal(keyfilePath); err != nil {
		return fmt.Errorf("dconf: backup keyfile: %w", err)
	}
	if err := BackupOriginal(locksPath); err != nil {
		return fmt.Errorf("dconf: backup locksfile: %w", err)
	}

	if err := WriteFileAtomically(keyfilePath, append([]byte(dconfManagedHeader), keyfile...)); err != nil {
		return fmt.Errorf("dconf: write keyfile: %w", err)
	}
	if err := WriteFileAtomically(locksPath, locksfile); err != nil {
		return fmt.Errorf("dconf: write locksfile: %w", err)
	}

	if err := ensureDConfProfile(dbName); err != nil {
		return fmt.Errorf("dconf: update profile: %w", err)
	}

	if out, err := exec.Command("dconf", "update").CombinedOutput(); err != nil {
		return fmt.Errorf("dconf update failed: %w\noutput: %s", err, out)
	}

	return nil
}

// ensureDConfProfile ensures /etc/dconf/profile/user contains the line
// "system-db:<dbName>".  Existing content is preserved.
func ensureDConfProfile(dbName string) error {
	profilePath := "/etc/dconf/profile/user"
	desired := "system-db:" + dbName

	data, err := os.ReadFile(profilePath) //nolint:gosec // G304: fixed system path
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read profile: %w", err)
	}

	// Scan existing lines.
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == desired {
			return nil // already present
		}
	}

	// Append the line.
	f, err := os.OpenFile(profilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // G304
	if err != nil {
		return fmt.Errorf("open profile: %w", err)
	}
	defer f.Close()

	// Make sure the file doesn't end mid-line before appending.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	// First line must be "user-db:user" if the file was just created.
	if len(data) == 0 {
		if _, err := f.WriteString("user-db:user\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(f, "%s\n", desired)
	return err
}

// CheckDConfCompliance checks whether the current system state matches
// the given policy. It uses gsettings(1) to read current values.
//
// Returns one result per entry. Schema not installed → INAPPLICABLE.
// Value mismatch → NON_COMPLIANT. Match → COMPLIANT.
func CheckDConfCompliance(pol *pb.DConfPolicy, knownSchemas map[string]struct{}) []DConfItemResult {
	var results []DConfItemResult

	for _, e := range pol.GetEntries() {
		sid := e.GetSchemaId()
		key := e.GetKey()

		if _, ok := knownSchemas[sid]; !ok {
			results = append(results, DConfItemResult{
				SchemaID: sid,
				Key:      key,
				Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE,
				Message:  "schema not installed on this node",
			})
			continue
		}

		args := []string{"get", sid, key}
		out, err := exec.Command("gsettings", args...).Output() //nolint:gosec // G204: args from trusted policy
		if err != nil {
			results = append(results, DConfItemResult{
				SchemaID: sid,
				Key:      key,
				Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message:  fmt.Sprintf("gsettings get failed: %v", err),
			})
			continue
		}

		current := strings.TrimSpace(string(out))
		if current == e.GetValue() {
			results = append(results, DConfItemResult{
				SchemaID: sid,
				Key:      key,
				Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			})
		} else {
			results = append(results, DConfItemResult{
				SchemaID: sid,
				Key:      key,
				Status:   pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message:  fmt.Sprintf("expected %q, got %q", e.GetValue(), current),
			})
		}
	}

	return results
}

// RollupDConfCompliance converts a slice of DConfItemResults into an overall
// ComplianceStatus and summary message.
//
// Rules:
//   - All INAPPLICABLE → INAPPLICABLE
//   - Any ERROR        → ERROR
//   - Any NON_COMPLIANT → NON_COMPLIANT
//   - Otherwise        → COMPLIANT
func RollupDConfCompliance(results []DConfItemResult) (pb.ComplianceStatus, string) {
	if len(results) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "no entries"
	}

	allInapplicable := true
	var nonCompliant, errors int
	var msgs []string

	for _, r := range results {
		switch r.Status {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
			// continue
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			allInapplicable = false
			errors++
			msgs = append(msgs, fmt.Sprintf("%s/%s: %s", r.SchemaID, r.Key, r.Message))
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			allInapplicable = false
			nonCompliant++
			msgs = append(msgs, fmt.Sprintf("%s/%s: %s", r.SchemaID, r.Key, r.Message))
		default:
			allInapplicable = false
		}
	}

	switch {
	case allInapplicable:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "schema not available on this node"
	case errors > 0:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case nonCompliant > 0:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, ""
	}
}
