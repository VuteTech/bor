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
	// ErrStepUpTOTPNeeded is returned when the user's TOTP code is needed
	// to complete the re-authentication.
	ErrStepUpTOTPNeeded = fmt.Errorf("%w: a TOTP code is required", ErrStepUpInvalid)
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

	if err := s.checkMFA(ctx, userID, totpCode, false); err != nil {
		return "", err
	}
	return s.issueToken(userID, purpose)
}

// checkMFA applies the step-up second-factor rules. factorDone reports that
// the re-authentication already included a second factor (asserted by an
// identity provider).
func (s *StepUpService) checkMFA(ctx context.Context, userID, totpCode string, factorDone bool) error {
	if factorDone {
		return nil
	}
	mfaEnrolled := false
	if s.mfaSvc != nil {
		enrolled, mfaErr := s.mfaSvc.IsMFAEnabled(ctx, userID)
		if mfaErr != nil {
			return fmt.Errorf("check MFA enrollment: %w", mfaErr)
		}
		mfaEnrolled = enrolled
	}
	switch {
	case mfaEnrolled:
		// An enrolled second factor is always verified, even when the
		// deployment does not require one.
		if totpCode == "" {
			return ErrStepUpTOTPNeeded
		}
		if verifyErr := s.mfaSvc.VerifyCode(ctx, userID, totpCode); verifyErr != nil {
			return ErrStepUpInvalid
		}
	case s.requireMFA:
		return ErrStepUpMFARequired
	}
	return nil
}

func (s *StepUpService) issueToken(userID, purpose string) (string, error) {
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

// stepUpPendingAudience marks a federated re-authentication that the web UI
// has not collected yet.
const stepUpPendingAudience = "bor-step-up-pending"

// pendingStepUpClaims are the claims of a held federated re-authentication.
type pendingStepUpClaims struct {
	Purpose    string `json:"purpose"`
	FactorDone bool   `json:"factor_done"`
	jwt.RegisteredClaims
}

// HoldFederated records that a federated user has just re-authenticated at
// their identity provider for purpose. factorDone reports that the identity
// provider's login counts as the second factor. The result is kept in an
// httpOnly cookie until CollectFederated.
func (s *StepUpService) HoldFederated(userID, purpose string, factorDone bool) (string, error) {
	if userID == "" || purpose == "" {
		return "", fmt.Errorf("%w: user and purpose are required", ErrStepUpInvalid)
	}
	now := time.Now()
	claims := &pendingStepUpClaims{
		Purpose:    purpose,
		FactorDone: factorDone,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Audience:  jwt.ClaimStrings{stepUpPendingAudience},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(stepUpLifetime)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}

// CollectFederated turns a held federated re-authentication into a step-up
// token for the signed-in user, after the second factor when one is still
// needed (ErrStepUpTOTPNeeded asks for it; the held step-up stays valid).
// It returns the purpose with the token. A held step-up is collected once.
func (s *StepUpService) CollectFederated(ctx context.Context, pending, userID, totpCode string) (token, purpose string, err error) {
	parsed, err := jwt.ParseWithClaims(pending, &pendingStepUpClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	}, jwt.WithAudience(stepUpPendingAudience), jwt.WithExpirationRequired())
	if err != nil {
		return "", "", ErrStepUpTokenInvalid
	}
	claims, ok := parsed.Claims.(*pendingStepUpClaims)
	if !ok || !parsed.Valid || claims.Subject != userID || claims.ID == "" || claims.Purpose == "" {
		return "", "", ErrStepUpTokenInvalid
	}
	if s.isUsed(claims.ID) {
		return "", "", ErrStepUpTokenInvalid
	}
	if mfaErr := s.checkMFA(ctx, userID, totpCode, claims.FactorDone); mfaErr != nil {
		return "", "", mfaErr
	}
	if !s.markUsed(claims.ID, claims.ExpiresAt) {
		return "", "", ErrStepUpTokenInvalid
	}
	token, err = s.issueToken(userID, claims.Purpose)
	return token, claims.Purpose, err
}

func (s *StepUpService) isUsed(jti string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, used := s.usedJTIs[jti]
	return used
}

// markUsed records jti as used; it returns false when it already was.
func (s *StepUpService) markUsed(jti string, expiresAt *jwt.NumericDate) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, exp := range s.usedJTIs {
		if now.After(exp) {
			delete(s.usedJTIs, id)
		}
	}
	if _, used := s.usedJTIs[jti]; used {
		return false
	}
	exp := now.Add(stepUpLifetime)
	if expiresAt != nil {
		exp = expiresAt.Time
	}
	s.usedJTIs[jti] = exp
	return true
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

	if !s.markUsed(claims.ID, claims.ExpiresAt) {
		return ErrStepUpTokenInvalid
	}
	return nil
}
