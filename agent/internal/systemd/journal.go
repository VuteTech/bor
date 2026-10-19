// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/pkg/systemdunits"
)

// Journal reads the tail of a unit's journal for the compliance message of
// a failed task. The argv is constant apart from the unit name, which is
// accepted only when it is a Bor task unit.
type Journal struct {
	// Binary is the executable name or path (default "journalctl").
	Binary string
	// Lines is how many lines to read (default 20).
	Lines int
	// MaxBytes truncates the excerpt (default 2 KiB).
	MaxBytes int
	// Run executes the command; injected so tests can stub it.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultJournalBinary is the executable name resolved through PATH.
const DefaultJournalBinary = "journalctl"

// NewJournal returns a Journal that executes journalctl.
func NewJournal() *Journal {
	return &Journal{
		Binary:   DefaultJournalBinary,
		Lines:    20,
		MaxBytes: 2048,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // G204: constant binary and flags; the unit name is validated as a Bor task unit
		},
	}
}

// Excerpt returns the last lines a task unit logged, or "" when the unit
// name is not a Bor task unit, journalctl is missing, or nothing was
// logged. The result never exceeds MaxBytes.
func (j *Journal) Excerpt(ctx context.Context, unit string) string {
	if j == nil || !systemdunits.IsTaskUnit(unit) {
		return ""
	}
	bin := j.Binary
	if bin == "" {
		bin = DefaultJournalBinary
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	lines := j.Lines
	if lines <= 0 {
		lines = 20
	}
	limit := j.MaxBytes
	if limit <= 0 {
		limit = 2048
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := j.Run(ctx, bin, "-u", unit, "-n", itoa(lines), "-o", "cat", "--no-pager", "-q")
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ""
	}
	if len(text) > limit {
		text = text[len(text)-limit:]
		if i := strings.IndexByte(text, '\n'); i >= 0 && i < len(text)-1 {
			text = text[i+1:]
		}
		text = "..." + text
	}
	return text
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Actor is a journal record that attributes a unit change to a person: a
// sudo invocation that named the unit, or a polkit authorization for
// systemd's manage-units action that named it.
type Actor struct {
	PID  int32
	Comm string
	User string
}

// maxActors bounds the attribution attached to one drift event.
const maxActors = 5

// RecentActors returns, best effort, who changed a unit by hand within
// the window: sudo records whose command names the unit and polkit
// authorizations for org.freedesktop.systemd1.manage-units that name it.
// A root shell leaves no record, so an empty result means "not
// attributable", not "nobody".
func (j *Journal) RecentActors(ctx context.Context, unit string, window time.Duration) []Actor {
	if j == nil || systemdunits.ValidateName(unit) != nil {
		return nil
	}
	bin := j.Binary
	if bin == "" {
		bin = DefaultJournalBinary
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil
	}
	if window <= 0 {
		window = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	since := "-" + strconv.FormatInt(int64(window/time.Second), 10) + "s"
	out, err := j.Run(ctx, bin, "-o", "json", "--no-pager", "-q", "-n", "200", "--since="+since, "_COMM=sudo", "+", "_COMM=polkitd")
	if err != nil {
		return nil
	}
	return ParseJournalActors(out, unit)
}

// ParseJournalActors extracts the actors naming unit from journalctl's
// JSON lines.
func ParseJournalActors(out []byte, unit string) []Actor {
	var actors []Actor
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec struct {
			Message json.RawMessage `json:"MESSAGE"`
			PID     string          `json:"_PID"`
			Comm    string          `json:"_COMM"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		var msg string
		if err := json.Unmarshal(rec.Message, &msg); err != nil {
			continue // binary payloads are arrays of bytes; not ours
		}
		if !strings.Contains(msg, unit) {
			continue
		}
		user := ""
		switch rec.Comm {
		case "sudo":
			// "   alice : TTY=pts/0 ; PWD=/home/alice ; USER=root ; COMMAND=/usr/bin/systemctl stop x.service"
			if name, _, ok := strings.Cut(strings.TrimSpace(msg), " : "); ok && !strings.ContainsAny(name, " \t") {
				user = name
			}
		case "polkitd":
			// "... successfully authenticated as unix-user:alice to gain ... org.freedesktop.systemd1.manage-units ... [systemctl stop x.service] ..."
			if !strings.Contains(msg, "org.freedesktop.systemd1.") {
				continue
			}
			if _, rest, ok := strings.Cut(msg, "unix-user:"); ok {
				user, _, _ = strings.Cut(rest, " ")
			}
		default:
			continue
		}
		if user == "" {
			continue
		}
		pid64, _ := strconv.ParseInt(rec.PID, 10, 32)
		key := rec.Comm + "/" + user
		if seen[key] {
			continue
		}
		seen[key] = true
		actors = append(actors, Actor{PID: int32(pid64), Comm: rec.Comm, User: user})
		if len(actors) >= maxActors {
			break
		}
	}
	return actors
}
