// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package ext is the extension API of the Bor server: optional modules a
// distribution passes to app.Run (app.Options.Extensions) to add login
// methods, routes and storage without modifying the server. The community
// edition registers no modules.
//
// A module receives a Host bound to its name. Everything a module mounts,
// stores or logs goes through the Host, so the server keeps control of
// routing, authorization, sessions and auditing.
package ext

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
)

// Module is an optional server component.
type Module interface {
	// Name is a short, stable identifier ([a-z0-9-]+). It scopes the
	// module's routes (/api/v1/ext/<name>/...) and migrations.
	Name() string
	// Init is called once at startup, after the core services exist and
	// before the server accepts requests. An error aborts startup.
	Init(ctx context.Context, host Host) error
}

// Host is what the server exposes to one module.
type Host interface {
	// HandlePublic mounts an unauthenticated handler. pattern must start
	// with the module's prefix, /api/v1/ext/<name>/. The handler does its own
	// authentication (an IdP callback, a bearer token, ...).
	HandlePublic(pattern string, h http.Handler) error
	// HandleAdmin mounts a handler that requires an authenticated session
	// holding resource:action globally. Same prefix rule as HandlePublic.
	// State-changing requests are recorded by the audit middleware.
	HandleAdmin(pattern, resource, action string, h http.Handler) error

	// RunMigrations applies the module's *.up.sql files in dir of fsys, in
	// name order, once each. They are tracked per module, so their numbering
	// is independent of the server's.
	RunMigrations(fsys fs.FS, dir string) error
	// DB is the server's database, for the module's own tables.
	DB() *sql.DB

	// RequestUser returns the signed-in user of a request to an admin
	// route ("" outside HandleAdmin routes).
	RequestUser(r *http.Request) (userID, username string)
	// RegisterSettingsPanel adds a page to the web UI's Settings for
	// administrators (settings:manage). The UI renders the Panel JSON the
	// module serves at panel.DataURL, a HandleAdmin route.
	RegisterSettingsPanel(panel SettingsPanel) error
	Identity() Identity
	Logins() LoginRegistry
	Audit() Auditor
	// Secrets encrypts and decrypts data at rest with a key derived for this
	// module from the server's secret.
	Secrets() SecretBox
}

// User is a Bor user as seen by modules.
type User struct {
	ID       string
	Username string
	Email    string
	FullName string
	Enabled  bool
	// Source is "local", "ldap" or "federated".
	Source string
	// FederationProvider is the login connection that owns a federated
	// account ("" otherwise).
	FederationProvider string
}

// Group is a Bor user group.
type Group struct {
	ID          string
	Name        string
	Description string
	// ManagedBy is the owner of a managed group ("<module>:<owner>"), or "".
	ManagedBy string
}

type actorKey struct{}

// WithActor names who is acting for the changes made with ctx, for the
// audit log (for example "scim:corp"). Without it the actor is
// "ext:<module>".
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext returns the actor set by WithActor, or "".
func ActorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}

// UserAttributes are the profile fields an identity provider supplies.
type UserAttributes struct {
	Username string
	Email    string
	FullName string
}

// AuthContext describes how a user authenticated, for StartSession.
type AuthContext struct {
	// Method is the login method, e.g. "oidc" or "saml".
	Method string
	// Connection is the login connection, e.g. "oidc/corporate".
	Connection string
	// IdentityProvider and Subject are the external identity the user
	// signed in with, as passed to FindByExternalID.
	IdentityProvider string
	Subject          string
	// MFA reports that the identity provider performed multi-factor
	// authentication (asserted by the IdP or required by the connection).
	MFA bool
	// LocalMFA says whether Bor asks for its own second factor after the
	// identity provider login.
	LocalMFA LocalMFA
}

// LocalMFA says when Bor asks a federated user for a second factor
// registered in Bor (TOTP or a security key) after the identity provider
// login.
type LocalMFA int

const (
	// LocalMFANever never asks: only AuthContext.MFA counts.
	LocalMFANever LocalMFA = iota
	// LocalMFAAlways ignores AuthContext.MFA and applies Bor's second
	// factor exactly as for a local account: asked when the user has one,
	// and required (login refused until one is set up) when the server
	// requires MFA.
	LocalMFAAlways
	// LocalMFAFallback accepts AuthContext.MFA and otherwise applies Bor's
	// second factor as LocalMFAAlways does.
	LocalMFAFallback
)

// Errors returned by Identity.
var (
	ErrUsernameTaken = errors.New("ext: username belongs to another account")
	ErrUserDisabled  = errors.New("ext: user account is disabled")
	ErrMFARequired   = errors.New("ext: multi-factor authentication is required by policy")
	// ErrMFASetupRequired means the server requires MFA, the identity
	// provider did not provide it and the user has no Bor second factor.
	ErrMFASetupRequired = errors.New("ext: a Bor second factor is required but none is set up")
	ErrNotFederated     = errors.New("ext: not a federated account")
	// ErrGroupNameTaken means another user group already has the name.
	ErrGroupNameTaken = errors.New("ext: a user group with this name already exists")
	// ErrNotOwner means the group is not managed by the caller.
	ErrNotOwner = errors.New("ext: the group is not managed by this owner")
	// ErrLastSuperAdmin means the change would leave no Super Admin.
	ErrLastSuperAdmin = errors.New("ext: the last Super Admin cannot be disabled or deleted")
)

// Identity manages federated users and their sessions.
type Identity interface {
	// FindByExternalID returns the user linked to (provider, subject), or nil.
	// provider namespaces the subject, e.g. "oidc:" + issuer.
	FindByExternalID(ctx context.Context, provider, subject string) (*User, error)
	// CreateFederatedUser creates a federated account owned by connection and
	// links (provider, subject) to it. It never takes over an existing
	// account: a username already in use yields ErrUsernameTaken.
	CreateFederatedUser(ctx context.Context, connection, provider, subject string, attrs UserAttributes) (*User, error)
	// UpdateFederatedUser refreshes email and full name. The username is
	// immutable. Only federated accounts can be updated (ErrNotFederated).
	UpdateFederatedUser(ctx context.Context, userID string, attrs UserAttributes) error
	// SetEnabled enables or disables an account. Disabling revokes its
	// sessions.
	SetEnabled(ctx context.Context, userID string, enabled bool) error
	// RevokeSessions invalidates every session and refresh token of the user.
	RevokeSessions(ctx context.Context, userID string) error
	// LinkFederatedIdentity links (provider, subject) to an existing
	// federated account, e.g. when an account provisioned by SCIM signs in
	// for the first time. It fails when the identity is linked to another
	// user. Only federated accounts can be linked (ErrNotFederated).
	LinkFederatedIdentity(ctx context.Context, userID, provider, subject string) error
	// RenameFederatedUser changes a federated account's username (identity
	// providers rename users). ErrUsernameTaken when the name is in use.
	RenameFederatedUser(ctx context.Context, userID, username string) error
	// GetUser returns the user, or nil when it does not exist.
	GetUser(ctx context.Context, userID string) (*User, error)
	// DeleteFederatedUser erases a federated account, its identity links
	// and group memberships (GDPR erasure). Only federated accounts can be
	// deleted (ErrNotFederated).
	DeleteFederatedUser(ctx context.Context, userID string) error

	// Managed user groups: a module owns their name and membership (for
	// example SCIM provisioning), while Bor administrators assign their
	// roles. owner is a module-chosen key such as "scim/corp"; a module
	// can only change the groups it owns (ErrNotOwner).
	CreateManagedGroup(ctx context.Context, owner, name string) (*Group, error)
	RenameManagedGroup(ctx context.Context, owner, groupID, name string) error
	DeleteManagedGroup(ctx context.Context, owner, groupID string) error
	// ManagedGroups returns the groups the owner manages.
	ManagedGroups(ctx context.Context, owner string) ([]*Group, error)
	// ManagedGroupMembers returns the user IDs of the group's members.
	ManagedGroupMembers(ctx context.Context, owner, groupID string) ([]string, error)
	// UpdateManagedGroupMembers adds and removes members (user IDs).
	// Adding a member already present and removing one absent are no-ops.
	UpdateManagedGroupMembers(ctx context.Context, owner, groupID string, add, remove []string) error

	// UserGroupsByName returns all user groups as name -> ID.
	UserGroupsByName(ctx context.Context) (map[string]string, error)
	// SyncGroupMemberships makes the user a member of exactly the groups in
	// wanted, among the groups in managed. Memberships in groups outside
	// managed are left alone. Membership confers the groups' roles; it is
	// driven by configuration the operator controls, not by a caller's own
	// permissions.
	SyncGroupMemberships(ctx context.Context, userID string, managed, wanted []string) error

	// StartSession signs the user in on this browser: it sets the session,
	// refresh and CSRF cookies exactly like the built-in login methods and
	// records an auth.login audit event. It refuses disabled accounts and,
	// following ac.LocalMFA, sessions that lack a required second factor.
	//
	// When Bor's own second factor must still be checked, StartSession
	// starts no session. It holds the sign-in for a few minutes and returns
	// continueURL: the caller must redirect the browser there, where the
	// login page asks for the factor. continueURL is empty when the session
	// started.
	StartSession(w http.ResponseWriter, r *http.Request, userID string, ac AuthContext) (continueURL string, err error)

	// CompleteStepUp records that the user has just re-authenticated at the
	// identity provider for purpose (the module verified that the login is
	// fresh). It holds a step-up for this browser, which the web UI
	// collects; following ac.LocalMFA, the user may still be asked for a
	// Bor second factor then. The caller then sends the popup to
	// StepUpDoneURL.
	CompleteStepUp(w http.ResponseWriter, r *http.Request, userID, purpose string, ac AuthContext) error
}

// StepUpDoneURL is where a step-up popup goes when the module is done:
// result is "done" or an error code. The web UI closes the popup and
// collects the step-up.
func StepUpDoneURL(result string) string {
	if result == "done" {
		return "/?sso_stepup=done"
	}
	return "/?sso_stepup=failed&reason=" + url.QueryEscape(result)
}

// LoginMethod is a "Sign in with ..." option on the login page.
type LoginMethod struct {
	// ID is unique across modules, e.g. "oidc/corporate".
	ID string
	// DisplayName is the button label, e.g. "Microsoft Entra ID".
	DisplayName string
	// StartURL begins the login, e.g. "/api/v1/ext/sso/oidc/corporate/start".
	StartURL string
	// Enabled is consulted on every login page load (for example to follow
	// a license); nil means always enabled.
	Enabled func() bool
}

// LoginRegistry lets modules offer login methods and own federated accounts.
type LoginRegistry interface {
	// Register adds a login method to the login page.
	Register(m LoginMethod) error
	// ClaimAccounts makes the module responsible for accounts whose
	// FederationProvider is federationProvider: when such a user enters
	// their username, the login page is sent to startURL(username) instead
	// of asking for a password.
	ClaimAccounts(federationProvider string, startURL func(username string) string) error
	// ClaimStepUp makes the module re-authenticate those accounts before a
	// privileged action (instead of asking for a password they do not
	// have). The web UI opens startURL(username, purpose) in a popup; the
	// module forces a fresh login at the identity provider and then calls
	// Identity.CompleteStepUp.
	ClaimStepUp(federationProvider string, startURL func(username, purpose string) string) error
}

// Event is an audit record.
type Event struct {
	// Action, e.g. "sso.login_failed".
	Action string
	// ResourceType and ResourceID identify what the event is about.
	ResourceType string
	ResourceID   string
	// ActorUserID and ActorUsername identify who acted, if known.
	ActorUserID   string
	ActorUsername string
	Success       bool
	// Details are stored as a JSON object. Never put secrets or tokens here.
	Details map[string]string
}

// Auditor records audit events.
type Auditor interface {
	// Record stores e; r supplies the client IP (anonymized when the server
	// is configured to) and may be nil.
	Record(ctx context.Context, r *http.Request, e *Event)
}

// SecretBox encrypts small secrets (AES-256-GCM).
type SecretBox interface {
	Seal(plaintext []byte) (string, error)
	Open(sealed string) ([]byte, error)
}

// SettingsPanel is a page in the web UI's Settings.
type SettingsPanel struct {
	// ID is unique across modules, e.g. "sso".
	ID    string
	Title string
	// DataURL serves the Panel JSON, e.g. "/api/v1/ext/sso/admin/panel".
	DataURL string
}

// Panel is the content of a settings panel. The community web UI renders it
// without knowing the module: status, read-only fields, tables and actions
// that call the module's admin routes.
type Panel struct {
	Description string         `json:"description,omitempty"`
	Sections    []PanelSection `json:"sections"`
}

// PanelSection is one block of a panel, e.g. one identity provider.
type PanelSection struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// Status is "ok", "warning", "error" or "disabled", shown with
	// StatusText.
	Status     string        `json:"status,omitempty"`
	StatusText string        `json:"status_text,omitempty"`
	Fields     []PanelField  `json:"fields,omitempty"`
	Tables     []PanelTable  `json:"tables,omitempty"`
	Actions    []PanelAction `json:"actions,omitempty"`
}

// PanelField is a read-only label and value.
type PanelField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// Copy shows a copy-to-clipboard control (URLs to register at an IdP).
	Copy bool `json:"copy,omitempty"`
}

// PanelTable lists items, each with its own actions.
type PanelTable struct {
	Title   string     `json:"title"`
	Columns []string   `json:"columns"`
	Rows    []PanelRow `json:"rows"`
	// Empty is shown when there are no rows.
	Empty string `json:"empty,omitempty"`
}

// PanelRow is a table row.
type PanelRow struct {
	Cells   []string      `json:"cells"`
	Actions []PanelAction `json:"actions,omitempty"`
}

// PanelAction is a button that sends Method to URL (an admin route of the
// module). The panel reloads afterwards.
type PanelAction struct {
	Label  string `json:"label"`
	Method string `json:"method"`
	URL    string `json:"url"`
	// Danger marks a destructive action; ConfirmPhrase must then be typed
	// by the administrator to confirm it.
	Danger        bool   `json:"danger,omitempty"`
	ConfirmPhrase string `json:"confirm_phrase,omitempty"`
	// Confirm is a confirmation question for non-destructive actions.
	Confirm string `json:"confirm,omitempty"`
	// Result is how the response is shown: "secret" (the "token" member,
	// shown once with a copy control), "message" (the "message" member) or
	// "" (nothing).
	Result string `json:"result,omitempty"`
}
