// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

// FIPS 140-3 compliant password hashing using PBKDF2-SHA256.
// NIST SP 800-132 recommends >= 600,000 iterations for SHA-256.
const (
	pbkdf2Iterations = 600_000
	pbkdf2KeyLen     = 32 // 256 bits
	pbkdf2SaltLen    = 16 // 128 bits
)

// hashPassword hashes a plaintext password with PBKDF2-SHA256 and returns
// the encoded string: $pbkdf2$sha256$<iterations>$<base64-salt>$<base64-key>
func hashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", fmt.Errorf("failed to derive key: %w", err)
	}
	return fmt.Sprintf("$pbkdf2$sha256$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// verifyPassword checks a plaintext password against a stored PBKDF2 hash.
func verifyPassword(hash, password string) error {
	parts := strings.Split(hash, "$")
	// Expected: ["", "pbkdf2", "sha256", "<iter>", "<salt>", "<key>"]
	if len(parts) != 6 || parts[1] != "pbkdf2" || parts[2] != "sha256" {
		return fmt.Errorf("unsupported password hash format")
	}
	var iter int
	if _, err := fmt.Sscanf(parts[3], "%d", &iter); err != nil {
		return fmt.Errorf("invalid iteration count in hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("invalid salt in hash")
	}
	storedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("invalid key in hash")
	}
	candidate, err := pbkdf2.Key(sha256.New, password, salt, iter, len(storedKey))
	if err != nil {
		return fmt.Errorf("failed to derive key: %w", err)
	}
	// Constant-time comparison to prevent timing attacks.
	if len(candidate) != len(storedKey) {
		return fmt.Errorf("invalid username or password")
	}
	var diff byte
	for i := range candidate {
		diff |= candidate[i] ^ storedKey[i]
	}
	if diff != 0 {
		return fmt.Errorf("invalid username or password")
	}
	return nil
}

// AuthService handles authentication and authorization
type AuthService struct {
	userRepo    *database.UserRepository
	roleRepo    *database.RoleRepository
	bindingRepo *database.UserRoleBindingRepository
	jwtSecret   string
	ldapSvc     *LDAPService
	mfaSvc      *MFAService
	webauthnSvc *WebAuthnService
}

// NewAuthService creates a new AuthService
func NewAuthService(userRepo *database.UserRepository, roleRepo *database.RoleRepository, bindingRepo *database.UserRoleBindingRepository, jwtSecret string, ldapSvc *LDAPService) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		roleRepo:    roleRepo,
		bindingRepo: bindingRepo,
		jwtSecret:   jwtSecret,
		ldapSvc:     ldapSvc,
	}
}

// NewAuthServiceWithMFA creates a new AuthService with MFA support.
func NewAuthServiceWithMFA(userRepo *database.UserRepository, roleRepo *database.RoleRepository, bindingRepo *database.UserRoleBindingRepository, jwtSecret string, ldapSvc *LDAPService, mfaSvc *MFAService) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		roleRepo:    roleRepo,
		bindingRepo: bindingRepo,
		jwtSecret:   jwtSecret,
		ldapSvc:     ldapSvc,
		mfaSvc:      mfaSvc,
	}
}

// NewAuthServiceWithMFAAndWebAuthn creates a new AuthService with MFA and WebAuthn support.
func NewAuthServiceWithMFAAndWebAuthn(userRepo *database.UserRepository, roleRepo *database.RoleRepository, bindingRepo *database.UserRoleBindingRepository, jwtSecret string, ldapSvc *LDAPService, mfaSvc *MFAService, webauthnSvc *WebAuthnService) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		roleRepo:    roleRepo,
		bindingRepo: bindingRepo,
		jwtSecret:   jwtSecret,
		ldapSvc:     ldapSvc,
		mfaSvc:      mfaSvc,
		webauthnSvc: webauthnSvc,
	}
}

// Claims represents JWT claims
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// AuthSessionClaims is a short-lived JWT used during the multi-step auth flow.
type AuthSessionClaims struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Source      string `json:"source"` // "local" or "ldap"
	TOTPDone    bool   `json:"totp_done"`
	SessionType string `json:"session_type"` // always "auth_session"
	jwt.RegisteredClaims
}

// Login authenticates a user and returns a JWT token
func (s *AuthService) Login(ctx context.Context, req *models.LoginRequest) (*models.LoginResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	// Try local authentication first
	user, err := s.userRepo.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to look up user: %w", err)
	}

	if user != nil && user.Source == models.SourceLocal {
		return s.authenticateLocal(user, req.Password)
	}

	// Try LDAP authentication if configured
	if s.ldapSvc != nil && s.ldapSvc.IsEnabled() {
		return s.authenticateLDAP(ctx, req.Username, req.Password)
	}

	return nil, fmt.Errorf("invalid username or password")
}

// authenticateLocal verifies credentials against local database
func (s *AuthService) authenticateLocal(user *models.User, password string) (*models.LoginResponse, error) {
	if !user.Enabled {
		return nil, fmt.Errorf("user account is disabled")
	}

	if err := verifyPassword(user.PasswordHash, password); err != nil {
		return nil, fmt.Errorf("invalid username or password")
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &models.LoginResponse{
		Token: token,
		User:  *user,
	}, nil
}

// authenticateLDAP verifies credentials against LDAP server
func (s *AuthService) authenticateLDAP(ctx context.Context, username, password string) (*models.LoginResponse, error) {
	ldapUser, err := s.ldapSvc.Authenticate(username, password)
	if err != nil {
		log.Printf("LDAP authentication failed for user %s: %v", username, err)
		return nil, fmt.Errorf("invalid username or password")
	}

	// Check if user exists in local DB, create/update if needed
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to look up user: %w", err)
	}

	if user == nil {
		// Create LDAP user in local database
		user = &models.User{
			Username: ldapUser.Username,
			Email:    ldapUser.Email,
			FullName: ldapUser.FullName,
			Source:   models.SourceLDAP,
			Enabled:  true,
		}
		err = s.userRepo.Create(ctx, user)
		if err != nil {
			return nil, fmt.Errorf("failed to create LDAP user: %w", err)
		}
	} else {
		if !user.Enabled {
			return nil, fmt.Errorf("user account is disabled")
		}
		// Update user info from LDAP
		email := ldapUser.Email
		fullName := ldapUser.FullName
		err = s.userRepo.Update(ctx, user.ID, &models.UpdateUserRequest{
			Email:    &email,
			FullName: &fullName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update LDAP user: %w", err)
		}
		user.Email = email
		user.FullName = fullName
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &models.LoginResponse{
		Token: token,
		User:  *user,
	}, nil
}

// generateToken creates a JWT token for the given user
func (s *AuthService) generateToken(user *models.User) (string, error) {
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

// ValidateToken validates a JWT token and returns the claims.
// It rejects auth session tokens (session_type == "auth_session") so they
// cannot be used as regular bearer tokens.
func (s *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Reject auth-session tokens — they must not be used as bearer tokens.
	// We parse the raw map to check the session_type field because Claims
	// does not carry it.
	if raw, ok2 := token.Claims.(*Claims); ok2 {
		// Re-parse as MapClaims to check session_type without a full decode.
		mapToken, _ := jwt.ParseWithClaims(tokenString, jwt.MapClaims{}, func(_ *jwt.Token) (interface{}, error) {
			return []byte(s.jwtSecret), nil
		})
		if mapToken != nil {
			if mc, ok3 := mapToken.Claims.(jwt.MapClaims); ok3 {
				if st, ok4 := mc["session_type"].(string); ok4 && st == "auth_session" {
					return nil, fmt.Errorf("invalid token: auth session tokens cannot be used as bearer tokens")
				}
			}
		}
		_ = raw
	}

	return claims, nil
}

// GenerateSessionToken creates a short-lived (5 min) JWT for the auth flow.
// It is exported so that WebAuthn handlers can re-issue a session token with
// MFA done after a successful WebAuthn assertion.
func (s *AuthService) GenerateSessionToken(userID, username, source string, totpDone bool) (string, error) {
	return s.generateSessionToken(userID, username, source, totpDone)
}

// generateSessionToken creates a short-lived (5 min) JWT for the auth flow.
func (s *AuthService) generateSessionToken(userID, username, source string, totpDone bool) (string, error) {
	claims := &AuthSessionClaims{
		UserID:      userID,
		Username:    username,
		Source:      source,
		TOTPDone:    totpDone,
		SessionType: "auth_session",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   userID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

// ValidateSessionToken parses and validates an AuthSessionClaims token.
// Exported so WebAuthn handlers can verify the session token.
func (s *AuthService) ValidateSessionToken(tokenString string) (*AuthSessionClaims, error) {
	return s.validateSessionToken(tokenString)
}

// validateSessionToken parses and validates an AuthSessionClaims token.
func (s *AuthService) validateSessionToken(tokenString string) (*AuthSessionClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AuthSessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid session token: %w", err)
	}

	claims, ok := token.Claims.(*AuthSessionClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid session token claims")
	}
	if claims.SessionType != "auth_session" {
		return nil, fmt.Errorf("invalid session token: wrong session_type")
	}
	return claims, nil
}

// AuthBegin starts the multi-step authentication flow.
// Returns a short-lived session token and the next required step.
func (s *AuthService) AuthBegin(ctx context.Context, req *models.AuthBeginRequest) (*models.AuthBeginResponse, error) {
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}

	user, err := s.userRepo.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to look up user: %w", err)
	}

	// If the user does not exist locally but LDAP is enabled, let the full
	// authentication step handle it — LDAP users are created on first login.
	if user == nil {
		if s.ldapSvc != nil && s.ldapSvc.IsEnabled() {
			// Issue a session token without a real user ID; the password step
			// will authenticate against LDAP and create the user record.
			sessionToken, err := s.generateSessionToken("", req.Username, models.SourceLDAP, false)
			if err != nil {
				return nil, fmt.Errorf("failed to generate session token: %w", err)
			}
			return &models.AuthBeginResponse{
				SessionToken: sessionToken,
				Next:         "password",
			}, nil
		}
		return nil, fmt.Errorf("invalid username or password")
	}
	if !user.Enabled {
		return nil, fmt.Errorf("user account is disabled")
	}

	source := user.Source

	// Check whether MFA is needed. Applies to all user sources (local and LDAP).
	// Only future OAuth/SAML sources, which delegate authentication entirely to an
	// external IdP, would be exempt — they are not yet implemented.
	var mfaMethods []string
	if s.mfaSvc != nil {
		var enabled bool
		enabled, err = s.mfaSvc.IsMFAEnabled(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check user MFA: %w", err)
		}
		if enabled {
			mfaMethods = append(mfaMethods, "totp")
		}
	}
	if s.webauthnSvc != nil {
		var hasCreds bool
		hasCreds, err = s.webauthnSvc.HasCredentials(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check user WebAuthn credentials: %w", err)
		}
		if hasCreds {
			mfaMethods = append([]string{"webauthn"}, mfaMethods...)
		}
	}

	sessionToken, err := s.generateSessionToken(user.ID, user.Username, source, false)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	next := "password"
	if len(mfaMethods) > 0 {
		next = "mfa"
	}

	return &models.AuthBeginResponse{
		SessionToken: sessionToken,
		Next:         next,
		MFAMethods:   mfaMethods,
	}, nil
}

// AuthStep advances the multi-step authentication flow.
func (s *AuthService) AuthStep(ctx context.Context, req *models.AuthStepRequest) (*models.AuthStepResponse, error) {
	sessionClaims, err := s.validateSessionToken(req.SessionToken)
	if err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}

	switch req.Type {
	case "totp":
		if s.mfaSvc == nil {
			return nil, fmt.Errorf("MFA not configured")
		}
		if err := s.mfaSvc.VerifyCode(ctx, sessionClaims.UserID, req.Credential); err != nil {
			return nil, fmt.Errorf("invalid TOTP code")
		}
		// Issue new session token with totp_done=true.
		newToken, err := s.generateSessionToken(sessionClaims.UserID, sessionClaims.Username, sessionClaims.Source, true)
		if err != nil {
			return nil, fmt.Errorf("failed to generate session token: %w", err)
		}
		return &models.AuthStepResponse{
			SessionToken: newToken,
			Next:         "password",
		}, nil

	case "password":
		// UserID is empty for first-time LDAP users (not yet in local DB).
		// In that case, skip MFA checks (no local record) and go straight to LDAP.
		if sessionClaims.UserID == "" && sessionClaims.Source == models.SourceLDAP {
			loginResp, err := s.authenticateLDAP(ctx, sessionClaims.Username, req.Credential)
			if err != nil {
				return nil, err
			}
			return &models.AuthStepResponse{Next: "done", Token: loginResp.Token, User: &loginResp.User}, nil
		}

		user, err := s.userRepo.GetByID(ctx, sessionClaims.UserID)
		if err != nil || user == nil {
			return nil, fmt.Errorf("invalid username or password")
		}
		if !user.Enabled {
			return nil, fmt.Errorf("user account is disabled")
		}

		var loginResp *models.LoginResponse
		if sessionClaims.Source == models.SourceLDAP {
			loginResp, err = s.authenticateLDAP(ctx, user.Username, req.Credential)
		} else {
			loginResp, err = s.authenticateLocal(user, req.Credential)
		}
		if err != nil {
			return nil, err
		}

		return &models.AuthStepResponse{
			Token: loginResp.Token,
			User:  &loginResp.User,
		}, nil

	default:
		return nil, fmt.Errorf("unknown step type: %s", req.Type)
	}
}

// CreateUser creates a new local user
func (s *AuthService) CreateUser(ctx context.Context, req *models.CreateUserRequest) (*models.User, error) {
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.Password == "" {
		return nil, fmt.Errorf("password is required")
	}

	// Check if user already exists
	existing, err := s.userRepo.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing user: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("username already exists")
	}

	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &models.User{
		Username:     req.Username,
		PasswordHash: hashedPassword,
		Email:        req.Email,
		FullName:     req.FullName,
		Source:       models.SourceLocal,
		Enabled:      true,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Assign role binding if a role name is provided.
	// Note: If role assignment fails, the user exists without roles (no permissions).
	// This is safe since the user simply cannot perform any authorized operations.
	if req.RoleName != "" {
		if err := s.assignRoleToUser(ctx, user.ID, req.RoleName); err != nil {
			return nil, fmt.Errorf("failed to assign role: %w", err)
		}
	}

	return user, nil
}

// assignRoleToUser creates a global role binding for a user by role name
func (s *AuthService) assignRoleToUser(ctx context.Context, userID, roleName string) error {
	role, err := s.roleRepo.GetByName(ctx, roleName)
	if err != nil {
		return fmt.Errorf("failed to look up role: %w", err)
	}
	if role == nil {
		return fmt.Errorf("role not found: %s", roleName)
	}

	binding := &models.UserRoleBinding{
		UserID:    userID,
		RoleID:    role.ID,
		ScopeType: models.ScopeGlobal,
	}
	return s.bindingRepo.Create(ctx, binding)
}

// GetUser retrieves a user by ID
func (s *AuthService) GetUser(ctx context.Context, id string) (*models.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

// GetUserPermissions returns a deduplicated, sorted list of "resource:action"
// permission strings for the given user, aggregated from all their role bindings.
// All bindings (global, organization, and group scoped) are included so the
// frontend has the full set of permissions to show/hide UI elements.
// Note: The backend still enforces scoped permissions at request time via the
// Authorizer middleware — the frontend list is for display purposes only.
func (s *AuthService) GetUserPermissions(ctx context.Context, userID string) ([]string, error) {
	bindings, err := s.bindingRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch role bindings: %w", err)
	}

	seen := make(map[string]struct{})
	var perms []string

	for _, b := range bindings {
		rolePerms, err := s.roleRepo.GetPermissionsByRoleID(ctx, b.RoleID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch permissions for role %s: %w", b.RoleID, err)
		}
		for _, p := range rolePerms {
			key := p.Resource + ":" + p.Action
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				perms = append(perms, key)
			}
		}
	}

	sort.Strings(perms)
	return perms, nil
}

// ListUsers returns all users
func (s *AuthService) ListUsers(ctx context.Context, limit, offset int) ([]*models.User, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.userRepo.List(ctx, limit, offset)
}

// UpdateUser updates a user
func (s *AuthService) UpdateUser(ctx context.Context, id string, req *models.UpdateUserRequest) error {
	return s.userRepo.Update(ctx, id, req)
}

// DeleteUser deletes a user
func (s *AuthService) DeleteUser(ctx context.Context, id string) error {
	return s.userRepo.Delete(ctx, id)
}

// IssueTokenByUserID issues a final JWT for a user by ID without password verification.
// Used by WebAuthn authentication after a successful assertion.
func (s *AuthService) IssueTokenByUserID(ctx context.Context, userID string) (*models.LoginResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if !user.Enabled {
		return nil, fmt.Errorf("user account is disabled")
	}
	token, err := s.generateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	return &models.LoginResponse{Token: token, User: *user}, nil
}

// EnsureDefaultAdmin creates a default admin user if no admin exists
func (s *AuthService) EnsureDefaultAdmin(ctx context.Context) error {
	users, err := s.userRepo.List(ctx, 1, 0)
	if err != nil {
		return fmt.Errorf("failed to check for existing users: %w", err)
	}

	if len(users) > 0 {
		return nil
	}

	adminPassword := os.Getenv("BOR_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin"
		log.Println("WARNING: Using default admin password. Set BOR_ADMIN_PASSWORD environment variable for production.")
	}

	_, err = s.CreateUser(ctx, &models.CreateUserRequest{
		Username: "admin",
		Password: adminPassword,
		Email:    "admin@bor.local",
		FullName: "Administrator",
		RoleName: models.RoleSuperAdmin,
	})
	if err != nil {
		return fmt.Errorf("failed to create default admin: %w", err)
	}

	return nil
}
