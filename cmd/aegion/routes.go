package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/core/router"
	coresession "github.com/aegion/aegion/core/session"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
	platformconfig "github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/trustedproxy"
)

const (
	systemConfigKeyPolicy = "policy.settings"
	systemConfigKeyProxy  = "proxy.settings"
	adminModuleID         = "admin"
	coreModuleID          = "core"
	maxJSONBodyBytes      = 1 << 20
	defaultFlowRateLimit  = 60
	defaultFlowRateWindow = time.Minute
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
	TrustForwardedHeaders       bool     `json:"trust_forwarded_headers"`
	StripInboundIdentityHeaders bool     `json:"strip_inbound_identity_headers"`
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
	TrustForwardedHeaders       bool     `json:"trust_forwarded_headers"`
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
	TrustForwardedHeaders       *bool     `json:"trust_forwarded_headers"`
	StripInboundIdentityHeaders *bool     `json:"strip_inbound_identity_headers"`
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
	r.Get("/.well-known/openid-configuration", s.handleOIDCDiscovery)
	r.Get("/.well-known/jwks.json", s.handleJWKS)
	r.Get("/oauth2/userinfo", s.handleOAuth2UserInfo)
	r.Post("/oauth2/userinfo", s.handleOAuth2UserInfo)
	r.Get("/self-service/login/methods/link/verify", s.handleMagicLinkLoginVerify)
	r.Get("/self-service/recovery/methods/link/verify", s.handleMagicLinkRecoveryVerify)
	r.Get("/self-service/verification/methods/link/verify", s.handleMagicLinkVerificationVerify)

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
		if limitCfg, ok := selfServiceRateLimitConfig(s.cfg); ok {
			r.Use(router.RateLimitWithTrustProxy(limitCfg, s.cfg.Proxy.TrustForwardedHeaders))
		}

		// Login flow
		r.Route("/login", func(r chi.Router) {
			r.Get("/browser", s.handleInitLoginBrowser)
			r.Get("/api", s.handleInitLoginAPI)
			r.Get("/flows", s.handleGetLoginFlow)
			r.Get("/methods/link/verify", s.handleMagicLinkLoginVerify)
			r.Post("/methods/external/complete", s.handleCompleteExternalLogin)
			r.Post("/methods/passkey/start", s.handleStartLoginPasskey)
			r.Post("/methods/passkey/finish", s.handleFinishLoginPasskey)
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
			r.Get("/methods/link/verify", s.handleMagicLinkRecoveryVerify)
			r.Post("/", s.handleSubmitRecovery)
		})

		// Settings flow
		r.Route("/settings", func(r chi.Router) {
			r.Get("/browser", s.handleInitSettingsBrowser)
			r.Get("/api", s.handleInitSettingsAPI)
			r.Get("/flows", s.handleGetSettingsFlow)
			r.Post("/methods/totp/start", s.handleStartSettingsTOTPEnrollment)
			r.Post("/methods/totp/finish", s.handleFinishSettingsTOTPEnrollment)
			r.Post("/methods/backup-codes/regenerate", s.handleRegenerateSettingsBackupCodes)
			r.Post("/methods/passkey/start", s.handleStartSettingsPasskeyRegistration)
			r.Post("/methods/passkey/finish", s.handleFinishSettingsPasskeyRegistration)
			r.Post("/", s.handleSubmitSettings)
		})

		// Verification flow
		r.Route("/verification", func(r chi.Router) {
			r.Get("/browser", s.handleInitVerificationBrowser)
			r.Get("/api", s.handleInitVerificationAPI)
			r.Get("/flows", s.handleGetVerificationFlow)
			r.Get("/methods/link/verify", s.handleMagicLinkVerificationVerify)
			r.Post("/", s.handleSubmitVerification)
		})
	})
}

func selfServiceRateLimitConfig(cfg *platformconfig.Config) (router.RateLimitConfig, bool) {
	limit := router.RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: float64(defaultFlowRateLimit) / defaultFlowRateWindow.Seconds(),
		Burst:             defaultFlowRateLimit,
	}
	if cfg == nil {
		return limit, true
	}

	rule := cfg.Security.RateLimits.API
	if rule.Requests <= 0 || time.Duration(rule.Period) <= 0 {
		rule = cfg.Security.RateLimits.Global
	}
	if rule.Requests > 0 && time.Duration(rule.Period) > 0 {
		seconds := time.Duration(rule.Period).Seconds()
		if seconds <= 0 {
			return router.RateLimitConfig{}, false
		}
		return router.RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: float64(rule.Requests) / seconds,
			Burst:             rule.Requests,
		}, true
	}

	return limit, true
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
	r.Use(authtoken.RequireModuleID(adminModuleID, coreModuleID))

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
	s.handleFlowSubmit(w, r, flows.TypeLogin)
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
	s.handleFlowSubmit(w, r, flows.TypeRegistration)
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
	s.handleFlowSubmit(w, r, flows.TypeRecovery)
}

func (s *Server) handleInitSettingsBrowser(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		writeError(w, http.StatusInternalServerError, "session manager unavailable", nil)
		return
	}

	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		switch {
		case errors.Is(err, coresession.ErrSessionNotFound),
			errors.Is(err, coresession.ErrSessionExpired),
			errors.Is(err, coresession.ErrSessionInvalid):
			writeError(w, http.StatusUnauthorized, "active session required", nil)
		default:
			writeError(w, http.StatusInternalServerError, "failed to resolve session", err)
		}
		return
	}

	flow, err := s.flowService.CreateSettingsFlow(r.Context(), r.URL.String(), currentSession.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create settings flow", err)
		return
	}
	redirectURL := "/ui/settings?flow=" + flow.ID.String()
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleInitSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		writeError(w, http.StatusInternalServerError, "session manager unavailable", nil)
		return
	}

	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		switch {
		case errors.Is(err, coresession.ErrSessionNotFound),
			errors.Is(err, coresession.ErrSessionExpired),
			errors.Is(err, coresession.ErrSessionInvalid):
			writeError(w, http.StatusUnauthorized, "active session required", nil)
		default:
			writeError(w, http.StatusInternalServerError, "failed to resolve session", err)
		}
		return
	}

	flow, err := s.flowService.CreateSettingsFlow(r.Context(), r.URL.String(), currentSession.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create settings flow", err)
		return
	}
	writeJSON(w, http.StatusOK, flow)
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
	s.handleFlowSubmit(w, r, flows.TypeSettings)
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
	s.handleFlowSubmit(w, r, flows.TypeVerification)
}

type flowSubmitPayload struct {
	FlowID    string `json:"flow_id"`
	Flow      string `json:"flow"`
	ID        string `json:"id"`
	CSRFToken string `json:"csrf_token"`
}

func (s *Server) handleFlowSubmit(w http.ResponseWriter, r *http.Request, expectedType flows.FlowType) {
	input, err := parseFlowSubmitRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow submission payload", err)
		return
	}

	flow, err := s.flowService.ValidateFlow(r.Context(), input.FlowID, input.CSRFToken)
	if err != nil {
		s.writeFlowValidationError(w, err)
		return
	}
	if flow.Type != expectedType {
		writeError(w, http.StatusBadRequest, "flow type mismatch for submission endpoint", nil)
		return
	}

	result, err := s.executeFlowSubmission(r.Context(), w, r, flow, input)
	if err != nil {
		s.writeFlowExecutionError(w, err)
		return
	}
	if result == nil {
		writeError(w, http.StatusBadRequest, "unsupported flow submission method", nil)
		return
	}

	if result != nil && result.KeepFlowActive {
		response := map[string]any{
			"status":    result.Status,
			"flow_id":   input.FlowID.String(),
			"flow_type": string(expectedType),
		}
		if result.Message != "" {
			response["message"] = result.Message
		}
		if result.FlowPayload != nil {
			response["flow"] = result.FlowPayload
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	if err := s.flowService.CompleteFlow(r.Context(), input.FlowID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete flow", err)
		return
	}

	response := map[string]any{
		"status":    "completed",
		"flow_id":   input.FlowID.String(),
		"flow_type": string(expectedType),
	}
	if result != nil {
		if result.Status != "" {
			response["status"] = result.Status
		}
		if result.Message != "" {
			response["message"] = result.Message
		}
		mergeAuthContext(response, result.AuthContext)
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) writeFlowValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, flows.ErrFlowNotFound):
		writeError(w, http.StatusNotFound, "flow not found", err)
	case errors.Is(err, flows.ErrInvalidCSRF):
		writeError(w, http.StatusForbidden, "invalid csrf token", err)
	case errors.Is(err, flows.ErrFlowExpired):
		writeError(w, http.StatusGone, "flow has expired", err)
	case errors.Is(err, flows.ErrFlowCompleted), errors.Is(err, flows.ErrFlowFailed):
		writeError(w, http.StatusConflict, "flow is no longer active", err)
	default:
		writeError(w, http.StatusInternalServerError, "failed to validate flow", err)
	}
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

// Session handlers

func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		writeError(w, http.StatusInternalServerError, "session manager unavailable", nil)
		return
	}

	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		switch {
		case errors.Is(err, coresession.ErrSessionNotFound),
			errors.Is(err, coresession.ErrSessionExpired),
			errors.Is(err, coresession.ErrSessionInvalid):
			writeError(w, http.StatusUnauthorized, "active session required", nil)
		default:
			writeError(w, http.StatusInternalServerError, "failed to resolve session", err)
		}
		return
	}

	writeJSON(w, http.StatusOK, currentSession)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.sessionManager == nil {
		writeError(w, http.StatusInternalServerError, "session manager unavailable", nil)
		return
	}

	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		switch {
		case errors.Is(err, coresession.ErrSessionNotFound),
			errors.Is(err, coresession.ErrSessionExpired),
			errors.Is(err, coresession.ErrSessionInvalid):
			s.sessionManager.ClearCookie(w)
			writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
		default:
			writeError(w, http.StatusInternalServerError, "failed to resolve session", err)
		}
		return
	}

	if err := s.sessionManager.Revoke(r.Context(), currentSession.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke session", err)
		return
	}

	s.sessionManager.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	s.proxyOAuth2Endpoint(w, r, "/.well-known/jwks.json")
}

func (s *Server) handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	s.proxyOAuth2Endpoint(w, r, "/.well-known/openid-configuration")
}

func (s *Server) handleOAuth2UserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	s.proxyOAuth2Endpoint(w, r, "/oidc/userinfo")
}

func (s *Server) proxyOAuth2Endpoint(w http.ResponseWriter, r *http.Request, modulePath string) {
	target, err := s.oauth2EndpointURL(modulePath)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "oauth2 module unavailable", err)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.URL.Path = modulePath
		req.URL.RawPath = ""
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		writeError(w, http.StatusBadGateway, "oauth2 upstream unavailable", proxyErr)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) oauth2EndpointURL(modulePath string) (*url.URL, error) {
	module, err := s.registry.GetModule("oauth2")
	if err != nil {
		return nil, err
	}
	if module.Status != registry.StatusHealthy && module.Status != registry.StatusStarting {
		return nil, errors.New("oauth2 module is not healthy")
	}

	for _, endpoint := range module.Endpoints {
		if endpoint.Type != registry.EndpointHTTP {
			continue
		}
		parsed, parseErr := url.Parse(strings.TrimSpace(endpoint.URL))
		if parseErr != nil {
			return nil, parseErr
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("oauth2 module endpoint must use http or https")
		}
		if parsed.Host == "" {
			return nil, errors.New("oauth2 module endpoint is missing host")
		}
		parsed.Path = modulePath
		parsed.RawPath = ""
		return parsed, nil
	}

	return nil, errors.New("oauth2 module has no HTTP endpoint")
}

// Module registration handlers

func (s *Server) handleModuleRegister(w http.ResponseWriter, r *http.Request) {
	var req registry.RegistrationRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	moduleID := strings.TrimSpace(authtoken.ModuleIDFromContext(r.Context()))
	if moduleID == "" {
		writeError(w, http.StatusUnauthorized, "module not identified", nil)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "module id is required", nil)
		return
	}
	if req.ID != moduleID {
		writeError(w, http.StatusForbidden, "module id mismatch", nil)
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
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	moduleID := strings.TrimSpace(authtoken.ModuleIDFromContext(r.Context()))
	if moduleID == "" {
		writeError(w, http.StatusUnauthorized, "module not identified", nil)
		return
	}
	req.ModuleID = strings.TrimSpace(req.ModuleID)
	if req.ModuleID == "" {
		writeError(w, http.StatusBadRequest, "module id is required", nil)
		return
	}
	if req.ModuleID != moduleID {
		writeError(w, http.StatusForbidden, "module id mismatch", nil)
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
	if !proxySettings.Enabled {
		writeError(w, http.StatusServiceUnavailable, "module proxy disabled", nil)
		return
	}

	checker := s.policyChecker
	requirePolicy := policySettings.Enabled
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
		TrustForwardedHeaders:       proxySettings.TrustForwardedHeaders,
		StripInboundIdentityHeaders: proxySettings.StripInboundIdentityHeaders,
		IdentitySigningSecret:       s.proxyIdentitySigningSecret(),
		IdentitySignatureHeader:     proxySettings.IdentitySignatureHeader,
		SignedIdentityHeaders:       proxySettings.SignedIdentityHeaders,
		PolicyChecker:               checker,
		RequirePolicy:               requirePolicy,
		PolicyModel:                 policySettings.DefaultModel,
		Logger:                      s.log.Logger.With("component", "module_proxy"),
	})

	moduleProxy.ServeHTTP(w, r.WithContext(withModuleProxyRequestContextWithTrust(r.Context(), r, proxySettings.TrustForwardedHeaders)))
}

func (s *Server) moduleProxyRuntimeSettings(ctx context.Context) (runtimeProxySettings, runtimePolicySettings) {
	proxySettings, err := s.loadRuntimeProxySettings(ctx)
	if err != nil {
		s.log.Warn("failed to load runtime proxy config, using bootstrap defaults", "error", err)
		proxySettings = defaultRuntimeProxySettings(s.cfg)
	}
	if err := s.ensureRuntimeProxySigning(proxySettings); err != nil {
		s.log.Warn("runtime proxy config missing signing secret, forcing identity header stripping", "error", err)
		proxySettings.StripInboundIdentityHeaders = true
	}

	policySettings, err := s.loadRuntimePolicySettings(ctx)
	if err != nil {
		s.log.Warn("failed to load runtime policy config, using bootstrap defaults", "error", err)
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
		s.log.Warn("failed to generate internal token for module proxy", "error", err)
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

func (s *Server) proxyIdentitySigningSecret() []byte {
	if secret := strings.TrimSpace(s.cfg.Proxy.IdentitySigningSecret); secret != "" {
		return []byte(secret)
	}
	if len(s.cfg.Secrets.Internal) > 0 {
		return []byte(s.cfg.Secrets.Internal[0])
	}
	return nil
}

func withModuleProxyRequestContext(ctx context.Context, r *http.Request) context.Context {
	return withModuleProxyRequestContextWithTrust(ctx, r, false)
}

func withModuleProxyRequestContextWithTrust(ctx context.Context, r *http.Request, trustForwardedHeaders bool) context.Context {
	ip := extractRequestIPWithTrust(r, trustForwardedHeaders)
	if ip == "" {
		return ctx
	}
	return router.WithRequestContextIP(ctx, ip)
}

func extractRequestIP(r *http.Request) string {
	return extractRequestIPWithTrust(r, false)
}

func extractRequestIPWithTrust(r *http.Request, trustForwardedHeaders bool) string {
	return trustedproxy.ClientIP(r, trustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS")
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
	if err := decodeJSONBody(w, r, &req); err != nil {
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
	flowID := chi.URLParam(r, "id")
	id, err := uuid.Parse(flowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if len(strings.TrimSpace(string(rawBody))) == 0 {
		writeError(w, http.StatusBadRequest, "invalid request body", errors.New("empty body"))
		return
	}

	var wrapped struct {
		UI *flows.UIState `json:"ui"`
	}
	if err := json.Unmarshal(rawBody, &wrapped); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	var ui flows.UIState
	if wrapped.UI != nil {
		ui = *wrapped.UI
	} else if err := json.Unmarshal(rawBody, &ui); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if err := s.flowService.UpdateFlowUI(r.Context(), id, &ui); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, flows.ErrFlowNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "failed to update flow ui", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleInternalHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"module_count":  s.registry.ModuleCount(),
		"healthy_count": s.registry.HealthyCount(),
	})
}

// Admin handlers

type adminCreateIdentityRequest struct {
	SchemaID string                 `json:"schema_id"`
	Traits   map[string]interface{} `json:"traits"`
	State    string                 `json:"state"`
}

type adminUpdateIdentityRequest struct {
	Traits map[string]interface{} `json:"traits"`
	State  *string                `json:"state"`
}

type adminIdentityView struct {
	ID        string                 `json:"id"`
	SchemaID  string                 `json:"schema_id"`
	Traits    map[string]interface{} `json:"traits"`
	State     string                 `json:"state"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type adminSessionView struct {
	ID              string    `json:"id"`
	IdentityID      string    `json:"identity_id"`
	AAL             string    `json:"aal"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	IPAddress       string    `json:"ip_address,omitempty"`
	UserAgent       string    `json:"user_agent,omitempty"`
}

func (s *Server) handleAdminListIdentities(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminDatabase(w) {
		return
	}

	page, perPage, offset := s.parseAdminPagination(r)
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	sortExpr := adminIdentitySortExpr(r.URL.Query().Get("sort"))

	where := "WHERE ci.deleted_at IS NULL"
	args := []interface{}{}
	argPos := 1

	if filter != "" {
		where += `
		 AND (
			LOWER(COALESCE(
				(SELECT value FROM core_identity_addresses a
				 WHERE a.identity_id = ci.id AND a.type = 'email'
				 ORDER BY a.is_primary DESC, a.created_at ASC LIMIT 1),
				ci.traits->>'email',
				''
			)) LIKE LOWER($` + strconv.Itoa(argPos) + `)
			OR LOWER(COALESCE(ci.traits->>'display_name', ci.traits->>'name', '')) LIKE LOWER($` + strconv.Itoa(argPos) + `)
		 )`
		args = append(args, "%"+filter+"%")
		argPos++
	}

	var total int64
	if err := s.queryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_identities ci
	`+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identities", err)
		return
	}

	query := `
		SELECT ci.id, ci.schema_id, ci.traits, ci.state, ci.created_at, ci.updated_at
		FROM core_identities ci
	` + where + `
		ORDER BY ` + sortExpr + `
		LIMIT $` + strconv.Itoa(argPos) + ` OFFSET $` + strconv.Itoa(argPos+1)
	args = append(args, perPage, offset)

	rows, err := s.query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identities", err)
		return
	}
	defer rows.Close()

	identities := make([]adminIdentityView, 0, perPage)
	for rows.Next() {
		var (
			identityID uuid.UUID
			schemaID   uuid.UUID
			traitsRaw  []byte
			state      string
			item       adminIdentityView
		)
		if scanErr := rows.Scan(&identityID, &schemaID, &traitsRaw, &state, &item.CreatedAt, &item.UpdatedAt); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read identities", scanErr)
			return
		}

		item.ID = identityID.String()
		item.SchemaID = schemaID.String()
		item.State = mapAdminIdentityStateFromDB(state)
		item.Traits = map[string]interface{}{}
		if unmarshalErr := json.Unmarshal(traitsRaw, &item.Traits); unmarshalErr != nil {
			item.Traits = map[string]interface{}{}
		}

		identities = append(identities, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identities", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"identities": identities,
		"pagination": adminPaginationMeta(total, page, perPage),
	})
}

func (s *Server) handleAdminCreateIdentity(w http.ResponseWriter, r *http.Request) {
	var req adminCreateIdentityRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if !s.requireAdminDatabase(w) {
		return
	}

	if req.Traits == nil {
		req.Traits = map[string]interface{}{}
	}

	state, err := normalizeAdminIdentityState(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity state", err)
		return
	}

	schemaID, err := s.resolveAdminSchemaID(r.Context(), req.SchemaID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity schema", err)
		return
	}

	traitsJSON, err := json.Marshal(req.Traits)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity traits", err)
		return
	}

	tx, err := s.begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create identity", err)
		return
	}
	defer func() {
		if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	identityID := uuid.New()
	_, err = tx.Exec(r.Context(), `
		INSERT INTO core_identities (id, schema_id, traits, state, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, NOW(), NOW())
	`, identityID, schemaID, string(traitsJSON), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create identity", err)
		return
	}

	if email, ok := adminEmailFromTraits(req.Traits); ok {
		if upsertErr := upsertPrimaryIdentityEmail(r.Context(), tx, identityID, email); upsertErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist identity email", upsertErr)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create identity", err)
		return
	}

	identity, found, err := s.getAdminIdentityByID(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load created identity", err)
		return
	}
	if !found {
		writeError(w, http.StatusInternalServerError, "created identity not found", nil)
		return
	}

	writeJSON(w, http.StatusCreated, identity)
}

func (s *Server) handleAdminGetIdentity(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity id", err)
		return
	}
	if !s.requireAdminDatabase(w) {
		return
	}

	identity, found, err := s.getAdminIdentityByID(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load identity", err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "identity not found", nil)
		return
	}

	writeJSON(w, http.StatusOK, identity)
}

func (s *Server) handleAdminUpdateIdentity(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity id", err)
		return
	}

	var req adminUpdateIdentityRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if req.Traits == nil && req.State == nil {
		writeError(w, http.StatusBadRequest, "empty update payload", nil)
		return
	}
	if !s.requireAdminDatabase(w) {
		return
	}

	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argPos := 1

	emailToUpsert := ""
	if req.Traits != nil {
		traitsJSON, marshalErr := json.Marshal(req.Traits)
		if marshalErr != nil {
			writeError(w, http.StatusBadRequest, "invalid identity traits", marshalErr)
			return
		}
		setClauses = append(setClauses, "traits = COALESCE(traits, '{}'::jsonb) || $"+strconv.Itoa(argPos)+"::jsonb")
		args = append(args, string(traitsJSON))
		argPos++

		if email, ok := adminEmailFromTraits(req.Traits); ok {
			emailToUpsert = email
		}
	}

	if req.State != nil {
		stateValue := strings.TrimSpace(*req.State)
		if stateValue == "" {
			writeError(w, http.StatusBadRequest, "invalid identity state", errors.New("state cannot be empty"))
			return
		}
		normalizedState, stateErr := normalizeAdminIdentityState(stateValue)
		if stateErr != nil {
			writeError(w, http.StatusBadRequest, "invalid identity state", stateErr)
			return
		}
		setClauses = append(setClauses, "state = $"+strconv.Itoa(argPos))
		args = append(args, normalizedState)
		argPos++
	}

	tx, err := s.begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update identity", err)
		return
	}
	defer func() {
		if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	query := `
		UPDATE core_identities
		SET ` + strings.Join(setClauses, ", ") + `
		WHERE id = $` + strconv.Itoa(argPos) + `
		  AND deleted_at IS NULL
	`
	args = append(args, identityID)

	result, err := tx.Exec(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update identity", err)
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "identity not found", nil)
		return
	}

	if emailToUpsert != "" {
		if upsertErr := upsertPrimaryIdentityEmail(r.Context(), tx, identityID, emailToUpsert); upsertErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist identity email", upsertErr)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update identity", err)
		return
	}

	identity, found, err := s.getAdminIdentityByID(r.Context(), identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated identity", err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "identity not found", nil)
		return
	}

	writeJSON(w, http.StatusOK, identity)
}

func (s *Server) handleAdminDeleteIdentity(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity id", err)
		return
	}
	if !s.requireAdminDatabase(w) {
		return
	}

	tx, err := s.begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete identity", err)
		return
	}
	defer func() {
		if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	result, err := tx.Exec(r.Context(), `
		UPDATE core_identities
		SET state = 'inactive', deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
	`, identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete identity", err)
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "identity not found", nil)
		return
	}

	if _, err := tx.Exec(r.Context(), `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE identity_id = $1
		  AND active = TRUE
	`, identityID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke identity sessions", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete identity", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminListSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminDatabase(w) {
		return
	}

	page, perPage, offset := s.parseAdminPagination(r)
	identityFilter := strings.TrimSpace(r.URL.Query().Get("identity_id"))

	where := "WHERE cs.active = TRUE AND cs.expires_at > NOW()"
	args := []interface{}{}
	argPos := 1

	if identityFilter != "" {
		identityID, parseErr := uuid.Parse(identityFilter)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid identity id filter", parseErr)
			return
		}
		where += " AND cs.identity_id = $" + strconv.Itoa(argPos)
		args = append(args, identityID)
		argPos++
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM core_sessions cs " + where
	if err := s.queryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions", err)
		return
	}

	query := `
		SELECT
			cs.id,
			cs.identity_id,
			cs.aal,
			cs.active,
			cs.created_at,
			cs.expires_at,
			cs.authenticated_at,
			COALESCE(NULLIF((cs.devices->0->>'ip_address'), ''), '') AS ip_address,
			COALESCE(NULLIF((cs.devices->0->>'user_agent'), ''), '') AS user_agent
		FROM core_sessions cs
	` + where + `
		ORDER BY cs.created_at DESC
		LIMIT $` + strconv.Itoa(argPos) + ` OFFSET $` + strconv.Itoa(argPos+1)
	args = append(args, perPage, offset)

	rows, err := s.query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions", err)
		return
	}
	defer rows.Close()

	sessions := make([]adminSessionView, 0, perPage)
	for rows.Next() {
		var (
			sessionID  uuid.UUID
			identityID uuid.UUID
			item       adminSessionView
		)
		if scanErr := rows.Scan(
			&sessionID,
			&identityID,
			&item.AAL,
			&item.Active,
			&item.CreatedAt,
			&item.ExpiresAt,
			&item.AuthenticatedAt,
			&item.IPAddress,
			&item.UserAgent,
		); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read sessions", scanErr)
			return
		}
		item.ID = sessionID.String()
		item.IdentityID = identityID.String()
		sessions = append(sessions, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions":   sessions,
		"pagination": adminPaginationMeta(total, page, perPage),
	})
}

func (s *Server) handleAdminDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id", err)
		return
	}
	if !s.requireAdminDatabase(w) {
		return
	}

	result, err := s.exec(r.Context(), `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE id = $1
		  AND active = TRUE
	`, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke session", err)
		return
	}

	if result.RowsAffected() == 0 {
		var exists bool
		if err := s.queryRow(r.Context(), `
			SELECT EXISTS(SELECT 1 FROM core_sessions WHERE id = $1)
		`, sessionID).Scan(&exists); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke session", err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "session not found", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":     "already_revoked",
			"session_id": sessionID.String(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "revoked",
		"session_id": sessionID.String(),
	})
}

func (s *Server) handleAdminDeleteIdentitySessions(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "identityId")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity id", err)
		return
	}
	if !s.requireAdminDatabase(w) {
		return
	}

	result, err := s.exec(r.Context(), `
		UPDATE core_sessions
		SET active = FALSE, updated_at = NOW()
		WHERE identity_id = $1
		  AND active = TRUE
	`, identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke identity sessions", err)
		return
	}

	if result.RowsAffected() == 0 {
		var exists bool
		if err := s.queryRow(r.Context(), `
			SELECT EXISTS(
				SELECT 1
				FROM core_identities
				WHERE id = $1
				  AND deleted_at IS NULL
			)
		`, identityID).Scan(&exists); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke identity sessions", err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "identity not found", nil)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "revoked",
		"identity_id": identityID.String(),
		"revoked":     result.RowsAffected(),
	})
}

func (s *Server) requireAdminDatabase(w http.ResponseWriter) bool {
	if !s.hasDatabaseAccess() {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", nil)
		return false
	}
	return true
}

func (s *Server) parseAdminPagination(r *http.Request) (page, perPage, offset int) {
	defaultPageSize, maxPageSize := s.adminPageSettings()

	page = parsePositiveInt(r.URL.Query().Get("page"), 1)
	perPage = parsePositiveInt(r.URL.Query().Get("per_page"), defaultPageSize)

	if perPage > maxPageSize {
		perPage = maxPageSize
	}
	if perPage < 1 {
		perPage = defaultPageSize
	}

	offset = (page - 1) * perPage
	return
}

func (s *Server) adminPageSettings() (defaultPageSize, maxPageSize int) {
	defaultPageSize = 20
	maxPageSize = 100

	if s.cfg != nil {
		if s.cfg.Admin.DefaultPageSize > 0 {
			defaultPageSize = s.cfg.Admin.DefaultPageSize
		}
		if s.cfg.Admin.MaxPageSize > 0 {
			maxPageSize = s.cfg.Admin.MaxPageSize
		}
	}

	if maxPageSize < 1 {
		maxPageSize = 100
	}
	if defaultPageSize > maxPageSize {
		defaultPageSize = maxPageSize
	}

	return
}

func parsePositiveInt(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func adminPaginationMeta(total int64, page, perPage int) map[string]interface{} {
	totalPages := 1
	if perPage > 0 && total > 0 {
		totalPages = int(total) / perPage
		if int(total)%perPage != 0 {
			totalPages++
		}
	}

	return map[string]interface{}{
		"page":        page,
		"per_page":    perPage,
		"total":       total,
		"total_pages": totalPages,
	}
}

func adminIdentitySortExpr(sort string) string {
	switch strings.TrimSpace(sort) {
	case "created_at":
		return "ci.created_at ASC"
	case "-created_at":
		return "ci.created_at DESC"
	case "updated_at":
		return "ci.updated_at ASC"
	case "-updated_at":
		return "ci.updated_at DESC"
	case "state":
		return "ci.state ASC, ci.created_at DESC"
	case "-state":
		return "ci.state DESC, ci.created_at DESC"
	default:
		return "ci.created_at DESC"
	}
}

func mapAdminIdentityStateFromDB(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "inactive":
		return "inactive"
	case "banned":
		return "blocked"
	default:
		return "active"
	}
}

func normalizeAdminIdentityState(state string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(state))
	switch normalized {
	case "":
		return "active", nil
	case "active", "inactive", "banned":
		return normalized, nil
	case "blocked":
		return "banned", nil
	default:
		return "", errors.New("state must be one of: active, inactive, blocked")
	}
}

func adminEmailFromTraits(traits map[string]interface{}) (string, bool) {
	if traits == nil {
		return "", false
	}
	raw, ok := traits["email"]
	if !ok {
		return "", false
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", false
	}
	return value, true
}

func upsertPrimaryIdentityEmail(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, email string) error {
	result, err := tx.Exec(ctx, `
		UPDATE core_identity_addresses
		SET value = $1, verified = FALSE, verified_at = NULL, updated_at = NOW()
		WHERE identity_id = $2
		  AND type = 'email'
		  AND is_primary = TRUE
	`, email, identityID)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO core_identity_addresses (id, identity_id, type, value, is_primary, verified, created_at, updated_at)
		VALUES ($1, $2, 'email', $3, TRUE, FALSE, NOW(), NOW())
	`, uuid.New(), identityID, email)
	return err
}

func (s *Server) resolveAdminSchemaID(ctx context.Context, schemaRef string) (uuid.UUID, error) {
	var (
		schemaID uuid.UUID
		err      error
	)

	schemaRef = strings.TrimSpace(schemaRef)
	switch {
	case schemaRef == "":
		err = s.queryRow(ctx, `
			SELECT id
			FROM core_identity_schemas
			ORDER BY is_default DESC, created_at ASC
			LIMIT 1
		`).Scan(&schemaID)
	case isUUID(schemaRef):
		parsed := uuid.MustParse(schemaRef)
		err = s.queryRow(ctx, `
			SELECT id
			FROM core_identity_schemas
			WHERE id = $1
		`, parsed).Scan(&schemaID)
	default:
		err = s.queryRow(ctx, `
			SELECT id
			FROM core_identity_schemas
			WHERE name = $1
			LIMIT 1
		`, schemaRef).Scan(&schemaID)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, errors.New("identity schema not found")
		}
		return uuid.Nil, err
	}
	return schemaID, nil
}

func (s *Server) getAdminIdentityByID(ctx context.Context, identityID uuid.UUID) (*adminIdentityView, bool, error) {
	var (
		id        uuid.UUID
		schemaID  uuid.UUID
		traitsRaw []byte
		state     string
		item      adminIdentityView
	)

	err := s.queryRow(ctx, `
		SELECT id, schema_id, traits, state, created_at, updated_at
		FROM core_identities
		WHERE id = $1
		  AND deleted_at IS NULL
	`, identityID).Scan(&id, &schemaID, &traitsRaw, &state, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	item.ID = id.String()
	item.SchemaID = schemaID.String()
	item.State = mapAdminIdentityStateFromDB(state)
	item.Traits = map[string]interface{}{}
	if err := json.Unmarshal(traitsRaw, &item.Traits); err != nil {
		item.Traits = map[string]interface{}{}
	}

	return &item, true, nil
}

func isUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
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
	moduleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if moduleID == "" {
		writeError(w, http.StatusBadRequest, "module id is required", nil)
		return
	}
	if _, err := s.registry.GetModule(moduleID); err != nil {
		writeError(w, http.StatusNotFound, "module not found", err)
		return
	}
	if s.orchestrator == nil {
		writeError(w, http.StatusServiceUnavailable, "module orchestrator unavailable", nil)
		return
	}

	err := s.orchestrator.RestartModule(r.Context(), moduleID)
	if err != nil {
		switch {
		case errors.Is(err, orchestrator.ErrModuleNotFound):
			writeError(w, http.StatusNotFound, "module not found", err)
		case errors.Is(err, orchestrator.ErrOrchestratorClosed):
			writeError(w, http.StatusServiceUnavailable, "module orchestrator unavailable", err)
		default:
			writeError(w, http.StatusInternalServerError, "failed to restart module", err)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "restarted",
		"module_id": moduleID,
	})
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
	resp.Proxy.TrustForwardedHeaders = proxySettings.TrustForwardedHeaders
	resp.Proxy.StripInboundIdentityHeaders = proxySettings.StripInboundIdentityHeaders
	resp.Proxy.IdentitySigningSecretSet = len(s.proxyIdentitySigningSecret()) > 0
	resp.Proxy.IdentitySignatureHeader = proxySettings.IdentitySignatureHeader
	resp.Proxy.SignedIdentityHeaders = append([]string(nil), proxySettings.SignedIdentityHeaders...)

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req runtimeConfigPatchRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
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
		if err := s.ensureRuntimeProxySigning(next); err != nil {
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
	if !s.hasDatabaseAccess() {
		return settings, nil
	}

	var raw []byte
	err := s.queryRow(ctx, sqlSelectSystemConfigByKey, systemConfigKeyPolicy).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings, nil
		}
		return runtimePolicySettings{}, err
	}

	if unmarshalErr := json.Unmarshal(raw, &settings); unmarshalErr != nil {
		return runtimePolicySettings{}, unmarshalErr
	}

	settings.DefaultModel = normalizeRuntimePolicyModel(settings.DefaultModel)
	if err := validateRuntimePolicySettings(settings); err != nil {
		return runtimePolicySettings{}, err
	}

	return settings, nil
}

func (s *Server) loadRuntimeProxySettings(ctx context.Context) (runtimeProxySettings, error) {
	settings := defaultRuntimeProxySettings(s.cfg)
	if !s.hasDatabaseAccess() {
		return settings, nil
	}

	var raw []byte
	err := s.queryRow(ctx, sqlSelectSystemConfigByKey, systemConfigKeyProxy).Scan(&raw)
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
	settings.DefaultModel = normalizeRuntimePolicyModel(cfg.Policy.DefaultModel)
	settings.RBAC.Enabled = cfg.Policy.RBAC.Enabled
	settings.ABAC.Enabled = cfg.Policy.ABAC.Enabled
	settings.ReBAC.Enabled = cfg.Policy.ReBAC.Enabled
	switch settings.DefaultModel {
	case "rbac", "abac", "rebac":
	default:
		settings.DefaultModel = "rbac"
	}

	if settings.Enabled {
		if !settings.RBAC.Enabled && !settings.ABAC.Enabled && !settings.ReBAC.Enabled {
			settings.RBAC.Enabled = true
		}
		if !runtimePolicyModelEnabled(settings, settings.DefaultModel) {
			settings.DefaultModel = firstEnabledRuntimePolicyModel(settings)
		}
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
	settings.TrustForwardedHeaders = cfg.Proxy.TrustForwardedHeaders
	settings.StripInboundIdentityHeaders = cfg.Proxy.StripInboundIdentityHeaders

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
	if patch.TrustForwardedHeaders != nil {
		next.TrustForwardedHeaders = *patch.TrustForwardedHeaders
	}
	if patch.StripInboundIdentityHeaders != nil {
		next.StripInboundIdentityHeaders = *patch.StripInboundIdentityHeaders
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
	if !s.hasDatabaseAccess() {
		return errors.New("database unavailable")
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}

	_, err = s.exec(ctx, sqlUpsertSystemConfig, key, string(raw))
	return err
}

func validateRuntimePolicySettings(settings runtimePolicySettings) error {
	settings.DefaultModel = normalizeRuntimePolicyModel(settings.DefaultModel)
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
	if settings.Enabled && enabledModels == 0 {
		return errors.New("at least one policy model must be enabled")
	}
	if settings.Enabled && !runtimePolicyModelEnabled(settings, settings.DefaultModel) {
		return errors.New("default_model must reference an enabled policy model")
	}

	return nil
}

func normalizeRuntimePolicyModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "rbac"
	}
	return model
}

func runtimePolicyModelEnabled(settings runtimePolicySettings, model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "rbac":
		return settings.RBAC.Enabled
	case "abac":
		return settings.ABAC.Enabled
	case "rebac":
		return settings.ReBAC.Enabled
	default:
		return false
	}
}

func firstEnabledRuntimePolicyModel(settings runtimePolicySettings) string {
	switch {
	case settings.RBAC.Enabled:
		return "rbac"
	case settings.ABAC.Enabled:
		return "abac"
	case settings.ReBAC.Enabled:
		return "rebac"
	default:
		return "rbac"
	}
}

func validateRuntimeProxySettings(settings runtimeProxySettings) error {
	if _, err := time.ParseDuration(settings.UpstreamTimeout); err != nil {
		return errors.New("upstream_timeout must be a valid duration")
	}

	if settings.TrustForwardedHeaders && strings.TrimSpace(os.Getenv("AEGION_TRUSTED_PROXY_CIDRS")) == "" {
		return errors.New("trusted proxy CIDRs are required when trust_forwarded_headers is enabled")
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

	if isRuntimeProductionEnvironment() && !settings.StripInboundIdentityHeaders {
		return errors.New("strip_inbound_identity_headers cannot be disabled in production")
	}

	return nil
}

func (s *Server) ensureRuntimeProxySigning(settings runtimeProxySettings) error {
	if settings.StripInboundIdentityHeaders {
		return nil
	}
	if len(s.proxyIdentitySigningSecret()) == 0 {
		return errors.New("bootstrap proxy identity signing secret is required when strip_inbound_identity_headers is disabled")
	}
	return nil
}

func isRuntimeProductionEnvironment() bool {
	for _, key := range []string{"AEGION_ENV", "AEGION_ENVIRONMENT"} {
		env := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if env == "production" || env == "prod" {
			return true
		}
	}
	return false
}

func (s *Server) handleAdminSystemHealth(w http.ResponseWriter, r *http.Request) {
	moduleCount := 0
	healthyCount := 0
	if s.registry != nil {
		moduleCount = s.registry.ModuleCount()
		healthyCount = s.registry.HealthyCount()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "healthy",
		"module_count":  moduleCount,
		"healthy_count": healthyCount,
	})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	moduleCount := 0
	healthyCount := 0
	if s.registry != nil {
		moduleCount = s.registry.ModuleCount()
		healthyCount = s.registry.HealthyCount()
	}

	resp := map[string]interface{}{
		"module_count":  moduleCount,
		"healthy_count": healthyCount,
	}

	if !s.hasDatabaseAccess() {
		resp["database"] = "unavailable"
		writeJSON(w, http.StatusOK, resp)
		return
	}

	var identityCount int64
	if err := s.queryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_identities
		WHERE deleted_at IS NULL
	`).Scan(&identityCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load identity metrics", err)
		return
	}

	var activeSessionCount int64
	if err := s.queryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_sessions
		WHERE active = TRUE
		  AND expires_at > NOW()
	`).Scan(&activeSessionCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load session metrics", err)
		return
	}

	resp["database"] = "connected"
	resp["identities_total"] = identityCount
	resp["sessions_active"] = activeSessionCount
	writeJSON(w, http.StatusOK, resp)
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	_ = err // Avoid leaking internal error details in API responses.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error": message,
		"code":  status,
	}
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
