// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type testAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func (a *testAuthority) pem() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw})
}

func newAuthority(t *testing.T, cn string) *testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testAuthority{cert: cert, key: key}
}

// issue returns a certificate and key in PEM for cn, for server or client
// authentication.
func (a *testAuthority) issue(t *testing.T, cn string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, key.Public(), a.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// heartbeatServer answers heartbeats and remembers the client certificate
// of the last one.
type heartbeatServer struct {
	pb.UnimplementedPolicyServiceServer
	mu       sync.Mutex
	lastCN   string
	lastSHA  string
	response *pb.HeartbeatResponse
}

func (s *heartbeatServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := peer.FromContext(ctx); ok {
		if info, isTLS := p.AuthInfo.(credentials.TLSInfo); isTLS && len(info.State.PeerCertificates) > 0 {
			s.lastCN = info.State.PeerCertificates[0].Subject.CommonName
		}
	}
	s.lastSHA = req.GetTrustBundleSha256()
	if s.response != nil {
		return s.response, nil
	}
	return &pb.HeartbeatResponse{Accepted: true}, nil
}

// serve starts a policy server on addr ("127.0.0.1:0" for a new port) with
// a certificate from ca, accepting client certificates of clientCAs.
func serve(t *testing.T, addr string, ca *testAuthority, clientCAs *x509.CertPool, impl *heartbeatServer) (listenAddr string, stop func()) {
	t.Helper()
	certPEM, keyPEM := ca.issue(t, "server", x509.ExtKeyUsageServerAuth)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	var ln net.Listener
	for i := 0; i < 50; i++ { // the port of a just-stopped server may linger
		if ln, err = net.Listen("tcp", addr); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listen on %s: %v", addr, err)
	}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	})))
	pb.RegisterPolicyServiceServer(srv, impl)
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), srv.Stop
}

func heartbeat(c *Client, sha string) (*HeartbeatResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Heartbeat(ctx, &NodeInfo{FQDN: "node-1"}, sha)
}

func pool(cas ...*testAuthority) *x509.CertPool {
	p := x509.NewCertPool()
	for _, ca := range cas {
		p.AddCert(ca.cert)
	}
	return p
}

// An enrolled agent follows a CA replacement without a restart: it trusts
// the new CA once the bundle is on disk, uses a renewed certificate on the
// next connection, and never accepts a server of a CA it was not given.
func TestClient_FollowsCAReplacement(t *testing.T) {
	oldCA, newCA, rogueCA := newAuthority(t, "Old CA"), newAuthority(t, "New CA"), newAuthority(t, "Rogue CA")
	dir := t.TempDir()
	caPath, certPath, keyPath := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "agent.crt"), filepath.Join(dir, "agent.key")
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(caPath, oldCA.pem())
	certPEM, keyPEM := oldCA.issue(t, "node-1", x509.ExtKeyUsageClientAuth)
	write(certPath, certPEM)
	write(keyPath, keyPEM)

	impl := &heartbeatServer{}
	addr, stop := serve(t, "127.0.0.1:0", oldCA, pool(oldCA), impl)
	client, err := New(addr, "node-1", caPath, certPath, keyPath, false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	sha := TrustBundleSHA256(caPath)
	if _, err = heartbeat(client, sha); err != nil {
		t.Fatalf("heartbeat to the old CA's server: %v", err)
	}
	if impl.lastCN != "node-1" || impl.lastSHA != sha || sha == "" {
		t.Errorf("server saw CN %q, trust %q", impl.lastCN, impl.lastSHA)
	}

	// The certificate is valid for 127.0.0.1 only: the same server reached
	// by another name is rejected, although its CA is trusted.
	_, port, _ := net.SplitHostPort(addr)
	byName, err := New(net.JoinHostPort("localhost", port), "node-1", caPath, certPath, keyPath, false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err = heartbeat(byName, sha); err == nil {
		t.Error("the agent accepted a certificate that is not valid for the name it connected to")
	}
	_ = byName.Close()

	// The server hands out the new CA next to the old one.
	impl.response = &pb.HeartbeatResponse{Accepted: true, CaBundlePem: append(oldCA.pem(), newCA.pem()...), RenewCertificate: true}
	res, err := heartbeat(client, sha)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !res.RenewCertificate || len(res.TrustBundlePEM) == 0 {
		t.Fatalf("heartbeat result = %+v", res)
	}
	if err = WriteTrustBundle(caPath, res.TrustBundlePEM); err != nil {
		t.Fatalf("WriteTrustBundle() error = %v", err)
	}
	if TrustBundleSHA256(caPath) == sha {
		t.Error("the trust fingerprint did not change with the bundle")
	}
	impl.response = nil

	// The server restarts on the new CA and still accepts the old CA's
	// client certificates.
	stop()
	_, stop = serve(t, addr, newCA, pool(oldCA, newCA), impl)
	if _, err = heartbeat(client, TrustBundleSHA256(caPath)); err != nil {
		t.Fatalf("heartbeat to the new CA's server with the same client: %v", err)
	}

	// The agent renews: the new certificate is used by the next connection.
	certPEM, keyPEM = newCA.issue(t, "node-1-renewed", x509.ExtKeyUsageClientAuth)
	write(keyPath, keyPEM)
	write(certPath, certPEM)
	stop()
	_, stop = serve(t, addr, newCA, pool(newCA), impl) // the old CA is retired
	if _, err = heartbeat(client, TrustBundleSHA256(caPath)); err != nil {
		t.Fatalf("heartbeat with the renewed certificate: %v", err)
	}
	if impl.lastCN != "node-1-renewed" {
		t.Errorf("server saw the client certificate %q, want the renewed one", impl.lastCN)
	}

	// A server of a CA the agent was never given is rejected, and so is
	// the old CA once the agent no longer trusts it.
	stop()
	_, stop = serve(t, addr, rogueCA, pool(newCA), impl)
	if _, err = heartbeat(client, ""); err == nil {
		t.Error("the agent accepted a server of a CA it does not trust")
	}
	write(caPath, newCA.pem())
	stop()
	_, stop = serve(t, addr, oldCA, pool(newCA), impl)
	defer stop()
	if _, err = heartbeat(client, ""); err == nil {
		t.Error("the agent accepted a server of the CA it stopped trusting")
	}
}

func TestWriteTrustBundle(t *testing.T) {
	ca := newAuthority(t, "CA")
	leafPEM, keyPEM := ca.issue(t, "leaf", x509.ExtKeyUsageServerAuth)
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}

	bad := map[string][]byte{
		"empty":              nil,
		"not PEM":            []byte("hello"),
		"private key":        append(ca.pem(), keyPEM...),
		"end-entity":         leafPEM,
		"broken certificate": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1, 2, 3}}),
	}
	for name, bundle := range bad {
		if err := WriteTrustBundle(path, bundle); err == nil {
			t.Errorf("%s: WriteTrustBundle() succeeded", name)
		}
		if got, _ := os.ReadFile(path); string(got) != "ORIGINAL" {
			t.Fatalf("%s: a rejected bundle changed the CA file", name)
		}
	}

	good := append(ca.pem(), newAuthority(t, "CA 2").pem()...)
	if err := WriteTrustBundle(path, good); err != nil {
		t.Fatalf("WriteTrustBundle() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, good) {
		t.Errorf("CA file after the update: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("CA file mode = %o, want 644", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
	if TrustBundleSHA256(filepath.Join(t.TempDir(), "absent")) != "" {
		t.Error("TrustBundleSHA256() of a missing file is not empty")
	}
}
