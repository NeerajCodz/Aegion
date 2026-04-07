package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aegion/aegion/core/router"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
	platformconfig "github.com/aegion/aegion/internal/platform/config"
)

const (
	systemConfigKeyPolicy = "policy.settings"
	systemConfigKeyProxy  = "proxy.settings"
)

type runtimePolicySettings struct {
	Enabled      bool   `json:"enabled"`
	DefaultModel string `json:"default_model"`
	RBAC         struct {
		Enabled bool `json:"enabled"`
	} `json:"rbac"`
	ABAC struct {
		Enabled bool `json:"enabled"`
	} `json:"abac"`
	ReBAC struct {
		Enabled bool `json:"enabled"`
	} `json:"rebac"`
}

type runtimeProxySettings struct {
	Enabled                     bool     `json:"enabled"`
	UpstreamTimeout             string   `json:"upstream_timeout"`
	PreserveHost                bool     `json:"preserve_host"`
	StripInboundIdentityHeaders bool     `json:"strip_inbound_identity_headers"`
	IdentitySigningSecret       string   `json:"identity_signing_secret,omitempty"`
	IdentitySignatureHeader     string   `json:"identity_signature_header"`
	SignedIdentityHeaders       []string `json:"signed_identity_headers"`
}

type runtimeConfigResponse struct {
	Policy runtimePolicySettingsResponse `json:"policy"`
	Proxy  runtimeProxySettingsResponse  `json:"proxy"`
}

type runtimePolicySettingsResponse struct {
	Enabled      bool   `json:"enabled"`
	DefaultModel string `json:"default_model"`
	RBAC         struct {
		Enabled bool `json:"enabled"`
	} `json:"rbac"`
	ABAC struct {
		Enabled bool `json:"enabled"`
	} `json:"abac"`
	ReBAC struct {
		Enabled bool `json:"enabled"`
	} `json:"rebac"`
}

type runtimeProxySettingsResponse struct {
	Enabled                     bool     `json:"enabled"`
	UpstreamTimeout             string   `json:"upstream_timeout"`
	PreserveHost                bool     `json:"preserve_host"`
	StripInboundIdentityHeaders bool     `json:"strip_inbound_identity_headers"`
	IdentitySigningSecretSet    bool     `json:"identity_signing_secret_set"`
	IdentitySignatureHeader     string   `json:"identity_signature_header"`
	SignedIdentityHeaders       []string `json:"signed_identity_headers"`
}

type runtimeConfigPatchRequest struct {
	Policy *runtimePolicySettingsPatch `json:"policy"`
	Proxy  *runtimeProxySettingsPatch  `json:"proxy"`
}

type runtimePolicySettingsPatch struct {
	Enabled      *bool   `json:"enabled"`
	DefaultModel *string `json:"default_model"`
	RBAC         *struct {
		Enabled *bool `json:"enabled"`
	} `json:"rbac"`
	ABAC *struct {
		Enabled *bool `json:"enabled"`
	} `json:"abac"`
	ReBAC *struct {
		Enabled *bool `json:"enabled"`
	} `json:"rebac"`
}

type runtimeProxySettingsPatch struct {
	Enabled                     *bool     `json:"enabled"`
	UpstreamTimeout             *string   `json:"upstream_timeout"`
	PreserveHost                *bool     `json:"preserve_host"`
	StripInboundIdentityHeaders *bool     `json:"strip_inbound_identity_headers"`
	IdentitySigningSecret       *string   `json:"identity_signing_secret"`
	IdentitySignatureHeader     *string   `json:"identity_signature_header"`
	SignedIdentityHeaders       *[]string `json:"signed_identity_headers"`
}

// SetupRoutes configures all HTTP routes for the server.
func SetupRoutes(s *Server) chi.Router {
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(s.cfg.Server.RequestTimeout.Duration()))

	// CORS
	if s.cfg.Server.CORS.Enabled {
		r.Use(s.corsMiddleware)
	}

	// Health check endpoints (no auth required)
	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)
	r.Get("/health/live", s.handleLive)

	// Public API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Self-service flow routes
		setupSelfServiceRoutes(r, s)

		// Session endpoints
		r.Route("/sessions", func(r chi.Router) {
			r.Get("/whoami", s.handleWhoAmI)
			r.Delete("/", s.handleLogout)
		})

		// JWKS endpoint
		r.Get("/.well-known/jwks.json", s.handleJWKS)
	})

	// Internal API routes (module-to-core communication)
	r.Route("/internal", func(r chi.Router) {
		// Authenticate internal requests
		r.Use(authtoken.Middleware(authtoken.MiddlewareConfig{
			Generator: s.tokenGen,
			SkipPaths: []string{"/internal/health"},
		}))

		setupModuleRoutes(r, s)
	})

	// Admin routes (if enabled)
	if s.cfg.Admin.Enabled {
		r.Route(s.cfg.Admin.Path, func(r chi.Router) {
			setupAdminRoutes(r, s)
		})
	}

	return r
}

// setupSelfServiceRoutes configures self-service flow endpoints.
func setupSelfServiceRoutes(r chi.Router, s *Server) {
	r.Route("/self-service", func(r chi.Router) {
		// Login flow
		r.Route("/login", func(r chi.Router) {
			r.Get("/browser", s.handleInitLoginBrowser)
			r.Get("/api", s.handleInitLoginAPI)
			r.Get("/flows", s.handleGetLoginFlow)
			r.Post("/", s.handleSubmitLogin)
		})

		// Registration flow
		r.Route("/registration", func(r chi.Router) {
			r.Get("/browser", s.handleInitRegistrationBrowser)
			r.Get("/api", s.handleInitRegistrationAPI)
			r.Get("/flows", s.handleGetRegistrationFlow)
			r.Post("/", s.handleSubmitRegistration)
		})

		// Recovery flow
		r.Route("/recovery", func(r chi.Router) {
			r.Get("/browser", s.handleInitRecoveryBrowser)
			r.Get("/api", s.handleInitRecoveryAPI)
			r.Get("/flows", s.handleGetRecoveryFlow)
			r.Post("/", s.handleSubmitRecovery)
		})

		// Settings flow
		r.Route("/settings", func(r chi.Router) {
			r.Get("/browser", s.handleInitSettingsBrowser)
			r.Get("/api", s.handleInitSettingsAPI)
			r.Get("/flows", s.handleGetSettingsFlow)
			r.Post("/", s.handleSubmitSettings)
		})

		// Verification flow
		r.Route("/verification", func(r chi.Router) {
			r.Get("/browser", s.handleInitVerificationBrowser)
			r.Get("/api", s.handleInitVerificationAPI)
			r.Get("/flows", s.handleGetVerificationFlow)
			r.Post("/", s.handleSubmitVerification)
		})
	})
}

// setupModuleRoutes configures internal module communication endpoints.
func setupModuleRoutes(r chi.Router, s *Server) {
	// Module registration
	r.Route("/registry", func(r chi.Router) {
		r.Post("/register", s.handleModuleRegister)
		r.Post("/deregister", s.handleModuleDeregister)
		r.Get("/modules", s.handleListModules)
		r.Get("/modules/{id}", s.handleGetModule)
		r.Post("/heartbeat", s.handleModuleHeartbeat)
	})

	// Module proxy (for inter-module communication)
	r.HandleFunc("/proxy/{moduleId}/*", s.handleModuleProxy)

	// Flow management (for modules)
	r.Route("/flows", func(r chi.Router) {
		r.Get("/{id}", s.handleInternalGetFlow)
		r.Post("/{id}/complete", s.handleInternalCompleteFlow)
		r.Post("/{id}/fail", s.handleInternalFailFlow)
		r.Patch("/{id}/ui", s.handleInternalUpdateFlowUI)
	})

	// Health endpoint
	r.Get("/health", s.handleInternalHealth)
}

// setupAdminRoutes configures admin API endpoints.
func setupAdminRoutes(r chi.Router, s *Server) {
	// Protect core admin endpoints from public access by requiring trusted internal auth.
	r.Use(authtoken.Middleware(authtoken.MiddlewareConfig{
		Generator: s.tokenGen,
	}))

	// Admin API
	r.Route("/api/v1", func(r chi.Router) {
		// Identity management
		r.Route("/identities", func(r chi.Router) {
			r.Get("/", s.handleAdminListIdentities)
			r.Post("/", s.handleAdminCreateIdentity)
			r.Get("/{id}", s.handleAdminGetIdentity)
			r.Patch("/{id}", s.handleAdminUpdateIdentity)
			r.Delete("/{id}", s.handleAdminDeleteIdentity)
		})

		// Session management
		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", s.handleAdminListSessions)
			r.Delete("/{id}", s.handleAdminDeleteSession)
			r.Delete("/identity/{identityId}", s.handleAdminDeleteIdentitySessions)
		})

		// Module management
		r.Route("/modules", func(r chi.Router) {
			r.Get("/", s.handleAdminListModules)
			r.Get("/{id}", s.handleAdminGetModule)
			r.Post("/{id}/restart", s.handleAdminRestartModule)
		})

		// System configuration
		r.Route("/system", func(r chi.Router) {
			r.Get("/config", s.handleAdminGetConfig)
			r.Patch("/config", s.handleAdminUpdateConfig)
			r.Get("/health", s.handleAdminSystemHealth)
			r.Get("/metrics", s.handleAdminMetrics)
		})
	})
}

// Self-service flow handlers

func (s *Server) handleInitLoginBrowser(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateLoginFlow(r.Context(), r.URL.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create login flow", err)
		return
	}

	// Redirect to UI with flow ID
	redirectURL := "/ui/login?flow=" + flow.ID.String()
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleInitLoginAPI(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateLoginFlow(r.Context(), r.URL.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create login flow", err)
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleGetLoginFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("id")
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "missing flow id", nil)
		return
	}

	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	flow, err := s.flowService.GetFlow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found", err)
		return
	}

	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleSubmitLogin(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleInitRegistrationBrowser(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateRegistrationFlow(r.Context(), r.URL.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create registration flow", err)
		return
	}
	redirectURL := "/ui/registration?flow=" + flow.ID.String()
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleInitRegistrationAPI(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateRegistrationFlow(r.Context(), r.URL.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create registration flow", err)
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleGetRegistrationFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("id")
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "missing flow id", nil)
		return
	}

	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	flow, err := s.flowService.GetFlow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found", err)
		return
	}

	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleSubmitRegistration(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleInitRecoveryBrowser(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateRecoveryFlow(r.Context(), r.URL.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recovery flow", err)
		return
	}
	redirectURL := "/ui/recovery?flow=" + flow.ID.String()
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleInitRecoveryAPI(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateRecoveryFlow(r.Context(), r.URL.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recovery flow", err)
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleGetRecoveryFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("id")
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "missing flow id", nil)
		return
	}

	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	flow, err := s.flowService.GetFlow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found", err)
		return
	}

	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleSubmitRecovery(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleInitSettingsBrowser(w http.ResponseWriter, r *http.Request) {
	// TODO: Get identity from session
	s.handleNotImplemented(w, r)
}

func (s *Server) handleInitSettingsAPI(w http.ResponseWriter, r *http.Request) {
	// TODO: Get identity from session
	s.handleNotImplemented(w, r)
}

func (s *Server) handleGetSettingsFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("id")
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "missing flow id", nil)
		return
	}

	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	flow, err := s.flowService.GetFlow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found", err)
		return
	}

	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleSubmitSettings(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleInitVerificationBrowser(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateVerificationFlow(r.Context(), r.URL.String(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create verification flow", err)
		return
	}
	redirectURL := "/ui/verification?flow=" + flow.ID.String()
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleInitVerificationAPI(w http.ResponseWriter, r *http.Request) {
	flow, err := s.flowService.CreateVerificationFlow(r.Context(), r.URL.String(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create verification flow", err)
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleGetVerificationFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("id")
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "missing flow id", nil)
		return
	}

	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	flow, err := s.flowService.GetFlow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found", err)
		return
	}

	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleSubmitVerification(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

// Session handlers

func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

// Module registration handlers

func (s *Server) handleModuleRegister(w http.ResponseWriter, r *http.Request) {
	var req registry.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	resp, err := s.registry.Register(req)
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case registry.ErrModuleAlreadyExists:
			status = http.StatusConflict
		case registry.ErrInvalidModule:
			status = http.StatusBadRequest
		}
		writeError(w, status, "registration failed", err)
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleModuleDeregister(w http.ResponseWriter, r *http.Request) {
	var req registry.DeregistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	resp, err := s.registry.Deregister(req.ModuleID)
	if err != nil {
		status := http.StatusInternalServerError
		if err == registry.ErrModuleNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, "deregistration failed", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	modules := s.registry.ListModules(nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"modules": modules,
		"count":   len(modules),
	})
}

func (s *Server) handleGetModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")
	module, err := s.registry.GetModule(moduleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "module not found", err)
		return
	}
	writeJSON(w, http.StatusOK, module)
}

func (s *Server) handleModuleHeartbeat(w http.ResponseWriter, r *http.Request) {
	moduleID := authtoken.ModuleIDFromContext(r.Context())
	if moduleID == "" {
		writeError(w, http.StatusUnauthorized, "module not identified", nil)
		return
	}

	if err := s.registry.UpdateStatus(moduleID, registry.StatusHealthy); err != nil {
		writeError(w, http.StatusNotFound, "module not found", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleModuleProxy(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")
	proxySettings, policySettings := s.moduleProxyRuntimeSettings(r.Context())

	checker := s.policyChecker
	if !policySettings.Enabled {
		checker = nil
	}

	timeout := s.cfg.Proxy.UpstreamTimeout.Duration()
	if parsed, err := time.ParseDuration(strings.TrimSpace(proxySettings.UpstreamTimeout)); err == nil && parsed > 0 {
		timeout = parsed
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	moduleProxy := router.NewModuleProxy(router.ModuleProxyConfig{
		Registry:                    s.registry,
		ModuleID:                    moduleID,
		InternalToken:               s.currentInternalTokenForProxy(),
		SessionSecret:               s.sessionSecretForProxy(),
		Timeout:                     timeout,
		PreserveHost:                proxySettings.PreserveHost,
		StripInboundIdentityHeaders: proxySettings.StripInboundIdentityHeaders,
		IdentitySigningSecret:       s.proxyIdentitySigningSecret(proxySettings.IdentitySigningSecret),
		IdentitySignatureHeader:     proxySettings.IdentitySignatureHeader,
		SignedIdentityHeaders:       proxySettings.SignedIdentityHeaders,
		PolicyChecker:               checker,
		PolicyModel:                 policySettings.DefaultModel,
		Logger:                      s.log.With().Str("component", "module_proxy").Logger(),
	})

	moduleProxy.ServeHTTP(w, r.WithContext(withModuleProxyRequestContext(r.Context(), r)))
}

func (s *Server) moduleProxyRuntimeSettings(ctx context.Context) (runtimeProxySettings, runtimePolicySettings) {
	proxySettings, err := s.loadRuntimeProxySettings(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to load runtime proxy config, using bootstrap defaults")
		proxySettings = defaultRuntimeProxySettings(s.cfg)
	}

	policySettings, err := s.loadRuntimePolicySettings(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to load runtime policy config, using bootstrap defaults")
		policySettings = defaultRuntimePolicySettings(s.cfg)
	}

	return proxySettings, policySettings
}

func (s *Server) currentInternalTokenForProxy() string {
	if s.tokenGen == nil {
		return ""
	}
	token, err := s.tokenGen.Generate("core")
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to generate internal token for module proxy")
		return ""
	}
	return token
}

func (s *Server) sessionSecretForProxy() []byte {
	if len(s.cfg.Secrets.Cookie) > 0 {
		return []byte(s.cfg.Secrets.Cookie[0])
	}
	if len(s.cfg.Secrets.Internal) > 0 {
		return []byte(s.cfg.Secrets.Internal[0])
	}
	if len(s.cfg.Secrets.Cipher) > 0 {
		return []byte(s.cfg.Secrets.Cipher[0])
	}
	return nil
}

func (s *Server) proxyIdentitySigningSecret(override string) []byte {
	if secret := strings.TrimSpace(override); secret != "" {
		return []byte(secret)
	}
	if secret := strings.TrimSpace(s.cfg.Proxy.IdentitySigningSecret); secret != "" {
		return []byte(secret)
	}
	if len(s.cfg.Secrets.Internal) > 0 {
		return []byte(s.cfg.Secrets.Internal[0])
	}
	return nil
}

func withModuleProxyRequestContext(ctx context.Context, r *http.Request) context.Context {
	ip := extractRequestIP(r)
	if ip == "" {
		return ctx
	}
	return router.WithRequestContextIP(ctx, ip)
}

func extractRequestIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(addr, "[]")
}

// Internal flow handlers

func (s *Server) handleInternalGetFlow(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "id")
	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	flow, err := s.flowService.GetFlow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found", err)
		return
	}

	writeJSON(w, http.StatusOK, flow)
}

func (s *Server) handleInternalCompleteFlow(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "id")
	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	if err := s.flowService.CompleteFlow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete flow", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (s *Server) handleInternalFailFlow(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "id")
	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	var req struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if err := s.flowService.FailFlow(r.Context(), id, req.Error); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fail flow", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "failed"})
}

func (s *Server) handleInternalUpdateFlowUI(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleInternalHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"module_count":  s.registry.ModuleCount(),
		"healthy_count": s.registry.HealthyCount(),
	})
}

// Admin handlers (stubs)

func (s *Server) handleAdminListIdentities(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminCreateIdentity(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminGetIdentity(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminUpdateIdentity(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminDeleteIdentity(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminListSessions(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminDeleteSession(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminDeleteIdentitySessions(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminListModules(w http.ResponseWriter, r *http.Request) {
	modules := s.registry.ListModules(nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"modules": modules,
		"count":   len(modules),
	})
}

func (s *Server) handleAdminGetModule(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "id")
	module, err := s.registry.GetModule(moduleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "module not found", err)
		return
	}
	writeJSON(w, http.StatusOK, module)
}

func (s *Server) handleAdminRestartModule(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	policySettings, err := s.loadRuntimePolicySettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load policy runtime config", err)
		return
	}
	proxySettings, err := s.loadRuntimeProxySettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load proxy runtime config", err)
		return
	}

	resp := runtimeConfigResponse{}
	resp.Policy.Enabled = policySettings.Enabled
	resp.Policy.DefaultModel = policySettings.DefaultModel
	resp.Policy.RBAC.Enabled = policySettings.RBAC.Enabled
	resp.Policy.ABAC.Enabled = policySettings.ABAC.Enabled
	resp.Policy.ReBAC.Enabled = policySettings.ReBAC.Enabled

	resp.Proxy.Enabled = proxySettings.Enabled
	resp.Proxy.UpstreamTimeout = proxySettings.UpstreamTimeout
	resp.Proxy.PreserveHost = proxySettings.PreserveHost
	resp.Proxy.StripInboundIdentityHeaders = proxySettings.StripInboundIdentityHeaders
	resp.Proxy.IdentitySigningSecretSet = strings.TrimSpace(proxySettings.IdentitySigningSecret) != ""
	resp.Proxy.IdentitySignatureHeader = proxySettings.IdentitySignatureHeader
	resp.Proxy.SignedIdentityHeaders = append([]string(nil), proxySettings.SignedIdentityHeaders...)

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req runtimeConfigPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if req.Policy == nil && req.Proxy == nil {
		writeError(w, http.StatusBadRequest, "empty config update payload", nil)
		return
	}

	ctx := r.Context()

	if req.Policy != nil {
		current, err := s.loadRuntimePolicySettings(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load policy runtime config", err)
			return
		}
		next, err := applyRuntimePolicyPatch(current, req.Policy)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid policy runtime config patch", err)
			return
		}
		if err := s.saveRuntimeConfig(ctx, systemConfigKeyPolicy, next); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist policy runtime config", err)
			return
		}
	}

	if req.Proxy != nil {
		current, err := s.loadRuntimeProxySettings(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load proxy runtime config", err)
			return
		}
		next, err := applyRuntimeProxyPatch(current, req.Proxy)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid proxy runtime config patch", err)
			return
		}
		if err := s.saveRuntimeConfig(ctx, systemConfigKeyProxy, next); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist proxy runtime config", err)
			return
		}
	}

	s.handleAdminGetConfig(w, r)
}

func (s *Server) loadRuntimePolicySettings(ctx context.Context) (runtimePolicySettings, error) {
	settings := defaultRuntimePolicySettings(s.cfg)
	if s.db == nil || s.db.Pool == nil {
		return settings, nil
	}

	var raw []byte
	err := s.db.Pool.QueryRow(ctx, `
		SELECT value
		FROM core_system_config
		WHERE key = $1
	`, systemConfigKeyPolicy).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings, nil
		}
		return runtimePolicySettings{}, err
	}

	if unmarshalErr := json.Unmarshal(raw, &settings); unmarshalErr != nil {
		return runtimePolicySettings{}, unmarshalErr
	}

	settings.DefaultModel = strings.ToLower(strings.TrimSpace(settings.DefaultModel))
	if settings.DefaultModel == "" {
		settings.DefaultModel = "rbac"
	}

	return settings, nil
}

func (s *Server) loadRuntimeProxySettings(ctx context.Context) (runtimeProxySettings, error) {
	settings := defaultRuntimeProxySettings(s.cfg)
	if s.db == nil || s.db.Pool == nil {
		return settings, nil
	}

	var raw []byte
	err := s.db.Pool.QueryRow(ctx, `
		SELECT value
		FROM core_system_config
		WHERE key = $1
	`, systemConfigKeyProxy).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings, nil
		}
		return runtimeProxySettings{}, err
	}

	if unmarshalErr := json.Unmarshal(raw, &settings); unmarshalErr != nil {
		return runtimeProxySettings{}, unmarshalErr
	}

	settings.UpstreamTimeout = strings.TrimSpace(settings.UpstreamTimeout)
	if settings.UpstreamTimeout == "" {
		settings.UpstreamTimeout = (30 * time.Second).String()
	}
	settings.IdentitySignatureHeader = strings.TrimSpace(settings.IdentitySignatureHeader)
	if settings.IdentitySignatureHeader == "" {
		settings.IdentitySignatureHeader = "X-Aegion-Signature"
	}
	if len(settings.SignedIdentityHeaders) == 0 {
		settings.SignedIdentityHeaders = []string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"}
	}

	return settings, nil
}

func defaultRuntimePolicySettings(cfg *platformconfig.Config) runtimePolicySettings {
	settings := runtimePolicySettings{}
	if cfg == nil {
		settings.DefaultModel = "rbac"
		settings.RBAC.Enabled = true
		return settings
	}

	settings.Enabled = cfg.Policy.Enabled
	settings.DefaultModel = strings.TrimSpace(cfg.Policy.DefaultModel)
	settings.RBAC.Enabled = cfg.Policy.RBAC.Enabled
	settings.ABAC.Enabled = cfg.Policy.ABAC.Enabled
	settings.ReBAC.Enabled = cfg.Policy.ReBAC.Enabled

	if settings.DefaultModel == "" {
		settings.DefaultModel = "rbac"
	}

	return settings
}

func defaultRuntimeProxySettings(cfg *platformconfig.Config) runtimeProxySettings {
	settings := runtimeProxySettings{
		UpstreamTimeout:         (30 * time.Second).String(),
		IdentitySignatureHeader: "X-Aegion-Signature",
		SignedIdentityHeaders:   []string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"},
	}
	if cfg == nil {
		return settings
	}

	settings.Enabled = cfg.Proxy.Enabled
	settings.PreserveHost = cfg.Proxy.PreserveHost
	settings.StripInboundIdentityHeaders = cfg.Proxy.StripInboundIdentityHeaders
	settings.IdentitySigningSecret = strings.TrimSpace(cfg.Proxy.IdentitySigningSecret)

	if timeout := cfg.Proxy.UpstreamTimeout.Duration().String(); timeout != "" && timeout != "0s" {
		settings.UpstreamTimeout = timeout
	}
	if sigHeader := strings.TrimSpace(cfg.Proxy.IdentitySignatureHeader); sigHeader != "" {
		settings.IdentitySignatureHeader = sigHeader
	}
	if len(cfg.Proxy.SignedIdentityHeaders) > 0 {
		settings.SignedIdentityHeaders = append([]string(nil), cfg.Proxy.SignedIdentityHeaders...)
	}

	return settings
}

func applyRuntimePolicyPatch(current runtimePolicySettings, patch *runtimePolicySettingsPatch) (runtimePolicySettings, error) {
	next := current

	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.DefaultModel != nil {
		next.DefaultModel = strings.ToLower(strings.TrimSpace(*patch.DefaultModel))
	}
	if patch.RBAC != nil && patch.RBAC.Enabled != nil {
		next.RBAC.Enabled = *patch.RBAC.Enabled
	}
	if patch.ABAC != nil && patch.ABAC.Enabled != nil {
		next.ABAC.Enabled = *patch.ABAC.Enabled
	}
	if patch.ReBAC != nil && patch.ReBAC.Enabled != nil {
		next.ReBAC.Enabled = *patch.ReBAC.Enabled
	}

	if err := validateRuntimePolicySettings(next); err != nil {
		return runtimePolicySettings{}, err
	}

	return next, nil
}

func applyRuntimeProxyPatch(current runtimeProxySettings, patch *runtimeProxySettingsPatch) (runtimeProxySettings, error) {
	next := current

	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.UpstreamTimeout != nil {
		next.UpstreamTimeout = strings.TrimSpace(*patch.UpstreamTimeout)
	}
	if patch.PreserveHost != nil {
		next.PreserveHost = *patch.PreserveHost
	}
	if patch.StripInboundIdentityHeaders != nil {
		next.StripInboundIdentityHeaders = *patch.StripInboundIdentityHeaders
	}
	if patch.IdentitySigningSecret != nil {
		next.IdentitySigningSecret = strings.TrimSpace(*patch.IdentitySigningSecret)
	}
	if patch.IdentitySignatureHeader != nil {
		next.IdentitySignatureHeader = strings.TrimSpace(*patch.IdentitySignatureHeader)
	}
	if patch.SignedIdentityHeaders != nil {
		next.SignedIdentityHeaders = append([]string(nil), (*patch.SignedIdentityHeaders)...)
	}

	if err := validateRuntimeProxySettings(next); err != nil {
		return runtimeProxySettings{}, err
	}

	return next, nil
}

func (s *Server) saveRuntimeConfig(ctx context.Context, key string, value interface{}) error {
	if s.db == nil || s.db.Pool == nil {
		return errors.New("database unavailable")
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO core_system_config (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, string(raw))
	return err
}

func validateRuntimePolicySettings(settings runtimePolicySettings) error {
	settings.DefaultModel = strings.ToLower(strings.TrimSpace(settings.DefaultModel))
	switch settings.DefaultModel {
	case "rbac", "abac", "rebac":
	default:
		return errors.New("default_model must be one of: rbac, abac, rebac")
	}

	enabledModels := 0
	if settings.RBAC.Enabled {
		enabledModels++
	}
	if settings.ABAC.Enabled {
		enabledModels++
	}
	if settings.ReBAC.Enabled {
		enabledModels++
	}
	if enabledModels == 0 {
		return errors.New("at least one policy model must be enabled")
	}

	return nil
}

func validateRuntimeProxySettings(settings runtimeProxySettings) error {
	if _, err := time.ParseDuration(settings.UpstreamTimeout); err != nil {
		return errors.New("upstream_timeout must be a valid duration")
	}

	settings.IdentitySignatureHeader = strings.TrimSpace(settings.IdentitySignatureHeader)
	if settings.IdentitySignatureHeader == "" {
		return errors.New("identity_signature_header cannot be empty")
	}

	if len(settings.SignedIdentityHeaders) == 0 {
		return errors.New("signed_identity_headers cannot be empty")
	}

	normalized := make([]string, 0, len(settings.SignedIdentityHeaders))
	seen := map[string]struct{}{}
	for _, header := range settings.SignedIdentityHeaders {
		header = strings.TrimSpace(header)
		if header == "" {
			return errors.New("signed_identity_headers cannot contain empty values")
		}
		key := strings.ToLower(header)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, header)
	}
	settings.SignedIdentityHeaders = normalized

	if strings.TrimSpace(settings.IdentitySigningSecret) != "" && len(settings.IdentitySigningSecret) < 16 {
		return errors.New("identity_signing_secret must be at least 16 characters when set")
	}

	return nil
}

func (s *Server) handleAdminSystemHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "healthy",
		"module_count":  s.registry.ModuleCount(),
		"healthy_count": s.registry.HealthyCount(),
	})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error": message,
		"code":  status,
	}
	if err != nil {
		resp["details"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)
}
