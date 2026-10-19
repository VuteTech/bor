// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

func TestSanitizeSystemdUnitInfoSecurity(t *testing.T) {
	tests := []struct {
		name          string
		in            *pb.SystemdUnitInfo
		wantExposure  string
		wantPredicate string
	}{
		{"service score kept", &pb.SystemdUnitInfo{Name: "cups.service", SecurityExposure: "9.6", SecurityPredicate: "UNSAFE"}, "9.6", "UNSAFE"},
		{"integer exposure kept", &pb.SystemdUnitInfo{Name: "cups.service", SecurityExposure: "10", SecurityPredicate: "UNSAFE"}, "10", "UNSAFE"},
		{"malformed exposure dropped", &pb.SystemdUnitInfo{Name: "cups.service", SecurityExposure: "9.6; rm -rf", SecurityPredicate: "UNSAFE"}, "", ""},
		{"lower-case predicate dropped", &pb.SystemdUnitInfo{Name: "cups.service", SecurityExposure: "1.2", SecurityPredicate: "safe"}, "", ""},
		{"timer never scored", &pb.SystemdUnitInfo{Name: "fstrim.timer", SecurityExposure: "1.2", SecurityPredicate: "OK"}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sanitizeSystemdUnitInfo(tc.in)
			if !ok {
				t.Fatal("unit dropped")
			}
			if got.GetSecurityExposure() != tc.wantExposure || got.GetSecurityPredicate() != tc.wantPredicate {
				t.Errorf("got %q/%q, want %q/%q", got.GetSecurityExposure(), got.GetSecurityPredicate(), tc.wantExposure, tc.wantPredicate)
			}
		})
	}
}

func TestTamperResourcePattern(t *testing.T) {
	for _, ok := range []string{"unit:bluetooth.service", "unit:bor-cleanup.timer", "unit:dbus-:1.2@x.service"} {
		if !tamperResourcePattern.MatchString(ok) {
			t.Errorf("%q must match", ok)
		}
	}
	for _, bad := range []string{"bluetooth.service", "unit:", "UNIT:x.service", "unit:../x", "unit:x y.service"} {
		if tamperResourcePattern.MatchString(bad) {
			t.Errorf("%q must not match", bad)
		}
	}
}
