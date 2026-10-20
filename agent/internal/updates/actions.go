// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package updates

// RefreshCommand is the command that refreshes the package metadata on
// the node, or "" when the engine has none.
func RefreshCommand(p *Platform) (bin string, args []string) {
	switch p.Engine {
	case EngineDnf:
		if p.DnfBin != "" {
			return p.DnfBin, []string{"makecache", "--refresh"}
		}
	case EngineApt:
		if p.AptGetBin != "" {
			return p.AptGetBin, []string{"update"}
		}
	case EngineOsUpdate, EngineTransactional:
		if p.ZypperBin != "" {
			return p.ZypperBin, []string{"--non-interactive", "refresh"}
		}
	case EngineRpmOstree:
		if p.RpmOstreeBin != "" {
			return p.RpmOstreeBin, []string{"upgrade", "--check"}
		}
	}
	return "", nil
}

// EngineRunCommand is the command that runs the engine once now with the
// node's configuration, the way its timer would. reason says why it
// cannot run when bin is empty.
func EngineRunCommand(p *Platform, mode Mode) (bin string, args []string, reason string) {
	switch mode {
	case ModeKeep:
		return "", nil, "no policy sets a mode on this node"
	case ModeOff:
		return "", nil, "the policy switches unattended updates off"
	case ModeCheck:
		if p.Engine != EngineRpmOstree {
			return "", nil, "the policy's mode installs nothing (check only)"
		}
	}
	if !p.Installed || p.EngineRunBin == "" {
		return "", nil, "the update engine is not installed"
	}
	switch p.Engine {
	case EngineDnf:
		if p.DNF5 {
			return p.EngineRunBin, []string{"automatic"}, ""
		}
		return p.EngineRunBin, []string{"/etc/dnf/automatic.conf"}, ""
	case EngineApt:
		return p.EngineRunBin, []string{"-v"}, ""
	case EngineOsUpdate:
		return p.EngineRunBin, nil, ""
	case EngineTransactional:
		return p.EngineRunBin, []string{"--non-interactive", "--quiet", TransactionalMethod(mode, p.OSID, p.Variant)}, ""
	case EngineRpmOstree:
		if mode == ModeCheck {
			return p.EngineRunBin, []string{"upgrade", "--check"}, ""
		}
		return p.EngineRunBin, []string{"upgrade"}, ""
	}
	return "", nil, "no update engine on this node"
}
