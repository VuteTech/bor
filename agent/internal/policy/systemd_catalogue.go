// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/VuteTech/Bor/agent/internal/systemd"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
)

// MaxSystemdCatalogueUnits caps the units one node reports.
const MaxSystemdCatalogueUnits = 4000

// ScanSystemdUnits builds the unit catalogue of the node: every installed
// unit file of a managed type, with the description, schedule and
// activation target read from the file and the live states from the
// loaded units. Units are not loaded into systemd for the scan.
func ScanSystemdUnits(ctx context.Context, mgr systemd.Manager) ([]*pb.SystemdUnitInfo, error) {
	files, err := mgr.ListUnitFiles(ctx, systemd.UnitPatterns())
	if err != nil {
		return nil, err
	}
	loaded, err := mgr.ListUnits(ctx, systemd.UnitPatterns())
	if err != nil {
		return nil, err
	}
	live := make(map[string]*systemd.UnitStatus, len(loaded))
	for i := range loaded {
		live[loaded[i].Name] = &loaded[i]
	}

	byName := make(map[string]*pb.SystemdUnitInfo, len(files))
	for _, f := range files {
		name := filepath.Base(f.Path)
		if systemdunits.ValidateName(name) != nil {
			continue
		}
		if _, dup := byName[name]; dup {
			continue
		}
		info := systemd.UnitFileInfo{}
		if f.State != "masked" && f.State != "masked-runtime" {
			info = systemd.ScanUnitFile(f.Path)
		}
		typ := systemdunits.Type(name)
		triggers := info.Triggers
		if triggers == "" && (typ == "timer" || typ == "socket" || typ == "path") {
			triggers = strings.TrimSuffix(name, "."+typ) + ".service"
		}
		if triggers != "" && systemdunits.ValidateName(triggers) != nil {
			triggers = ""
		}
		entry := &pb.SystemdUnitInfo{
			Name:          name,
			Description:   info.Description,
			Type:          typ,
			UnitFileState: f.State,
			HasInstall:    info.HasInstall,
			OnCalendar:    info.OnCalendar,
			Triggers:      triggers,
			FragmentPath:  f.Path,
			ServiceType:   info.ServiceType,
		}
		if st, ok := live[name]; ok {
			entry.ActiveState = st.ActiveState
			entry.SubState = st.SubState
			if entry.Description == "" {
				entry.Description = st.Description
			}
		}
		byName[name] = entry
	}

	out := make([]*pb.SystemdUnitInfo, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
	if len(out) > MaxSystemdCatalogueUnits {
		out = out[:MaxSystemdCatalogueUnits]
	}
	return out, nil
}
