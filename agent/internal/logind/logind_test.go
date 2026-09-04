// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package logind

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParseShowSession(t *testing.T) {
	// Real loginctl show-session output for two sessions.
	out := `Id=772
Name=alice
User=1000
Seat=seat0
Type=x11
Class=user
Active=yes
LockedHint=yes

Id=773
Name=alice
User=1000
Seat=
Type=
Class=manager
Active=no
LockedHint=no
`
	got := parseShowSession(out)
	want := []Session{
		{ID: "772", Name: "alice", UID: 1000, Seat: "seat0", Type: "x11", Class: "user", Active: true, LockedHint: true},
		{ID: "773", Name: "alice", UID: 1000, Seat: "", Type: "", Class: "manager", Active: false, LockedHint: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseShowSession =\n%#v\nwant\n%#v", got, want)
	}
	if !got[0].IsGraphicalUser() {
		t.Error("session 772 should be a graphical user session")
	}
	if got[1].IsGraphicalUser() {
		t.Error("manager session 773 must not be graphical user")
	}
}

func TestListSessions_Integration(t *testing.T) {
	var shown []string
	c := &CLI{run: func(args ...string) (string, error) {
		switch args[0] {
		case "list-sessions":
			// The manager-class session must be filtered out before any show.
			return "772 1000 alice seat0 35654 user    tty2 yes 11min ago\n" +
				"773 1000 alice -     35664 manager -    no  -\n" +
				"780 1000 alice -     36000 user    -    no  -\n", nil
		case "show-session":
			shown = append(shown, args[1])
			switch args[1] {
			case "772":
				return "Id=772\nName=alice\nUser=1000\nSeat=seat0\nType=wayland\nClass=user\nActive=yes\nLockedHint=no\n", nil
			case "780":
				return "Id=780\nName=alice\nUser=1000\nSeat=\nType=tty\nClass=user\nActive=yes\nLockedHint=no\n", nil
			}
		}
		return "", fmt.Errorf("unexpected call %v", args)
	}}
	got, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shown, []string{"772", "780"}) {
		t.Errorf("show-session ids = %v, want only user-class [772 780]", shown)
	}
	if len(got) != 2 || got[0].ID != "772" || !got[0].Active || got[0].Type != "wayland" {
		t.Fatalf("unexpected sessions: %#v", got)
	}
}

func TestListSessions_VanishedSessionIsSkipped(t *testing.T) {
	// A session that ends between list and show must not fail the whole
	// pass — that gap is exactly what let the watchdog go blind.
	c := &CLI{run: func(args ...string) (string, error) {
		switch args[0] {
		case "list-sessions":
			return "772 1000 alice seat0 35654 user tty2 yes -\n" +
				"799 1000 alice -     36999 user -    no  -\n", nil
		case "show-session":
			if args[1] == "799" {
				return "", fmt.Errorf("Failed to get path for session '799': No session '799' known")
			}
			return "Id=772\nName=alice\nUser=1000\nSeat=seat0\nType=x11\nClass=user\nActive=yes\nLockedHint=yes\n", nil
		}
		return "", fmt.Errorf("unexpected call %v", args)
	}}
	got, err := c.ListSessions()
	if err != nil {
		t.Fatalf("a vanished session must be skipped, not returned as an error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "772" || !got[0].LockedHint {
		t.Fatalf("expected only the surviving session 772, got %#v", got)
	}
}

func TestListUserSessionIDs_OldLoginctlWithoutClassColumn(t *testing.T) {
	// Older loginctl prints SESSION UID USER SEAT TTY only — no CLASS to
	// filter on, so every id is returned.
	c := &CLI{run: func(_ ...string) (string, error) {
		return "2 1000 alice seat0 tty2\n3 1000 alice - -\n", nil
	}}
	ids, err := c.listUserSessionIDs()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"2", "3"}) {
		t.Errorf("ids = %v, want [2 3]", ids)
	}
}

func TestListSessions_Empty(t *testing.T) {
	c := &CLI{run: func(args ...string) (string, error) {
		if args[0] == "list-sessions" {
			return "\n", nil
		}
		t.Fatalf("show-session must not be called when there are no sessions: %v", args)
		return "", nil
	}}
	got, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil sessions, got %#v", got)
	}
}

func TestLockTerminate(t *testing.T) {
	var got [][]string
	c := &CLI{run: func(args ...string) (string, error) {
		got = append(got, args)
		return "", nil
	}}
	if err := c.Lock("772"); err != nil {
		t.Fatal(err)
	}
	if err := c.Terminate("773"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"lock-session", "772"}, {"terminate-session", "773"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}
