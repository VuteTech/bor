// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/VuteTech/Bor/server/internal/api"
	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/config"
	"github.com/VuteTech/Bor/server/internal/database"
	grpcserver "github.com/VuteTech/Bor/server/internal/grpc"
	"github.com/VuteTech/Bor/server/internal/pki"
	"github.com/VuteTech/Bor/server/internal/services"
	enrollpb "github.com/VuteTech/Bor/server/pkg/grpc/enrollment"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/web"
	"google.golang.org/grpc"
)

func main() {
	resetMFAUser := flag.String("reset-mfa", "", "Disable MFA for the given username and exit")
	flag.Parse()

	if *resetMFAUser != "" {
		if err := resetMFAForUser(*resetMFAUser); err != nil {
			log.Fatalf("reset-mfa failed: %v", err)
		}
		os.Exit(0)
	}

	log.Println("Bor Policy Management Server")

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// ─── Internal CA for agent mTLS (must be created first so it can
	//     sign the UI cert when auto-generating) ──────────────────────────
	var caCert *x509.Certificate
	var caKey crypto.Signer
	var caCertFile string // retained for LoadCACertPool below

	if cfg.CA.PKCS11.IsConfigured() {
		// PKCS#11 HSM path: the CA private key lives in the hardware security
		// module. The CA certificate is still stored on disk so that agents can
		// fetch and verify it. EnsureCAWithHSM creates the cert if missing.
		caCertFile = cfg.CA.CertFile
		if caCertFile == "" {
			caCertFile = filepath.Join(cfg.CA.AutogenDir, "ca.crt")
		}
		log.Printf("Loading CA key from PKCS#11 HSM (token=%q key=%q)",
			cfg.CA.PKCS11.TokenLabel, cfg.CA.PKCS11.KeyLabel)
		caCert, caKey, err = pki.EnsureCAWithHSM(
			caCertFile,
			cfg.CA.PKCS11.Lib, cfg.CA.PKCS11.TokenLabel,
			cfg.CA.PKCS11.KeyLabel, cfg.CA.PKCS11.PIN,
		)
		if err != nil {
			log.Fatalf("Failed to initialise CA with PKCS#11 HSM: %v", err)
		}
		log.Printf("CA loaded from HSM; certificate at %s", caCertFile)
	} else {
		// Software path: CA key stored in a file on disk (default).
		caCertFile = cfg.CA.CertFile
		caKeyFile := cfg.CA.KeyFile
		if caCertFile == "" && caKeyFile == "" {
			log.Println("BOR_CA_CERT_FILE/KEY not set – generating internal CA")
			caCertFile, caKeyFile, err = pki.EnsureCA(cfg.CA.AutogenDir)
			if err != nil {
				log.Fatalf("Failed to generate internal CA: %v", err)
			}
			log.Printf("Internal CA stored in %s", cfg.CA.AutogenDir)
		}
		caCert, caKey, err = pki.LoadCA(caCertFile, caKeyFile)
		if err != nil {
			log.Fatalf("Failed to load internal CA: %v", err)
		}
	}

	caCertPool, err := pki.LoadCACertPool(caCertFile)
	if err != nil {
		log.Fatalf("Failed to load CA cert pool: %v", err)
	}

	// ─── TLS: UI HTTPS certificate (signed by the CA when auto-generated) ──
	tlsCertFile := cfg.TLS.CertFile
	tlsKeyFile := cfg.TLS.KeyFile
	if tlsCertFile == "" && tlsKeyFile == "" {
		log.Println("BOR_TLS_CERT_FILE/KEY not set – ensuring server certificate (signed by internal CA)")
		tlsCertFile, tlsKeyFile, err = pki.EnsureServerCert(cfg.TLS.AutogenDir, caCert, caKey, cfg.Server.Hostnames)
		if err != nil {
			log.Fatalf("Failed to ensure server certificate: %v", err)
		}
		log.Printf("Server certificate ready in %s", cfg.TLS.AutogenDir)
	}

	// Initialize database connection
	db, err := database.New(database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		Database: cfg.Database.Database,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Run database migrations
	if err := db.RunMigrations(); err != nil {
		log.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize repositories
	userRepo := database.NewUserRepository(db)
	policyRepo := database.NewPolicyRepository(db)
	nodeRepo := database.NewNodeRepository(db)
	nodeGroupRepo := database.NewNodeGroupRepository(db)
	userGroupRepo := database.NewUserGroupRepository(db)
	policyBindingRepo := database.NewPolicyBindingRepository(db)
	roleRepo := database.NewRoleRepository(db)
	permRepo := database.NewPermissionRepository(db)
	userRoleBindingRepo := database.NewUserRoleBindingRepository(db)
	userGroupMemberRepo := database.NewUserGroupMemberRepository(db)
	userGroupRoleBindingRepo := database.NewUserGroupRoleBindingRepository(db)
	auditLogRepo := database.NewAuditLogRepository(db)
	settingsRepo := database.NewSettingsRepository(db)
	revocationRepo := database.NewRevocationRepository(db)
	mfaRepo := database.NewMFARepository(db)
	webauthnRepo := database.NewWebAuthnRepository(db)

	// Initialize LDAP service
	var ldapSvc *services.LDAPService
	if cfg.LDAP.Enabled {
		ldapSvc = services.NewLDAPService(services.LDAPConfig{
			Enabled:      cfg.LDAP.Enabled,
			Host:         cfg.LDAP.Host,
			Port:         cfg.LDAP.Port,
			UseTLS:       cfg.LDAP.UseTLS,
			BindDN:       cfg.LDAP.BindDN,
			BindPassword: cfg.LDAP.BindPassword,
			BaseDN:       cfg.LDAP.BaseDN,
			UserFilter:   cfg.LDAP.UserFilter,
			AttrUsername:  cfg.LDAP.AttrUsername,
			AttrEmail:    cfg.LDAP.AttrEmail,
			AttrFullName: cfg.LDAP.AttrFullName,
		})
		log.Println("LDAP authentication enabled")
	}

	// Initialize MFA service
	mfaSvc := services.NewMFAService(mfaRepo, settingsRepo, cfg.Security.JWTSecret)

	// Initialize WebAuthn service (optional — only if RPID is configured)
	var webauthnSvc *services.WebAuthnService
	if cfg.WebAuthn.RPID != "" {
		var waErr error
		webauthnSvc, waErr = services.NewWebAuthnService(webauthnRepo, cfg.WebAuthn.RPID, cfg.WebAuthn.DisplayName, cfg.WebAuthn.RPOrigins)
		if waErr != nil {
			log.Printf("WARNING: Failed to initialize WebAuthn service: %v", waErr)
			webauthnSvc = nil
		} else {
			log.Printf("WebAuthn enabled (RPID=%s)", cfg.WebAuthn.RPID)
		}
	} else {
		log.Println("WebAuthn disabled: set BOR_WEBAUTHN_RPID to enable")
	}

	// Initialize auth service
	authSvc := services.NewAuthServiceWithMFAAndWebAuthn(userRepo, roleRepo, userRoleBindingRepo, cfg.Security.JWTSecret, ldapSvc, mfaSvc, webauthnSvc)

	// Initialize policy service
	policySvc := services.NewPolicyService(policyRepo, policyBindingRepo)

	// Initialize node service
	nodeSvc := services.NewNodeService(nodeRepo)

	// Initialize node group service
	nodeGroupSvc := services.NewNodeGroupService(nodeGroupRepo)

	// Initialize user group service (identity domain — separate from node groups)
	userGroupSvc := services.NewUserGroupService(userGroupRepo)

	// Initialize policy binding service
	policyBindingSvc := services.NewPolicyBindingService(policyBindingRepo, policyRepo, nodeGroupRepo)

	// Initialize enrollment service
	enrollSvc := services.NewEnrollmentService(caCert, caKey, nodeGroupSvc, nodeSvc, revocationRepo)

	// Initialize audit service
	auditSvc := services.NewAuditService(auditLogRepo)

	// Initialize settings service
	settingsSvc := services.NewSettingsService(settingsRepo)

	// Initialize authorizer
	az := authz.New(userRoleBindingRepo, roleRepo)

	// Create default admin if no users exist
	if err := authSvc.EnsureDefaultAdmin(context.Background()); err != nil {
		log.Printf("Warning: failed to ensure default admin: %v", err)
	}

	// PolicyHub provides in-process pub/sub for streaming policy updates.
	policyHub := grpcserver.NewPolicyHub()

	// Initialize API handlers
	authHandler := api.NewAuthHandler(authSvc, mfaSvc, webauthnSvc)
	userHandler := api.NewUserHandler(authSvc)
	roleHandler := api.NewRoleHandler(roleRepo, permRepo, userRoleBindingRepo)
	bindingHandler := api.NewUserRoleBindingHandler(userRoleBindingRepo)
	policyHandler := api.NewPolicyHandler(policySvc)
	nodeHandler := api.NewNodeHandler(nodeSvc, enrollSvc, policyHub)
	nodeGroupHandler := api.NewNodeGroupHandler(nodeGroupSvc, enrollSvc)
	userGroupHandler := api.NewUserGroupHandler(userGroupSvc, userGroupMemberRepo, userGroupRoleBindingRepo)
	policyBindingHandler := api.NewPolicyBindingHandler(policyBindingSvc)
	auditLogHandler := api.NewAuditLogHandler(auditSvc)
	settingsHandler := api.NewSettingsHandler(settingsSvc, mfaSvc)

	// Wire policy and binding change notifications to the hub so that
	// connected streaming agents receive a fresh snapshot when the
	// policy set changes.
	notifyAgents := func() { policyHub.PublishResync() }
	policyHandler.OnPolicyChange = notifyAgents
	policyBindingHandler.OnBindingChange = notifyAgents

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Audit middleware for logging state-changing API calls
	auditMw := api.AuditMiddleware(auditSvc)

	// Public routes
	mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("/api/v1/auth/begin", authHandler.Begin)
	mux.HandleFunc("/api/v1/auth/step", authHandler.Step)
	mux.HandleFunc("/api/v1/auth/webauthn/begin", authHandler.WebAuthnAuthBegin)
	mux.HandleFunc("/api/v1/auth/webauthn/finish", authHandler.WebAuthnAuthFinish)

	// Protected routes — all require authentication AND specific permissions.
	// Deny-by-default: routes without explicit permission middleware are not accessible.
	authMiddleware := api.AuthMiddleware(authSvc)

	// Auth routes (no additional permission needed — user just needs to be authenticated)
	mux.Handle("/api/v1/auth/me", authMiddleware(http.HandlerFunc(authHandler.Me)))

	// MFA routes for the current user
	mux.Handle("/api/v1/users/me/mfa", authMiddleware(http.HandlerFunc(authHandler.MFAStatus)))
	mux.Handle("/api/v1/users/me/mfa/setup/begin", authMiddleware(http.HandlerFunc(authHandler.MFASetupBegin)))
	mux.Handle("/api/v1/users/me/mfa/setup/finish", authMiddleware(http.HandlerFunc(authHandler.MFASetupFinish)))
	mux.Handle("/api/v1/users/me/mfa/disable", authMiddleware(http.HandlerFunc(authHandler.MFADisable)))

	// WebAuthn routes for the current user
	mux.Handle("/api/v1/users/me/webauthn/register/begin", authMiddleware(http.HandlerFunc(authHandler.WebAuthnRegisterBegin)))
	mux.Handle("/api/v1/users/me/webauthn/register/finish", authMiddleware(http.HandlerFunc(authHandler.WebAuthnRegisterFinish)))
	mux.Handle("/api/v1/users/me/webauthn/credentials", authMiddleware(http.HandlerFunc(authHandler.WebAuthnCredentialHandler)))
	mux.Handle("/api/v1/users/me/webauthn/credentials/", authMiddleware(http.HandlerFunc(authHandler.WebAuthnCredentialHandler)))

	// Policy routes — method-based permission checking
	policyPerms := api.RequireMethodPermission(az, []api.MethodPermission{
		{Method: http.MethodGet, Resource: "policy", Action: "view"},
		{Method: http.MethodPost, Resource: "policy", Action: "create"},
		{Method: http.MethodPut, Resource: "policy", Action: "edit"},
		{Method: http.MethodDelete, Resource: "policy", Action: "delete"},
	})
	mux.Handle("/api/v1/policies", authMiddleware(api.RequirePermission(az, "policy", "view")(http.HandlerFunc(policyHandler.List))))
	mux.Handle("/api/v1/policies/all", authMiddleware(policyPerms(auditMw(http.HandlerFunc(policyHandler.ServeHTTP)))))
	mux.Handle("/api/v1/policies/all/", authMiddleware(policyPerms(auditMw(http.HandlerFunc(policyHandler.ServeHTTP)))))

	// Node routes — method-based permission checking
	nodePerms := api.RequireMethodPermission(az, []api.MethodPermission{
		{Method: http.MethodGet, Resource: "node", Action: "view"},
		{Method: http.MethodPost, Resource: "node", Action: "create"},
		{Method: http.MethodPut, Resource: "node", Action: "edit"},
		{Method: http.MethodDelete, Resource: "node", Action: "delete"},
	})
	mux.Handle("/api/v1/nodes", authMiddleware(api.RequirePermission(az, "node", "view")(http.HandlerFunc(nodeHandler.List))))
	mux.Handle("/api/v1/nodes/status-counts", authMiddleware(api.RequirePermission(az, "node", "view")(http.HandlerFunc(nodeHandler.CountByStatus))))
	mux.Handle("/api/v1/nodes/", authMiddleware(nodePerms(auditMw(http.HandlerFunc(nodeHandler.ServeHTTP)))))

	// Node group routes — method-based permission checking
	groupPerms := api.RequireMethodPermission(az, []api.MethodPermission{
		{Method: http.MethodGet, Resource: "node_group", Action: "view"},
		{Method: http.MethodPost, Resource: "node_group", Action: "create"},
		{Method: http.MethodPut, Resource: "node_group", Action: "edit"},
		{Method: http.MethodDelete, Resource: "node_group", Action: "delete"},
	})
	mux.Handle("/api/v1/node-groups", authMiddleware(groupPerms(auditMw(http.HandlerFunc(nodeGroupHandler.ServeHTTP)))))
	mux.Handle("/api/v1/node-groups/", authMiddleware(groupPerms(auditMw(http.HandlerFunc(nodeGroupHandler.ServeHTTP)))))

	// User group routes — identity domain (separate from node groups)
	userGroupPerms := api.RequireMethodPermission(az, []api.MethodPermission{
		{Method: http.MethodGet, Resource: "user_group", Action: "view"},
		{Method: http.MethodPost, Resource: "user_group", Action: "create"},
		{Method: http.MethodPut, Resource: "user_group", Action: "edit"},
		{Method: http.MethodDelete, Resource: "user_group", Action: "delete"},
	})
	mux.Handle("/api/v1/user-groups", authMiddleware(userGroupPerms(auditMw(http.HandlerFunc(userGroupHandler.ServeHTTP)))))
	mux.Handle("/api/v1/user-groups/", authMiddleware(userGroupPerms(auditMw(http.HandlerFunc(userGroupHandler.ServeHTTP)))))

	// Policy binding routes — method-based permission checking
	bindingPerms := api.RequireMethodPermission(az, []api.MethodPermission{
		{Method: http.MethodGet, Resource: "binding", Action: "view"},
		{Method: http.MethodPost, Resource: "binding", Action: "create"},
		{Method: http.MethodPut, Resource: "binding", Action: "toggle"},
		{Method: http.MethodPatch, Resource: "binding", Action: "toggle"},
		{Method: http.MethodDelete, Resource: "binding", Action: "toggle"},
	})
	mux.Handle("/api/v1/policy-bindings", authMiddleware(bindingPerms(auditMw(http.HandlerFunc(policyBindingHandler.ServeHTTP)))))
	mux.Handle("/api/v1/policy-bindings/", authMiddleware(bindingPerms(auditMw(http.HandlerFunc(policyBindingHandler.ServeHTTP)))))

	// Admin-only routes (requires "user:manage" permission)
	adminMiddleware := api.AdminOnly(az)
	mux.Handle("/api/v1/users", authMiddleware(adminMiddleware(auditMw(userHandler))))
	mux.Handle("/api/v1/users/", authMiddleware(adminMiddleware(auditMw(userHandler))))

	// Role management routes (requires "role:manage" permission)
	roleMiddleware := api.RequirePermission(az, "role", "manage")
	mux.Handle("/api/v1/roles", authMiddleware(roleMiddleware(auditMw(roleHandler))))
	mux.Handle("/api/v1/roles/", authMiddleware(roleMiddleware(auditMw(roleHandler))))
	mux.Handle("/api/v1/permissions", authMiddleware(roleMiddleware(http.HandlerFunc(roleHandler.ListAllPermissions))))

	// User role binding routes (requires "user:manage" permission)
	mux.Handle("/api/v1/user-role-bindings", authMiddleware(adminMiddleware(auditMw(bindingHandler))))
	mux.Handle("/api/v1/user-role-bindings/", authMiddleware(adminMiddleware(auditMw(bindingHandler))))

	// Audit log routes
	mux.Handle("/api/v1/audit-logs", authMiddleware(api.RequirePermission(az, "audit_log", "view")(http.HandlerFunc(auditLogHandler.List))))
	mux.Handle("/api/v1/audit-logs/export", authMiddleware(api.RequirePermission(az, "audit_log", "export")(http.HandlerFunc(auditLogHandler.Export))))

	// Settings routes
	mux.Handle("/api/v1/settings/agent-notifications", authMiddleware(api.RequirePermission(az, "settings", "manage")(auditMw(http.HandlerFunc(settingsHandler.AgentNotifications)))))
	mux.Handle("/api/v1/settings/mfa", authMiddleware(api.RequirePermission(az, "settings", "manage")(http.HandlerFunc(settingsHandler.MFASettings))))

	// Serve embedded frontend on root path
	mux.Handle("/", api.FrontendHandler(web.StaticFiles))

	// ─── TLS certificate for both servers ───────────────────────────────
	uiTLSCert, err := pki.LoadTLSCert(tlsCertFile, tlsKeyFile)
	if err != nil {
		log.Fatalf("Failed to load UI TLS certificate: %v", err)
	}

	// ─── Enrollment gRPC server (no mandatory client cert at TLS layer) ──
	// Require a verified client certificate for all RPCs except Enroll
	// (which is the bootstrapping call that exchanges a token for a cert).
	exemptMethods := map[string]bool{
		"/bor.enrollment.v1.EnrollmentService/Enroll": true,
	}

	enrollGrpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(grpcserver.RequireClientCertInterceptor(exemptMethods, revocationRepo)),
		grpc.StreamInterceptor(grpcserver.RequireClientCertStreamInterceptor(exemptMethods, revocationRepo)),
	)
	enrollpb.RegisterEnrollmentServiceServer(enrollGrpcSrv, grpcserver.NewEnrollmentServer(enrollSvc, cfg.Security.AdminToken))

	// ─── Policy gRPC server (mandatory client cert — agents only) ────────
	policyGrpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(grpcserver.RequireClientCertInterceptor(map[string]bool{}, revocationRepo)),
		grpc.StreamInterceptor(grpcserver.RequireClientCertStreamInterceptor(map[string]bool{}, revocationRepo)),
	)
	pb.RegisterPolicyServiceServer(policyGrpcSrv, grpcserver.NewPolicyServer(policySvc, nodeSvc, settingsSvc, auditSvc, enrollSvc, policyHub))

	// ─── UI + Enrollment server (:8443) — VerifyClientCertIfGiven ────────
	// Explicit cipher suites per BSI TR-02102-2 (2024): ECDHE+AEAD only.
	// These are the only suites allowed for TLS 1.2; TLS 1.3 suites are
	// automatically selected by Go and not configurable.
	// CurvePreferences: P-256 and P-384 satisfy FIPS 140-3, BSI, ANSSI, NCSC.
	// X25519 is listed first for performance where FIPS mode is not enforced;
	// GODEBUG=fips140=on will automatically remove it at runtime.
	uiTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{uiTLSCert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		CurvePreferences: []tls.CurveID{
			tls.X25519,    // BSI/ANSSI/NCSC approved; dropped by GODEBUG=fips140=on
			tls.CurveP256, // FIPS 140-3 + all EU standards
			tls.CurveP384, // FIPS 140-3 + all EU standards
		},
	}

	uiGrpcRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			enrollGrpcSrv.ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	uiServer := &http.Server{
		Addr:              cfg.Server.EnrollmentAddr(),
		Handler:           uiGrpcRouter,
		TLSConfig:         uiTLSConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// ─── Agent policy server (:8444) — RequireAndVerifyClientCert ────────
	// TLS 1.3 minimum: agent-only port, no browser clients, strongest TLS.
	agentTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{uiTLSCert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2"},
	}

	agentGrpcRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policyGrpcSrv.ServeHTTP(w, r)
	})

	agentServer := &http.Server{
		Addr:              cfg.Server.PolicyAddr(),
		Handler:           agentGrpcRouter,
		TLSConfig:         agentTLSConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start both servers.
	go func() {
		if err := uiServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			log.Fatalf("UI server error: %v", err)
		}
	}()

	go func() {
		if err := agentServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			log.Fatalf("Agent server error: %v", err)
		}
	}()

	log.Printf("HTTPS + enrollment gRPC listening on %s", cfg.Server.EnrollmentAddr())
	log.Printf("Agent policy gRPC (mTLS) listening on %s", cfg.Server.PolicyAddr())

	// Block until shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down server...")
	enrollGrpcSrv.GracefulStop()
	policyGrpcSrv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := uiServer.Shutdown(ctx); err != nil {
		log.Printf("UI server shutdown error: %v", err)
	}
	if err := agentServer.Shutdown(ctx); err != nil {
		log.Printf("Agent server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

// resetMFAForUser connects to the database using environment config, looks up
// the user by username, and deletes their MFA record.
func resetMFAForUser(username string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.New(database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		Database: cfg.Database.Database,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	userRepo := database.NewUserRepository(db)
	ctx := context.Background()
	user, err := userRepo.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("look up user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user %q not found", username)
	}

	mfaRepo := database.NewMFARepository(db)
	if err := mfaRepo.Delete(ctx, user.ID); err != nil {
		return fmt.Errorf("delete MFA record: %w", err)
	}

	log.Printf("MFA disabled for user %q (id=%s)", username, user.ID)
	return nil
}
