// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package exthost implements the server side of the extension API
// (pkg/ext): it gives each registered module a Host bound to its name.
package exthost

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/lib/pq"
	"golang.org/x/crypto/hkdf"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/VuteTech/Bor/server/internal/api"
	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	"github.com/VuteTech/Bor/server/pkg/ext"
	auditpb "github.com/VuteTech/Bor/server/pkg/grpc/audit"
)

// Deps are the server components the hosts are built on.
type Deps struct {
	Mux             *http.ServeMux
	AuthMiddleware  func(http.Handler) http.Handler
	Gate            *api.PermissionGate
	AuditMiddleware func(http.Handler) http.Handler

	DB         *database.DB
	Users      *database.UserRepository
	Identities *database.ExternalIdentityRepository
	Groups     *database.UserGroupRepository
	Members    *database.UserGroupMemberRepository

	Auth         *services.AuthService
	StepUp       *services.StepUpService
	AuditSvc     *services.AuditService
	AnonymizeIPs bool
	// SecretRoot is the server secret module SecretBox keys derive from.
	SecretRoot string
}

var moduleName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Init gives every module its Host and calls its Init, in order. Module
// names must be valid and unique.
func Init(ctx context.Context, deps *Deps, reg *Registry, modules []ext.Module) error {
	seen := make(map[string]bool)
	for _, m := range modules {
		name := m.Name()
		if !moduleName.MatchString(name) {
			return fmt.Errorf("extension module name %q is invalid", name)
		}
		if seen[name] {
			return fmt.Errorf("extension module %q registered twice", name)
		}
		seen[name] = true
		h := &host{name: name, deps: deps, reg: reg}
		if err := m.Init(ctx, h); err != nil {
			return fmt.Errorf("extension module %q: %w", name, err)
		}
	}
	return nil
}

// host is the Host of one module.
type host struct {
	name string
	deps *Deps
	reg  *Registry
}

func (h *host) prefix() string { return "/api/v1/ext/" + h.name + "/" }

func (h *host) checkPattern(pattern string) error {
	if !strings.HasPrefix(pattern, h.prefix()) || strings.ContainsAny(pattern, " \t{}") || strings.Contains(pattern, "..") {
		return fmt.Errorf("route %q must be a plain path under %s", pattern, h.prefix())
	}
	return nil
}

func (h *host) handle(pattern string, handler http.Handler) (err error) {
	if perr := h.checkPattern(pattern); perr != nil {
		return perr
	}
	defer func() {
		// ServeMux panics on a duplicate pattern; report it instead.
		if r := recover(); r != nil {
			err = fmt.Errorf("route %q: %v", pattern, r)
		}
	}()
	h.deps.Mux.Handle(pattern, handler)
	return nil
}

func (h *host) HandlePublic(pattern string, handler http.Handler) error {
	if err := h.handle(pattern, handler); err != nil {
		return err
	}
	// Public routes authenticate requests themselves (bearer tokens,
	// identity provider responses), never with the session cookie.
	api.ExemptFromCSRF(pattern)
	return nil
}

func (h *host) HandleAdmin(pattern, resource, action string, handler http.Handler) error {
	if resource == "" || action == "" {
		return errors.New("admin routes need a resource and an action")
	}
	return h.handle(pattern, h.deps.AuthMiddleware(h.deps.Gate.Require(resource, action)(h.deps.AuditMiddleware(handler))))
}

func (h *host) RunMigrations(fsys fs.FS, dir string) error {
	return h.deps.DB.RunModuleMigrations(h.name, fsys, dir)
}

func (h *host) DB() *sql.DB { return h.deps.DB.DB }

func (h *host) RegisterSettingsPanel(p ext.SettingsPanel) error {
	if p.ID == "" || p.Title == "" {
		return errors.New("settings panel needs an ID and a title")
	}
	if !strings.HasPrefix(p.DataURL, h.prefix()) {
		return fmt.Errorf("settings panel data URL must be under %s", h.prefix())
	}
	h.reg.mu.Lock()
	defer h.reg.mu.Unlock()
	for _, existing := range h.reg.panels {
		if existing.ID == p.ID {
			return fmt.Errorf("settings panel %q registered twice", p.ID)
		}
	}
	h.reg.panels = append(h.reg.panels, p)
	return nil
}

func (h *host) RequestUser(r *http.Request) (userID, username string) {
	if claims := api.GetUserFromContext(r.Context()); claims != nil {
		return claims.UserID, claims.Username
	}
	return "", ""
}

func (h *host) Identity() ext.Identity    { return &identity{deps: h.deps, module: h.name} }
func (h *host) Logins() ext.LoginRegistry { return &logins{reg: h.reg, prefix: h.prefix()} }
func (h *host) Audit() ext.Auditor        { return &auditor{deps: h.deps} }

func (h *host) Secrets() ext.SecretBox {
	r := hkdf.New(sha256.New, []byte(h.deps.SecretRoot), []byte("bor-ext-secretbox-v1"), []byte("ext:"+h.name))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		panic("hkdf: " + err.Error()) // only fails if output exceeds 255*HashLen
	}
	return &secretBox{key: key}
}

// ─── Identity ────────────────────────────────────────────────────────────

type identity struct {
	deps   *Deps
	module string
}

// auditChange records an account or privilege change made by the module.
// The actor is the module itself (or the actor it set with ext.WithActor):
// these changes follow from identity provider data, not from a request by a
// Bor user.
func (i *identity) auditChange(ctx context.Context, action, resourceType, resourceID string, details map[string]string) {
	actor := ext.ActorFromContext(ctx)
	if actor == "" {
		actor = "ext:" + i.module
	}
	(&auditor{deps: i.deps}).Record(ctx, nil, &ext.Event{
		Action: action, ResourceType: resourceType, ResourceID: resourceID,
		ActorUsername: actor, Success: true, Details: details,
	})
}

func toExt(u *models.User) *ext.User {
	out := &ext.User{
		ID: u.ID, Username: u.Username, Email: u.Email, FullName: u.FullName,
		Enabled: u.Enabled, Source: u.Source,
	}
	if u.FederationProvider != nil {
		out.FederationProvider = *u.FederationProvider
	}
	return out
}

func (i *identity) FindByExternalID(ctx context.Context, provider, subject string) (*ext.User, error) {
	userID, err := i.deps.Identities.FindUserID(ctx, provider, subject)
	if err != nil || userID == "" {
		return nil, err
	}
	u, err := i.deps.Users.GetByID(ctx, userID)
	if err != nil || u == nil {
		return nil, err
	}
	return toExt(u), nil
}

func (i *identity) CreateFederatedUser(ctx context.Context, connection, provider, subject string, attrs ext.UserAttributes) (*ext.User, error) {
	if strings.TrimSpace(attrs.Username) == "" || connection == "" || provider == "" || subject == "" {
		return nil, errors.New("ext: username, connection, provider and subject are required")
	}
	existing, err := i.deps.Users.GetByUsername(ctx, attrs.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ext.ErrUsernameTaken
	}
	conn := connection
	u := &models.User{
		Username:           attrs.Username,
		Email:              attrs.Email,
		FullName:           attrs.FullName,
		Source:             models.SourceFederated,
		FederationProvider: &conn,
		Enabled:            true,
	}
	if err := i.deps.Users.Create(ctx, u); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ext.ErrUsernameTaken
		}
		return nil, err
	}
	if err := i.deps.Identities.Link(ctx, provider, subject, u.ID); err != nil {
		_ = i.deps.Users.Delete(ctx, u.ID)
		return nil, err
	}
	i.auditChange(ctx, "user.create", "users", u.ID, map[string]string{
		"username": u.Username, "source": models.SourceFederated, "connection": connection,
	})
	return toExt(u), nil
}

func (i *identity) federatedUser(ctx context.Context, userID string) (*models.User, error) {
	u, err := i.deps.Users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.Source != models.SourceFederated {
		return nil, ext.ErrNotFederated
	}
	return u, nil
}

func (i *identity) UpdateFederatedUser(ctx context.Context, userID string, attrs ext.UserAttributes) error {
	if _, err := i.federatedUser(ctx, userID); err != nil {
		return err
	}
	email, fullName := attrs.Email, attrs.FullName
	return i.deps.Users.Update(ctx, userID, &models.UpdateUserRequest{Email: &email, FullName: &fullName})
}

func (i *identity) SetEnabled(ctx context.Context, userID string, enabled bool) error {
	if _, err := i.federatedUser(ctx, userID); err != nil {
		return err
	}
	// UpdateUser keeps the last-Super-Admin guard and revokes tokens on
	// disable.
	if err := i.deps.Auth.UpdateUser(ctx, userID, &models.UpdateUserRequest{Enabled: &enabled}); err != nil {
		if errors.Is(err, services.ErrLastSuperAdmin) {
			return ext.ErrLastSuperAdmin
		}
		return err
	}
	i.auditChange(ctx, "user.update", "users", userID, map[string]string{"enabled": fmt.Sprint(enabled)})
	return nil
}

func (i *identity) RevokeSessions(ctx context.Context, userID string) error {
	if _, err := i.federatedUser(ctx, userID); err != nil {
		return err
	}
	return i.deps.Auth.InvalidateUserTokens(ctx, userID)
}

func (i *identity) UserGroupsByName(ctx context.Context) (map[string]string, error) {
	groups, err := i.deps.Groups.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		out[g.Name] = g.ID
	}
	return out, nil
}

func (i *identity) SyncGroupMemberships(ctx context.Context, userID string, managed, wanted []string) error {
	current, err := i.deps.Members.ListByUserID(ctx, userID)
	if err != nil {
		return err
	}
	memberIDByGroup := make(map[string]string, len(current))
	groupByMemberID := make(map[string]string, len(current))
	for _, m := range current {
		memberIDByGroup[m.GroupID] = m.ID
		groupByMemberID[m.ID] = m.GroupID
	}
	// Groups owned by provisioning (SCIM) have a single writer: their owner.
	managed, wanted, err = i.dropOwnedGroups(ctx, managed, wanted)
	if err != nil {
		return err
	}
	add, remove, err := planMembership(managed, wanted, memberIDByGroup)
	if err != nil {
		return err
	}
	for _, groupID := range add {
		if err := i.deps.Members.Create(ctx, &models.UserGroupMember{GroupID: groupID, UserID: userID}); err != nil {
			return err
		}
		i.auditChange(ctx, "user_group.member_add", "user-groups", groupID, map[string]string{"user_id": userID})
	}
	for _, memberID := range remove {
		if err := i.deps.Members.Delete(ctx, memberID); err != nil {
			return err
		}
		i.auditChange(ctx, "user_group.member_remove", "user-groups", groupByMemberID[memberID], map[string]string{"user_id": userID})
	}
	return nil
}

// dropOwnedGroups removes managed groups (ext.Identity.CreateManagedGroup)
// from login-time group sync.
func (i *identity) dropOwnedGroups(ctx context.Context, managed, wanted []string) (keptManaged, keptWanted []string, err error) {
	owned := make(map[string]bool)
	for _, id := range managed {
		g, gErr := i.deps.Groups.GetByID(ctx, id)
		if gErr != nil {
			return nil, nil, gErr
		}
		if g != nil && g.ManagedBy != nil {
			owned[id] = true
			continue
		}
		keptManaged = append(keptManaged, id)
	}
	for _, id := range wanted {
		if !owned[id] {
			keptWanted = append(keptWanted, id)
		}
	}
	return keptManaged, keptWanted, nil
}

// planMembership returns the managed groups to join and the membership IDs
// to remove so that, among managed, the user is in exactly wanted. current
// maps group ID to membership ID. wanted must be a subset of managed.
func planMembership(managed, wanted []string, current map[string]string) (add, remove []string, err error) {
	isManaged := make(map[string]bool, len(managed))
	for _, g := range managed {
		isManaged[g] = true
	}
	isWanted := make(map[string]bool, len(wanted))
	for _, g := range wanted {
		if !isManaged[g] {
			return nil, nil, fmt.Errorf("ext: group %s is wanted but not managed", g)
		}
		isWanted[g] = true
	}
	for _, g := range managed {
		memberID, has := current[g]
		switch {
		case isWanted[g] && !has:
			add = append(add, g)
		case !isWanted[g] && has:
			remove = append(remove, memberID)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	return add, remove, nil
}

// LocalMFAContinueURL is where the browser finishes a federated sign-in
// that needs a Bor second factor: the login page resumes it.
const LocalMFAContinueURL = "/?sso_continue=mfa"

func (i *identity) StartSession(w http.ResponseWriter, r *http.Request, userID string, ac ext.AuthContext) (string, error) { //nolint:gocritic // signature fixed by ext.Identity
	ctx := r.Context()
	u, err := i.federatedUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if !u.Enabled {
		i.auditLogin(r, u, &ac, false, "account disabled")
		return "", ext.ErrUserDisabled
	}
	methods, err := i.deps.Auth.UserMFAMethods(ctx, u.ID)
	if err != nil {
		return "", err
	}
	switch decideMFA(&ac, i.deps.Auth.MFARequiredByPolicy(ctx), len(methods) > 0) {
	case mfaRefuseIDP:
		i.auditLogin(r, u, &ac, false, "identity provider did not assert MFA")
		return "", ext.ErrMFARequired
	case mfaRefuseSetup:
		if !i.deps.Auth.MFAEnrollmentPossible(ctx) {
			i.auditLogin(r, u, &ac, false, "no Bor second factor is set up")
			return "", ext.ErrMFASetupRequired
		}
		// The user sets up a Bor second factor on the login page.
		fallthrough
	case mfaAskLocal:
		token, tErr := i.deps.Auth.FederatedSessionToken(u, ac.Method, ac.Connection)
		if tErr != nil {
			return "", tErr
		}
		api.SetPendingLoginCookie(w, token)
		if ac.IdentityProvider != "" && ac.Subject != "" {
			_ = i.deps.Identities.TouchLogin(ctx, ac.IdentityProvider, ac.Subject)
		}
		return LocalMFAContinueURL, nil
	}

	resp, err := i.deps.Auth.IssueTokenByUserID(ctx, u.ID)
	if err != nil {
		return "", err
	}
	api.SetSessionCookie(w, resp.Token, i.deps.Auth.TokenLifetime())
	api.SetCSRFCookie(w)
	if refresh, rErr := i.deps.Auth.GenerateRefreshToken(&resp.User); rErr == nil {
		api.SetRefreshCookie(w, refresh, i.deps.Auth.RefreshLifetimeSeconds())
	}
	if ac.IdentityProvider != "" && ac.Subject != "" {
		_ = i.deps.Identities.TouchLogin(ctx, ac.IdentityProvider, ac.Subject)
	}
	i.auditLogin(r, u, &ac, true, "")
	return "", nil
}

// CompleteStepUp implements ext.Identity.
func (i *identity) CompleteStepUp(w http.ResponseWriter, r *http.Request, userID, purpose string, ac ext.AuthContext) error { //nolint:gocritic // signature fixed by ext.Identity
	ctx := r.Context()
	u, err := i.federatedUser(ctx, userID)
	if err != nil {
		return err
	}
	if !u.Enabled {
		return ext.ErrUserDisabled
	}
	if i.deps.StepUp == nil {
		return errors.New("ext: step-up is not available")
	}
	// The identity provider's MFA counts as the second factor unless the
	// connection wants Bor's own; the user is asked for it on collection.
	factorDone := ac.MFA && ac.LocalMFA != ext.LocalMFAAlways
	pending, err := i.deps.StepUp.HoldFederated(u.ID, purpose, factorDone)
	if err != nil {
		return err
	}
	api.SetPendingStepUpCookie(w, pending)
	(&auditor{deps: i.deps}).Record(ctx, r, &ext.Event{
		Action: "auth.step_up.reauthenticate", ResourceType: "auth", ResourceID: u.Username,
		ActorUserID: u.ID, ActorUsername: u.Username, Success: true,
		Details: map[string]string{
			"purpose": purpose, "method": ac.Method, "connection": ac.Connection,
			"mfa": fmt.Sprint(factorDone),
		},
	})
	return nil
}

type mfaDecision int

const (
	mfaStart       mfaDecision = iota // start the session
	mfaAskLocal                       // ask for the user's Bor second factor
	mfaRefuseIDP                      // refuse: MFA required, the IdP did not provide it
	mfaRefuseSetup                    // refuse: MFA required, no Bor factor set up
)

// decideMFA applies ac.LocalMFA. required is the server's MFA policy and
// hasLocal reports whether the user has a Bor second factor.
func decideMFA(ac *ext.AuthContext, required, hasLocal bool) mfaDecision {
	switch ac.LocalMFA {
	case ext.LocalMFAAlways, ext.LocalMFAFallback:
		if ac.LocalMFA == ext.LocalMFAFallback && ac.MFA {
			return mfaStart
		}
		if hasLocal {
			return mfaAskLocal
		}
		if required {
			return mfaRefuseSetup
		}
		return mfaStart
	default:
		if required && !ac.MFA {
			return mfaRefuseIDP
		}
		return mfaStart
	}
}

func (i *identity) auditLogin(r *http.Request, u *models.User, ac *ext.AuthContext, ok bool, reason string) {
	details := map[string]string{"method": ac.Method, "connection": ac.Connection, "mfa": fmt.Sprint(ac.MFA)}
	if reason != "" {
		details["reason"] = reason
	}
	(&auditor{deps: i.deps}).Record(r.Context(), r, &ext.Event{
		Action: "auth.login", ResourceType: "auth", ResourceID: u.Username,
		ActorUserID: u.ID, ActorUsername: u.Username, Success: ok, Details: details,
	})
}

// ─── Login registry ──────────────────────────────────────────────────────

// Registry holds the login methods and account claims of all modules. The
// login flow and the public configuration endpoint read it.
type Registry struct {
	mu      sync.RWMutex
	methods []ext.LoginMethod
	claims  map[string]func(username string) string
	stepUps map[string]func(username, purpose string) string
	logouts map[string]string
	panels  []ext.SettingsPanel
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		claims:  make(map[string]func(string) string),
		stepUps: make(map[string]func(string, string) string),
		logouts: make(map[string]string),
	}
}

// ResolveLogout returns where an account of federationProvider goes after
// logging out of Bor.
func (reg *Registry) ResolveLogout(federationProvider string) (string, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	url, ok := reg.logouts[federationProvider]
	return url, ok
}

// Panels serves GET /api/v1/ext-panels: the modules' settings panels.
func (reg *Registry) Panels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	reg.mu.RLock()
	out := make([]map[string]string, 0, len(reg.panels))
	for _, p := range reg.panels {
		out = append(out, map[string]string{"id": p.ID, "title": p.Title, "data_url": p.DataURL})
	}
	reg.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ResolveStepUp returns where an account of federationProvider
// re-authenticates for purpose.
func (reg *Registry) ResolveStepUp(federationProvider, username, purpose string) (string, bool) {
	reg.mu.RLock()
	start, ok := reg.stepUps[federationProvider]
	reg.mu.RUnlock()
	if !ok {
		return "", false
	}
	url := start(username, purpose)
	return url, url != ""
}

// Resolve returns where an account of federationProvider signs in.
func (reg *Registry) Resolve(federationProvider, username string) (string, bool) {
	reg.mu.RLock()
	start, ok := reg.claims[federationProvider]
	reg.mu.RUnlock()
	if !ok {
		return "", false
	}
	url := start(username)
	return url, url != ""
}

// Methods returns the currently enabled login methods, in registration
// order.
func (reg *Registry) Methods() []models.LoginMethodInfo {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]models.LoginMethodInfo, 0, len(reg.methods))
	for _, m := range reg.methods {
		if m.Enabled == nil || m.Enabled() {
			out = append(out, models.LoginMethodInfo{ID: m.ID, DisplayName: m.DisplayName, StartURL: m.StartURL})
		}
	}
	return out
}

type logins struct {
	reg    *Registry
	prefix string
}

func (l *logins) Register(m ext.LoginMethod) error {
	if m.ID == "" || m.DisplayName == "" {
		return errors.New("login method needs an ID and a display name")
	}
	if !strings.HasPrefix(m.StartURL, l.prefix) {
		return fmt.Errorf("login method start URL must be under %s", l.prefix)
	}
	l.reg.mu.Lock()
	defer l.reg.mu.Unlock()
	for _, existing := range l.reg.methods {
		if existing.ID == m.ID {
			return fmt.Errorf("login method %q registered twice", m.ID)
		}
	}
	l.reg.methods = append(l.reg.methods, m)
	return nil
}

func (l *logins) ClaimAccounts(federationProvider string, startURL func(string) string) error {
	if federationProvider == "" || startURL == nil {
		return errors.New("claim needs a federation provider and a start URL")
	}
	l.reg.mu.Lock()
	defer l.reg.mu.Unlock()
	if _, taken := l.reg.claims[federationProvider]; taken {
		return fmt.Errorf("accounts of %q are already claimed", federationProvider)
	}
	prefix := l.prefix
	l.reg.claims[federationProvider] = func(username string) string {
		u := startURL(username)
		if !strings.HasPrefix(u, prefix) {
			return "" // never redirect outside the module's routes
		}
		return u
	}
	return nil
}

func (l *logins) ClaimLogout(federationProvider, logoutURL string) error {
	if federationProvider == "" || !strings.HasPrefix(logoutURL, l.prefix) {
		return fmt.Errorf("logout claim needs a federation provider and a URL under %s", l.prefix)
	}
	l.reg.mu.Lock()
	defer l.reg.mu.Unlock()
	if _, taken := l.reg.logouts[federationProvider]; taken {
		return fmt.Errorf("logout of %q is already claimed", federationProvider)
	}
	l.reg.logouts[federationProvider] = logoutURL
	return nil
}

func (l *logins) ClaimStepUp(federationProvider string, startURL func(string, string) string) error {
	if federationProvider == "" || startURL == nil {
		return errors.New("step-up claim needs a federation provider and a start URL")
	}
	l.reg.mu.Lock()
	defer l.reg.mu.Unlock()
	if _, taken := l.reg.stepUps[federationProvider]; taken {
		return fmt.Errorf("step-up of %q is already claimed", federationProvider)
	}
	prefix := l.prefix
	l.reg.stepUps[federationProvider] = func(username, purpose string) string {
		u := startURL(username, purpose)
		if !strings.HasPrefix(u, prefix) {
			return ""
		}
		return u
	}
	return nil
}

// ─── Audit ───────────────────────────────────────────────────────────────

type auditor struct{ deps *Deps }

func (a *auditor) Record(ctx context.Context, r *http.Request, e *ext.Event) {
	if a.deps.AuditSvc == nil {
		return
	}
	details, _ := json.Marshal(e.Details)
	outcome := auditpb.Outcome_OUTCOME_SUCCESS
	if !e.Success {
		outcome = auditpb.Outcome_OUTCOME_FAILURE
	}
	ev := &auditpb.AuditEvent{
		OccurredAt: timestamppb.Now(),
		Actor:      &auditpb.Actor{UserId: e.ActorUserID, Username: e.ActorUsername},
		Action:     e.Action,
		Resource:   &auditpb.Resource{Type: e.ResourceType, Id: e.ResourceID},
		Outcome:    outcome,
		Payload: &auditpb.AuditEvent_HttpChange{
			HttpChange: &auditpb.HttpPayload{BodyJson: string(details)},
		},
	}
	if r != nil {
		ev.SrcIp = api.AuditClientIP(r, a.deps.AnonymizeIPs)
		ev.GetHttpChange().Method = r.Method
		ev.GetHttpChange().Path = r.URL.Path
	}
	a.deps.AuditSvc.Emit(ctx, ev)
}

// ─── Secrets ─────────────────────────────────────────────────────────────

type secretBox struct{ key []byte }

func (s *secretBox) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *secretBox) Seal(plaintext []byte) (string, error) {
	gcm, err := s.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

func (s *secretBox) Open(sealed string) ([]byte, error) {
	gcm, err := s.aead()
	if err != nil {
		return nil, err
	}
	data, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(data) < gcm.NonceSize() {
		return nil, errors.New("ext: malformed sealed value")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}
