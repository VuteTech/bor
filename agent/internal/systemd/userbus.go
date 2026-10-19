// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

// UserRunDir is where systemd keeps the runtime directories of users with
// a running manager; <uid>/bus is the user's bus socket.
const UserRunDir = "/run/user"

// DefaultStdioBridge is the executable that proxies a D-Bus connection,
// looked up in PATH.
const DefaultStdioBridge = "systemd-stdio-bridge"

// UserSession is a user whose manager runs: a bus socket exists under
// the user's runtime directory.
type UserSession struct {
	UID        uint32
	GID        uint32
	User       string
	RuntimeDir string
}

// String names the session for messages ("alice" or "uid 1000").
func (s UserSession) String() string {
	if s.User != "" {
		return s.User
	}
	return "uid " + strconv.FormatUint(uint64(s.UID), 10)
}

// ListUserSessions lists the users with a running manager by the bus
// sockets under runDir (UserRunDir), sorted by uid.
func ListUserSessions(runDir string) []UserSession {
	if runDir == "" {
		runDir = UserRunDir
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil
	}
	var out []UserSession
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		uid, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil {
			continue
		}
		dir := filepath.Join(runDir, e.Name())
		if st, err := os.Stat(filepath.Join(dir, "bus")); err != nil || st.Mode()&os.ModeSocket == 0 {
			continue
		}
		sess := UserSession{UID: uint32(uid), RuntimeDir: dir}
		if u, err := user.LookupId(e.Name()); err == nil {
			sess.User = u.Username
			if gid, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
				sess.GID = uint32(gid)
			}
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out
}

// bridgeConn is the stdio of a systemd-stdio-bridge process as one
// connection; closing it ends the process.
type bridgeConn struct {
	io.Reader
	io.WriteCloser
	cmd  *exec.Cmd
	once sync.Once
	err  error
}

func (c *bridgeConn) Close() error {
	c.once.Do(func() {
		c.err = c.WriteCloser.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
	})
	return c.err
}

// ConnectUser connects to a user's manager. Root cannot join another
// user's bus directly (the bus accepts its owner only), so the connection
// goes through systemd-stdio-bridge running as that user, the way
// "systemctl --user -M user@" works. bridge is the executable (empty for
// the default); the caller closes the Bus, which ends the bridge.
func ConnectUser(sess UserSession, bridge string, jobTimeout time.Duration) (*Bus, error) {
	if jobTimeout <= 0 {
		jobTimeout = DefaultJobTimeout
	}
	if bridge == "" {
		bridge = DefaultStdioBridge
	}
	path, err := exec.LookPath(bridge)
	if err != nil {
		return nil, fmt.Errorf("user manager of %s: %w", sess, err)
	}
	runtimeDir := sess.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = filepath.Join(UserRunDir, strconv.FormatUint(uint64(sess.UID), 10))
	}
	cmd := exec.Command(path, "-p", "unix:path="+filepath.Join(runtimeDir, "bus")) //nolint:gosec // G204: constant executable resolved from PATH; the path argument is built from a uid
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: sess.UID, Gid: sess.GID, NoSetGroups: true},
		Setpgid:    true,
	}
	cmd.Env = []string{"XDG_RUNTIME_DIR=" + runtimeDir, "PATH=/usr/bin:/bin"}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("user manager of %s: start %s: %w", sess, bridge, err)
	}
	rwc := &bridgeConn{Reader: stdout, WriteCloser: stdin, cmd: cmd}
	conn, err := dbus.NewConn(rwc)
	if err != nil {
		_ = rwc.Close()
		return nil, fmt.Errorf("user manager of %s: %w", sess, err)
	}
	// The bridge authenticates to the real bus itself and accepts an
	// anonymous client on its stdio side.
	if err := conn.Auth([]dbus.Auth{dbus.AuthAnonymous()}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("user manager of %s: auth: %w", sess, err)
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("user manager of %s: hello: %w", sess, err)
	}
	b := &Bus{conn: conn, jobTimeout: jobTimeout, user: true}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := b.manager().CallWithContext(ctx, managerIface+".Subscribe", 0).Err; err != nil && !strings.Contains(err.Error(), "AlreadySubscribed") {
		_ = conn.Close()
		return nil, fmt.Errorf("user manager of %s: subscribe: %w", sess, err)
	}
	return b, nil
}

// UserManagers provides the managers of the users whose session manager
// runs on the node, for policies in the global scope.
type UserManagers interface {
	// Sessions lists the users with a running manager.
	Sessions(ctx context.Context) ([]UserSession, error)
	// Manager returns a Manager for one of those users. The provider owns
	// the connection; callers do not close it.
	Manager(ctx context.Context, sess UserSession) (Manager, error)
}

// BridgeUserManagers is the UserManagers over stdio-bridge connections,
// one per user, reconnected when lost and closed with Close.
type BridgeUserManagers struct {
	// RunDir is where user runtime directories live (default UserRunDir).
	RunDir string
	// Bridge is the systemd-stdio-bridge executable (default from PATH).
	Bridge string
	// JobTimeout bounds start, stop and restart jobs.
	JobTimeout time.Duration

	mu    sync.Mutex
	buses map[uint32]*Bus
}

// Sessions implements UserManagers.
func (m *BridgeUserManagers) Sessions(context.Context) ([]UserSession, error) {
	return ListUserSessions(m.RunDir), nil
}

// Manager implements UserManagers.
func (m *BridgeUserManagers) Manager(ctx context.Context, sess UserSession) (Manager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buses == nil {
		m.buses = map[uint32]*Bus{}
	}
	if b := m.buses[sess.UID]; b != nil {
		if _, err := b.Version(ctx); err == nil {
			return b, nil
		}
		_ = b.Close()
		delete(m.buses, sess.UID)
	}
	b, err := ConnectUser(sess, m.Bridge, m.JobTimeout)
	if err != nil {
		return nil, err
	}
	m.buses[sess.UID] = b
	return b, nil
}

// Close ends every connection.
func (m *BridgeUserManagers) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for uid, b := range m.buses {
		_ = b.Close()
		delete(m.buses, uid)
	}
}

// FakeUserManagers is an in-memory UserManagers for tests.
type FakeUserManagers struct {
	Users []UserSession
	// Managers maps a uid to its Fake; a session without one answers an
	// error, like a user whose bridge cannot be started.
	Managers map[uint32]*Fake
}

// Sessions implements UserManagers.
func (f *FakeUserManagers) Sessions(context.Context) ([]UserSession, error) {
	return f.Users, nil
}

// Manager implements UserManagers.
func (f *FakeUserManagers) Manager(_ context.Context, sess UserSession) (Manager, error) {
	if m, ok := f.Managers[sess.UID]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("no manager for %s", sess)
}
