// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package exthost

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/VuteTech/Bor/server/pkg/ext"
)

type stubModule struct {
	name string
	init func(ext.Host) error
}

func (m stubModule) Name() string { return m.name }
func (m stubModule) Init(_ context.Context, h ext.Host) error {
	if m.init != nil {
		return m.init(h)
	}
	return nil
}

func testDeps() *Deps {
	return &Deps{
		Mux:             http.NewServeMux(),
		AuthMiddleware:  func(h http.Handler) http.Handler { return h },
		AuditMiddleware: func(h http.Handler) http.Handler { return h },
		SecretRoot:      "test-root-secret",
	}
}

func TestInit_ValidatesModuleNames(t *testing.T) {
	ok := stubModule{name: "sso"}
	tests := []struct {
		name    string
		modules []ext.Module
		wantErr bool
	}{
		{"valid", []ext.Module{ok}, false},
		{"no modules", nil, false},
		{"uppercase", []ext.Module{stubModule{name: "SSO"}}, true},
		{"path characters", []ext.Module{stubModule{name: "../x"}}, true},
		{"empty", []ext.Module{stubModule{name: ""}}, true},
		{"duplicate", []ext.Module{ok, ok}, true},
		{"init error", []ext.Module{stubModule{name: "bad", init: func(ext.Host) error { return errors.New("boom") }}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Init(context.Background(), testDeps(), NewRegistry(), tt.modules)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHost_RoutesAreConfinedToTheModulePrefix(t *testing.T) {
	deps := testDeps()
	h := &host{name: "sso", deps: deps, reg: NewRegistry()}
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	if err := h.HandlePublic("/api/v1/ext/sso/oidc/x/start", noop); err != nil {
		t.Errorf("own prefix rejected: %v", err)
	}
	for _, bad := range []string{
		"/api/v1/auth/login",               // core route
		"/api/v1/ext/other/x",              // another module
		"/api/v1/ext/sso",                  // prefix without trailing slash
		"GET /api/v1/ext/sso/x",            // method pattern
		"/api/v1/ext/sso/{id}",             // wildcard
		"/api/v1/ext/sso/../../auth/login", // traversal
	} {
		if err := h.HandlePublic(bad, noop); err == nil {
			t.Errorf("pattern %q was accepted", bad)
		}
	}
	if err := h.HandlePublic("/api/v1/ext/sso/oidc/x/start", noop); err == nil {
		t.Error("a duplicate route must be reported, not panic")
	}
}

func TestRegistry_MethodsAndClaims(t *testing.T) {
	reg := NewRegistry()
	l := &logins{reg: reg, prefix: "/api/v1/ext/sso/"}
	on := true

	if err := l.Register(ext.LoginMethod{ID: "oidc/corp", DisplayName: "Corp", StartURL: "/api/v1/ext/sso/oidc/corp/start", Enabled: func() bool { return on }}); err != nil {
		t.Fatal(err)
	}
	if err := l.Register(ext.LoginMethod{ID: "evil", DisplayName: "Evil", StartURL: "https://attacker.example/"}); err == nil {
		t.Error("a start URL outside the module prefix must be rejected")
	}
	if err := l.Register(ext.LoginMethod{ID: "oidc/corp", DisplayName: "Dup", StartURL: "/api/v1/ext/sso/x"}); err == nil {
		t.Error("duplicate method IDs must be rejected")
	}
	if got := reg.Methods(); len(got) != 1 || got[0].ID != "oidc/corp" {
		t.Errorf("Methods() = %+v", got)
	}
	on = false
	if got := reg.Methods(); len(got) != 0 {
		t.Errorf("disabled methods must not be listed, got %+v", got)
	}

	if err := l.ClaimAccounts("oidc/corp", func(u string) string { return "/api/v1/ext/sso/oidc/corp/start?login_hint=" + u }); err != nil {
		t.Fatal(err)
	}
	if err := l.ClaimAccounts("oidc/corp", func(string) string { return "" }); err == nil {
		t.Error("a federation provider can only be claimed once")
	}
	if url, ok := reg.Resolve("oidc/corp", "alice"); !ok || url != "/api/v1/ext/sso/oidc/corp/start?login_hint=alice" {
		t.Errorf("Resolve = %q, %v", url, ok)
	}
	if _, ok := reg.Resolve("saml/other", "alice"); ok {
		t.Error("an unclaimed provider must not resolve")
	}

	_ = l.ClaimAccounts("oidc/rogue", func(string) string { return "https://attacker.example/" })
	if _, ok := reg.Resolve("oidc/rogue", "alice"); ok {
		t.Error("a claim must never redirect outside the module's routes")
	}
}

func TestPlanMembership(t *testing.T) {
	current := map[string]string{"g-admins": "m1", "g-manual": "m2", "g-staff": "m3"}
	add, remove, err := planMembership(
		[]string{"g-admins", "g-staff", "g-devs"}, // managed by the connection
		[]string{"g-staff", "g-devs"},             // from the IdP
		current,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(add, []string{"g-devs"}) {
		t.Errorf("add = %v, want [g-devs]", add)
	}
	if !reflect.DeepEqual(remove, []string{"m1"}) {
		t.Errorf("remove = %v, want [m1] (g-manual is unmanaged and must stay)", remove)
	}
	if _, _, err := planMembership([]string{"g-a"}, []string{"g-b"}, nil); err == nil {
		t.Error("a wanted group outside the managed set must be rejected")
	}
}

func TestSecrets_RoundTripAndModuleIsolation(t *testing.T) {
	deps := testDeps()
	a := (&host{name: "sso", deps: deps}).Secrets()
	b := (&host{name: "scim", deps: deps}).Secrets()

	sealed, err := a.Seal([]byte("client-secret"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Open(sealed)
	if err != nil || string(got) != "client-secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := b.Open(sealed); err == nil {
		t.Error("another module's key must not open the secret")
	}
	if _, err := a.Open(sealed[:len(sealed)-2] + "AA"); err == nil {
		t.Error("a tampered secret must not open")
	}
	other, _ := a.Seal([]byte("client-secret"))
	if other == sealed {
		t.Error("sealing must be randomized")
	}
}

func TestDecideMFA(t *testing.T) {
	tests := []struct {
		name     string
		mode     ext.LocalMFA
		idpMFA   bool
		required bool
		hasLocal bool
		want     mfaDecision
	}{
		{"idp: not required", ext.LocalMFANever, false, false, true, mfaStart},
		{"idp: required, asserted", ext.LocalMFANever, true, true, false, mfaStart},
		{"idp: required, not asserted", ext.LocalMFANever, false, true, true, mfaRefuseIDP},
		{"bor: has factor", ext.LocalMFAAlways, false, false, true, mfaAskLocal},
		{"bor: ignores the IdP assertion", ext.LocalMFAAlways, true, true, true, mfaAskLocal},
		{"bor: required, no factor", ext.LocalMFAAlways, true, true, false, mfaRefuseSetup},
		{"bor: not required, no factor", ext.LocalMFAAlways, false, false, false, mfaStart},
		{"fallback: IdP asserted", ext.LocalMFAFallback, true, true, true, mfaStart},
		{"fallback: not asserted, has factor", ext.LocalMFAFallback, false, true, true, mfaAskLocal},
		{"fallback: not asserted, factor set up, not required", ext.LocalMFAFallback, false, false, true, mfaAskLocal},
		{"fallback: required, no factor", ext.LocalMFAFallback, false, true, false, mfaRefuseSetup},
		{"fallback: not required, no factor", ext.LocalMFAFallback, false, false, false, mfaStart},
	}
	for _, tt := range tests {
		ac := ext.AuthContext{LocalMFA: tt.mode, MFA: tt.idpMFA}
		if got := decideMFA(&ac, tt.required, tt.hasLocal); got != tt.want {
			t.Errorf("%s: decideMFA = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestHost_RegisterMetrics(t *testing.T) {
	reg := NewRegistry()
	h := &host{name: "reports", deps: testDeps(), reg: reg}
	if err := h.RegisterMetrics(nil); err == nil {
		t.Error("a nil collector must be rejected")
	}
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "bor_test", Help: "Test."})
	if err := h.RegisterMetrics(g); err != nil {
		t.Fatal(err)
	}
	if got := reg.Collectors(); len(got) != 1 || got[0] != g {
		t.Errorf("Collectors() = %v", got)
	}
}
