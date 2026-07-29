// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// FirewalldZonesDir is the directory holding admin-overridable firewalld zone
// configuration files. Files here shadow the distro defaults in
// /usr/lib/firewalld/zones/.
const FirewalldZonesDir = "/etc/firewalld/zones"

// firewalldZoneNameRE matches a valid firewalld zone name. firewalld also
// limits zone names to 17 characters.
var firewalldZoneNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// runCommand executes an external command and returns its combined output.
// It is a package var so tests can stub firewall-cmd interactions. All call
// sites use a fixed binary ("firewall-cmd") and constant flags; no
// server-supplied data is ever passed as an argument (rule content goes into
// the zone XML file, and the only dynamic value — the zone name — is validated
// by ValidateFirewalldZoneName before use).
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput() //nolint:gosec // G204: fixed binary + constant/validated args, see doc comment
}

// lookPath reports whether an executable is found on PATH. It is a package var
// so tests can stub binary availability.
var lookPath = func(file string) error {
	_, err := exec.LookPath(file)
	return err
}

// FirewalldEntry pairs a FirewalldPolicy with its binding priority so scalar
// fields (e.g. target) can be resolved highest-priority-wins on merge.
type FirewalldEntry struct {
	Priority int32
	Policy   *pb.FirewalldPolicy
}

// firewalldDesired is the merged, rendered-ready ruleset for a single zone.
type firewalldDesired struct {
	target             string // "", "ACCEPT", "REJECT", "DROP"
	targetPriority     int32
	services           []string
	ports              []*pb.FirewalldPort
	sourcePorts        []*pb.FirewalldPort
	protocols          []string
	icmpBlocks         []string
	icmpBlockInversion bool
	masquerade         bool
	forwardPorts       []*pb.FirewalldForwardPort
	richRules          []string
}

// ValidateFirewalldZoneName reports whether a server-supplied zone name is safe
// to use as a single path component and is a valid firewalld zone name. This
// is the same path-traversal guard applied to other server-supplied
// identifiers (see sanitize.go).
func ValidateFirewalldZoneName(zone string) error {
	if zone == "" {
		return fmt.Errorf("zone name must not be empty")
	}
	if len(zone) > 17 || !firewalldZoneNameRE.MatchString(zone) {
		return fmt.Errorf("invalid zone name %q: must match [A-Za-z0-9_-] and be at most 17 characters", zone)
	}
	return nil
}

// FirewalldTargetZones returns the sorted, unique set of zone names that the
// given entries target (resolving empty zones to defaultZone). Used by the
// caller to pre-suppress the file watcher for the files about to be written.
func FirewalldTargetZones(entries []FirewalldEntry, defaultZone string) []string {
	byZone := mergeFirewalldEntries(entries, defaultZone)
	zones := make([]string, 0, len(byZone))
	for z := range byZone {
		zones = append(zones, z)
	}
	sort.Strings(zones)
	return zones
}

// mergeFirewalldEntries groups entries by target zone and merges them. List
// fields are unioned (deduplicated); boolean fields are OR-ed; the string
// target is won by the highest-priority entry that sets it. An empty zone on an
// entry resolves to defaultZone.
func mergeFirewalldEntries(entries []FirewalldEntry, defaultZone string) map[string]*firewalldDesired {
	// Process highest priority first so target resolution is deterministic.
	sorted := make([]FirewalldEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority > sorted[j].Priority })

	byZone := make(map[string]*firewalldDesired)
	for _, e := range sorted {
		if e.Policy == nil {
			continue
		}
		zone := e.Policy.GetZone()
		if zone == "" {
			zone = defaultZone
		}
		if zone == "" {
			// No explicit zone and no default resolved — skip; caller logs.
			continue
		}
		d := byZone[zone]
		if d == nil {
			d = &firewalldDesired{}
			byZone[zone] = d
		}
		if t := e.Policy.GetTarget(); t != "" && t != "default" {
			if d.target == "" || e.Priority > d.targetPriority {
				d.target = t
				d.targetPriority = e.Priority
			}
		}
		d.services = appendUnique(d.services, e.Policy.GetServices()...)
		d.protocols = appendUnique(d.protocols, e.Policy.GetProtocols()...)
		d.icmpBlocks = appendUnique(d.icmpBlocks, e.Policy.GetIcmpBlocks()...)
		d.richRules = appendUnique(d.richRules, e.Policy.GetRichRules()...)
		d.ports = appendUniquePorts(d.ports, e.Policy.GetPorts())
		d.sourcePorts = appendUniquePorts(d.sourcePorts, e.Policy.GetSourcePorts())
		d.forwardPorts = appendUniqueForwardPorts(d.forwardPorts, e.Policy.GetForwardPorts())
		d.masquerade = d.masquerade || e.Policy.GetMasquerade()
		d.icmpBlockInversion = d.icmpBlockInversion || e.Policy.GetIcmpBlockInversion()
	}
	return byZone
}

func appendUnique(dst []string, vals ...string) []string {
	for _, v := range vals {
		if v == "" {
			continue
		}
		found := false
		for _, e := range dst {
			if e == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

func appendUniquePorts(dst, vals []*pb.FirewalldPort) []*pb.FirewalldPort {
	for _, v := range vals {
		if v == nil {
			continue
		}
		found := false
		for _, e := range dst {
			if e.GetPort() == v.GetPort() && e.GetProtocol() == v.GetProtocol() {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

func appendUniqueForwardPorts(dst, vals []*pb.FirewalldForwardPort) []*pb.FirewalldForwardPort {
	for _, v := range vals {
		if v == nil {
			continue
		}
		found := false
		for _, e := range dst {
			if e.GetPort() == v.GetPort() && e.GetProtocol() == v.GetProtocol() &&
				e.GetToPort() == v.GetToPort() && e.GetToAddr() == v.GetToAddr() {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

// ── Zone XML rendering ──────────────────────────────────────────────────────

// xmlZone is the subset of the firewalld zone schema Bor reads and writes.
// Host-specific bindings (interface/source) and labels (short/description) are
// preserved from the existing file; the rest is replaced by Bor's desired set.
type xmlZone struct {
	XMLName     xml.Name      `xml:"zone"`
	Target      string        `xml:"target,attr,omitempty"`
	Short       string        `xml:"short,omitempty"`
	Description string        `xml:"description,omitempty"`
	Interfaces  []xmlNameAttr `xml:"interface"`
	Sources     []xmlSource   `xml:"source"`
}

type xmlNameAttr struct {
	Name string `xml:"name,attr"`
}

type xmlSource struct {
	Address string `xml:"address,attr,omitempty"`
	MAC     string `xml:"mac,attr,omitempty"`
	IPSet   string `xml:"ipset,attr,omitempty"`
}

// parseExistingZone extracts the bindings/labels to preserve from an existing
// zone file. Missing or unparseable input yields an empty zone (Bor will write
// a fresh one).
func parseExistingZone(data []byte) xmlZone {
	var z xmlZone
	if len(bytes.TrimSpace(data)) == 0 {
		return z
	}
	_ = xml.Unmarshal(data, &z)
	return z
}

// RenderZoneXML produces the complete firewalld zone XML for a zone, preserving
// the labels/bindings found in existing and applying the desired Bor ruleset.
func RenderZoneXML(existing []byte, d *firewalldDesired) ([]byte, error) {
	prev := parseExistingZone(existing)

	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString("<!-- This file is managed by Bor. Do not edit manually. -->\n")

	// target attribute: omit for default; firewalld stores REJECT as %%REJECT%%.
	targetAttr := ""
	switch d.target {
	case "", "default":
		// omit
	case "REJECT":
		targetAttr = ` target="%%REJECT%%"`
	default:
		targetAttr = fmt.Sprintf(" target=%q", d.target)
	}
	b.WriteString("<zone" + targetAttr + ">\n")

	if prev.Short != "" {
		fmt.Fprintf(&b, "  <short>%s</short>\n", xmlEscape(prev.Short))
	}
	if prev.Description != "" {
		fmt.Fprintf(&b, "  <description>%s</description>\n", xmlEscape(prev.Description))
	}
	// Preserve host bindings verbatim.
	for _, ifc := range prev.Interfaces {
		fmt.Fprintf(&b, "  <interface name=%q/>\n", ifc.Name)
	}
	for _, s := range prev.Sources {
		switch {
		case s.Address != "":
			fmt.Fprintf(&b, "  <source address=%q/>\n", s.Address)
		case s.MAC != "":
			fmt.Fprintf(&b, "  <source mac=%q/>\n", s.MAC)
		case s.IPSet != "":
			fmt.Fprintf(&b, "  <source ipset=%q/>\n", s.IPSet)
		}
	}

	// Bor-managed ruleset.
	for _, svc := range d.services {
		fmt.Fprintf(&b, "  <service name=%q/>\n", svc)
	}
	for _, p := range d.ports {
		fmt.Fprintf(&b, "  <port port=%q protocol=%q/>\n", p.GetPort(), p.GetProtocol())
	}
	for _, pr := range d.protocols {
		fmt.Fprintf(&b, "  <protocol value=%q/>\n", pr)
	}
	for _, p := range d.sourcePorts {
		fmt.Fprintf(&b, "  <source-port port=%q protocol=%q/>\n", p.GetPort(), p.GetProtocol())
	}
	for _, ib := range d.icmpBlocks {
		fmt.Fprintf(&b, "  <icmp-block name=%q/>\n", ib)
	}
	if d.icmpBlockInversion {
		b.WriteString("  <icmp-block-inversion/>\n")
	}
	if d.masquerade {
		b.WriteString("  <masquerade/>\n")
	}
	for _, f := range d.forwardPorts {
		b.WriteString("  <forward-port")
		fmt.Fprintf(&b, " port=%q protocol=%q", f.GetPort(), f.GetProtocol())
		if f.GetToPort() != "" {
			fmt.Fprintf(&b, " to-port=%q", f.GetToPort())
		}
		if f.GetToAddr() != "" {
			fmt.Fprintf(&b, " to-addr=%q", f.GetToAddr())
		}
		b.WriteString("/>\n")
	}
	for _, rr := range d.richRules {
		xmlRule, err := richRuleToXML(rr)
		if err != nil {
			return nil, fmt.Errorf("rich rule %q: %w", rr, err)
		}
		fmt.Fprintf(&b, "  %s\n", xmlRule)
	}

	b.WriteString("</zone>\n")
	return b.Bytes(), nil
}

// xmlEscape escapes a string for use in XML text/attribute content.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// ── firewalld interaction ───────────────────────────────────────────────────

// FirewalldAvailable reports whether the firewall-cmd binary is present.
func FirewalldAvailable() bool {
	return lookPath("firewall-cmd") == nil
}

// FirewalldActive reports whether the firewalld daemon is running.
func FirewalldActive() bool {
	_, err := runCommand("firewall-cmd", "--state")
	return err == nil
}

// FirewalldDefaultZone returns the node's configured default zone.
func FirewalldDefaultZone() (string, error) {
	out, err := runCommand("firewall-cmd", "--get-default-zone")
	if err != nil {
		return "", fmt.Errorf("get default zone: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// firewalldCheckConfig validates the permanent configuration without applying it.
func firewalldCheckConfig() error {
	if out, err := runCommand("firewall-cmd", "--check-config"); err != nil {
		return fmt.Errorf("check-config failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// firewalldReload applies the permanent configuration to the running firewall.
func firewalldReload() error {
	if out, err := runCommand("firewall-cmd", "--reload"); err != nil {
		return fmt.Errorf("reload failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// FirewalldSyncResult is the outcome of a sync, mapped to four-state compliance.
type FirewalldSyncResult struct {
	Status  pb.ComplianceStatus
	Message string
}

// SyncFirewalldFromProto renders the merged ruleset for each target zone into
// /etc/firewalld/zones, validates with --check-config, and reloads. When
// entries is empty it restores every Bor-managed zone file from backup. The
// zonesDir parameter is the zones directory (defaults to FirewalldZonesDir when
// empty) so tests can use a temp dir.
func SyncFirewalldFromProto(entries []FirewalldEntry, zonesDir string) FirewalldSyncResult {
	if zonesDir == "" {
		zonesDir = FirewalldZonesDir
	}

	if !FirewalldAvailable() {
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "firewalld is not installed on this node"}
	}

	// Empty desired state → restore all previously managed zone files.
	if len(entries) == 0 {
		if err := restoreManagedZones(zonesDir); err != nil {
			return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
		}
		if FirewalldActive() {
			_ = firewalldReload()
		}
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "no firewalld policies; restored defaults"}
	}

	if !FirewalldActive() {
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, "firewalld is installed but not running"}
	}

	defaultZone, err := FirewalldDefaultZone()
	if err != nil {
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
	}

	byZone := mergeFirewalldEntries(entries, defaultZone)

	// Restore any previously managed zone that is no longer targeted (stale).
	if err := restoreUnusedZones(zonesDir, byZone); err != nil {
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
	}

	for zone, d := range byZone {
		if err := ValidateFirewalldZoneName(zone); err != nil {
			return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
		}
		zonePath := filepath.Join(zonesDir, zone+".xml")
		existing, _ := os.ReadFile(zonePath) //nolint:gosec // path built from validated zone name under a constant dir
		xmlData, renderErr := RenderZoneXML(existing, d)
		if renderErr != nil {
			return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, renderErr.Error()}
		}
		if err := BackupOriginal(zonePath); err != nil {
			return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
		}
		if err := WriteFileAtomically(zonePath, xmlData); err != nil {
			return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
		}
	}

	// Validate before reloading — never apply a config that fails validation.
	if err := firewalldCheckConfig(); err != nil {
		// Restore the zones we just wrote so a bad render can't break the firewall.
		_ = restoreManagedZones(zonesDir)
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
	}
	if err := firewalldReload(); err != nil {
		return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, err.Error()}
	}

	return FirewalldSyncResult{pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"}
}

// ListBorManagedFirewalldFiles returns the absolute paths of zone files in
// zonesDir that Bor currently manages, identified by the presence of a
// .bor-backup sentinel alongside them.
func ListBorManagedFirewalldFiles(zonesDir string) ([]string, error) {
	if zonesDir == "" {
		zonesDir = FirewalldZonesDir
	}
	managed, err := ManagedFiles(zonesDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, name := range managed {
		if strings.HasSuffix(name, ".xml") {
			paths = append(paths, filepath.Join(zonesDir, name))
		}
	}
	return paths, nil
}

// restoreManagedZones restores every Bor-managed zone file from its backup.
func restoreManagedZones(zonesDir string) error {
	paths, err := ListBorManagedFirewalldFiles(zonesDir)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err := RestoreOriginal(p); err != nil {
			return err
		}
	}
	return nil
}

// restoreUnusedZones restores managed zone files that are no longer targeted by
// any active policy (e.g. a policy's zone changed or it was deleted).
func restoreUnusedZones(zonesDir string, wanted map[string]*firewalldDesired) error {
	paths, err := ListBorManagedFirewalldFiles(zonesDir)
	if err != nil {
		return err
	}
	for _, p := range paths {
		zone := strings.TrimSuffix(filepath.Base(p), ".xml")
		if _, ok := wanted[zone]; !ok {
			if err := RestoreOriginal(p); err != nil {
				return err
			}
		}
	}
	return nil
}
