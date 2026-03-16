// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// webAuthnUser implements the webauthn.User interface.
type webAuthnUser struct {
	id          []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte                         { return u.id }
func (u *webAuthnUser) WebAuthnName() string                       { return u.name }
func (u *webAuthnUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// WebAuthnService handles WebAuthn registration and authentication.
type WebAuthnService struct {
	repo *database.WebAuthnRepository
	wa   *webauthn.WebAuthn
}

// NewWebAuthnService creates a WebAuthnService. Returns an error if RPID is empty.
func NewWebAuthnService(repo *database.WebAuthnRepository, rpid, displayName string, origins []string) (*WebAuthnService, error) {
	if rpid == "" {
		return nil, fmt.Errorf("WebAuthn RPID must not be empty")
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpid,
		RPDisplayName: displayName,
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, fmt.Errorf("create webauthn instance: %w", err)
	}
	return &WebAuthnService{repo: repo, wa: wa}, nil
}

// buildUser loads a user's credentials from DB and returns a webAuthnUser.
func (s *WebAuthnService) buildUser(ctx context.Context, userID, username string) (*webAuthnUser, error) {
	rows, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	creds := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		rawID, err := base64.RawURLEncoding.DecodeString(row.CredentialID)
		if err != nil {
			continue
		}
		transports := make([]protocol.AuthenticatorTransport, len(row.Transports))
		for i, t := range row.Transports {
			transports[i] = protocol.AuthenticatorTransport(t)
		}
		creds = append(creds, webauthn.Credential{
			ID:        rawID,
			PublicKey: row.PublicKey,
			Flags: webauthn.CredentialFlags{
				BackupEligible: row.BackupEligible,
				BackupState:    row.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    []byte(row.AAGUID),
				SignCount: row.SignCount,
			},
			Transport: transports,
		})
	}
	return &webAuthnUser{
		id:          []byte(userID),
		name:        username,
		displayName: username,
		credentials: creds,
	}, nil
}

// BeginRegistration starts the WebAuthn registration ceremony.
// Returns JSON-encoded PublicKeyCredentialCreationOptions.
func (s *WebAuthnService) BeginRegistration(ctx context.Context, userID, username string) ([]byte, error) {
	user, err := s.buildUser(ctx, userID, username)
	if err != nil {
		return nil, fmt.Errorf("build webauthn user: %w", err)
	}

	options, sessionData, err := s.wa.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("begin webauthn registration: %w", err)
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return nil, fmt.Errorf("marshal webauthn session: %w", err)
	}

	err = s.repo.CreateSession(ctx, &database.WebAuthnSessionRow{
		UserID:      userID,
		SessionType: "registration",
		SessionData: string(sessionJSON),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		return nil, fmt.Errorf("store webauthn session: %w", err)
	}

	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("marshal webauthn options: %w", err)
	}
	return optionsJSON, nil
}

// FinishRegistration completes the WebAuthn registration ceremony.
func (s *WebAuthnService) FinishRegistration(ctx context.Context, userID, username, credentialName string, responseJSON []byte) (*models.WebAuthnCredential, error) {
	sessionRow, err := s.repo.GetSession(ctx, userID, "registration")
	if err != nil {
		return nil, fmt.Errorf("get webauthn session: %w", err)
	}
	if sessionRow == nil {
		return nil, fmt.Errorf("no active registration session")
	}

	var sessionData webauthn.SessionData
	err = json.Unmarshal([]byte(sessionRow.SessionData), &sessionData)
	if err != nil {
		return nil, fmt.Errorf("unmarshal webauthn session data: %w", err)
	}

	user, err := s.buildUser(ctx, userID, username)
	if err != nil {
		return nil, fmt.Errorf("build webauthn user: %w", err)
	}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(responseJSON))
	if err != nil {
		return nil, fmt.Errorf("parse credential creation response: %w", err)
	}

	credential, err := s.wa.CreateCredential(user, sessionData, parsedResponse)
	if err != nil {
		return nil, fmt.Errorf("finish webauthn registration: %w", err)
	}

	// Delete the used session.
	_ = s.repo.DeleteSession(ctx, userID, "registration")

	// Extract AAGUID as hex string.
	aaguid := base64.RawURLEncoding.EncodeToString(credential.Authenticator.AAGUID)

	// Extract transports.
	transports := make([]string, len(credential.Transport))
	for i, t := range credential.Transport {
		transports[i] = string(t)
	}

	name := credentialName
	if name == "" {
		name = "Security Key"
	}

	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)

	row := &database.WebAuthnCredentialRow{
		UserID:         userID,
		CredentialID:   credentialID,
		PublicKey:      credential.PublicKey,
		AAGUID:         aaguid,
		SignCount:      credential.Authenticator.SignCount,
		Name:           name,
		Transports:     transports,
		BackupEligible: credential.Flags.BackupEligible,
		BackupState:    credential.Flags.BackupState,
	}
	err = s.repo.Create(ctx, row)
	if err != nil {
		return nil, fmt.Errorf("store webauthn credential: %w", err)
	}

	// Fetch the saved credential to get the DB-generated ID and timestamps.
	saved, err := s.repo.GetByCredentialID(ctx, credentialID)
	if err != nil || saved == nil {
		return nil, fmt.Errorf("retrieve saved credential: %w", err)
	}

	return &models.WebAuthnCredential{
		ID:         saved.ID,
		Name:       saved.Name,
		AAGUID:     saved.AAGUID,
		Transports: saved.Transports,
		CreatedAt:  saved.CreatedAt,
	}, nil
}

// BeginAuthentication starts the WebAuthn authentication ceremony.
// Returns JSON-encoded PublicKeyCredentialRequestOptions.
func (s *WebAuthnService) BeginAuthentication(ctx context.Context, userID string) ([]byte, error) {
	// Load user credentials from DB.
	rows, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list webauthn credentials: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no WebAuthn credentials registered")
	}

	user := &webAuthnUser{
		id:          []byte(userID),
		name:        userID,
		displayName: userID,
	}
	for _, row := range rows {
		rawID, decodeErr := base64.RawURLEncoding.DecodeString(row.CredentialID)
		if decodeErr != nil {
			continue
		}
		transports := make([]protocol.AuthenticatorTransport, len(row.Transports))
		for i, t := range row.Transports {
			transports[i] = protocol.AuthenticatorTransport(t)
		}
		user.credentials = append(user.credentials, webauthn.Credential{
			ID:        rawID,
			PublicKey: row.PublicKey,
			Flags: webauthn.CredentialFlags{
				BackupEligible: row.BackupEligible,
				BackupState:    row.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    []byte(row.AAGUID),
				SignCount: row.SignCount,
			},
			Transport: transports,
		})
	}

	options, sessionData, err := s.wa.BeginLogin(user)
	if err != nil {
		return nil, fmt.Errorf("begin webauthn login: %w", err)
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return nil, fmt.Errorf("marshal webauthn session: %w", err)
	}

	err = s.repo.CreateSession(ctx, &database.WebAuthnSessionRow{
		UserID:      userID,
		SessionType: "authentication",
		SessionData: string(sessionJSON),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		return nil, fmt.Errorf("store webauthn session: %w", err)
	}

	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("marshal webauthn options: %w", err)
	}
	return optionsJSON, nil
}

// FinishAuthentication completes the WebAuthn authentication ceremony.
func (s *WebAuthnService) FinishAuthentication(ctx context.Context, userID string, responseJSON []byte) error {
	sessionRow, err := s.repo.GetSession(ctx, userID, "authentication")
	if err != nil {
		return fmt.Errorf("get webauthn session: %w", err)
	}
	if sessionRow == nil {
		return fmt.Errorf("no active authentication session")
	}

	var sessionData webauthn.SessionData
	err = json.Unmarshal([]byte(sessionRow.SessionData), &sessionData)
	if err != nil {
		return fmt.Errorf("unmarshal webauthn session data: %w", err)
	}

	// Build user with credentials.
	rows, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("list webauthn credentials: %w", err)
	}
	user := &webAuthnUser{
		id:          []byte(userID),
		name:        userID,
		displayName: userID,
	}
	for _, row := range rows {
		rawID, decodeErr := base64.RawURLEncoding.DecodeString(row.CredentialID)
		if decodeErr != nil {
			continue
		}
		transports := make([]protocol.AuthenticatorTransport, len(row.Transports))
		for i, t := range row.Transports {
			transports[i] = protocol.AuthenticatorTransport(t)
		}
		user.credentials = append(user.credentials, webauthn.Credential{
			ID:        rawID,
			PublicKey: row.PublicKey,
			Flags: webauthn.CredentialFlags{
				BackupEligible: row.BackupEligible,
				BackupState:    row.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    []byte(row.AAGUID),
				SignCount: row.SignCount,
			},
			Transport: transports,
		})
	}

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(responseJSON))
	if err != nil {
		return fmt.Errorf("parse credential request response: %w", err)
	}

	credential, err := s.wa.ValidateLogin(user, sessionData, parsedResponse)
	if err != nil {
		return fmt.Errorf("finish webauthn login: %w", err)
	}

	// Delete the used session.
	_ = s.repo.DeleteSession(ctx, userID, "authentication")

	// Update sign count and backup state in DB.
	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
	if err := s.repo.UpdateAfterLogin(ctx, credentialID, credential.Authenticator.SignCount, credential.Flags.BackupState); err != nil {
		return fmt.Errorf("update credential after login: %w", err)
	}

	return nil
}

// ListCredentials returns the WebAuthn credentials for a user.
func (s *WebAuthnService) ListCredentials(ctx context.Context, userID string) ([]*models.WebAuthnCredential, error) {
	rows, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]*models.WebAuthnCredential, 0, len(rows))
	for _, row := range rows {
		cred := &models.WebAuthnCredential{
			ID:         row.ID,
			Name:       row.Name,
			AAGUID:     row.AAGUID,
			Transports: row.Transports,
			CreatedAt:  row.CreatedAt,
			LastUsedAt: row.LastUsedAt,
		}
		result = append(result, cred)
	}
	return result, nil
}

// RenameCredential renames a credential for a user.
func (s *WebAuthnService) RenameCredential(ctx context.Context, id, userID, name string) error {
	return s.repo.Rename(ctx, id, userID, name)
}

// DeleteCredential deletes a credential for a user.
func (s *WebAuthnService) DeleteCredential(ctx context.Context, id, userID string) error {
	return s.repo.Delete(ctx, id, userID)
}

// HasCredentials returns true if the user has at least one registered credential.
func (s *WebAuthnService) HasCredentials(ctx context.Context, userID string) (bool, error) {
	rows, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}
