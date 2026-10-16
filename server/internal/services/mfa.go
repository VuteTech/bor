// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const (
	totpIssuer    = "Bor"
	backupCodeNum = 8

	// qrImageSize is the edge length in pixels of the rendered QR code and
	// qrQuietZone the white margin around it. The UI shows it at 200px, so
	// this renders sharp on high-DPI screens.
	qrImageSize = 400
	qrQuietZone = 32
)

// MFAService handles TOTP setup, verification, and settings.
type MFAService struct {
	mfaRepo      *database.MFARepository
	settingsRepo *database.SettingsRepository
	aesKey       []byte
	legacyAESKey []byte // pre-HKDF key for migrating existing secrets
}

// NewMFAService creates a new MFAService.
func NewMFAService(mfaRepo *database.MFARepository, settingsRepo *database.SettingsRepository, jwtSecret string) *MFAService {
	secret := os.Getenv("BOR_MFA_SECRET")
	if secret == "" {
		secret = jwtSecret
	}
	return &MFAService{
		mfaRepo:      mfaRepo,
		settingsRepo: settingsRepo,
		aesKey:       deriveAESKey(secret),
		legacyAESKey: deriveLegacyAESKey(secret),
	}
}

// GetStatus returns the MFA status for the given user, including the global
// enforcement flag so the frontend can display the setup gate when required.
func (s *MFAService) GetStatus(ctx context.Context, userID string) (*models.MFAStatusResponse, error) {
	required, err := s.IsMFARequired(ctx)
	if err != nil {
		return nil, err
	}
	row, err := s.mfaRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil || !row.TOTPEnabled {
		return &models.MFAStatusResponse{Enabled: false, MFARequired: required}, nil
	}
	return &models.MFAStatusResponse{Enabled: true, Algorithm: row.TOTPAlgorithm, MFARequired: required}, nil
}

// BeginSetup generates a new TOTP secret for the user (not yet enabled).
func (s *MFAService) BeginSetup(ctx context.Context, userID, username string) (*models.MFASetupBeginResponse, error) {
	algorithm := s.getTOTPAlgorithm(ctx)

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
		Algorithm:   algorithm,
		Digits:      otp.DigitsSix,
		Period:      30,
	})
	if err != nil {
		return nil, fmt.Errorf("generate totp key: %w", err)
	}

	encSecret, err := aesEncrypt(s.aesKey, []byte(key.Secret()))
	if err != nil {
		return nil, fmt.Errorf("encrypt totp secret: %w", err)
	}

	algStr := otpAlgorithmToString(algorithm)
	if err := s.mfaRepo.Upsert(ctx, &database.UserMFARow{
		UserID:        userID,
		TOTPSecret:    encSecret,
		TOTPAlgorithm: algStr,
		TOTPEnabled:   false,
		BackupCodes:   []string{},
	}); err != nil {
		return nil, fmt.Errorf("store totp secret: %w", err)
	}

	return &models.MFASetupBeginResponse{
		Secret:    key.Secret(),
		Algorithm: algStr,
	}, nil
}

// GenerateSetupQR returns a PNG image of the QR code for a pending (not yet
// enabled) TOTP setup. The QR encodes the otpauth:// URI that authenticator
// apps scan. Generating the QR server-side avoids sending the TOTP secret to
// any third-party service.
func (s *MFAService) GenerateSetupQR(ctx context.Context, userID, username string) ([]byte, error) {
	row, err := s.mfaRepo.GetByUserID(ctx, userID)
	if err != nil || row == nil {
		return nil, fmt.Errorf("no pending MFA setup found")
	}
	if row.TOTPEnabled {
		return nil, fmt.Errorf("MFA is already enabled")
	}

	secret, err := s.decryptSecret(ctx, row)
	if err != nil {
		return nil, err
	}

	algStr := strings.ToUpper(row.TOTPAlgorithm)
	if algStr == "" {
		algStr = "SHA256"
	}

	return renderTOTPQR(username, algStr, secret)
}

// renderTOTPQR renders the otpauth:// URI for a base32 TOTP secret as a PNG
// QR code with a white quiet zone.
func renderTOTPQR(username, algStr, secret string) ([]byte, error) {
	// Build the otpauth:// URI directly. The secret from the DB is already
	// base32-encoded, so we embed it as-is (totp.Generate would double-encode).
	params := url.Values{}
	params.Set("algorithm", algStr)
	params.Set("digits", "6")
	params.Set("issuer", totpIssuer)
	params.Set("period", "30")
	params.Set("secret", secret)
	otpauthURL := url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + totpIssuer + ":" + username,
		RawQuery: params.Encode(),
	}

	key, err := otp.NewKeyFromURL(otpauthURL.String())
	if err != nil {
		return nil, fmt.Errorf("parse otpauth uri: %w", err)
	}

	code, err := key.Image(qrImageSize, qrImageSize)
	if err != nil {
		return nil, fmt.Errorf("create qr code: %w", err)
	}

	// key.Image draws the code edge to edge. Add a white quiet zone so
	// scanners find the code on any page background, including dark themes.
	full := qrImageSize + 2*qrQuietZone
	img := image.NewGray(image.Rect(0, 0, full, full))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(img, code.Bounds().Add(image.Pt(qrQuietZone, qrQuietZone)), code, code.Bounds().Min, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("render qr code: %w", err)
	}

	return buf.Bytes(), nil
}

// FinishSetup verifies the first TOTP code and enables MFA.
func (s *MFAService) FinishSetup(ctx context.Context, userID, code string) (*models.MFASetupFinishResponse, error) {
	row, err := s.mfaRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("mfa setup not started")
	}

	secret, err := s.decryptSecret(ctx, row)
	if err != nil {
		return nil, err
	}

	algorithm := stringToOTPAlgorithm(row.TOTPAlgorithm)
	valid, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: algorithm,
	})
	if err != nil || !valid {
		return nil, fmt.Errorf("invalid TOTP code")
	}

	backupPlain, backupHashed, err := generateBackupCodes()
	if err != nil {
		return nil, err
	}

	if err := s.mfaRepo.SetEnabled(ctx, userID, true, backupHashed); err != nil {
		return nil, fmt.Errorf("enable mfa: %w", err)
	}

	return &models.MFASetupFinishResponse{BackupCodes: backupPlain}, nil
}

// Disable removes MFA for the user.
func (s *MFAService) Disable(ctx context.Context, userID string) error {
	return s.mfaRepo.Delete(ctx, userID)
}

// IsMFARequired returns whether MFA is globally enforced.
func (s *MFAService) IsMFARequired(ctx context.Context) (bool, error) {
	val, err := s.settingsRepo.Get(ctx, "mfa_required")
	if err != nil {
		return false, err
	}
	return val == "true", nil
}

// IsMFAEnabled returns whether the given user has MFA enabled.
func (s *MFAService) IsMFAEnabled(ctx context.Context, userID string) (bool, error) {
	row, err := s.mfaRepo.GetByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	return row != nil && row.TOTPEnabled, nil
}

// VerifyCode verifies a TOTP code or backup code for a user.
func (s *MFAService) VerifyCode(ctx context.Context, userID, code string) error {
	row, err := s.mfaRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if row == nil || !row.TOTPEnabled {
		return fmt.Errorf("mfa not enabled")
	}

	secret, err := s.decryptSecret(ctx, row)
	if err != nil {
		return err
	}

	algorithm := stringToOTPAlgorithm(row.TOTPAlgorithm)
	valid, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: algorithm,
	})
	if err == nil && valid {
		return nil
	}

	// Try backup codes
	normalised := strings.ToUpper(strings.ReplaceAll(code, "-", ""))
	for i, hashed := range row.BackupCodes {
		if !verifyBackupCode(normalised, hashed) {
			continue
		}
		remaining := make([]string, 0, len(row.BackupCodes)-1)
		remaining = append(remaining, row.BackupCodes[:i]...)
		remaining = append(remaining, row.BackupCodes[i+1:]...)
		_ = s.mfaRepo.SetEnabled(ctx, userID, true, remaining)
		return nil
	}

	return fmt.Errorf("invalid TOTP code")
}

// GetMFASettings returns the global MFA settings.
func (s *MFAService) GetMFASettings(ctx context.Context) (*models.MFASettings, error) {
	required, err := s.settingsRepo.Get(ctx, "mfa_required")
	if err != nil {
		return nil, err
	}
	alg, err := s.settingsRepo.Get(ctx, "totp_algorithm")
	if err != nil {
		return nil, err
	}
	if alg == "" {
		alg = "SHA256"
	}
	selfEnroll, err := s.SelfEnrollmentAllowed(ctx)
	if err != nil {
		return nil, err
	}
	return &models.MFASettings{
		MFARequired:         required == "true",
		TOTPAlgorithm:       alg,
		AllowSelfEnrollment: &selfEnroll,
	}, nil
}

// SelfEnrollmentAllowed reports whether users without a second factor may
// set one up while signing in when MFA is required (the default).
func (s *MFAService) SelfEnrollmentAllowed(ctx context.Context) (bool, error) {
	v, err := s.settingsRepo.Get(ctx, "mfa_self_enrollment")
	if err != nil {
		return false, err
	}
	return v != "false", nil
}

// UpdateMFASettings persists global MFA settings.
func (s *MFAService) UpdateMFASettings(ctx context.Context, settings *models.MFASettings) error {
	required := "false"
	if settings.MFARequired {
		required = "true"
	}
	alg := settings.TOTPAlgorithm
	if alg != "SHA256" && alg != "SHA512" {
		alg = "SHA256"
	}
	if err := s.settingsRepo.Set(ctx, "mfa_required", required); err != nil {
		return err
	}
	if settings.AllowSelfEnrollment != nil {
		if err := s.settingsRepo.Set(ctx, "mfa_self_enrollment", fmt.Sprint(*settings.AllowSelfEnrollment)); err != nil {
			return err
		}
	}
	return s.settingsRepo.Set(ctx, "totp_algorithm", alg)
}

// decryptSecret decrypts a stored TOTP secret. Secrets still encrypted with
// the legacy (pre-HKDF) key are transparently re-encrypted with the current
// key, so the legacy derivation can be removed once all rows have migrated.
func (s *MFAService) decryptSecret(ctx context.Context, row *database.UserMFARow) (string, error) {
	// Try the current HKDF-derived key first.
	b, err := aesDecrypt(s.aesKey, row.TOTPSecret)
	if err == nil {
		return string(b), nil
	}
	// Fall back to the legacy SHA-256-derived key for secrets encrypted
	// before the HKDF migration.
	b, legacyErr := aesDecrypt(s.legacyAESKey, row.TOTPSecret)
	if legacyErr != nil {
		return "", fmt.Errorf("decrypt totp secret: %w", err)
	}
	// Migrate on read. Failures are non-fatal: the secret still decrypts via
	// the legacy key, and the next read retries the migration.
	if s.mfaRepo != nil {
		if enc, encErr := aesEncrypt(s.aesKey, b); encErr == nil {
			if upErr := s.mfaRepo.UpdateSecret(ctx, row.UserID, enc); upErr != nil {
				log.Printf("mfa: re-encrypting TOTP secret for user %s failed: %v", row.UserID, upErr)
			} else {
				row.TOTPSecret = enc
				log.Printf("mfa: TOTP secret for user %s re-encrypted with current key", row.UserID)
			}
		}
	}
	return string(b), nil
}

func (s *MFAService) getTOTPAlgorithm(ctx context.Context) otp.Algorithm {
	algStr, err := s.settingsRepo.Get(ctx, "totp_algorithm")
	if err != nil || algStr == "" {
		algStr = "SHA256"
	}
	return stringToOTPAlgorithm(algStr)
}

func stringToOTPAlgorithm(s string) otp.Algorithm {
	if strings.EqualFold(s, "SHA512") {
		return otp.AlgorithmSHA512
	}
	return otp.AlgorithmSHA256
}

func otpAlgorithmToString(a otp.Algorithm) string {
	if a == otp.AlgorithmSHA512 {
		return "SHA512"
	}
	return "SHA256"
}

func generateBackupCodes() (plain, hashed []string, err error) {
	for range backupCodeNum {
		b := make([]byte, 5)
		if _, err = rand.Read(b); err != nil {
			return nil, nil, fmt.Errorf("backup code rand: %w", err)
		}
		code := strings.ToUpper(hex.EncodeToString(b))
		plain = append(plain, code[:5]+"-"+code[5:])
		hash, bcryptErr := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if bcryptErr != nil {
			return nil, nil, fmt.Errorf("backup code hash: %w", bcryptErr)
		}
		hashed = append(hashed, string(hash))
	}
	return plain, hashed, nil
}

func verifyBackupCode(plain, hashed string) bool {
	// bcrypt hashes start with "$2". Support legacy unsalted SHA-256
	// (hex-encoded, 64 chars) for a transition period.
	if !strings.HasPrefix(hashed, "$2") {
		h := sha256.Sum256([]byte(plain))
		candidate := hex.EncodeToString(h[:])
		if len(candidate) != len(hashed) {
			return false
		}
		var diff byte
		for i := range candidate {
			diff |= candidate[i] ^ hashed[i]
		}
		return diff == 0
	}
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}
