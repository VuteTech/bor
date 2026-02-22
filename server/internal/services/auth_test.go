// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

func TestAuthService_GenerateAndValidateToken(t *testing.T) {
	secret := "test-secret-key"
	authSvc := &AuthService{jwtSecret: secret}

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

	claims, err := authSvc.ValidateToken(token)
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

	_, err := authSvc.ValidateToken("invalid-token")
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

	_, err = authSvc2.ValidateToken(token)
	if err == nil {
		t.Fatal("ValidateToken() should return error for wrong secret")
	}
}

func TestAuthService_ValidateToken_ExpiredToken(t *testing.T) {
	secret := "test-secret-key"
	authSvc := &AuthService{jwtSecret: secret}

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

	_, err = authSvc.ValidateToken(tokenString)
	if err == nil {
		t.Fatal("ValidateToken() should return error for expired token")
	}
}

func TestAuthService_ValidateToken_WrongSigningMethod(t *testing.T) {
	secret := "test-secret-key"
	authSvc := &AuthService{jwtSecret: secret}

	// Create a token with "none" signing method
	token := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{
		UserID:   "test-id",
		Username: "testuser",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	})
	tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	_, err := authSvc.ValidateToken(tokenString)
	if err == nil {
		t.Fatal("ValidateToken() should reject tokens with non-HMAC signing method")
	}
}
