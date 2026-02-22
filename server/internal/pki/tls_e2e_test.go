// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package pki_test

import (
    "crypto/rand"
    "crypto/rsa"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "fmt"
    "net"
    "net/http"
    "os"
    "testing"
    "time"

    "github.com/VuteTech/Bor/server/internal/pki"
)

func TestFullTLSChain(t *testing.T) {
    caDir := t.TempDir()
    uiDir := t.TempDir()
    agentDir := t.TempDir()

    // Step 1: Generate CA
    caCertPath, caKeyPath, err := pki.EnsureCA(caDir)
    if err != nil {
        t.Fatal("EnsureCA:", err)
    }
    caCert, caKey, err := pki.LoadCA(caCertPath, caKeyPath)
    if err != nil {
        t.Fatal("LoadCA:", err)
    }
    caCertPool, err := pki.LoadCACertPool(caCertPath)
    if err != nil {
        t.Fatal("LoadCACertPool:", err)
    }

    // Step 2: Generate server cert signed by CA
    certPath, keyPath, err := pki.EnsureServerCert(uiDir, caCert, caKey)
    if err != nil {
        t.Fatal("EnsureServerCert:", err)
    }

    // Step 3: Generate agent cert (simulating enrollment)
    agentKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal("GenerateKey:", err)
    }
    csrTmpl := &x509.CertificateRequest{
        Subject: pkix.Name{CommonName: "test-agent", Organization: []string{"Bor Agent"}},
    }
    csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTmpl, agentKey)
    if err != nil {
        t.Fatal("CreateCertificateRequest:", err)
    }
    csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
    agentCertPEM, err := pki.SignCSR(csrPEM, caCert, caKey)
    if err != nil {
        t.Fatal("SignCSR:", err)
    }

    // Save agent artifacts
    agentCertFile := agentDir + "/agent.crt"
    agentKeyFile := agentDir + "/agent.key"
    agentCACertFile := agentDir + "/ca.crt"
    if err := os.WriteFile(agentCertFile, agentCertPEM, 0644); err != nil {
        t.Fatal("WriteFile agent cert:", err)
    }
    if err := os.WriteFile(agentKeyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(agentKey)}), 0600); err != nil {
        t.Fatal("WriteFile agent key:", err)
    }
    caCertPEM := pki.EncodeCertPEM(caCert)
    if err := os.WriteFile(agentCACertFile, caCertPEM, 0644); err != nil {
        t.Fatal("WriteFile CA cert:", err)
    }

    // Step 4: Create server
    serverTLSCert, err := pki.LoadTLSCert(certPath, keyPath)
    if err != nil {
        t.Fatal("LoadTLSCert:", err)
    }

    serverTLSConfig := &tls.Config{
        Certificates: []tls.Certificate{serverTLSCert},
        ClientCAs:    caCertPool,
        ClientAuth:   tls.VerifyClientCertIfGiven,
        MinVersion:   tls.VersionTLS12,
        NextProtos:   []string{"h2", "http/1.1"},
    }

    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatal("Listen:", err)
    }
    tlsListener := tls.NewListener(listener, serverTLSConfig)

    server := &http.Server{
        Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            fmt.Fprintf(w, "OK")
        }),
    }
    go server.Serve(tlsListener)
    defer server.Close()

    addr := listener.Addr().String()
    time.Sleep(100 * time.Millisecond)

    // Step 5: Agent connects with CA cert
    agentCAPEM2, err := os.ReadFile(agentCACertFile)
    if err != nil {
        t.Fatal("ReadFile agent CA cert:", err)
    }
    agentCAPool := x509.NewCertPool()
    if !agentCAPool.AppendCertsFromPEM(agentCAPEM2) {
        t.Fatal("Failed to parse agent CA cert")
    }

    agentTLSCert, err := tls.LoadX509KeyPair(agentCertFile, agentKeyFile)
    if err != nil {
        t.Fatal("LoadX509KeyPair agent:", err)
    }

    clientTLSConfig := &tls.Config{
        RootCAs:      agentCAPool,
        Certificates: []tls.Certificate{agentTLSCert},
        MinVersion:   tls.VersionTLS12,
        ServerName:   "localhost", // Important: must match server cert SAN
    }

    conn, err := tls.Dial("tcp", addr, clientTLSConfig)
    if err != nil {
        t.Fatalf("TLS dial with CA cert failed: %v", err)
    }
    conn.Close()
    t.Log("PASS: TLS connection with CA cert succeeded")

    // Also verify cert chain details
    serverCertPEM2, err := os.ReadFile(certPath)
    if err != nil {
        t.Fatal("ReadFile server cert:", err)
    }
    serverBlock, _ := pem.Decode(serverCertPEM2)
    if serverBlock == nil {
        t.Fatal("Failed to decode server cert PEM")
    }
    serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
    if err != nil {
        t.Fatal("ParseCertificate server:", err)
    }

    t.Logf("Server cert issuer:  %s", serverCert.Issuer.CommonName)
    t.Logf("Server cert subject: %s", serverCert.Subject.CommonName)
    t.Logf("CA cert subject:     %s", caCert.Subject.CommonName)
    t.Logf("Server cert DNSNames: %v", serverCert.DNSNames)
}
