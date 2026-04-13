// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	enrollpb "github.com/VuteTech/Bor/server/pkg/grpc/enrollment"
	krb5client "github.com/jcmturner/gokrb5/v8/client"
	krb5cfg "github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// EnrollmentPaths holds the paths to persisted enrollment artifacts.
type EnrollmentPaths struct {
	CertFile string // agent client certificate (signed by server CA)
	KeyFile  string // agent private key
	CACert   string // server CA certificate
}

// DefaultPaths returns the standard paths inside the given data directory.
func DefaultPaths(dataDir string) EnrollmentPaths {
	return EnrollmentPaths{
		CertFile: filepath.Join(dataDir, "agent.crt"),
		KeyFile:  filepath.Join(dataDir, "agent.key"),
		CACert:   filepath.Join(dataDir, "ca.crt"),
	}
}

// IsEnrolled returns true if the agent cert and key exist on disk.
func IsEnrolled(paths EnrollmentPaths) bool {
	_, errC := os.Stat(paths.CertFile)
	_, errK := os.Stat(paths.KeyFile)
	_, errCA := os.Stat(paths.CACert)
	return errC == nil && errK == nil && errCA == nil
}

// RemoveEnrollmentCerts deletes the certificate, private key, and CA
// certificate from disk so that the agent can be re-enrolled. Missing
// files are silently ignored.
func RemoveEnrollmentCerts(paths EnrollmentPaths) error {
	for _, p := range []string{paths.CertFile, paths.KeyFile, paths.CACert} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", p, err)
		}
	}
	return nil
}

// Enroll performs the one-time enrollment flow:
//  1. Generate an RSA 2048 key pair.
//  2. Create a CSR with the agent's node name.
//  3. Connect to the server (TLS, optionally skip-verify for self-signed).
//  4. Call EnrollmentService.Enroll with the token + CSR.
//  5. Persist the signed cert, private key, and CA cert to disk.
func Enroll(serverAddr, token, nodeName string, insecureSkipVerify bool, paths EnrollmentPaths) error {
	if err := os.MkdirAll(filepath.Dir(paths.CertFile), 0o700); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	// 1. Generate ECDSA P-256 key pair (FIPS 140-3, BSI TR-02102-1, ANSSI, ENISA approved).
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDSA P-256 key: %w", err)
	}

	// 2. Create CSR
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   nodeName,
			Organization: []string{"Bor Agent"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// 3. Connect to server with TLS (no client cert yet — this is the bootstrap call)
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // G402: controlled by admin config, only used for initial enrollment
	}
	if insecureSkipVerify {
		log.Println("WARNING: server certificate verification disabled for enrollment")
	}

	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	)
	if err != nil {
		return fmt.Errorf("failed to connect for enrollment: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// 4. Call Enroll RPC
	client := enrollpb.NewEnrollmentServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Enroll(ctx, &enrollpb.EnrollRequest{
		EnrollmentToken: token,
		CsrPem:          csrPEM,
		NodeName:        nodeName,
	})
	if err != nil {
		return fmt.Errorf("enrollment RPC failed: %w", err)
	}

	log.Printf("Enrolled successfully: node_group=%s", resp.GetAssignedNodeGroup())

	// 5. Persist artifacts
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal agent key: %w", err)
	}
	if err := writeFile(paths.KeyFile, pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	}), 0o600); err != nil {
		return fmt.Errorf("failed to save agent key: %w", err)
	}

	if err := writeFile(paths.CertFile, resp.GetSignedCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save agent cert: %w", err)
	}

	if err := writeFile(paths.CACert, resp.GetCaCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save CA cert: %w", err)
	}

	return nil
}

// EnrollWithKerberos performs token-free enrollment using the machine keytab.
// The agent must be domain-joined (FreeIPA or Active Directory / Samba AD).
//
// Flow:
//  1. Load Kerberos configuration (from kdc arg or /etc/krb5.conf).
//  2. Load the machine keytab.
//  3. Determine the host principal (host/<hostname>@<realm>).
//  4. Authenticate using the keytab to obtain a TGT.
//  5. Acquire a service ticket for servicePrincipal.
//  6. Build a SPNEGO NegTokenInit and marshal it to DER bytes.
//  7. Call KerberosEnroll with the SPNEGO token and a fresh CSR.
//  8. Persist the signed cert, key, and CA cert.
//
// kdc is optional: when non-empty the Kerberos config is built from kdc and
// the realm embedded in servicePrincipal, bypassing /etc/krb5.conf entirely.
func EnrollWithKerberos(serverAddr, keytabPath, servicePrincipal, kdc, machinePrincipal, nodeName string,
	insecureSkipVerify bool, paths EnrollmentPaths) error {

	if err := os.MkdirAll(filepath.Dir(paths.CertFile), 0o700); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	// ── 1. Load Kerberos config ───────────────────────────────────────────────
	// Extract realm from servicePrincipal ("SVC/host@REALM").
	realm := realmFromPrincipal(servicePrincipal)

	var krb5Conf *krb5cfg.Config
	var err error
	if kdc != "" {
		// Build a minimal config from the supplied KDC address — no dependency
		// on /etc/krb5.conf, which may have syntax incompatible with gokrb5.
		cfgStr := fmt.Sprintf(
			"[libdefaults]\n default_realm = %s\n[realms]\n %s = {\n  kdc = %s\n }\n",
			realm, realm, kdc,
		)
		krb5Conf, err = krb5cfg.NewFromString(cfgStr)
		if err != nil {
			return fmt.Errorf("failed to build Kerberos config for KDC %s: %w", kdc, err)
		}
	} else {
		krb5Conf, err = loadKrb5Config("/etc/krb5.conf")
		if err != nil {
			return fmt.Errorf("failed to load /etc/krb5.conf: %w", err)
		}
		if realm == "" {
			realm = krb5Conf.LibDefaults.DefaultRealm
		}
	}
	if realm == "" {
		return fmt.Errorf("cannot determine Kerberos realm: set kdc in config or ensure /etc/krb5.conf has default_realm")
	}

	// ── 2. Load machine keytab ───────────────────────────────────────────────
	kt, err := keytab.Load(keytabPath)
	if err != nil {
		return fmt.Errorf("failed to load keytab %s: %w", keytabPath, err)
	}

	// ── 3. Determine host principal ──────────────────────────────────────────

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}
	if nodeName == "" {
		nodeName = hostname
	}

	// Machine account principal: use explicit config value when provided,
	// otherwise default to "host/<hostname>" (correct for FreeIPA).
	// AD / Samba AD hosts joined with realm(1) or adcli often use
	// "<HOSTNAME>$" — run "klist -kte /etc/krb5.keytab" to find the name.
	// Strip the "@REALM" suffix if included — gokrb5 takes realm separately.
	hostPrincipal := machinePrincipal
	if hostPrincipal == "" {
		hostPrincipal = "host/" + hostname
	} else if at := strings.LastIndex(hostPrincipal, "@"); at >= 0 {
		hostPrincipal = hostPrincipal[:at]
	}

	// ── 4. Authenticate with keytab ─────────────────────────────────────────
	cl := krb5client.NewWithKeytab(hostPrincipal, realm, kt, krb5Conf,
		krb5client.DisablePAFXFAST(true))
	if loginErr := cl.Login(); loginErr != nil {
		return fmt.Errorf("kerberos login failed (keytab=%s, principal=%s@%s): %w",
			keytabPath, hostPrincipal, realm, loginErr)
	}
	defer cl.Destroy()

	// ── 5. Acquire service ticket ────────────────────────────────────────────
	// gokrb5 GetServiceTicket calls NewPrincipalName which splits on "/" only,
	// so "HTTP/localhost@REALM" would produce NameString=["HTTP","localhost@REALM"].
	// Strip the "@REALM" suffix before passing to GetServiceTicket; the realm is
	// already known to the client from the krb5 config.
	spnForTicket := servicePrincipal
	if at := strings.LastIndex(spnForTicket, "@"); at >= 0 {
		spnForTicket = spnForTicket[:at]
	}
	tkt, sessionKey, err := cl.GetServiceTicket(spnForTicket)
	if err != nil {
		return fmt.Errorf("failed to get service ticket for %s: %w", servicePrincipal, err)
	}

	// ── 6. Build SPNEGO NegTokenInit ─────────────────────────────────────────
	negToken, err := spnego.NewNegTokenInitKRB5(cl, tkt, sessionKey)
	if err != nil {
		return fmt.Errorf("failed to create SPNEGO token: %w", err)
	}
	spnegoTok := spnego.SPNEGOToken{
		Init:         true,
		NegTokenInit: negToken,
	}
	spnegoBytes, err := spnegoTok.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal SPNEGO token: %w", err)
	}

	// ── 7. Generate key pair and CSR ─────────────────────────────────────────
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDSA P-256 key: %w", err)
	}
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   nodeName,
			Organization: []string{"Bor Agent"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// ── 8. Connect and call KerberosEnroll ───────────────────────────────────
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // G402: controlled by admin config
	}
	if insecureSkipVerify {
		log.Println("WARNING: server certificate verification disabled for Kerberos enrollment")
	}

	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	)
	if err != nil {
		return fmt.Errorf("failed to connect for Kerberos enrollment: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client := enrollpb.NewEnrollmentServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.KerberosEnroll(ctx, &enrollpb.KerberosEnrollRequest{
		SpnegoToken: spnegoBytes,
		CsrPem:      csrPEM,
		NodeName:    nodeName,
	})
	if err != nil {
		return fmt.Errorf("KerberosEnroll RPC failed: %w", err)
	}

	log.Printf("Kerberos enrollment successful: principal=%s@%s node_group=%s",
		hostPrincipal, realm, resp.GetAssignedNodeGroup())

	// ── 9. Persist artifacts ─────────────────────────────────────────────────
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal agent key: %w", err)
	}
	if err := writeFile(paths.KeyFile, pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	}), 0o600); err != nil {
		return fmt.Errorf("failed to save agent key: %w", err)
	}
	if err := writeFile(paths.CertFile, resp.GetSignedCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save agent cert: %w", err)
	}
	if err := writeFile(paths.CACert, resp.GetCaCertPem(), 0o644); err != nil {
		return fmt.Errorf("failed to save CA cert: %w", err)
	}

	return nil
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

// realmFromPrincipal extracts the realm from a Kerberos principal string of
// the form "service/host@REALM" or "user@REALM".  Returns "" if not found.
func realmFromPrincipal(principal string) string {
	for i := len(principal) - 1; i >= 0; i-- {
		if principal[i] == '@' {
			return principal[i+1:]
		}
	}
	return ""
}

// nonBoolKrb5Setting matches krb5.conf lines where a known boolean setting
// has a non-boolean value (e.g. "dns_canonicalize_hostname = fallback").
// gokrb5 only accepts "true"/"false"; "fallback" is a newer MIT Kerberos
// extension. We normalise such values to "true" before parsing.
var nonBoolKrb5Setting = regexp.MustCompile(
	`(?i)(dns_canonicalize_hostname\s*=\s*)(?:fallback|auto)`,
)

// loadKrb5Config reads /etc/krb5.conf, sanitises non-boolean values that
// gokrb5 cannot parse (notably "dns_canonicalize_hostname = fallback" used
// on Fedora/RHEL), and returns a parsed *krb5cfg.Config.
func loadKrb5Config(path string) (*krb5cfg.Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path is a fixed system config path (/etc/krb5.conf)
	if err != nil {
		return nil, err
	}
	sanitised := nonBoolKrb5Setting.ReplaceAllString(string(raw), "${1}true")
	return krb5cfg.NewFromString(sanitised)
}
