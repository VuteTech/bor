// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

const polkitManagedHeader = `// This file is managed by Bor. Do not edit manually.
// Changes will be overwritten by policy enforcement.
// Generated: %s
`

// PolkitRulesDir is the directory polkitd monitors for JavaScript rules files.
const PolkitRulesDir = "/etc/polkit-1/rules.d"

// polkitManagedPrefix is the byte prefix used to identify Bor-managed rules files.
var polkitManagedPrefix = []byte("// This file is managed by Bor.")

// PolkitRuleResult is the compliance result for a single polkit rule.
type PolkitRuleResult struct {
	RuleIndex   int
	Description string
	Status      pb.ComplianceStatus
	Message     string
}

// MergePolkitPolicies merges multiple PolkitPolicy values into one.
// Policies must be provided sorted DESCENDING by binding priority
// (highest priority first) so their rules take precedence in polkit's
// first-match-wins evaluation.
func MergePolkitPolicies(policies []*pb.PolkitPolicy) *pb.PolkitPolicy {
	var allRules []*pb.PolkitRule
	filePrefix := "50"

	for _, pol := range policies {
		if pol == nil {
			continue
		}
		// Use the file_prefix from the highest-priority policy (first non-empty).
		if filePrefix == "50" && pol.GetFilePrefix() != "" {
			filePrefix = pol.GetFilePrefix()
		}
		allRules = append(allRules, pol.GetRules()...)
	}

	return &pb.PolkitPolicy{
		Rules:      allRules,
		FilePrefix: filePrefix,
	}
}

// PolkitPoliciesToJS converts a PolkitPolicy to the content of a
// .rules JavaScript file suitable for /etc/polkit-1/rules.d/.
func PolkitPoliciesToJS(pol *pb.PolkitPolicy) ([]byte, error) {
	if pol == nil {
		return nil, fmt.Errorf("polkit: nil policy")
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, polkitManagedHeader, time.Now().UTC().Format(time.RFC3339))

	for _, rule := range pol.GetRules() {
		if err := writeRuleJS(&buf, rule); err != nil {
			return nil, fmt.Errorf("polkit: write rule %q: %w", rule.GetDescription(), err)
		}
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// writeRuleJS writes a single polkit.addRule(...) call to buf.
func writeRuleJS(buf *bytes.Buffer, rule *pb.PolkitRule) error {
	if len(rule.GetActionIds()) == 0 && len(rule.GetActionPrefixes()) == 0 {
		return fmt.Errorf("rule %q has no action_ids or action_prefixes", rule.GetDescription())
	}
	if rule.GetResult() == pb.PolkitResult_POLKIT_RESULT_NOT_SET {
		return fmt.Errorf("rule %q has result NOT_SET", rule.GetDescription())
	}

	if rule.GetDescription() != "" {
		fmt.Fprintf(buf, "// %s\n", rule.GetDescription())
	}

	buf.WriteString("polkit.addRule(function(action, subject) {\n")
	buf.WriteString("  if (\n")

	// Build action conditions.
	var actionConds []string
	for _, id := range rule.GetActionIds() {
		actionConds = append(actionConds, fmt.Sprintf("    action.id === %q", id))
	}
	for _, prefix := range rule.GetActionPrefixes() {
		actionConds = append(actionConds, fmt.Sprintf("    action.id.indexOf(%q) === 0", prefix))
	}

	// Build subject conditions.
	var subjectConds []string
	if subj := rule.GetSubject(); subj != nil {
		if subj.GetInGroup() != "" {
			if subj.GetNegateGroup() {
				subjectConds = append(subjectConds, fmt.Sprintf("    !subject.isInGroup(%q)", subj.GetInGroup()))
			} else {
				subjectConds = append(subjectConds, fmt.Sprintf("    subject.isInGroup(%q)", subj.GetInGroup()))
			}
		}
		if subj.GetIsUser() != "" {
			subjectConds = append(subjectConds, fmt.Sprintf("    subject.user === %q", subj.GetIsUser()))
		}
		if subj.GetRequireLocal() {
			subjectConds = append(subjectConds, "    subject.local === true")
		}
		if subj.GetRequireActive() {
			subjectConds = append(subjectConds, "    subject.active === true")
		}
		if subj.GetSystemUnit() != "" {
			subjectConds = append(subjectConds, fmt.Sprintf("    subject.system_unit === %q", subj.GetSystemUnit()))
		}
	}

	allConds := make([]string, 0, len(actionConds)+len(subjectConds))
	allConds = append(allConds, actionConds...)
	allConds = append(allConds, subjectConds...)
	buf.WriteString(strings.Join(allConds, " &&\n"))
	buf.WriteString("\n  ) {\n")
	fmt.Fprintf(buf, "    return polkit.Result.%s;\n", polkitResultJS(rule.GetResult()))
	buf.WriteString("  }\n")
	buf.WriteString("});\n")

	return nil
}

// polkitResultJS converts a PolkitResult enum to its JS string.
func polkitResultJS(r pb.PolkitResult) string {
	switch r {
	case pb.PolkitResult_POLKIT_RESULT_YES:
		return "YES"
	case pb.PolkitResult_POLKIT_RESULT_NO:
		return "NO"
	case pb.PolkitResult_POLKIT_RESULT_AUTH_SELF:
		return "AUTH_SELF"
	case pb.PolkitResult_POLKIT_RESULT_AUTH_SELF_KEEP:
		return "AUTH_SELF_KEEP"
	case pb.PolkitResult_POLKIT_RESULT_AUTH_ADMIN:
		return "AUTH_ADMIN"
	case pb.PolkitResult_POLKIT_RESULT_AUTH_ADMIN_KEEP:
		return "AUTH_ADMIN_KEEP"
	default:
		return "NOT_SET"
	}
}

// PolkitRulesPath returns the absolute path for a policy's managed rules file.
// The filename is <priority>-bor-<shortID>.rules where shortID is the first
// 8 hex characters of the policy UUID (without dashes). The priority is
// zero-padded to three digits so that alphabetical filename order matches
// numeric order: polkitd evaluates files in alphabetical order, so a lower
// number is evaluated first and wins in first-match-wins evaluation.
func PolkitRulesPath(priority int32, policyID string) string {
	shortID := strings.ReplaceAll(policyID, "-", "")
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return filepath.Join(PolkitRulesDir, fmt.Sprintf("%03d-bor-%s.rules", priority, shortID))
}

// SyncPolkitRules atomically writes the polkit rules file at rulesPath.
// polkitd monitors PolkitRulesDir via inotify and hot-reloads automatically —
// no explicit reload command is needed.
func SyncPolkitRules(rulesPath string, js []byte) error {
	// Ensure the directory exists.
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0o755); err != nil { //nolint:gosec // G301: polkit rules directory must be world-readable
		return fmt.Errorf("polkit: create rules dir: %w", err)
	}

	// Write atomically: write to a temp file then rename.
	dir := filepath.Dir(rulesPath)
	tmp, err := os.CreateTemp(dir, ".bor-polkit-*.rules")
	if err != nil {
		return fmt.Errorf("polkit: create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(js); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("polkit: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("polkit: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil { //nolint:gosec // G302: polkit rules files must be world-readable for polkitd
		_ = os.Remove(tmpPath)
		return fmt.Errorf("polkit: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, rulesPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("polkit: rename rules file: %w", err)
	}

	return nil
}

// RemovePolkitRules removes the polkit rules file at rulesPath if it exists.
// Called when a policy is deleted or its binding priority changes (leaving
// behind a stale file at the old priority-derived path).
func RemovePolkitRules(rulesPath string) error {
	if err := os.Remove(rulesPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("polkit: remove rules file: %w", err)
	}
	return nil
}

// ListBorManagedPolkitFiles returns the absolute paths of all polkit rules
// files in PolkitRulesDir that were written by Bor. Bor-managed files are
// identified by the managed header comment on the first line.
func ListBorManagedPolkitFiles() ([]string, error) {
	entries, err := os.ReadDir(PolkitRulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("polkit: read rules dir: %w", err)
	}

	var managed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".rules") {
			continue
		}
		path := filepath.Join(PolkitRulesDir, entry.Name())
		f, err := os.Open(path) //nolint:gosec // G304: path constructed from constant PolkitRulesDir + directory listing
		if err != nil {
			continue
		}
		buf := make([]byte, len(polkitManagedPrefix))
		n, _ := f.Read(buf)
		_ = f.Close()
		if bytes.HasPrefix(buf[:n], polkitManagedPrefix) {
			managed = append(managed, path)
		}
	}
	return managed, nil
}

// CheckPolkitCompliance checks whether the managed rules file has the
// expected content by comparing it to the generated JS byte-for-byte.
// Returns one result per rule in pol.Rules.
func CheckPolkitCompliance(pol *pb.PolkitPolicy, rulesPath string) []PolkitRuleResult {
	rules := pol.GetRules()
	results := make([]PolkitRuleResult, 0, len(rules))

	// Generate expected content.
	expected, genErr := PolkitPoliciesToJS(pol)
	if genErr != nil {
		// If we can't generate, mark all rules as error.
		for i, r := range rules {
			results = append(results, PolkitRuleResult{
				RuleIndex:   i,
				Description: r.GetDescription(),
				Status:      pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR,
				Message:     fmt.Sprintf("failed to generate rules JS: %v", genErr),
			})
		}
		return results
	}

	actual, readErr := os.ReadFile(rulesPath) //nolint:gosec // G304: rulesPath is derived from trusted policy config
	if readErr != nil {
		// File missing or unreadable — all rules non-compliant.
		msg := fmt.Sprintf("rules file missing or unreadable at %s", rulesPath)
		for i, r := range rules {
			results = append(results, PolkitRuleResult{
				RuleIndex:   i,
				Description: r.GetDescription(),
				Status:      pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message:     msg,
			})
		}
		return results
	}

	if bytes.Equal(actual, expected) {
		// Perfect match — all rules compliant.
		for i, r := range rules {
			results = append(results, PolkitRuleResult{
				RuleIndex:   i,
				Description: r.GetDescription(),
				Status:      pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			})
		}
		return results
	}

	// Content differs — check each rule block individually.
	actualStr := string(actual)
	for i, rule := range rules {
		var singleBuf bytes.Buffer
		_ = writeRuleJS(&singleBuf, rule)
		block := singleBuf.String()

		if strings.Contains(actualStr, block) {
			results = append(results, PolkitRuleResult{
				RuleIndex:   i,
				Description: rule.GetDescription(),
				Status:      pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT,
			})
		} else {
			results = append(results, PolkitRuleResult{
				RuleIndex:   i,
				Description: rule.GetDescription(),
				Status:      pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT,
				Message:     fmt.Sprintf("rule block missing or modified in %s", rulesPath),
			})
		}
	}

	return results
}

// RollupPolkitCompliance derives an overall status from per-rule results.
func RollupPolkitCompliance(results []PolkitRuleResult) (status pb.ComplianceStatus, message string) {
	if len(results) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_UNKNOWN, ""
	}

	var hasError, hasNonCompliant bool
	var msgs []string

	for _, r := range results {
		switch r.Status {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			hasError = true
			msgs = append(msgs, fmt.Sprintf("rule[%d] %q: %s", r.RuleIndex, r.Description, r.Message))
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			hasNonCompliant = true
			msgs = append(msgs, fmt.Sprintf("rule[%d] %q: %s", r.RuleIndex, r.Description, r.Message))
		}
	}

	switch {
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, ""
	}
}
