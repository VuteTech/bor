// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package mac

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Record is one audit record of either LSM, as read from the journal.
type Record struct {
	Time time.Time
	// Kind is "selinux" or "apparmor".
	Kind string
	// Type is the audit type name: AVC, USER_AVC, or for AppArmor the
	// value of apparmor= (DENIED, ALLOWED, AUDIT, STATUS).
	Type string
	// Fields are the key=value pairs of the record body.
	Fields map[string]string
	// Perms are the braced SELinux permissions ("{ read open }").
	Perms []string
	// Raw is the record in the "type=AVC msg=audit(...): ..." form audit
	// tools accept.
	Raw string
}

// Collector reads audit records from the journal. The argv is constant
// apart from the time window.
type Collector struct {
	// Binary is the journalctl executable (default "journalctl").
	Binary string
	// MaxLines bounds how many journal entries are read (default 20000).
	MaxLines int
	// Run executes the command; injected so tests can stub it.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewCollector returns a Collector that executes journalctl.
func NewCollector() *Collector {
	return &Collector{
		Binary:   "journalctl",
		MaxLines: 20000,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: constant binary and flags; the only variable is a formatted timestamp
			var out bytes.Buffer
			cmd.Stdout = &out
			err := cmd.Run()
			return out.Bytes(), err
		},
	}
}

// Available reports whether journalctl can be found.
func (c *Collector) Available() bool {
	if c == nil {
		return false
	}
	_, err := exec.LookPath(c.Binary)
	return err == nil
}

// Records returns the LSM records logged since the given time, from the
// audit transport (auditd or journald's audit socket) and the kernel log
// (where they land without either).
func (c *Collector) Records(ctx context.Context, since time.Time) ([]Record, error) {
	if !c.Available() {
		return nil, fmt.Errorf("journalctl is not installed")
	}
	args := []string{
		"--no-pager", "-q", "-o", "json",
		"--since=" + since.UTC().Format("2006-01-02 15:04:05 UTC"),
		"-n", strconv.Itoa(c.MaxLines),
		"_TRANSPORT=audit", "+", "_TRANSPORT=kernel",
	}
	out, err := c.Run(ctx, c.Binary, args...)
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("journalctl: %w", err)
	}
	return ParseJournalJSON(out), nil
}

// journalEntry is the subset of journalctl -o json fields used here.
// MESSAGE may be a byte array for non-UTF-8 entries; those are skipped.
type journalEntry struct {
	Message       json.RawMessage `json:"MESSAGE"`
	Transport     string          `json:"_TRANSPORT"`
	AuditTypeName string          `json:"_AUDIT_TYPE_NAME"`
	AuditID       string          `json:"_AUDIT_ID"`
	Realtime      string          `json:"__REALTIME_TIMESTAMP"`
}

var kernelAuditRE = regexp.MustCompile(`^(?:audit: )?type=(\d+) audit\(([\d.]+):(\d+)\): (.*)$`)

// auditTypeNames maps the numeric types the kernel log carries.
var auditTypeNames = map[string]string{"1400": "AVC", "1107": "USER_AVC", "1404": "MAC_STATUS", "1405": "MAC_CONFIG_CHANGE", "1403": "MAC_POLICY_LOAD"}

// ParseJournalJSON parses journalctl -o json output into LSM records;
// entries that are not SELinux or AppArmor records are dropped.
func ParseJournalJSON(data []byte) []Record {
	var out []Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e journalEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		var msg string
		if err := json.Unmarshal(e.Message, &msg); err != nil {
			continue
		}
		var rec *Record
		switch e.Transport {
		case "audit":
			rec = parseAuditTransport(msg, e.AuditTypeName, e.AuditID, e.Realtime)
		default:
			rec = parseKernelLine(msg, e.Realtime)
		}
		if rec != nil {
			out = append(out, *rec)
		}
	}
	return out
}

func realtime(usec string) time.Time {
	n, err := strconv.ParseInt(usec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMicro(n)
}

// parseAuditTransport handles journald's audit socket form, where MESSAGE
// is "<TYPE> <body>" and the type name and serial are separate fields.
func parseAuditTransport(msg, typeName, id, rt string) *Record {
	body := msg
	if typeName == "" {
		typeName, body, _ = strings.Cut(msg, " ")
	} else if strings.HasPrefix(msg, typeName+" ") {
		body = msg[len(typeName)+1:]
	}
	t := realtime(rt)
	raw := fmt.Sprintf("type=%s msg=audit(%d.%03d:%s): %s", typeName, t.Unix(), t.Nanosecond()/1e6, id, body)
	return classify(typeName, body, raw, t)
}

// parseKernelLine handles the kernel log form "audit: type=1400
// audit(1792447101.241:765): ..." used without auditd.
func parseKernelLine(msg, rt string) *Record {
	m := kernelAuditRE.FindStringSubmatch(msg)
	if m == nil {
		return nil
	}
	typeName := auditTypeNames[m[1]]
	if typeName == "" {
		typeName = "UNKNOWN[" + m[1] + "]"
	}
	raw := fmt.Sprintf("type=%s msg=audit(%s:%s): %s", typeName, m[2], m[3], m[4])
	return classify(typeName, m[4], raw, realtime(rt))
}

// classify decides which LSM a record belongs to and parses its fields.
func classify(typeName, body, raw string, t time.Time) *Record {
	switch {
	case strings.Contains(body, `apparmor="`):
		fields := parseFields(body)
		return &Record{Time: t, Kind: "apparmor", Type: fields["apparmor"], Fields: fields, Raw: raw}
	case strings.Contains(body, "avc:") && (typeName == "AVC" || typeName == "USER_AVC"):
		perms := parsePerms(body)
		fields := parseFields(body)
		return &Record{Time: t, Kind: "selinux", Type: typeName, Fields: fields, Perms: perms, Raw: raw}
	case typeName == "MAC_STATUS" || typeName == "MAC_CONFIG_CHANGE" || typeName == "MAC_POLICY_LOAD":
		// SELinux state changes (setenforce, setsebool, semodule): never a
		// denial, but the audit follower reconciles on them and reads the
		// actor's login uid.
		return &Record{Time: t, Kind: "selinux", Type: typeName, Fields: parseFields(body), Raw: raw}
	}
	return nil
}

var permsRE = regexp.MustCompile(`\{([^}]*)\}`)

func parsePerms(body string) []string {
	m := permsRE.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	return strings.Fields(m[1])
}

// parseFields extracts key=value and key="value" pairs, descending into
// the msg='...' envelope of USER_AVC records.
func parseFields(body string) map[string]string {
	fields := make(map[string]string)
	s := body
	for s != "" {
		i := strings.IndexByte(s, '=')
		if i < 0 {
			break
		}
		// Key is the run of key characters right before '='.
		j := i - 1
		for j >= 0 && (isKeyChar(s[j])) {
			j--
		}
		key := s[j+1 : i]
		rest := s[i+1:]
		var val string
		switch {
		case key == "":
			s = rest
			continue
		case strings.HasPrefix(rest, "'"):
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				val, rest = rest[1:], ""
			} else {
				val, rest = rest[1:end+1], rest[end+2:]
			}
			// Nested envelope (USER_AVC msg='avc: denied ...').
			if key == "msg" {
				for k, v := range parseFields(val) {
					fields[k] = v
				}
				s = rest
				continue
			}
		case strings.HasPrefix(rest, "\""):
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				val, rest = rest[1:], ""
			} else {
				val, rest = rest[1:end+1], rest[end+2:]
			}
		default:
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				val, rest = rest, ""
			} else {
				val, rest = rest[:end], rest[end:]
			}
		}
		if _, dup := fields[key]; !dup {
			fields[key] = val
		}
		s = rest
	}
	return fields
}

func isKeyChar(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// contextType returns the type of an SELinux security context
// ("system_u:system_r:virt_qemu_ga_t:s0" -> "virt_qemu_ga_t").
func contextType(ctx string) string {
	parts := strings.Split(ctx, ":")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ctx
}

var (
	homeDirRE  = regexp.MustCompile(`/home/[^/\s"]+`)
	userFields = regexp.MustCompile(`\s(?:auid|sauid|uid|gid|euid|suid|fsuid|egid|sgid|fsgid|ouid|ses|AUID|UID|GID|EUID|SUID|FSUID|EGID|SGID|FSGID|OUID|hostname|addr|terminal)=\S+`)
)

// Redact removes the fields that identify a person from a record and
// collapses home directory names.
func Redact(s string) string {
	s = userFields.ReplaceAllString(s, "")
	return homeDirRE.ReplaceAllString(s, "/home/*")
}

// pathPrefix keeps the first three components of a path.
func pathPrefix(p string) string {
	if !strings.HasPrefix(p, "/") {
		return p
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return "/" + strings.Join(parts, "/")
}

// IsDenial reports whether a record counts for the denial summary: an
// SELinux denial, an AppArmor DENIED or ALLOWED record, or the AUDIT
// record of a user namespace transition.
func (r *Record) IsDenial() bool {
	switch r.Kind {
	case "selinux":
		return (r.Type == "AVC" || r.Type == "USER_AVC") && strings.Contains(r.Raw, "denied")
	case "apparmor":
		switch r.Type {
		case "DENIED", "ALLOWED":
			return true
		case "AUDIT":
			return r.Fields["operation"] == "userns_create"
		}
	}
	return false
}

// Subject returns the SELinux source type or the AppArmor profile.
func (r *Record) Subject() string {
	if r.Kind == "selinux" {
		return contextType(r.Fields["scontext"])
	}
	return r.Fields["profile"]
}

// Group aggregates denials by subject, operation, class and target, most
// frequent first, at most max groups.
func Group(records []Record, limit int) []*pb.MacDenialGroup {
	type agg struct {
		g     *pb.MacDenialGroup
		last  time.Time
		count int
	}
	groups := make(map[string]*agg)
	for i := range records {
		r := &records[i]
		if !r.IsDenial() {
			continue
		}
		g := &pb.MacDenialGroup{Subject: r.Subject()}
		if g.Subject == "" {
			continue
		}
		switch r.Kind {
		case "selinux":
			g.Operation = strings.Join(r.Perms, " ")
			g.Class = r.Fields["tclass"]
			g.Target = contextType(r.Fields["tcontext"])
			g.Permissive = r.Fields["permissive"] == "1"
		case "apparmor":
			g.Operation = r.Fields["operation"]
			g.Class = r.Fields["class"]
			g.Permissive = r.Type == "ALLOWED"
			switch {
			case r.Fields["capname"] != "":
				g.Target = r.Fields["capname"]
				if g.Class == "" {
					g.Class = "cap"
				}
			case r.Fields["name"] != "":
				g.Target = pathPrefix(homeDirRE.ReplaceAllString(r.Fields["name"], "/home/*"))
				if g.Class == "" {
					g.Class = "file"
				}
			case r.Fields["family"] != "":
				g.Target = r.Fields["family"] + " " + r.Fields["sock_type"]
				if g.Class == "" {
					g.Class = "net"
				}
			case r.Fields["peer"] != "":
				g.Target = r.Fields["peer"]
			}
			if g.Operation == "userns_create" {
				g.Execpath = r.Fields["execpath"]
				if g.Execpath == "" {
					g.Execpath = r.Fields["comm"]
				}
				g.Target = r.Fields["target"]
			}
		}
		key := strings.Join([]string{g.Subject, g.Operation, g.Class, g.Target, g.Execpath}, "\x00")
		a, ok := groups[key]
		if !ok {
			g.Example = Redact(r.Raw)
			a = &agg{g: g}
			groups[key] = a
		}
		a.count++
		if r.Time.After(a.last) {
			a.last = r.Time
		}
	}
	out := make([]*pb.MacDenialGroup, 0, len(groups))
	for _, a := range groups {
		a.g.Count = int32(min(a.count, 1<<30)) //nolint:gosec // G115: bounded
		if !a.last.IsZero() {
			a.g.LastSeen = timestamppb.New(a.last)
		}
		out = append(out, a.g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Subject+out[i].Operation < out[j].Subject+out[j].Operation
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ForSubject returns the denial records of one subject.
func ForSubject(records []Record, kind, subject string) []Record {
	var out []Record
	for i := range records {
		r := &records[i]
		if r.Kind == kind && r.IsDenial() && r.Subject() == subject {
			out = append(out, *r)
		}
	}
	return out
}

// AuditLines joins the raw records the way audit tools read them.
func AuditLines(records []Record) []byte {
	var b bytes.Buffer
	for _, r := range records {
		b.WriteString(r.Raw)
		b.WriteByte('\n')
	}
	return b.Bytes()
}
