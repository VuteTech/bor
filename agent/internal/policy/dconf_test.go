// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// ── MergeDConfPolicies ────────────────────────────────────────────────────────

func TestMergeDConfPolicies_Empty(t *testing.T) {
	merged := MergeDConfPolicies(nil)
	if len(merged.GetEntries()) != 0 {
		t.Errorf("expected 0 entries, got %d", len(merged.GetEntries()))
	}
}

func TestMergeDConfPolicies_SinglePolicy(t *testing.T) {
	pol := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-enabled", Value: "true"},
		},
	}
	merged := MergeDConfPolicies([]*pb.DConfPolicy{pol})
	if len(merged.GetEntries()) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(merged.GetEntries()))
	}
}

func TestMergeDConfPolicies_LaterEntryOverrides(t *testing.T) {
	p1 := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-enabled", Value: "false"},
		},
	}
	p2 := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-enabled", Value: "true"},
		},
	}
	merged := MergeDConfPolicies([]*pb.DConfPolicy{p1, p2})
	if len(merged.GetEntries()) != 1 {
		t.Fatalf("expected 1 entry after merge, got %d", len(merged.GetEntries()))
	}
	if merged.GetEntries()[0].GetValue() != "true" {
		t.Errorf("expected merged value 'true', got %q", merged.GetEntries()[0].GetValue())
	}
}

func TestMergeDConfPolicies_DifferentKeysPreserved(t *testing.T) {
	p1 := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-enabled", Value: "true"},
		},
	}
	p2 := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-delay", Value: "uint32 300"},
		},
	}
	merged := MergeDConfPolicies([]*pb.DConfPolicy{p1, p2})
	if len(merged.GetEntries()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(merged.GetEntries()))
	}
}

// ── DConfPolicyToFiles ────────────────────────────────────────────────────────

func TestDConfPolicyToFiles_KeyfileFormat(t *testing.T) {
	pol := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-enabled", Value: "true", Lock: true},
			{SchemaId: "org.gnome.desktop.screensaver", Key: "lock-delay", Value: "uint32 300"},
		},
	}
	keyfile, locksfile := DConfPolicyToFiles(pol)

	kf := string(keyfile)
	if !strings.Contains(kf, "[org/gnome/desktop/screensaver]") {
		t.Errorf("keyfile missing expected section header, got:\n%s", kf)
	}
	if !strings.Contains(kf, "lock-enabled=true") {
		t.Errorf("keyfile missing lock-enabled entry, got:\n%s", kf)
	}
	if !strings.Contains(kf, "lock-delay=uint32 300") {
		t.Errorf("keyfile missing lock-delay entry, got:\n%s", kf)
	}

	lf := string(locksfile)
	if !strings.Contains(lf, "/org/gnome/desktop/screensaver/lock-enabled") {
		t.Errorf("locksfile missing expected lock path, got:\n%s", lf)
	}
	if strings.Contains(lf, "lock-delay") {
		t.Errorf("locksfile should not contain non-locked key lock-delay, got:\n%s", lf)
	}
}

func TestDConfPolicyToFiles_PathOverride(t *testing.T) {
	pol := &pb.DConfPolicy{
		Entries: []*pb.DConfEntry{
			{
				SchemaId: "org.gnome.desktop.screensaver",
				Path:     "/custom/path/",
				Key:      "lock-enabled",
				Value:    "true",
				Lock:     true,
			},
		},
	}
	keyfile, locksfile := DConfPolicyToFiles(pol)

	kf := string(keyfile)
	if !strings.Contains(kf, "[custom/path]") {
		t.Errorf("keyfile should use path override, got:\n%s", kf)
	}

	lf := string(locksfile)
	if !strings.Contains(lf, "/custom/path/lock-enabled") {
		t.Errorf("locksfile should use path override, got:\n%s", lf)
	}
}

func TestDConfPolicyToFiles_EmptyPolicy(t *testing.T) {
	pol := &pb.DConfPolicy{}
	keyfile, locksfile := DConfPolicyToFiles(pol)
	if len(keyfile) != 0 {
		t.Errorf("expected empty keyfile for empty policy, got %d bytes", len(keyfile))
	}
	if len(locksfile) != 0 {
		t.Errorf("expected empty locksfile for empty policy, got %d bytes", len(locksfile))
	}
}

// ── schemaToPath ─────────────────────────────────────────────────────────────

func TestSchemaToPath_DotsToSlashes(t *testing.T) {
	got := schemaToPath("org.gnome.desktop.screensaver", "")
	if got != "org/gnome/desktop/screensaver" {
		t.Errorf("expected 'org/gnome/desktop/screensaver', got %q", got)
	}
}

func TestSchemaToPath_OverrideStripsSlashes(t *testing.T) {
	got := schemaToPath("org.gnome.desktop.screensaver", "/custom/path/")
	if got != "custom/path" {
		t.Errorf("expected 'custom/path', got %q", got)
	}
}

// ── RollupDConfCompliance ─────────────────────────────────────────────────────

func TestRollupDConfCompliance_AllCompliant(t *testing.T) {
	results := []DConfItemResult{
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
	}
	status, _ := RollupDConfCompliance(results)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT {
		t.Errorf("expected COMPLIANT, got %v", status)
	}
}

func TestRollupDConfCompliance_AllInapplicable(t *testing.T) {
	results := []DConfItemResult{
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE},
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE},
	}
	status, _ := RollupDConfCompliance(results)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("expected INAPPLICABLE, got %v", status)
	}
}

func TestRollupDConfCompliance_MixedWithNonCompliant(t *testing.T) {
	results := []DConfItemResult{
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT},
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, Message: "wrong value"},
	}
	status, msg := RollupDConfCompliance(results)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT {
		t.Errorf("expected NON_COMPLIANT, got %v", status)
	}
	if !strings.Contains(msg, "wrong value") {
		t.Errorf("expected message to contain 'wrong value', got %q", msg)
	}
}

func TestRollupDConfCompliance_ErrorTakesPrecedence(t *testing.T) {
	results := []DConfItemResult{
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT},
		{Status: pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, Message: "exec failed"},
	}
	status, _ := RollupDConfCompliance(results)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR {
		t.Errorf("expected ERROR, got %v", status)
	}
}

func TestRollupDConfCompliance_Empty(t *testing.T) {
	status, _ := RollupDConfCompliance(nil)
	if status != pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE {
		t.Errorf("expected INAPPLICABLE for empty results, got %v", status)
	}
}

// ── ScanGSettingsSchemasFrom ──────────────────────────────────────────────────

func TestScanGSettingsSchemasFrom_NonExistentDir(t *testing.T) {
	schemas, err := ScanGSettingsSchemasFrom("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas, got %d", len(schemas))
	}
}

func TestScanGSettingsSchemasFrom_ValidSchema(t *testing.T) {
	dir := t.TempDir()
	xmlData := `<schemalist>
  <schema id="org.gnome.desktop.screensaver" path="/org/gnome/desktop/screensaver/">
    <key name="lock-enabled" type="b">
      <default>true</default>
      <summary>Lock on activation</summary>
      <description>Set this to TRUE to lock the screen when the screensaver goes active.</description>
    </key>
    <key name="lock-delay" type="u">
      <default>uint32 0</default>
      <summary>Time before locking</summary>
      <range min="0" max="2147483647"/>
    </key>
  </schema>
</schemalist>`
	if err := os.WriteFile(filepath.Join(dir, "org.gnome.desktop.gschema.xml"), []byte(xmlData), 0o644); err != nil {
		t.Fatal(err)
	}

	schemas, err := ScanGSettingsSchemasFrom(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}

	s := schemas[0]
	if s.GetSchemaId() != "org.gnome.desktop.screensaver" {
		t.Errorf("unexpected schema_id: %s", s.GetSchemaId())
	}
	if s.GetPath() != "/org/gnome/desktop/screensaver/" {
		t.Errorf("unexpected path: %s", s.GetPath())
	}
	if s.GetRelocatable() {
		t.Error("expected non-relocatable schema")
	}
	if len(s.GetKeys()) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(s.GetKeys()))
	}

	lockKey := s.GetKeys()[0]
	if lockKey.GetName() != "lock-enabled" {
		t.Errorf("unexpected key name: %s", lockKey.GetName())
	}
	if lockKey.GetType() != "b" {
		t.Errorf("unexpected key type: %s", lockKey.GetType())
	}
	if lockKey.GetSummary() != "Lock on activation" {
		t.Errorf("unexpected summary: %s", lockKey.GetSummary())
	}

	rangeKey := s.GetKeys()[1]
	if rangeKey.GetRangeMin() != "0" {
		t.Errorf("unexpected range_min: %s", rangeKey.GetRangeMin())
	}
	if rangeKey.GetRangeMax() != "2147483647" {
		t.Errorf("unexpected range_max: %s", rangeKey.GetRangeMax())
	}
}

func TestScanGSettingsSchemasFrom_EnumResolution(t *testing.T) {
	dir := t.TempDir()
	xmlData := `<schemalist>
  <enum id="org.gnome.desktop.GDesktopColorScheme">
    <value nick="default" value="0"/>
    <value nick="prefer-dark" value="1"/>
    <value nick="prefer-light" value="2"/>
  </enum>
  <schema id="org.gnome.desktop.interface" path="/org/gnome/desktop/interface/">
    <key name="color-scheme" enum="org.gnome.desktop.GDesktopColorScheme">
      <default>'default'</default>
      <summary>Color scheme</summary>
    </key>
  </schema>
</schemalist>`
	if err := os.WriteFile(filepath.Join(dir, "org.gnome.desktop.interface.gschema.xml"), []byte(xmlData), 0o644); err != nil {
		t.Fatal(err)
	}

	schemas, err := ScanGSettingsSchemasFrom(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}

	key := schemas[0].GetKeys()[0]
	if key.GetType() != "s" {
		t.Errorf("enum key should have type 's', got %q", key.GetType())
	}
	if len(key.GetEnumValues()) != 3 {
		t.Errorf("expected 3 enum values, got %d", len(key.GetEnumValues()))
	}
	if key.GetEnumValues()[1].GetNick() != "prefer-dark" {
		t.Errorf("expected nick 'prefer-dark', got %q", key.GetEnumValues()[1].GetNick())
	}
}

func TestScanGSettingsSchemasFrom_RelocatableSchema(t *testing.T) {
	dir := t.TempDir()
	xmlData := `<schemalist>
  <schema id="org.gnome.desktop.notifications.application">
    <key name="enable" type="b">
      <default>true</default>
      <summary>Enable</summary>
    </key>
  </schema>
</schemalist>`
	if err := os.WriteFile(filepath.Join(dir, "test.gschema.xml"), []byte(xmlData), 0o644); err != nil {
		t.Fatal(err)
	}

	schemas, err := ScanGSettingsSchemasFrom(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if !schemas[0].GetRelocatable() {
		t.Error("expected schema with no path to be relocatable")
	}
}
