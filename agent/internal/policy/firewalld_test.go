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

func TestValidateFirewalldZoneName(t *testing.T) {
	valid := []string{"public", "FedoraWorkstation", "bor-zone", "z_1", "trusted"}
	for _, z := range valid {
		if err := ValidateFirewalldZoneName(z); err != nil {
			t.Errorf("ValidateFirewalldZoneName(%q) = %v, want nil", z, err)
		}
	}
	invalid := []string{"", "..", "../etc", "a/b", "zone with space", "thisnameiswaytoolongforzone"}
	for _, z := range invalid {
		if err := ValidateFirewalldZoneName(z); err == nil {
			t.Errorf("ValidateFirewalldZoneName(%q) = nil, want error", z)
		}
	}
}

func TestRichRuleToXML(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			`rule family="ipv4" source address="10.0.0.0/8" service name="ssh" accept`,
			`<rule family="ipv4"><source address="10.0.0.0/8"/><service name="ssh"/><accept/></rule>`,
		},
		{
			`rule port port="443" protocol="tcp" reject type="icmp-host-unreachable"`,
			`<rule><port port="443" protocol="tcp"/><reject type="icmp-host-unreachable"/></rule>`,
		},
		{
			`rule family="ipv6" source not address="2001:db8::/32" drop`,
			`<rule family="ipv6"><source invert="True" address="2001:db8::/32"/><drop/></rule>`,
		},
		{
			`rule protocol value="esp" accept`,
			`<rule><protocol value="esp"/><accept/></rule>`,
		},
		{
			`rule masquerade`,
			`<rule><masquerade/></rule>`,
		},
	}
	for _, c := range cases {
		got, err := richRuleToXML(c.in)
		if err != nil {
			t.Errorf("richRuleToXML(%q) error = %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("richRuleToXML(%q)\n got = %s\nwant = %s", c.in, got, c.want)
		}
	}
}

func TestRichRuleToXML_Rejects(t *testing.T) {
	bad := []string{
		"",
		"family=\"ipv4\" accept", // doesn't start with rule
		`rule family="ipv4"`,     // no element
		`rule bogus-token accept`,
	}
	for _, b := range bad {
		if _, err := richRuleToXML(b); err == nil {
			t.Errorf("richRuleToXML(%q) = nil error, want error", b)
		}
	}
}

func TestRenderZoneXML_PreservesBindings(t *testing.T) {
	existing := []byte(`<?xml version="1.0" encoding="utf-8"?>
<zone target="default">
  <short>Public</short>
  <description>For use in public areas.</description>
  <interface name="eth0"/>
  <source address="192.168.1.0/24"/>
  <service name="dhcpv6-client"/>
</zone>`)

	d := &firewalldDesired{
		target:   "DROP",
		services: []string{"ssh", "https"},
		ports:    []*pb.FirewalldPort{{Port: "8080", Protocol: "tcp"}},
	}
	out, err := RenderZoneXML(existing, d)
	if err != nil {
		t.Fatalf("RenderZoneXML error: %v", err)
	}
	s := string(out)

	// Host bindings + labels preserved.
	for _, want := range []string{
		`<short>Public</short>`,
		`<description>For use in public areas.</description>`,
		`<interface name="eth0"/>`,
		`<source address="192.168.1.0/24"/>`,
		`target="DROP"`,
		`<service name="ssh"/>`,
		`<service name="https"/>`,
		`<port port="8080" protocol="tcp"/>`,
		"managed by Bor",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered zone missing %q\n---\n%s", want, s)
		}
	}
	// The old service (dhcpv6-client) is NOT preserved — Bor owns the ruleset.
	if strings.Contains(s, "dhcpv6-client") {
		t.Errorf("rendered zone unexpectedly kept the old service:\n%s", s)
	}
}

func TestRenderZoneXML_RejectTargetMapping(t *testing.T) {
	out, err := RenderZoneXML(nil, &firewalldDesired{target: "REJECT"})
	if err != nil {
		t.Fatalf("RenderZoneXML error: %v", err)
	}
	if !strings.Contains(string(out), `target="%%REJECT%%"`) {
		t.Errorf("REJECT target not mapped to %%%%REJECT%%%%:\n%s", out)
	}
}

func TestMergeFirewalldEntries(t *testing.T) {
	entries := []FirewalldEntry{
		{Priority: 10, Policy: &pb.FirewalldPolicy{
			Zone: "public", Services: []string{"ssh"}, Masquerade: true, Target: "DROP",
		}},
		{Priority: 20, Policy: &pb.FirewalldPolicy{
			Zone: "public", Services: []string{"ssh", "http"}, Target: "REJECT",
		}},
		{Priority: 5, Policy: &pb.FirewalldPolicy{
			Zone: "", Services: []string{"dns"}, // resolves to default zone
		}},
	}
	byZone := mergeFirewalldEntries(entries, "home")

	pub := byZone["public"]
	if pub == nil {
		t.Fatal("expected merged 'public' zone")
	}
	// Services unioned and deduped.
	if len(pub.services) != 2 {
		t.Errorf("public services = %v, want [ssh http]", pub.services)
	}
	// Masquerade OR-ed from the lower-priority policy.
	if !pub.masquerade {
		t.Error("expected masquerade=true (OR semantics)")
	}
	// Target won by the higher-priority (20) policy → REJECT.
	if pub.target != "REJECT" {
		t.Errorf("public target = %q, want REJECT (highest priority wins)", pub.target)
	}
	// Empty zone resolved to the default ("home").
	if byZone["home"] == nil || len(byZone["home"].services) != 1 {
		t.Errorf("expected 'home' default zone with [dns], got %+v", byZone["home"])
	}
}

func TestListBorManagedFirewalldFiles(t *testing.T) {
	dir := t.TempDir()
	// A managed zone has both the .xml and a .bor-backup sentinel.
	managed := filepath.Join(dir, "public.xml")
	if err := os.WriteFile(managed, []byte("<zone/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed+BackupSuffix, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unmanaged zone has no backup.
	if err := os.WriteFile(filepath.Join(dir, "work.xml"), []byte("<zone/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListBorManagedFirewalldFiles(dir)
	if err != nil {
		t.Fatalf("ListBorManagedFirewalldFiles error: %v", err)
	}
	if len(got) != 1 || got[0] != managed {
		t.Errorf("got %v, want [%s]", got, managed)
	}
}
