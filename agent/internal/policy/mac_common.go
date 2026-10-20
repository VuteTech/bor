// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// MacDrift is a mandatory access control state a reconcile pass found
// changed by hand and corrected. It is reported as a tamper event with
// the resource "<kind>:<name>".
type MacDrift struct {
	Resource    string
	Description string
}

// macHeader is the first line of every file Bor writes for the two types.
const macHeader = "Managed by Bor - changes are overwritten"

func macHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// macItem builds one compliance item.
func macItem(schema, key string, status pb.ComplianceStatus, msg string) *pb.ComplianceItemResult {
	return &pb.ComplianceItemResult{SchemaId: schema, Key: key, Status: status, Message: msg}
}

// rollupMacItems derives the status and message of a policy from its
// items: inapplicable only when every item is, otherwise error, then
// non-compliant, then compliant.
func rollupMacItems(items []*pb.ComplianceItemResult, noneMsg, inapplicableMsg string) (status pb.ComplianceStatus, message string) {
	if len(items) == 0 {
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, noneMsg
	}
	allInapplicable := true
	var msgs []string
	var hasError, hasNonCompliant bool
	for _, it := range items {
		switch it.GetStatus() {
		case pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE:
		case pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR:
			allInapplicable = false
			hasError = true
			msgs = append(msgs, it.GetKey()+": "+it.GetMessage())
		case pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT:
			allInapplicable = false
			hasNonCompliant = true
			msgs = append(msgs, it.GetKey()+": "+it.GetMessage())
		default:
			allInapplicable = false
		}
	}
	switch {
	case allInapplicable:
		msg := inapplicableMsg
		for _, it := range items {
			if it.GetMessage() != "" {
				msg = it.GetMessage()
				break
			}
		}
		return pb.ComplianceStatus_COMPLIANCE_STATUS_INAPPLICABLE, msg
	case hasError:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_ERROR, strings.Join(msgs, "; ")
	case hasNonCompliant:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_NON_COMPLIANT, strings.Join(msgs, "; ")
	default:
		return pb.ComplianceStatus_COMPLIANCE_STATUS_COMPLIANT, "Deployed"
	}
}

// macSource identifies the policy a merged value came from.
type macSource struct {
	ID       string
	Name     string
	Priority int32
}

func (s macSource) label() string {
	return fmt.Sprintf("%q (priority %d)", s.Name, s.Priority)
}

// conflictMessage describes an equal-priority conflict.
func conflictMessage(a, b macSource) string {
	return fmt.Sprintf("conflicting values from policies %q and %q at the same priority %d; raise one binding's priority", a.Name, b.Name, a.Priority)
}

// sortedKeys returns map keys in order.
// clampInt32 converts an int to int32 with explicit bounds, for counts
// and small values read from the system that end up in proto fields.
func clampInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
