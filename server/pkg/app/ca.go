// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package app

import (
	"bytes"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/VuteTech/Bor/server/internal/config"
	"github.com/VuteTech/Bor/server/internal/pki"
)

// caStore returns where the configured CA keeps its certificate and key.
func caStore(cfg *config.Config) pki.CAStore {
	autogenCert := filepath.Join(cfg.CA.AutogenDir, "ca.crt")
	if cfg.CA.PKCS11.IsConfigured() {
		store := pki.CAStore{CertPath: cfg.CA.CertFile}
		if store.CertPath == "" {
			store.CertPath = autogenCert
		}
		return store
	}
	if cfg.CA.CertFile == "" && cfg.CA.KeyFile == "" {
		return pki.CAStore{CertPath: autogenCert, KeyPath: filepath.Join(cfg.CA.AutogenDir, "ca.key")}
	}
	return pki.CAStore{CertPath: cfg.CA.CertFile, KeyPath: cfg.CA.KeyFile}
}

// agentTrustIncludesIssuers reports whether agents receive the authorities
// above the Bor CA as well ("chain" mode), and logs what that means.
func agentTrustIncludesIssuers(chain *pki.CAChain, mode string) bool {
	if mode != config.AgentTrustChain {
		return false
	}
	if len(chain.Issuers) == 0 {
		log.Printf("BOR_CA_AGENT_TRUST=chain has no effect: the CA certificate file lists no issuer certificates")
		return false
	}
	log.Printf("WARNING: BOR_CA_AGENT_TRUST=chain: agents enrolled from now on trust every certificate authority under %q, "+
		"not only the Bor CA. Any of them can issue a certificate that agents accept as this server",
		chain.Issuers[len(chain.Issuers)-1].Subject)
	return true
}

// withCurrentClientCAs returns a tls.Config.GetConfigForClient callback that
// serves base with the client CA pool as it is at the time of the
// handshake, so a retired CA stops being accepted without a restart.
func withCurrentClientCAs(base *tls.Config, trust *pki.TrustState) func(*tls.ClientHelloInfo) (*tls.Config, error) {
	base = base.Clone()
	return func(*tls.ClientHelloInfo) (*tls.Config, error) {
		cfg := base.Clone()
		cfg.ClientCAs = trust.ClientCAs()
		return cfg, nil
	}
}

const caUsage = `Usage: %[1]s ca <command> [flags]

Have the Bor CA certified by an external certificate authority.

Commands:
  csr     Create a certificate signing request for the CA key
            -cn NAME    common name to request (default: the subject of the
                        current CA certificate, or "Bor CA")
            -out FILE   write the request to FILE instead of standard output
  import  Install the certificate the authority issued
            -in FILE    PEM file: the Bor CA certificate, optionally followed
                        by its issuers up to the root
  status  Show the CA certificate on disk

The CA key is created on first use and never leaves this server (or its
PKCS#11 token). Run the commands as the user the server runs as, with the
server's configuration (BOR_CONFIG and the BOR_CA_* variables). Restart the
server after "import".
`

// CACommand runs the "ca" command line: args are the arguments after "ca".
// Output goes to stdout.
func CACommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprintf(stdout, caUsage, os.Args[0])
		return errors.New("ca: a command is required: csr, import or status")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	store := caStore(cfg)

	fs := flag.NewFlagSet("ca "+args[0], flag.ContinueOnError)
	switch args[0] {
	case "csr":
		cn := fs.String("cn", "", "common name to request")
		out := fs.String("out", "", "file to write the request to")
		if parseErr := fs.Parse(args[1:]); parseErr != nil {
			return parseErr
		}
		return caCSR(cfg, store, *cn, *out, stdout)
	case "import":
		in := fs.String("in", "", "PEM file with the issued certificate and its issuers")
		if parseErr := fs.Parse(args[1:]); parseErr != nil {
			return parseErr
		}
		if *in == "" {
			return errors.New("ca import: -in FILE is required")
		}
		return caImport(cfg, store, *in, stdout)
	case "status":
		return caStatus(store, stdout)
	default:
		_, _ = fmt.Fprintf(stdout, caUsage, os.Args[0])
		return fmt.Errorf("ca: unknown command %q", args[0])
	}
}

// openCAManager opens the CA key (creating it when the CA has none yet).
func openCAManager(cfg *config.Config, store pki.CAStore, stdout io.Writer) (*pki.CAManager, error) {
	if p := cfg.CA.PKCS11; p.IsConfigured() {
		key, err := pki.OpenHSMKey(p.Lib, p.TokenLabel, p.KeyLabel, p.PIN)
		if err != nil {
			return nil, err
		}
		return &pki.CAManager{Store: store, Key: key}, nil
	}
	key, created, err := pki.LoadOrCreateCAKey(store.KeyPath)
	if err != nil {
		return nil, err
	}
	if created {
		_, _ = fmt.Fprintf(stdout, "Generated a new ECDSA P-384 CA key: %s\n", store.KeyPath)
	}
	return &pki.CAManager{Store: store, Key: key}, nil
}

func caCSR(cfg *config.Config, store pki.CAStore, cn, out string, stdout io.Writer) error {
	mgr, err := openCAManager(cfg, store, stdout)
	if err != nil {
		return err
	}
	csrPEM, err := mgr.CreateRequest(cn)
	if err != nil {
		return err
	}
	if out == "" {
		_, err = stdout.Write(csrPEM)
		return err
	}
	if err = os.WriteFile(out, csrPEM, 0o644); err != nil { //nolint:gosec // a signing request is public; the path is the operator's choice
		return fmt.Errorf("write %s: %w", out, err)
	}
	_, _ = fmt.Fprintf(stdout, "Certificate signing request written to %s\n"+
		"Have your certificate authority issue a CA certificate for it, then run:\n"+
		"  %s ca import -in <certificate-and-chain.pem>\n", out, os.Args[0])
	return nil
}

func caImport(cfg *config.Config, store pki.CAStore, in string, stdout io.Writer) error {
	bundle, err := os.ReadFile(in) //nolint:gosec // the path is the operator's choice
	if err != nil {
		return fmt.Errorf("read %s: %w", in, err)
	}
	mgr, err := openCAManager(cfg, store, io.Discard)
	if err != nil {
		return err
	}
	res, err := mgr.Install(bundle)
	if err != nil {
		return err
	}
	signing := res.Chain.Signing
	_, _ = fmt.Fprintf(stdout, "Installed CA certificate %s\n  issued by   %s\n  valid until %s\n  file        %s (%d issuer certificate(s))\n",
		signing.Subject, signing.Issuer, signing.NotAfter.UTC().Format(time.RFC3339), store.CertPath, len(res.Chain.Issuers))
	switch {
	case !res.Replaced:
	case res.SameIdentity:
		_, _ = fmt.Fprintf(stdout, "The previous certificate (kept in %s) had the same key and subject:\n"+
			"enrolled agents keep working while the certificate they hold is valid.\n", store.PreviousPath())
	default:
		_, _ = fmt.Fprintf(stdout, "The previous certificate (kept in %s) had a different key or subject:\n"+
			"agents enrolled under it must re-enroll.\n", store.PreviousPath())
	}
	_, _ = fmt.Fprintln(stdout, "Restart the server to use it.")
	return nil
}

func caStatus(store pki.CAStore, stdout io.Writer) error {
	mgr := &pki.CAManager{Store: store}
	chain, err := mgr.Current()
	if err != nil {
		return err
	}
	if chain == nil {
		_, _ = fmt.Fprintf(stdout, "No CA certificate at %s\n", store.CertPath)
	} else {
		s := chain.Signing
		kind := "subordinate CA"
		if bytes.Equal(s.RawSubject, s.RawIssuer) {
			kind = "self-signed CA"
		}
		_, _ = fmt.Fprintf(stdout, "CA certificate  %s (%s)\n  subject       %s\n  issuer        %s\n  key           %s\n  valid until   %s\n",
			store.CertPath, kind, s.Subject, s.Issuer, pki.KeyAlgorithm(s.PublicKey), s.NotAfter.UTC().Format(time.RFC3339))
		for i, issuer := range chain.Issuers {
			_, _ = fmt.Fprintf(stdout, "  issuer %d      %s (valid until %s)\n", i+1, issuer.Subject, issuer.NotAfter.UTC().Format(time.RFC3339))
		}
	}
	if _, statErr := os.Stat(store.RequestPath()); statErr == nil {
		_, _ = fmt.Fprintf(stdout, "Signing request pending: %s\n", store.RequestPath())
	}
	return nil
}
