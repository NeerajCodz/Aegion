package main

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
)

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
	r.Route("/proxy/{moduleId}/*", func(r chi.Router) {
		r.HandleFunc("/", s.handleModuleProxy)
	})

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
	// Admin API
	r.Route("/api/v1", func(r chi.Router) {
		// TODO: Add admin authentication middleware

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
		if err == registry.ErrModuleAlreadyExists {
			status = http.StatusConflict
		} else if err == registry.ErrInvalidModule {
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
	module, err := s.registry.GetModule(moduleID)
	if err != nil {
		writeError(w, http.StatusNotFound, "module not found", err)
		return
	}

	// Find HTTP endpoint
	var targetURL string
	for _, ep := range module.Endpoints {
		if ep.Type == registry.EndpointHTTP {
			targetURL = ep.URL
			break
		}
	}

	if targetURL == "" {
		writeError(w, http.StatusBadGateway, "no HTTP endpoint available", nil)
		return
	}

	target, err := url.Parse(targetURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid module endpoint", err)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ServeHTTP(w, r)
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
	json.NewDecoder(r.Body).Decode(&req)

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
	s.handleNotImplemented(w, r)
}

func (s *Server) handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	s.handleNotImplemented(w, r)
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
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error":   message,
		"code":    status,
	}
	if err != nil {
		resp["details"] = err.Error()
	}
	json.NewEncoder(w).Encode(resp)
}
