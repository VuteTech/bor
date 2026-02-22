// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// AuthService handles authentication and authorization
type AuthService struct {
	userRepo    *database.UserRepository
	roleRepo    *database.RoleRepository
	bindingRepo *database.UserRoleBindingRepository
	jwtSecret   string
	ldapSvc     *LDAPService
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

// Claims represents JWT claims
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
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

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
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
		if err := s.userRepo.Create(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to create LDAP user: %w", err)
		}
	} else {
		if !user.Enabled {
			return nil, fmt.Errorf("user account is disabled")
		}
		// Update user info from LDAP
		email := ldapUser.Email
		fullName := ldapUser.FullName
		if err := s.userRepo.Update(ctx, user.ID, &models.UpdateUserRequest{
			Email:    &email,
			FullName: &fullName,
		}); err != nil {
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

// ValidateToken validates a JWT token and returns the claims
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

	return claims, nil
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

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &models.User{
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
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
