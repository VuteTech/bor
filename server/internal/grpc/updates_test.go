// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"strings"
	"testing"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSanitizeUpdatesInventory(t *testing.T) {
	in := &pb.UpdatesInventory{
		Engine: "weird", EngineInstalled: true, EngineManagedBy: "hacker", EffectiveMode: "security",
		PendingTotal: -3, PendingSecurity: 2,
		LastRun:       &pb.UpdatesRun{Time: timestamppb.Now(), Success: true, Packages: -1, Message: "ok\x00\x1f" + strings.Repeat("x", 2000)},
		Reboot:        &pb.UpdatesRebootState{Required: true, Reasons: []string{"kernel\n7", "", "a", "b", "c", "d", "e", "f", "g", "h"}},
		RunningKernel: "7.2\x07", ForeignMechanisms: []string{"packagekit-background.timer"},
		PendingFirmware: -2, OldestSecuritySince: timestamppb.Now(),
	}
	for i := 0; i < 250; i++ {
		in.Pending = append(in.Pending, &pb.UpdatesPending{Name: "pkg" + strings.Repeat("a", i%3), Version: "1", Advisory: "FEDORA-1", Severity: "huge", Security: true, Kind: "odd",
			Cves: []string{"CVE-2026-1234", "not-a-cve", "CVE-1-2"}})
	}
	out := sanitizeUpdatesInventory(in)
	if out.Engine != "none" || out.EngineManagedBy != "" || out.EffectiveMode != "security" {
		t.Errorf("words not sanitised: %+v", out)
	}
	if out.PendingTotal != 0 || out.PendingSecurity != 2 || out.LastRun.Packages != 0 || out.PendingFirmware != 0 {
		t.Errorf("negative counts must clamp: %+v", out)
	}
	if out.OldestSecuritySince == nil {
		t.Error("oldest security timestamp must pass through")
	}
	if strings.ContainsAny(out.LastRun.Message, "\x00\x1f") || len(out.LastRun.Message) > maxUpdatesMessage {
		t.Error("message not cleaned")
	}
	if len(out.Pending) != maxReportedUpdatesPending {
		t.Errorf("pending list not capped: %d", len(out.Pending))
	}
	p := out.Pending[0]
	if p.Severity != "" || p.Kind != "package" || len(p.Cves) != 1 || p.Cves[0] != "CVE-2026-1234" {
		t.Errorf("pending item not sanitised: %+v", p)
	}
	if len(out.Reboot.Reasons) != maxReportedUpdatesReasons || out.Reboot.Reasons[0] != "kernel7" {
		t.Errorf("reasons not bounded or cleaned: %v", out.Reboot.Reasons)
	}
	if out.RunningKernel != "7.2" {
		t.Errorf("kernel not cleaned: %q", out.RunningKernel)
	}
	if nilOut := sanitizeUpdatesInventory(nil); nilOut.Engine != "none" {
		t.Error("nil inventory must yield engine none")
	}
}
