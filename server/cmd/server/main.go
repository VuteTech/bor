// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	log.Println("Bor Policy Management Server")

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// ─── Internal CA for agent mTLS (must be created first so it can
	//     sign the UI cert when auto-generating) ──────────────────────────
	caCertFile := cfg.CA.CertFile
	caKeyFile := cfg.CA.KeyFile
	if caCertFile == "" && caKeyFile == "" {
		log.Println("BOR_CA_CERT_FILE/KEY not set – generating internal CA")
		caCertFile, caKeyFile, err = pki.EnsureCA(cfg.CA.AutogenDir)
		if err != nil {
			log.Fatalf("Failed to generate internal CA: %v", err)
		}
		log.Printf("Internal CA stored in %s", cfg.CA.AutogenDir)
	}

	caCert, caKey, err := pki.LoadCA(caCertFile, caKeyFile)
	if err != nil {
		log.Fatalf("Failed to load internal CA: %v", err)
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
		tlsCertFile, tlsKeyFile, err = pki.EnsureServerCert(cfg.TLS.AutogenDir, caCert, caKey)
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

	// Initialize auth service
	authSvc := services.NewAuthService(userRepo, roleRepo, userRoleBindingRepo, cfg.Security.JWTSecret, ldapSvc)

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
	enrollSvc := services.NewEnrollmentService(caCert, caKey, nodeGroupSvc, nodeSvc)

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
	authHandler := api.NewAuthHandler(authSvc)
	userHandler := api.NewUserHandler(authSvc)
	roleHandler := api.NewRoleHandler(roleRepo, permRepo, userRoleBindingRepo)
	bindingHandler := api.NewUserRoleBindingHandler(userRoleBindingRepo)
	policyHandler := api.NewPolicyHandler(policySvc)
	nodeHandler := api.NewNodeHandler(nodeSvc, policyHub)
	nodeGroupHandler := api.NewNodeGroupHandler(nodeGroupSvc, enrollSvc)
	userGroupHandler := api.NewUserGroupHandler(userGroupSvc, userGroupMemberRepo, userGroupRoleBindingRepo)
	policyBindingHandler := api.NewPolicyBindingHandler(policyBindingSvc)
	auditLogHandler := api.NewAuditLogHandler(auditSvc)
	settingsHandler := api.NewSettingsHandler(settingsSvc)

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

	// Protected routes — all require authentication AND specific permissions.
	// Deny-by-default: routes without explicit permission middleware are not accessible.
	authMiddleware := api.AuthMiddleware(authSvc)

	// Auth routes (no additional permission needed — user just needs to be authenticated)
	mux.Handle("/api/v1/auth/me", authMiddleware(http.HandlerFunc(authHandler.Me)))

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

	// Serve embedded frontend on root path
	mux.Handle("/", api.FrontendHandler(web.StaticFiles))

	// ─── Single TLS server for both HTTPS UI and gRPC ───────────────────
	uiTLSCert, err := pki.LoadTLSCert(tlsCertFile, tlsKeyFile)
	if err != nil {
		log.Fatalf("Failed to load UI TLS certificate: %v", err)
	}

	// Setup gRPC server (no TLS credentials – TLS is handled by http.Server).
	// Require a verified client certificate for all RPCs except Enroll
	// (which is the bootstrapping call that exchanges a token for a cert).
	exemptMethods := map[string]bool{
		"/bor.enrollment.v1.EnrollmentService/Enroll": true,
	}

	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(grpcserver.RequireClientCertInterceptor(exemptMethods)),
		grpc.StreamInterceptor(grpcserver.RequireClientCertStreamInterceptor(exemptMethods)),
	)
	pb.RegisterPolicyServiceServer(grpcSrv, grpcserver.NewPolicyServer(policySvc, nodeSvc, settingsSvc, policyHub))
	enrollpb.RegisterEnrollmentServiceServer(grpcSrv, grpcserver.NewEnrollmentServer(enrollSvc, cfg.Security.AdminToken))

	// Route: if HTTP/2 + Content-Type starts with "application/grpc" → gRPC,
	// otherwise → normal HTTP handler (UI, API).
	grpcHTTPRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcSrv.ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{uiTLSCert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           grpcHTTPRouter,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server – empty cert/key args cause ListenAndServeTLS to use
	// the TLSConfig already set on the http.Server instead of loading files.
	go func() {
		if err := httpServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	log.Printf("HTTPS + gRPC (mTLS) listening on %s", cfg.Server.Addr)

	// Block until shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down server...")
	grpcSrv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
