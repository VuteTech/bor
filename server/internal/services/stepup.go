// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Step-up re-authentication:
// a short-lived, single-use token proving the user re-entered their
// credentials for one specific privileged action, e.g. revealing a LUKS
// recovery key. Generic by design so future privileged actions can reuse it.

// StepUpPurposeRevealRecoveryKey gates recovery-key reveals.
const StepUpPurposeRevealRecoveryKey = "reveal_recovery_key"

// stepUpAudience marks step-up tokens so they can never pass as session JWTs.
const stepUpAudience = "bor-step-up"

// stepUpLifetime is the validity window of a step-up token.
const stepUpLifetime = 5 * time.Minute

// Errors surfaced by StepUpService.
var (
	// ErrStepUpMFARequired is returned when the deployment requires a
	// second factor for the purpose but the user has no TOTP enrolled.
	ErrStepUpMFARequired = errors.New("enroll MFA (TOTP) to perform this action")
	// ErrStepUpInvalid is returned for failed re-authentication.
	ErrStepUpInvalid = errors.New("re-authentication failed")
	// ErrStepUpTokenInvalid is returned for missing, expired, replayed or
	// wrong-purpose step-up tokens.
	ErrStepUpTokenInvalid = errors.New("a valid step-up token is required for this action")
)

// StepUpClaims are the JWT claims of a step-up token.
type StepUpClaims struct {
	Purpose string `json:"purpose"`
	jwt.RegisteredClaims
}

// StepUpService issues and validates single-use step-up tokens.
type StepUpService struct {
	authSvc    *AuthService
	mfaSvc     *MFAService
	jwtSecret  string
	requireMFA bool

	// usedJTIs implements single use. Entries expire with the token.
	mu       sync.Mutex
	usedJTIs map[string]time.Time
}

// NewStepUpService creates a StepUpService. requireMFA enforces a TOTP code
// during step-up (BOR_ESCROW_REQUIRE_MFA, default true - NIS2: MFA for
// privileged access).
func NewStepUpService(authSvc *AuthService, mfaSvc *MFAService, jwtSecret string, requireMFA bool) *StepUpService {
	return &StepUpService{
		authSvc:    authSvc,
		mfaSvc:     mfaSvc,
		jwtSecret:  jwtSecret,
		requireMFA: requireMFA,
		usedJTIs:   map[string]time.Time{},
	}
}

// Issue re-authenticates the user (password, plus TOTP when required or
// enrolled) and returns a single-use step-up token for the purpose.
func (s *StepUpService) Issue(ctx context.Context, userID, username, purpose, password, totpCode string) (string, error) {
	if purpose == "" {
		return "", fmt.Errorf("%w: a purpose is required", ErrStepUpInvalid)
	}
	// Local users verify against their hash; LDAP users against a bind.
	user, err := s.authSvc.VerifyPassword(ctx, username, password)
	if err != nil || user == nil || user.ID != userID {
		return "", ErrStepUpInvalid
	}

	mfaEnrolled := false
	if s.mfaSvc != nil {
		enrolled, mfaErr := s.mfaSvc.IsMFAEnabled(ctx, userID)
		if mfaErr != nil {
			return "", fmt.Errorf("check MFA enrollment: %w", mfaErr)
		}
		mfaEnrolled = enrolled
	}
	switch {
	case mfaEnrolled:
		// An enrolled second factor is always verified, even when the
		// deployment does not require one.
		if totpCode == "" {
			return "", fmt.Errorf("%w: a TOTP code is required", ErrStepUpInvalid)
		}
		if verifyErr := s.mfaSvc.VerifyCode(ctx, userID, totpCode); verifyErr != nil {
			return "", ErrStepUpInvalid
		}
	case s.requireMFA:
		return "", ErrStepUpMFARequired
	}

	now := time.Now()
	claims := &StepUpClaims{
		Purpose: purpose,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Audience:  jwt.ClaimStrings{stepUpAudience},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(stepUpLifetime)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("sign step-up token: %w", err)
	}
	return signed, nil
}

// Lifetime returns the token lifetime in seconds.
func (s *StepUpService) Lifetime() int {
	return int(stepUpLifetime.Seconds())
}

// Consume validates a step-up token for the given user and purpose and marks
// it used. A second Consume of the same token fails (single use).
func (s *StepUpService) Consume(tokenString, userID, purpose string) error {
	if tokenString == "" {
		return ErrStepUpTokenInvalid
	}
	token, err := jwt.ParseWithClaims(tokenString, &StepUpClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	}, jwt.WithAudience(stepUpAudience), jwt.WithExpirationRequired())
	if err != nil {
		return ErrStepUpTokenInvalid
	}
	claims, ok := token.Claims.(*StepUpClaims)
	if !ok || !token.Valid {
		return ErrStepUpTokenInvalid
	}
	if claims.Subject != userID || claims.Purpose != purpose || claims.ID == "" {
		return ErrStepUpTokenInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// Prune expired entries so the map stays bounded.
	for jti, exp := range s.usedJTIs {
		if now.After(exp) {
			delete(s.usedJTIs, jti)
		}
	}
	if _, used := s.usedJTIs[claims.ID]; used {
		return ErrStepUpTokenInvalid
	}
	exp := now.Add(stepUpLifetime)
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	}
	s.usedJTIs[claims.ID] = exp
	return nil
}
