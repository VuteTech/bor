// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package export

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// Representative content per policy type, in the exact casing the UI editors
// store (PascalCase json_name for browser types, camelCase for
// Kconfig/Package/Firewalld, snake_case for Polkit/Dconf).
var typeContent = map[string]string{
	"Firefox":     `{"DisableTelemetry": true, "Homepage": {"URL": "https://intranet.example.com", "Locked": true}}`,
	"Thunderbird": `{"DisableTelemetry": true}`,
	"Chrome":      `{"HomepageLocation": "https://intranet.example.com", "PasswordManagerEnabled": false}`,
	"Edge":        `{"HomepageLocation": "https://kiosk.example.com", "SmartScreenEnabled": true}`,
	"Kconfig":     `{"shellAccess": false, "kcmRestrictions": ["kcm_users"]}`,
	"Dconf":       `{"db_name": "local", "entries": [{"schema_id": "org.gnome.desktop.screensaver", "key": "lock-enabled", "value": "true", "locked": true}]}`,
	"Polkit":      `{"rules": [{"description": "d", "action_ids": ["org.freedesktop.udisks2.filesystem-mount"], "result": "POLKIT_RESULT_YES"}]}`,
	"Package":     `{"packages": [{"name": "git", "state": "PACKAGE_STATE_PRESENT"}], "updateCache": true}`,
	"Firewalld":   `{"zone": "work", "services": ["ssh"], "ports": [{"port": "8443", "protocol": "tcp"}]}`,
}

func TestRoundTripAllTypes(t *testing.T) {
	for ptype, content := range typeContent {
		t.Run(ptype, func(t *testing.T) {
			p := &models.Policy{
				Name:        ptype + " — Test Baseline",
				Description: "round-trip test",
				Type:        ptype,
				Content:     content,
			}
			doc, err := BuildPolicyDoc(p, Slugify(p.Name))
			if err != nil {
				t.Fatalf("BuildPolicyDoc: %v", err)
			}
			bundle, err := MarshalBundle([][]byte{doc})
			if err != nil {
				t.Fatalf("MarshalBundle: %v", err)
			}
			docs, err := SplitAndConvert(bundle)
			if err != nil {
				t.Fatalf("SplitAndConvert: %v", err)
			}
			if len(docs) != 1 {
				t.Fatalf("expected 1 document, got %d", len(docs))
			}
			res, err := ParseResource(docs[0])
			if err != nil {
				t.Fatalf("ParseResource: %v", err)
			}
			if res.Kind != "Policy" || res.GetPolicy().GetType() != ptype {
				t.Fatalf("unexpected resource: kind=%s type=%s", res.Kind, res.GetPolicy().GetType())
			}
			if res.GetMetadata().GetDisplayName() != p.Name {
				t.Fatalf("displayName lost: %q", res.GetMetadata().GetDisplayName())
			}
			got, err := ContentJSON(res.GetPolicy())
			if err != nil {
				t.Fatalf("ContentJSON: %v", err)
			}
			// Canonical forms must be identical after the trip.
			want, err := CanonicalizeContent(ptype, content)
			if err != nil {
				t.Fatalf("canonicalize original: %v", err)
			}
			gotCanon, err := CanonicalizeContent(ptype, got)
			if err != nil {
				t.Fatalf("canonicalize round-tripped: %v", err)
			}
			if gotCanon != want {
				t.Fatalf("content mismatch\nwant: %s\ngot:  %s", want, gotCanon)
			}
			// The type-specific service validator must accept canonical output.
			if err := services.ValidatePolicyContentByType(ptype, gotCanon); err != nil {
				t.Fatalf("service validator rejected canonical content: %v", err)
			}
		})
	}
}

func TestExportDeterministic(t *testing.T) {
	p := &models.Policy{Name: "Determinism", Type: "Firefox", Content: typeContent["Firefox"]}
	a, err := BuildPolicyDoc(p, "determinism")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := BuildPolicyDoc(p, "determinism")
	if !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic export:\n%s\n%s", a, b)
	}
	ya, err := MarshalBundle([][]byte{a})
	if err != nil {
		t.Fatal(err)
	}
	yb, _ := MarshalBundle([][]byte{b})
	if !bytes.Equal(ya, yb) {
		t.Fatal("non-deterministic YAML")
	}
}

func TestCanonicalizePolkitNormalizesCasing(t *testing.T) {
	// camelCase input (protojson-accepted) must come out snake_case, the
	// casing the polkit editor stores.
	camel := `{"rules":[{"description":"d","actionIds":["a.b"],"result":"POLKIT_RESULT_YES"}]}`
	canon, err := CanonicalizeContent("Polkit", camel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(canon, `"action_ids"`) {
		t.Fatalf("expected snake_case action_ids, got: %s", canon)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Firefox — Corporate Baseline": "firefox-corporate-baseline",
		"  weird///name  ":             "weird-name",
		"---":                          "policy",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImportBoundaryRejections(t *testing.T) {
	cases := map[string]string{
		"alias":        "a: &x 1\nb: *x\n",
		"non-mapping":  "- just\n- a\n- list\n",
		"unknown kind": "apiVersion: bor.dev/v1\nkind: Gadget\nmetadata: {name: x}\nspec: {}\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			docs, err := SplitAndConvert([]byte(doc))
			if err != nil {
				return // rejected at the YAML layer — fine
			}
			if _, err := ParseResource(docs[0]); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestParseResourceStrictEnvelope(t *testing.T) {
	doc := []byte(`{"apiVersion":"bor.dev/v1","kind":"Policy","metadata":{"name":"x"},"spec":{"type":"Firefox","content":{}},"status":{"sneaky":true}}`)
	if _, err := ParseResource(doc); err == nil {
		t.Fatal("unknown envelope field accepted")
	}
	doc = []byte(`{"apiVersion":"bor.dev/v2","kind":"Policy","metadata":{"name":"x"},"spec":{"type":"Firefox","content":{}}}`)
	if _, err := ParseResource(doc); err == nil {
		t.Fatal("wrong apiVersion accepted")
	}
}

/* ── import pipeline with fake stores ── */

type fakePolicies struct {
	existing []*models.Policy
	created  []*models.CreatePolicyRequest
	updated  map[string]*models.UpdatePolicyRequest
}

func (f *fakePolicies) ListAllPolicies(_ context.Context) ([]*models.Policy, error) {
	return f.existing, nil
}
func (f *fakePolicies) CreatePolicy(_ context.Context, req *models.CreatePolicyRequest, _ string) (*models.Policy, error) {
	f.created = append(f.created, req)
	return &models.Policy{ID: fmt.Sprintf("new-%d", len(f.created)), Name: req.Name, Type: req.Type, State: "draft"}, nil
}
func (f *fakePolicies) UpdatePolicy(_ context.Context, id string, req *models.UpdatePolicyRequest) (*models.Policy, error) {
	if f.updated == nil {
		f.updated = map[string]*models.UpdatePolicyRequest{}
	}
	f.updated[id] = req
	return &models.Policy{ID: id}, nil
}

type fakeBindings struct {
	created []*models.CreatePolicyBindingRequest
}

func (f *fakeBindings) CreateBinding(_ context.Context, req *models.CreatePolicyBindingRequest) (*models.PolicyBinding, error) {
	f.created = append(f.created, req)
	return &models.PolicyBinding{ID: "b1"}, nil
}

type fakeGroups struct{ groups []*models.NodeGroup }

func (f *fakeGroups) ListNodeGroups(_ context.Context) ([]*models.NodeGroup, error) {
	return f.groups, nil
}

const importBundle = `apiVersion: bor.dev/v1
kind: Policy
metadata:
  name: firefox-baseline
  displayName: "Firefox Baseline"
spec:
  type: Firefox
  content:
    DisableTelemetry: true
---
apiVersion: bor.dev/v1
kind: PolicyBinding
metadata:
  name: firefox-eng
spec:
  policy: firefox-baseline
  group: "Engineering"
  priority: 10
`

func importFixture() (*fakePolicies, *fakeBindings, *fakeGroups) {
	return &fakePolicies{},
		&fakeBindings{},
		&fakeGroups{groups: []*models.NodeGroup{{ID: "g1", Name: "Engineering"}}}
}

func TestImportCreatesPolicyAndBinding(t *testing.T) {
	fp, fb, fg := importFixture()
	report, err := Import(context.Background(), fp, fb, fg, services.ValidatePolicyContentByType, []byte(importBundle), Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ok || report.Created != 2 || report.Errors != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(fp.created) != 1 || fp.created[0].Name != "Firefox Baseline" {
		t.Fatalf("policy not created correctly: %+v", fp.created)
	}
	if len(fb.created) != 1 || fb.created[0].GroupID != "g1" || fb.created[0].PolicyID != "new-1" {
		t.Fatalf("binding not created correctly: %+v", fb.created)
	}
}

func TestImportDryRunCreatesNothing(t *testing.T) {
	fp, fb, fg := importFixture()
	report, err := Import(context.Background(), fp, fb, fg, services.ValidatePolicyContentByType, []byte(importBundle), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ok || !report.DryRun || report.Created != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(fp.created) != 0 || len(fb.created) != 0 {
		t.Fatal("dry run must not create resources")
	}
}

func TestImportConflictModes(t *testing.T) {
	existing := &models.Policy{ID: "p1", Name: "Firefox Baseline", Type: "Firefox", State: "draft"}

	// error (default): whole bundle rejected, nothing created
	fp, fb, fg := importFixture()
	fp.existing = []*models.Policy{existing}
	report, err := Import(context.Background(), fp, fb, fg, nil, []byte(importBundle), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ok || report.Errors == 0 || len(fp.created) != 0 || len(fb.created) != 0 {
		t.Fatalf("conflict should reject bundle: %+v", report)
	}

	// skip: policy skipped, binding still attaches to the existing policy
	fp, fb, fg = importFixture()
	fp.existing = []*models.Policy{existing}
	report, err = Import(context.Background(), fp, fb, fg, nil, []byte(importBundle), Options{OnConflict: OnConflictSkip})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ok || report.Skipped != 1 || len(fb.created) != 1 || fb.created[0].PolicyID != "p1" {
		t.Fatalf("skip mode wrong: %+v created=%+v", report, fb.created)
	}

	// new-version: draft updated in place
	fp, fb, fg = importFixture()
	fp.existing = []*models.Policy{existing}
	report, err = Import(context.Background(), fp, fb, fg, nil, []byte(importBundle), Options{OnConflict: OnConflictNewVersion})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ok || report.Updated != 1 || fp.updated["p1"] == nil {
		t.Fatalf("new-version mode wrong: %+v", report)
	}

	// new-version against a released policy: rejected
	fp, fb, fg = importFixture()
	fp.existing = []*models.Policy{{ID: "p1", Name: "Firefox Baseline", Type: "Firefox", State: "released"}}
	report, err = Import(context.Background(), fp, fb, fg, nil, []byte(importBundle), Options{OnConflict: OnConflictNewVersion})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ok || report.Errors == 0 {
		t.Fatalf("released policy update should be rejected: %+v", report)
	}
}

func TestImportMissingGroupRejectsBundle(t *testing.T) {
	fp, fb, fg := importFixture()
	fg.groups = nil
	report, err := Import(context.Background(), fp, fb, fg, nil, []byte(importBundle), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ok || len(fp.created) != 0 {
		t.Fatalf("missing group must reject bundle: %+v", report)
	}
}

func TestImportRejectsUnknownType(t *testing.T) {
	bundle := strings.Replace(importBundle, "type: Firefox", "type: FluxCapacitor", 1)
	fp, fb, fg := importFixture()
	report, err := Import(context.Background(), fp, fb, fg, nil, []byte(bundle), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ok || report.Errors == 0 {
		t.Fatalf("unknown type accepted: %+v", report)
	}
}

func TestImportDocumentLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= maxDocuments; i++ {
		fmt.Fprintf(&b, "---\napiVersion: bor.dev/v1\nkind: Policy\nmetadata: {name: p%d}\nspec: {type: Firefox, content: {}}\n", i)
	}
	if _, err := SplitAndConvert([]byte(b.String())); err == nil {
		t.Fatal("document limit not enforced")
	}
}
