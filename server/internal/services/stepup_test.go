// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"errors"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func newTestStepUp() *StepUpService {
	return NewStepUpService(nil, nil, "step-up-test-secret", true)
}

// craftStepUpToken mirrors Issue's claim layout so Consume semantics can be
// tested without the re-authentication path.
func craftStepUpToken(t *testing.T, secret, userID, purpose, audience string, expiresIn time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := &StepUpClaims{
		Purpose: purpose,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Audience:  jwt.ClaimStrings{audience},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestStepUpConsumeSingleUse(t *testing.T) {
	s := newTestStepUp()
	token := craftStepUpToken(t, s.jwtSecret, "user-1", StepUpPurposeRevealRecoveryKey, stepUpAudience, stepUpLifetime)

	if err := s.Consume(token, "user-1", StepUpPurposeRevealRecoveryKey); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	if err := s.Consume(token, "user-1", StepUpPurposeRevealRecoveryKey); !errors.Is(err, ErrStepUpTokenInvalid) {
		t.Fatalf("second Consume = %v, want ErrStepUpTokenInvalid (single use)", err)
	}
}

func TestStepUpConsumeRejects(t *testing.T) {
	s := newTestStepUp()
	cases := []struct {
		name  string
		token string
		user  string
	}{
		{"empty token", "", "user-1"},
		{"wrong purpose", craftStepUpToken(t, s.jwtSecret, "user-1", "other_purpose", stepUpAudience, stepUpLifetime), "user-1"},
		{"wrong user", craftStepUpToken(t, s.jwtSecret, "user-2", StepUpPurposeRevealRecoveryKey, stepUpAudience, stepUpLifetime), "user-1"},
		{"expired", craftStepUpToken(t, s.jwtSecret, "user-1", StepUpPurposeRevealRecoveryKey, stepUpAudience, -time.Minute), "user-1"},
		{"wrong audience (session JWT)", craftStepUpToken(t, s.jwtSecret, "user-1", StepUpPurposeRevealRecoveryKey, "bor-session", stepUpLifetime), "user-1"},
		{"wrong secret", craftStepUpToken(t, "another-secret", "user-1", StepUpPurposeRevealRecoveryKey, stepUpAudience, stepUpLifetime), "user-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Consume(tc.token, tc.user, StepUpPurposeRevealRecoveryKey); !errors.Is(err, ErrStepUpTokenInvalid) {
				t.Fatalf("Consume = %v, want ErrStepUpTokenInvalid", err)
			}
		})
	}
}

func TestStepUpSessionTokenIsNotStepUp(t *testing.T) {
	// A regular session JWT signed with the same secret must never pass as
	// a step-up token (no audience, no purpose).
	s := newTestStepUp()
	authSvc := &AuthService{jwtSecret: s.jwtSecret, tokenLifetime: time.Hour}
	sessionToken, err := authSvc.generateToken(&models.User{ID: "user-1", Username: "alice"})
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if err := s.Consume(sessionToken, "user-1", StepUpPurposeRevealRecoveryKey); !errors.Is(err, ErrStepUpTokenInvalid) {
		t.Fatalf("Consume(session JWT) = %v, want ErrStepUpTokenInvalid", err)
	}
}
