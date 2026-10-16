// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

func TestAuthService_GenerateAndValidateToken(t *testing.T) {
	secret := "test-secret-key"
	authSvc := &AuthService{jwtSecret: secret, tokenLifetime: time.Hour}

	user := &models.User{
		ID:       "test-id-123",
		Username: "testuser",
	}

	token, err := authSvc.generateToken(user)
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("generateToken() returned empty token")
	}

	claims, err := authSvc.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if claims.UserID != user.ID {
		t.Errorf("claims.UserID = %v, want %v", claims.UserID, user.ID)
	}
	if claims.Username != user.Username {
		t.Errorf("claims.Username = %v, want %v", claims.Username, user.Username)
	}
}

func TestAuthService_ValidateToken_InvalidToken(t *testing.T) {
	authSvc := &AuthService{jwtSecret: "test-secret-key"}

	_, err := authSvc.ValidateToken(context.Background(), "invalid-token")
	if err == nil {
		t.Fatal("ValidateToken() should return error for invalid token")
	}
}

func TestAuthService_ValidateToken_WrongSecret(t *testing.T) {
	authSvc1 := &AuthService{jwtSecret: "secret-1"}
	authSvc2 := &AuthService{jwtSecret: "secret-2"}

	user := &models.User{
		ID:       "test-id",
		Username: "testuser",
	}

	token, err := authSvc1.generateToken(user)
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}

	_, err = authSvc2.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("ValidateToken() should return error for wrong secret")
	}
}

func TestAuthService_ValidateToken_ExpiredToken(t *testing.T) {
	secret := "test-secret-key"
	authSvc := &AuthService{jwtSecret: secret, tokenLifetime: time.Hour}

	// Create an expired token
	claims := &Claims{
		UserID:   "test-id",
		Username: "testuser",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Subject:   "test-id",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to create expired token: %v", err)
	}

	_, err = authSvc.ValidateToken(context.Background(), tokenString)
	if err == nil {
		t.Fatal("ValidateToken() should return error for expired token")
	}
}

func TestAuthService_ValidateToken_WrongSigningMethod(t *testing.T) {
	secret := "test-secret-key"
	authSvc := &AuthService{jwtSecret: secret, tokenLifetime: time.Hour}

	// Create a token with "none" signing method
	token := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{
		UserID:   "test-id",
		Username: "testuser",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	})
	tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	_, err := authSvc.ValidateToken(context.Background(), tokenString)
	if err == nil {
		t.Fatal("ValidateToken() should reject tokens with non-HMAC signing method")
	}
}

func TestFederatedSessionToken(t *testing.T) {
	authSvc := &AuthService{jwtSecret: "test-secret-key"}
	local := &models.User{ID: "u1", Username: "alice", Source: models.SourceLocal}
	if _, err := authSvc.FederatedSessionToken(local, "oidc", "oidc/corp"); err == nil {
		t.Error("issued a federated sign-in for a local account")
	}

	fed := &models.User{ID: "u2", Username: "bob", Source: models.SourceFederated}
	token, err := authSvc.FederatedSessionToken(fed, "oidc", "oidc/corp")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := authSvc.ValidateSessionToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Source != models.SourceFederated || claims.UserID != "u2" || claims.TOTPDone ||
		claims.Method != "oidc" || claims.Connection != "oidc/corp" {
		t.Errorf("claims = %+v", claims)
	}
}

func TestResumeFederated_RefusesOtherSignIns(t *testing.T) {
	authSvc := &AuthService{jwtSecret: "test-secret-key"}
	for _, source := range []string{models.SourceLocal, models.SourceLDAP} {
		token, err := authSvc.generateSessionToken("u1", "alice", source, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authSvc.ResumeFederated(context.Background(), token); err == nil {
			t.Errorf("resumed a %s sign-in as federated", source)
		}
	}
	other := &AuthService{jwtSecret: "another-secret"}
	token, _ := other.FederatedSessionToken(&models.User{ID: "u2", Source: models.SourceFederated}, "oidc", "oidc/corp")
	if _, err := authSvc.ResumeFederated(context.Background(), token); err == nil {
		t.Error("resumed a sign-in signed with another key")
	}
}

func TestLoginAuditDetail(t *testing.T) {
	if got := LoginAuditDetail(&AuthSessionClaims{Source: models.SourceLocal}, "totp"); got != "via=step" {
		t.Errorf("local detail = %q", got)
	}
	got := LoginAuditDetail(&AuthSessionClaims{Source: models.SourceFederated, Method: "oidc", Connection: "oidc/corp"}, "totp")
	want := `{"connection":"oidc/corp","method":"oidc","mfa":"true","second_factor":"bor-totp"}`
	if got != want {
		t.Errorf("federated detail = %s, want %s", got, want)
	}
}

func TestEnrollmentToken_IsNotASession(t *testing.T) {
	authSvc := &AuthService{jwtSecret: "test-secret-key"}
	user := &models.User{ID: "u1", Username: "alice", Source: models.SourceLocal}
	token, err := authSvc.enrollmentToken(user, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.ValidateSessionToken(token); err == nil {
		t.Error("an enrollment token passed as a step-flow session")
	}
	if _, err := authSvc.ValidateToken(context.Background(), token); err == nil {
		t.Error("an enrollment token passed as a login session")
	}
	// A step-flow session is not an enrollment token (rejected before any
	// database lookup).
	step, _ := authSvc.generateSessionToken("u1", "alice", models.SourceLocal, true)
	if _, _, err := authSvc.EnrollmentUser(context.Background(), step); err == nil {
		t.Error("a step-flow session passed as an enrollment token")
	}
	other := &AuthService{jwtSecret: "another-secret"}
	forged, _ := other.enrollmentToken(user, "", "")
	if _, _, err := authSvc.EnrollmentUser(context.Background(), forged); err == nil {
		t.Error("an enrollment token signed with another key was accepted")
	}
}

func TestEnrollmentAuditDetail(t *testing.T) {
	got := EnrollmentAuditDetail(&AuthSessionClaims{}, "totp")
	if got != `{"mfa":"true","second_factor":"bor-totp","via":"mfa_enrollment"}` {
		t.Errorf("local detail = %s", got)
	}
	got = EnrollmentAuditDetail(&AuthSessionClaims{Method: "saml", Connection: "saml/adfs"}, "webauthn")
	if got != `{"connection":"saml/adfs","method":"saml","mfa":"true","second_factor":"bor-webauthn","via":"mfa_enrollment"}` {
		t.Errorf("federated detail = %s", got)
	}
}

func TestValidateToken_RefusesOtherTokenTypes(t *testing.T) {
	authSvc := &AuthService{jwtSecret: "test-secret-key", tokenLifetime: time.Hour, refreshLifetime: time.Hour}
	user := &models.User{ID: "u1", Username: "alice", Source: models.SourceLocal}
	ctx := context.Background()

	session, _ := authSvc.generateToken(user)
	if _, err := authSvc.ValidateToken(ctx, session); err != nil {
		t.Fatalf("login session refused: %v", err)
	}
	step, _ := authSvc.generateSessionToken("u1", "alice", models.SourceLocal, true)
	enroll, _ := authSvc.enrollmentToken(user, "", "")
	refresh, _ := authSvc.GenerateRefreshToken(user)
	stepUp, _ := NewStepUpService(nil, nil, "test-secret-key", false).issueToken("u1", StepUpPurposeRevealRecoveryKey)
	for name, tok := range map[string]string{"step-flow": step, "enrollment": enroll, "refresh": refresh, "step-up": stepUp} {
		if _, err := authSvc.ValidateToken(ctx, tok); err == nil {
			t.Errorf("a %s token was accepted as a login session", name)
		}
	}
}

func TestLogoutTicket(t *testing.T) {
	authSvc := &AuthService{jwtSecret: "test-secret-key", tokenLifetime: time.Hour}
	ticket, err := authSvc.LogoutTicket("u1")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := authSvc.ValidateLogoutTicket(ticket); err != nil || id != "u1" {
		t.Errorf("ticket = %q, %v", id, err)
	}
	if _, err := authSvc.ValidateToken(context.Background(), ticket); err == nil {
		t.Error("a logout ticket was accepted as a login session")
	}
	session, _ := authSvc.generateToken(&models.User{ID: "u1", Username: "alice"})
	if _, err := authSvc.ValidateLogoutTicket(session); err == nil {
		t.Error("a login session was accepted as a logout ticket")
	}
}
