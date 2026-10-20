// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

// The relying party ID must be a DNS name. The WebAuthn library rejects an
// IP address or a malformed name when the service is created, so a bad
// configuration fails at startup instead of when a user first tries a key.
func TestNewWebAuthnService_RPID(t *testing.T) {
	tests := []struct {
		name    string
		rpid    string
		origins []string
		wantErr bool
	}{
		{name: "dns name with explicit origin", rpid: "bor.example.com", origins: []string{"https://bor.example.com:8443"}},
		{name: "localhost with default origin", rpid: "localhost"},
		{name: "empty", rpid: "", wantErr: true},
		{name: "ipv4", rpid: "192.168.1.10", origins: []string{"https://192.168.1.10:8443"}, wantErr: true},
		{name: "ipv6", rpid: "2001:db8::1", wantErr: true},
		{name: "trailing dot", rpid: "bor.example.com.", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWebAuthnService(nil, tc.rpid, "Bor", tc.origins)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewWebAuthnService(rpid=%q) error = %v, wantErr %v", tc.rpid, err, tc.wantErr)
			}
		})
	}
}

// memWebAuthnStore is an in-memory WebAuthnStore.
type memWebAuthnStore struct {
	creds    []*database.WebAuthnCredentialRow
	sessions map[string]*database.WebAuthnSessionRow
	nextID   int
}

func newMemWebAuthnStore() *memWebAuthnStore {
	return &memWebAuthnStore{sessions: map[string]*database.WebAuthnSessionRow{}}
}

func (m *memWebAuthnStore) ListByUserID(_ context.Context, userID string) ([]*database.WebAuthnCredentialRow, error) {
	var out []*database.WebAuthnCredentialRow
	for _, c := range m.creds {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memWebAuthnStore) GetByCredentialID(_ context.Context, credentialID string) (*database.WebAuthnCredentialRow, error) {
	for _, c := range m.creds {
		if c.CredentialID == credentialID {
			return c, nil
		}
	}
	return nil, fmt.Errorf("credential %q not found", credentialID)
}

func (m *memWebAuthnStore) Create(_ context.Context, row *database.WebAuthnCredentialRow) error {
	m.nextID++
	saved := *row
	saved.ID = fmt.Sprintf("cred-%d", m.nextID)
	saved.CreatedAt = time.Now()
	m.creds = append(m.creds, &saved)
	return nil
}

func (m *memWebAuthnStore) UpdateAfterLogin(_ context.Context, credentialID string, signCount uint32, backupState bool) error {
	for _, c := range m.creds {
		if c.CredentialID != credentialID {
			continue
		}
		c.SignCount = signCount
		c.BackupState = backupState
		now := time.Now()
		c.LastUsedAt = &now
		return nil
	}
	return fmt.Errorf("credential %q not found", credentialID)
}

func (m *memWebAuthnStore) Rename(_ context.Context, id, userID, name string) error {
	for _, c := range m.creds {
		if c.ID == id && c.UserID == userID {
			c.Name = name
			return nil
		}
	}
	return fmt.Errorf("credential %q not found", id)
}

func (m *memWebAuthnStore) Delete(_ context.Context, id, userID string) error {
	for i, c := range m.creds {
		if c.ID == id && c.UserID == userID {
			m.creds = append(m.creds[:i], m.creds[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("credential %q not found", id)
}

func (m *memWebAuthnStore) CreateSession(_ context.Context, row *database.WebAuthnSessionRow) error {
	m.sessions[row.UserID+"/"+row.SessionType] = row
	return nil
}

func (m *memWebAuthnStore) GetSession(_ context.Context, userID, sessionType string) (*database.WebAuthnSessionRow, error) {
	return m.sessions[userID+"/"+sessionType], nil
}

func (m *memWebAuthnStore) DeleteSession(_ context.Context, userID, sessionType string) error {
	delete(m.sessions, userID+"/"+sessionType)
	return nil
}

// softKey is a minimal software authenticator: one P-256 key and one
// credential, answering ceremonies for a fixed browser origin with a "none"
// attestation, the way a plain FIDO2 key does.
type softKey struct {
	priv   *ecdsa.PrivateKey
	credID []byte
	origin string
	count  uint32
}

const softKeyCredIDLen = 32

func newSoftKey(t *testing.T, origin string) *softKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	credID := make([]byte, softKeyCredIDLen)
	if _, err := rand.Read(credID); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	return &softKey{priv: priv, credID: credID, origin: origin}
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (k *softKey) clientData(t *testing.T, typ string, challenge []byte) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": typ, "challenge": b64url(challenge), "origin": k.origin, "crossOrigin": false})
	if err != nil {
		t.Fatalf("client data: %v", err)
	}
	return b
}

// register answers PublicKeyCredentialCreationOptions (as the service emits
// them) with an attestation response for a new credential.
func (k *softKey) register(t *testing.T, optionsJSON []byte) []byte {
	t.Helper()
	var cc protocol.CredentialCreation
	if err := json.Unmarshal(optionsJSON, &cc); err != nil {
		t.Fatalf("parse creation options: %v", err)
	}
	rpIDHash := sha256.Sum256([]byte(cc.Response.RelyingParty.ID))
	point, err := k.priv.PublicKey.Bytes() // uncompressed: 0x04 || X || Y
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	coseKey, err := webauthncbor.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: point[1:33],
		-3: point[33:65],
	})
	if err != nil {
		t.Fatalf("encode COSE key: %v", err)
	}
	authData := append([]byte{}, rpIDHash[:]...)
	authData = append(authData, 0x45) // user present, user verified, attested credential data
	authData = binary.BigEndian.AppendUint32(authData, k.count)
	authData = append(authData, make([]byte, 16)...) // AAGUID
	authData = binary.BigEndian.AppendUint16(authData, softKeyCredIDLen)
	authData = append(authData, k.credID...)
	authData = append(authData, coseKey...)
	attObj, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		t.Fatalf("encode attestation object: %v", err)
	}
	resp, err := json.Marshal(map[string]any{
		"id":    b64url(k.credID),
		"rawId": b64url(k.credID),
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": b64url(attObj),
			"clientDataJSON":    b64url(k.clientData(t, "webauthn.create", cc.Response.Challenge)),
			"transports":        []string{"usb"},
		},
		"clientExtensionResults": map[string]any{},
	})
	if err != nil {
		t.Fatalf("encode registration response: %v", err)
	}
	return resp
}

// assert answers PublicKeyCredentialRequestOptions with a signed assertion.
func (k *softKey) assert(t *testing.T, optionsJSON []byte, userID string) []byte {
	t.Helper()
	var ca protocol.CredentialAssertion
	if err := json.Unmarshal(optionsJSON, &ca); err != nil {
		t.Fatalf("parse request options: %v", err)
	}
	allowed := false
	for _, c := range ca.Response.AllowedCredentials {
		if bytes.Equal(c.CredentialID, k.credID) {
			allowed = true
		}
	}
	if !allowed {
		t.Fatalf("allowCredentials does not list the registered credential")
	}
	rpIDHash := sha256.Sum256([]byte(ca.Response.RelyingPartyID))
	k.count++
	authData := append([]byte{}, rpIDHash[:]...)
	authData = append(authData, 0x05) // user present, user verified
	authData = binary.BigEndian.AppendUint32(authData, k.count)
	clientData := k.clientData(t, "webauthn.get", ca.Response.Challenge)
	cdHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	resp, err := json.Marshal(map[string]any{
		"id":    b64url(k.credID),
		"rawId": b64url(k.credID),
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": b64url(authData),
			"clientDataJSON":    b64url(clientData),
			"signature":         b64url(sig),
			"userHandle":        b64url([]byte(userID)),
		},
		"clientExtensionResults": map[string]any{},
	})
	if err != nil {
		t.Fatalf("encode assertion: %v", err)
	}
	return resp
}

// corruptSignature flips a bit in the assertion signature.
func corruptSignature(t *testing.T, assertion []byte) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(assertion, &m); err != nil {
		t.Fatalf("parse assertion: %v", err)
	}
	resp, _ := m["response"].(map[string]any)
	sig, err := base64.RawURLEncoding.DecodeString(resp["signature"].(string))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	sig[len(sig)-1] ^= 0x01
	resp["signature"] = b64url(sig)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode assertion: %v", err)
	}
	return out
}

// TestWebAuthnService_Ceremonies runs a full registration and login through
// the service with a software authenticator, including the JSON round trip of
// the ceremony state the service stores between the two halves.
func TestWebAuthnService_Ceremonies(t *testing.T) {
	ctx := context.Background()
	const origin = "https://localhost:8443"
	const userID, username = "user-1", "alice"
	store := newMemWebAuthnStore()
	svc, err := NewWebAuthnService(store, "localhost", "Bor", []string{origin})
	if err != nil {
		t.Fatalf("NewWebAuthnService: %v", err)
	}
	key := newSoftKey(t, origin)

	// Registration.
	options, err := svc.BeginRegistration(ctx, userID, username)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	cred, err := svc.FinishRegistration(ctx, userID, username, "Desk key", key.register(t, options))
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	if cred.Name != "Desk key" || cred.ID == "" {
		t.Errorf("FinishRegistration returned %+v", cred)
	}
	if _, ok := store.sessions[userID+"/registration"]; ok {
		t.Error("registration session was not consumed")
	}
	if has, hasErr := svc.HasCredentials(ctx, userID); hasErr != nil || !has {
		t.Errorf("HasCredentials = %v, %v; want true", has, hasErr)
	}
	if _, replayErr := svc.FinishRegistration(ctx, userID, username, "again", key.register(t, options)); replayErr == nil {
		t.Error("FinishRegistration accepted a response without a live session")
	}

	// Registration from the wrong origin is refused.
	options, err = svc.BeginRegistration(ctx, userID, username)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if _, originErr := svc.FinishRegistration(ctx, userID, username, "evil", newSoftKey(t, "https://evil.example").register(t, options)); originErr == nil {
		t.Error("FinishRegistration accepted a response from another origin")
	}

	// Login.
	options, err = svc.BeginAuthentication(ctx, userID)
	if err != nil {
		t.Fatalf("BeginAuthentication: %v", err)
	}
	if err = svc.FinishAuthentication(ctx, userID, key.assert(t, options, userID)); err != nil {
		t.Fatalf("FinishAuthentication: %v", err)
	}
	rows, _ := store.ListByUserID(ctx, userID)
	if len(rows) != 1 {
		t.Fatalf("stored credentials = %d, want 1", len(rows))
	}
	if rows[0].SignCount != 1 || rows[0].LastUsedAt == nil {
		t.Errorf("credential after login: sign count %d, last used %v", rows[0].SignCount, rows[0].LastUsedAt)
	}
	if _, ok := store.sessions[userID+"/authentication"]; ok {
		t.Error("authentication session was not consumed")
	}

	// A corrupted signature is refused, and the sign count stays put.
	options, err = svc.BeginAuthentication(ctx, userID)
	if err != nil {
		t.Fatalf("BeginAuthentication: %v", err)
	}
	if err = svc.FinishAuthentication(ctx, userID, corruptSignature(t, key.assert(t, options, userID))); err == nil {
		t.Error("FinishAuthentication accepted a corrupted signature")
	}
	if rows[0].SignCount != 1 {
		t.Errorf("sign count changed to %d after a failed login", rows[0].SignCount)
	}

	// A second successful login advances the counter again.
	options, err = svc.BeginAuthentication(ctx, userID)
	if err != nil {
		t.Fatalf("BeginAuthentication: %v", err)
	}
	if err = svc.FinishAuthentication(ctx, userID, key.assert(t, options, userID)); err != nil {
		t.Fatalf("second FinishAuthentication: %v", err)
	}
	if rows[0].SignCount != 3 {
		t.Errorf("sign count after second login = %d, want 3", rows[0].SignCount)
	}

	// Users without a key cannot start a login ceremony.
	if _, err := svc.BeginAuthentication(ctx, "nobody"); err == nil {
		t.Error("BeginAuthentication succeeded for a user without credentials")
	}
}
