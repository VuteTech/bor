// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package reports

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/pkg/ext"
	"github.com/VuteTech/Bor/server/pkg/report"
)

var fixtureNow = time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)

func fixtureControls(schemaID string) []Control {
	if schemaID == "selinux:mode" {
		return []Control{{Framework: "CIS RHEL 9 v2.0.0", ID: "1.3.1.5", Title: "Ensure the SELinux mode is enforcing"}}
	}
	return nil
}

func fixtureSource() *Source {
	seen := fixtureNow.Add(-2 * time.Minute)
	old := fixtureNow.Add(-30 * 24 * time.Hour)
	tpm := true
	return &Source{
		Nodes: []SourceNode{
			{ID: "n2", Name: "zeta", Status: "offline", LastSeen: &old, GroupNames: []string{"Lab B"}, GroupIDs: []string{"g2"}},
			{
				ID: "n1", Name: "alpha", FQDN: "alpha.example.edu", OSName: "Fedora Linux", OSVersion: "43", AgentVersion: "0.11.0",
				Status: "online", LastSeen: &seen, GroupNames: []string{"Lab B", "Lab A"}, GroupIDs: []string{"g2", "g1"},
				LSM: "selinux", LSMMode: "enforcing",
				DiskEncryption: &SourceDiskEncryption{SystemVolumeEncrypted: true, Cipher: "aes-xts-plain64", Escrowed: true, TPM2Present: &tpm, SecureBoot: "enabled", ReportedAt: &seen},
				Updates:        &SourceUpdates{Engine: "dnf-automatic", EffectiveMode: "security", PendingSecurity: 2, RebootRequired: true, ReportedAt: &seen},
			},
			{ID: "n3", Name: "beta", Status: "online", LastSeen: &seen},
		},
		Results: []SourceResult{
			{NodeID: "n1", PolicyID: "p2", PolicyName: "Updates", PolicyType: "Updates", PolicyVersion: 1, Status: "non_compliant", Message: "2 security updates pending", ReportedAt: fixtureNow.Add(-time.Hour)},
			{NodeID: "n1", PolicyID: "p1", PolicyName: "SELinux Baseline", PolicyType: "Selinux", PolicyVersion: 3, Status: "compliant", Message: "Deployed", ReportedAt: fixtureNow.Add(-time.Hour),
				ItemsJSON: []byte(`[{"schema_id":"selinux:mode","key":"enforcing","status":"compliant","message":""},{"schema_id":"selinux:boolean","key":"deny_ptrace","status":"inapplicable","message":"not present"}]`)},
			{NodeID: "n3", PolicyID: "p1", PolicyName: "SELinux Baseline", PolicyType: "Selinux", PolicyVersion: 3, Status: "error", Message: "semodule failed", ReportedAt: old,
				ItemsJSON: []byte(`[{"schema_id":"selinux:mode","key":"enforcing","status":"non_compliant","message":"permissive"}]`)},
			{NodeID: "ghost", PolicyID: "p1", PolicyName: "SELinux Baseline", PolicyType: "Selinux", Status: "compliant", ReportedAt: fixtureNow},
		},
	}
}

func fixtureOptions() *Options {
	return &Options{
		ID: "r1", GeneratedAt: fixtureNow, Trigger: report.TriggerManual,
		Actor: report.Actor{UserID: "u1", Username: "alice"}, ServerVersion: "0.11.0", Label: "Q4 labs",
		Scope:      report.Scope{NodeGroups: []report.Ref{{ID: "g2", Name: "Lab B"}, {ID: "g1", Name: "Lab A"}}},
		StaleAfter: 7 * 24 * time.Hour, IncludeItems: true, ControlsFor: fixtureControls,
	}
}

func TestBuild_SummaryAndFlags(t *testing.T) {
	b := Build(fixtureSource(), fixtureOptions())

	if b.APIVersion != report.APIVersion || b.Kind != report.Kind {
		t.Fatalf("schema %s/%s", b.APIVersion, b.Kind)
	}
	s := b.Summary
	if s.Nodes != 3 || s.NodesOnline != 2 || s.NodesUnreachable != 1 || s.NodesWithoutResults != 1 {
		t.Fatalf("node summary %+v", s)
	}
	if s.Results != 3 || s.ResultsStale != 1 {
		t.Fatalf("result summary %+v", s)
	}
	if s.ByStatus != (report.StatusCounts{Compliant: 1, NonCompliant: 1, Error: 1}) {
		t.Fatalf("by status %+v", s.ByStatus)
	}
	if s.ScorePct == nil || *s.ScorePct != 33.3 {
		t.Fatalf("score %v", s.ScorePct)
	}

	// Nodes sorted by name; groups sorted; posture copied.
	if got := []string{b.Nodes[0].Name, b.Nodes[1].Name, b.Nodes[2].Name}; strings.Join(got, ",") != "alpha,beta,zeta" {
		t.Fatalf("node order %v", got)
	}
	alpha := b.Nodes[0]
	if strings.Join(alpha.Groups, ",") != "Lab A,Lab B" || alpha.Posture.DiskEncryption == nil || !alpha.Posture.DiskEncryption.RecoveryKeyEscrowed || alpha.Posture.Updates.PendingSecurity != 2 {
		t.Fatalf("alpha %+v", alpha)
	}
	if alpha.ByStatus != (report.StatusCounts{Compliant: 1, NonCompliant: 1}) {
		t.Fatalf("alpha counts %+v", alpha.ByStatus)
	}
	zeta := b.Nodes[2]
	if !zeta.Unreachable || zeta.ByStatus.Total() != 0 {
		t.Fatalf("zeta %+v", zeta)
	}

	// Results sorted by node then policy name; the ghost node's result is dropped.
	if len(b.Results) != 3 || b.Results[0].Node != "alpha" || b.Results[0].PolicyID != "p1" || b.Results[1].PolicyID != "p2" || b.Results[2].Node != "beta" {
		t.Fatalf("results %+v", b.Results)
	}
	if !b.Results[2].Stale || b.Results[2].AgeSeconds != 30*24*3600 || b.Results[0].Stale {
		t.Fatalf("stale flags %+v %+v", b.Results[0], b.Results[2])
	}
	if len(b.Results[0].Items) != 2 || len(b.Results[0].Items[0].Controls) != 1 || b.Results[0].Items[0].Controls[0].ID != "1.3.1.5" {
		t.Fatalf("items %+v", b.Results[0].Items)
	}

	// Controls aggregated across nodes.
	if len(b.Controls) != 1 {
		t.Fatalf("controls %+v", b.Controls)
	}
	c := b.Controls[0]
	if c.Framework != "CIS RHEL 9 v2.0.0" || c.ID != "1.3.1.5" || c.Title == "" || c.Pass != 1 || c.Fail != 1 || c.NodesFailing != 1 {
		t.Fatalf("control %+v", c)
	}

	// Policies sorted by name with counts.
	if len(b.Policies) != 2 || b.Policies[0].Name != "SELinux Baseline" || b.Policies[0].Version != 3 || b.Policies[0].ByStatus.Error != 1 || b.Policies[1].ByStatus.NonCompliant != 1 {
		t.Fatalf("policies %+v", b.Policies)
	}
	// Scope normalised (sorted).
	if b.Scope.NodeGroups[0].Name != "Lab A" {
		t.Fatalf("scope %+v", b.Scope)
	}
}

func TestBuild_ItemsOmittedButControlsKept(t *testing.T) {
	opts := fixtureOptions()
	opts.IncludeItems = false
	b := Build(fixtureSource(), opts)
	for _, r := range b.Results {
		if len(r.Items) != 0 {
			t.Fatalf("items present: %+v", r)
		}
	}
	if len(b.Controls) != 1 || b.Controls[0].Fail != 1 {
		t.Fatalf("controls %+v", b.Controls)
	}
}

func TestBuild_NoStaleThreshold(t *testing.T) {
	opts := fixtureOptions()
	opts.StaleAfter = 0
	b := Build(fixtureSource(), opts)
	if b.Summary.ResultsStale != 0 || b.Summary.NodesUnreachable != 0 || b.Thresholds.StaleAfterSeconds != 0 {
		t.Fatalf("flags without threshold %+v", b.Summary)
	}
}

func TestBuild_ScoreNilWhenNothingEvaluable(t *testing.T) {
	src := &Source{
		Nodes:   []SourceNode{{ID: "n1", Name: "a", Status: "online"}},
		Results: []SourceResult{{NodeID: "n1", PolicyID: "p", PolicyName: "P", PolicyType: "Dconf", Status: "inapplicable", ReportedAt: fixtureNow}},
	}
	b := Build(src, fixtureOptions())
	if b.Summary.ScorePct != nil || b.Summary.ByStatus.Inapplicable != 1 {
		t.Fatalf("summary %+v", b.Summary)
	}
}

func TestMarshal_CanonicalAndStable(t *testing.T) {
	first, err := Marshal(Build(fixtureSource(), fixtureOptions()))
	if err != nil {
		t.Fatal(err)
	}
	// Shuffle the source order: the output must not change.
	src := fixtureSource()
	src.Nodes[0], src.Nodes[2] = src.Nodes[2], src.Nodes[0]
	src.Results[0], src.Results[1] = src.Results[1], src.Results[0]
	second, err := Marshal(Build(src, fixtureOptions()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical bytes differ:\n%s\n%s", first, second)
	}
	if !bytes.HasPrefix(first, []byte(`{"apiVersion":"bor.report.v1","kind":"ComplianceReport","id":"r1","generated_at":"2026-10-20T14:00:00Z"`)) {
		t.Fatalf("prefix %s", first[:120])
	}
	if bytes.HasSuffix(first, []byte("\n")) {
		t.Fatal("trailing newline")
	}
	if bytes.Contains(first, []byte(`<`)) {
		t.Fatal("HTML escaping must be off")
	}
	// Round trip through the public parser.
	parsed, err := report.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Marshal(parsed)
	if !bytes.Equal(first, again) {
		t.Fatal("parse/marshal round trip changed the bytes")
	}
	if got := hex.EncodeToString(Hash(first)); len(got) != 64 || got != hex.EncodeToString(Hash(second)) {
		t.Fatalf("hash %s", got)
	}
}

func TestGzipRoundTrip(t *testing.T) {
	canonical, _ := Marshal(Build(fixtureSource(), fixtureOptions()))
	gz, err := Gzip(canonical)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Gunzip(gz)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, back) {
		t.Fatal("gzip round trip changed the bytes")
	}
	if _, err := Gunzip([]byte("not gzip")); err == nil {
		t.Fatal("expected an error for corrupt data")
	}
}

func TestParse_RejectsOtherDocuments(t *testing.T) {
	if _, err := report.Parse([]byte(`{"apiVersion":"bor.dev/v1","kind":"Policy"}`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestWriteCSV_Parts(t *testing.T) {
	b := Build(fixtureSource(), fixtureOptions())
	b.Results[0].Message = "=cmd()|'/C calc'!A0"
	for _, tc := range []struct {
		part   string
		header string
		rows   int
	}{
		{CSVResults, "node,node_groups,policy,policy_type,policy_version,status,message,reported_at,age_seconds,stale,items,items_failed", 3},
		{CSVItems, "node,policy,policy_type,schema_id,key,status,message,controls", 3},
		{CSVNodes, "node,fqdn,os,os_version,desktop,agent_version,status,last_seen,unreachable,node_groups,lsm,lsm_mode,system_volume_encrypted,recovery_key_escrowed,tpm2_present,secure_boot,updates_engine,pending_security,reboot_required,compliant,non_compliant,error,inapplicable,unknown", 3},
		{CSVControls, "framework,control_id,title,pass,fail,error,inapplicable,nodes_failing", 1},
	} {
		var buf bytes.Buffer
		if err := WriteCSV(&buf, b, tc.part); err != nil {
			t.Fatalf("%s: %v", tc.part, err)
		}
		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if lines[0] != tc.header {
			t.Fatalf("%s header %q", tc.part, lines[0])
		}
		if len(lines)-1 != tc.rows {
			t.Fatalf("%s rows %d: %s", tc.part, len(lines)-1, buf.String())
		}
	}
	var buf bytes.Buffer
	_ = WriteCSV(&buf, b, CSVResults)
	if !strings.Contains(buf.String(), `,'=cmd()|'/C calc'!A0,`) {
		t.Fatalf("formula not neutralised: %s", buf.String())
	}
	if err := WriteCSV(&buf, b, "bogus"); err == nil {
		t.Fatal("expected an error for an unknown part")
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()
	if reg.Signer() != nil || len(reg.Formats()) != 0 || reg.Renderer("html") != nil {
		t.Fatal("empty registry")
	}
	if err := reg.RegisterRenderer(fakeRenderer("json")); err == nil {
		t.Fatal("reserved format accepted")
	}
	if err := reg.RegisterRenderer(fakeRenderer("pdf")); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterRenderer(fakeRenderer("html")); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterRenderer(fakeRenderer("pdf")); err == nil {
		t.Fatal("duplicate accepted")
	}
	if got := strings.Join(reg.Formats(), ","); got != "html,pdf" {
		t.Fatalf("formats %s", got)
	}
	if err := reg.RegisterSigner(fakeSigner{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterSigner(fakeSigner{}); err == nil {
		t.Fatal("second signer accepted")
	}
}

type fakeRenderer string

func (f fakeRenderer) Format() string        { return string(f) }
func (f fakeRenderer) ContentType() string   { return "application/octet-stream" }
func (f fakeRenderer) FileExtension() string { return string(f) }
func (f fakeRenderer) Render(context.Context, []byte, *ext.ReportSignature) ([]byte, error) {
	return []byte(f), nil
}

type fakeSigner struct{}

func (fakeSigner) Sign(context.Context, []byte) (*ext.ReportSignature, error) {
	return &ext.ReportSignature{Format: "test", Value: "sig", KeyID: "k"}, nil
}
func (fakeSigner) Verify(context.Context, []byte, *ext.ReportSignature) error { return nil }

// Guard against accidental changes of the bundle shape: the fixture's
// summary block is what auditors parse.
func TestBundle_SummaryShape(t *testing.T) {
	canonical, _ := Marshal(Build(fixtureSource(), fixtureOptions()))
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"summary", "controls", "policies", "nodes", "results", "scope", "thresholds", "generated_by", "server"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if string(doc["summary"]) != `{"nodes":3,"nodes_online":2,"nodes_unreachable":1,"nodes_without_results":1,"results":3,"results_stale":1,"by_status":{"compliant":1,"non_compliant":1,"error":1,"inapplicable":0,"unknown":0},"score_pct":33.3}` {
		t.Fatalf("summary %s", doc["summary"])
	}
}
