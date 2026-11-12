// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"testing"

	"github.com/VuteTech/Bor/server/internal/database"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

type fakeTrustSource struct{ bundle, sha, issuer string }

func (f fakeTrustSource) AgentTrust() (bundlePEM []byte, sha string) { return []byte(f.bundle), f.sha }
func (f fakeTrustSource) ActiveIssuer() string                       { return f.issuer }

type fakeTrustStore struct {
	held    database.NodeCATrust
	bundles []string
	issuers []string
}

func (f *fakeTrustStore) GetCATrust(context.Context, string) (database.NodeCATrust, error) {
	return f.held, nil
}
func (f *fakeTrustStore) SetTrustBundle(_ context.Context, _, sha string) error {
	f.bundles = append(f.bundles, sha)
	return nil
}
func (f *fakeTrustStore) SetCertIssuer(_ context.Context, _, issuer string) error {
	f.issuers = append(f.issuers, issuer)
	return nil
}

func TestSyncCATrust(t *testing.T) {
	source := fakeTrustSource{bundle: "BUNDLE", sha: "current", issuer: "ca-new"}
	tests := []struct {
		name        string
		held        database.NodeCATrust
		reported    string
		wantBundle  bool
		wantRenew   bool
		wantRecords []string
	}{
		{
			name: "up to date", held: database.NodeCATrust{TrustBundleSHA256: "current", CertIssuerSHA256: "ca-new"},
			reported: "current",
		},
		{
			name: "outdated trust is replaced and recorded", held: database.NodeCATrust{TrustBundleSHA256: "old", CertIssuerSHA256: "ca-new"},
			reported: "older", wantBundle: true, wantRecords: []string{"older"},
		},
		{
			name: "certificate of a replaced CA is renewed", held: database.NodeCATrust{TrustBundleSHA256: "current", CertIssuerSHA256: "ca-old"},
			reported: "current", wantRenew: true,
		},
		{
			name: "both", held: database.NodeCATrust{CertIssuerSHA256: "ca-old"},
			reported: "old", wantBundle: true, wantRenew: true, wantRecords: []string{"old"},
		},
		{
			// An agent that reports nothing cannot act on either answer.
			name: "old agent is left alone", held: database.NodeCATrust{CertIssuerSHA256: "ca-old"},
		},
		{
			// Without a recorded issuer and without a client certificate
			// on the context nothing is known about the certificate.
			name: "unknown issuer does not trigger a renewal", held: database.NodeCATrust{TrustBundleSHA256: "current"},
			reported: "current",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeTrustStore{held: tt.held}
			s := (&PolicyServer{}).WithCATrust(source, store)
			resp := &pb.HeartbeatResponse{Accepted: true}
			s.syncCATrust(context.Background(), "node-id", "node-1", tt.reported, resp)

			if got := len(resp.GetCaBundlePem()) > 0; got != tt.wantBundle {
				t.Errorf("bundle sent = %v, want %v", got, tt.wantBundle)
			}
			if tt.wantBundle && string(resp.GetCaBundlePem()) != "BUNDLE" {
				t.Errorf("bundle = %q", resp.GetCaBundlePem())
			}
			if resp.GetRenewCertificate() != tt.wantRenew {
				t.Errorf("renew = %v, want %v", resp.GetRenewCertificate(), tt.wantRenew)
			}
			if len(store.bundles) != len(tt.wantRecords) || (len(store.bundles) == 1 && store.bundles[0] != tt.wantRecords[0]) {
				t.Errorf("recorded trust bundles = %v, want %v", store.bundles, tt.wantRecords)
			}
			if len(store.issuers) != 0 {
				t.Errorf("recorded issuers = %v without a client certificate", store.issuers)
			}
		})
	}

	// Without the dependencies the heartbeat answer is left untouched.
	resp := &pb.HeartbeatResponse{Accepted: true}
	(&PolicyServer{}).syncCATrust(context.Background(), "node-id", "node-1", "x", resp)
	if len(resp.GetCaBundlePem()) != 0 || resp.GetRenewCertificate() {
		t.Errorf("answer changed without a trust source: %v", resp)
	}
}
