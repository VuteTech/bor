// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package logind

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func propsChanged(changed map[string]dbus.Variant, invalidated []string) *dbus.Signal {
	return &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Body: []any{"org.freedesktop.login1.Session", changed, invalidated},
	}
}

func TestSignalTouchesLockedHint(t *testing.T) {
	cases := []struct {
		name string
		sig  *dbus.Signal
		want bool
	}{
		{"locked hint changed", propsChanged(map[string]dbus.Variant{"LockedHint": dbus.MakeVariant(false)}, nil), true},
		{"locked hint invalidated", propsChanged(nil, []string{"LockedHint"}), true},
		{"unrelated change (IdleHint)", propsChanged(map[string]dbus.Variant{"IdleHint": dbus.MakeVariant(true)}, nil), false},
		{"empty", propsChanged(nil, nil), false},
		{"nil signal", nil, false},
		{"short body", &dbus.Signal{Body: []any{"iface"}}, false},
	}
	for _, c := range cases {
		if got := signalTouchesLockedHint(c.sig); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
