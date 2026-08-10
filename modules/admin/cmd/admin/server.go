package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformobservability "github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/internal/xlog"
	admin "github.com/aegion/aegion/modules/admin"
	"github.com/aegion/aegion/modules/admin/handler"
	"github.com/aegion/aegion/modules/admin/scim"
	"github.com/aegion/aegion/modules/admin/security"
	"github.com/aegion/aegion/modules/admin/service"
)

// DBPinger is an interface for DB health checking
type DBPinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	Config      *Config
	DB          *pgxpool.Pool
	dbPing      DBPinger // for testing
	Handler     *handler.Handler
	SCIMService *scim.Service
	SCIMHandler *scim.Handler

	adminPath string
	spaServer *SPAFileServer
}

type dashboardObservabilityEndpoint struct {
	Key   string
	Label string
	URL   string
}

type dashboardObservabilityProbe struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	URL            string `json:"url"`
	Status         string `json:"status"`
	StatusCode     int    `json:"status_code"`
	ResponseTimeMS int64  `json:"response_time_ms"`
	Message        string `json:"message"`
	CheckedAt      string `json:"checked_at"`
}

type dashboardTelemetrySummary struct {
	ServiceName            string  `json:"service_name"`
	ServiceVersion         string  `json:"service_version"`
	Environment            string  `json:"environment"`
	InstanceID             string  `json:"instance_id"`
	TracesEnabled          bool    `json:"traces_enabled"`
	MetricsEnabled         bool    `json:"metrics_enabled"`
	LogsEnabled            bool    `json:"logs_enabled"`
	TracesEndpoint         string  `json:"traces_endpoint"`
	MetricsEndpoint        string  `json:"metrics_endpoint"`
	LogsEndpoint           string  `json:"logs_endpoint"`
	TraceSamplingRatio     float64 `json:"trace_sampling_ratio"`
	MetricExportInterval   string  `json:"metric_export_interval"`
	TraceExportTimeout     string  `json:"trace_export_timeout"`
	InsecureExporter       bool    `json:"insecure_exporter"`
	TracesEndpointPresent  bool    `json:"traces_endpoint_present"`
	MetricsEndpointPresent bool    `json:"metrics_endpoint_present"`
	LogsEndpointPresent    bool    `json:"logs_endpoint_present"`
}

type dashboardGuardrailsSummary struct {
	AdminAuthRequired         bool     `json:"admin_auth_required"`
	ObservabilityRBAC         bool     `json:"observability_rbac"`
	AdminRateLimiting         bool     `json:"admin_rate_limiting"`
	AdminCSRFProtection       bool     `json:"admin_csrf_protection"`
	StrictTransportSecurity   bool     `json:"strict_transport_security"`
	TrustedProxyHeaders       bool     `json:"trusted_proxy_headers"`
	SCIMBearerAuth            bool     `json:"scim_bearer_auth"`
	SCIMUnknownFieldRejection bool     `json:"scim_unknown_field_rejection"`
	SCIMBodyLimitBytes        int64    `json:"scim_body_limit_bytes"`
	TelemetrySecretsRedacted  bool     `json:"telemetry_secrets_redacted"`
	Warnings                  []string `json:"warnings"`
}

type dashboardSCIMSummary struct {
	Enabled            bool     `json:"enabled"`
	BasePath           string   `json:"base_path"`
	MappingCount       int      `json:"mapping_count"`
	TokenCount         int      `json:"token_count"`
	ActiveTokenCount   int      `json:"active_token_count"`
	ExpiredTokenCount  int      `json:"expired_token_count"`
	ExpiringTokenCount int      `json:"expiring_token_count"`
	WildcardTokenCount int      `json:"wildcard_token_count"`
	WriteTokenCount    int      `json:"write_token_count"`
	LastTokenUsedAt    string   `json:"last_token_used_at,omitempty"`
	TokenPrefix        string   `json:"token_prefix"`
	Warnings           []string `json:"warnings"`
}

type dashboardObservabilityResponse struct {
	Enabled     bool                          `json:"enabled"`
	GeneratedAt string                        `json:"generated_at"`
	Telemetry   dashboardTelemetrySummary     `json:"telemetry"`
	Guardrails  dashboardGuardrailsSummary    `json:"guardrails"`
	SCIM        *dashboardSCIMSummary         `json:"scim,omitempty"`
	Stack       []dashboardObservabilityProbe `json:"stack"`
}

func (s *Server) setupRouter() chi.Router {
	s.ensureRoutingAssets()
	return s.setupRouterAt("/api/admin")
}

// setupPublicRouter binds the admin module to the core-owned public prefix.
// buildRuntime fixes the configured SPA and SCIM paths before this is called.
func (s *Server) setupPublicRouter() chi.Router {
	s.ensureRoutingAssets()
	return s.setupRouterAt(adminPublicRoutePrefix + "/api/admin")
}

func (s *Server) setupRouterAt(apiPath string) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Compress(5))
	r.Use(s.securityHeaders)
	r.Use(s.logRequest)

	// Health endpoint (no auth required)
	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)

	// Admin API routes
	r.Route(apiPath, func(r chi.Router) {
		r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigRead)).
			Get("/dashboard/config", s.handleDashboardConfig)
		r.Group(func(r chi.Router) {
			r.Use(security.RateLimitAdmin)
			r.Use(security.CSRFProtection)
			r.Use(security.SecurityAudit)
			r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigRead)).
				Get("/dashboard/observability", s.handleDashboardObservability)
			if s.SCIMService != nil {
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigRead)).
					Get("/scim/tokens", s.handleListSCIMTokens)
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigUpdate)).
					Post("/scim/tokens", s.handleCreateSCIMToken)
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigUpdate)).
					Delete("/scim/tokens/{id}", s.handleDeleteSCIMToken)
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigRead)).
					Get("/scim/mappings", s.handleListSCIMMappings)
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigUpdate)).
					Post("/scim/mappings", s.handleCreateSCIMMapping)
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigUpdate)).
					Put("/scim/mappings/{id}", s.handleUpdateSCIMMapping)
				r.With(s.Handler.RequireAdmin, handler.RequirePermission(s.Handler, service.PermConfigUpdate)).
					Delete("/scim/mappings/{id}", s.handleDeleteSCIMMapping)
			}
			s.Handler.RegisterRoutes(r)
		})
	})

	if s.SCIMHandler != nil && s.Config.Admin.SCIM.Enabled {
		r.Route(normalizeMountedPath(s.Config.Admin.SCIM.BasePath), func(r chi.Router) {
			s.SCIMHandler.RegisterRoutes(r)
		})
	}

	// Serve the embedded SPA and its assets under the catalog prefix.
	r.Mount(s.adminPath, s.spaHandler())

	// Fallback route for SPA routing
	r.NotFound(s.spaFallback)

	return r
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	if isDevMode() {
		return security.DevHeaders(next)
	}

	return security.Headers(next)
}

func isDevMode() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENVIRONMENT")))
	}

	return env == "dev" || env == "development" || env == "local"
}

func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			ctx := r.Context()
			requestID := middleware.GetReqID(ctx)
			operatorID := ""
			if operator := handler.OperatorFromContext(ctx); operator != nil {
				operatorID = operator.ID.String()
			}

			xlog.Default().InfoContext(ctx, "request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start),
				"request_id", requestID,
				"operator_id", operatorID,
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	health := map[string]interface{}{
		"status":    "ok",
		"service":   "aegion-admin",
		"version":   "1.0.0",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		xlog.Default().ErrorContext(r.Context(), "Failed to encode health response", "error", err)
	}
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check database connectivity
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Use dbPing if set (for testing), otherwise use DB
	var pinger DBPinger
	if s.dbPing != nil {
		pinger = s.dbPing
	} else if s.DB != nil {
		pinger = s.DB
	}

	if pinger == nil {
		xlog.Default().ErrorContext(ctx, "No database connection configured")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "database not configured",
		}); err != nil {
			xlog.Default().Error("failed to encode health response", "error", err)
		}
		return
	}

	if err := pinger.Ping(ctx); err != nil {
		xlog.Default().ErrorContext(ctx, "Database health check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		if encErr := json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "database unavailable",
		}); encErr != nil {
			xlog.Default().Error("failed to encode health response", "error", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ready",
		"service":   "aegion-admin",
		"database":  "connected",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		xlog.Default().Error("failed to encode health response", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleDashboardConfig(w http.ResponseWriter, r *http.Request) {
	s.ensureRoutingAssets()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"base_path": s.adminPath,
	}); err != nil {
		xlog.Default().Error("failed to encode config response", "error", err)
	}
}

func (s *Server) handleDashboardObservability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := s.buildDashboardObservabilityResponse(r.Context())

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		xlog.Default().Error("failed to encode observability response", "error", err)
	}
}

func (s *Server) buildDashboardObservabilityResponse(ctx context.Context) dashboardObservabilityResponse {
	response := dashboardObservabilityResponse{
		Enabled:     s.Config.Observability.Enabled,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Telemetry:   s.dashboardTelemetrySummary(),
		Guardrails:  s.dashboardGuardrailsSummary(),
		Stack:       []dashboardObservabilityProbe{},
	}

	if s.Config.Observability.Enabled {
		timeout := s.Config.Observability.ProbeTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}

		response.Stack = make([]dashboardObservabilityProbe, 0, 5)
		for _, endpoint := range s.dashboardObservabilityEndpoints() {
			response.Stack = append(response.Stack, s.probeDashboardObservability(ctx, endpoint, timeout))
		}
	}

	if s.SCIMService != nil && s.Config.Admin.SCIM.Enabled {
		response.SCIM = s.dashboardSCIMSummary(ctx)
		response.Guardrails.Warnings = append(response.Guardrails.Warnings, response.SCIM.Warnings...)
	}

	return response
}

func (s *Server) dashboardTelemetrySummary() dashboardTelemetrySummary {
	cfg := s.Config.Observability.Telemetry
	defaults := platformobservability.DefaultConfig()

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = defaults.ServiceName
	}
	serviceVersion := strings.TrimSpace(cfg.ServiceVersion)
	if serviceVersion == "" {
		serviceVersion = defaults.ServiceVersion
	}
	environment := strings.TrimSpace(cfg.Environment)
	if environment == "" {
		environment = defaults.Environment
	}
	instanceID := strings.TrimSpace(cfg.InstanceID)
	if instanceID == "" {
		instanceID = defaults.InstanceID
	}
	tracesEndpoint := strings.TrimSpace(cfg.TracesEndpoint)
	if tracesEndpoint == "" {
		tracesEndpoint = defaults.TracesEndpoint
	}
	metricsEndpoint := strings.TrimSpace(cfg.MetricsEndpoint)
	if metricsEndpoint == "" {
		metricsEndpoint = defaults.MetricsEndpoint
	}
	logsEndpoint := strings.TrimSpace(cfg.LogsEndpoint)
	if logsEndpoint == "" {
		logsEndpoint = defaults.LogsEndpoint
	}
	traceSamplingRatio := cfg.TraceSamplingRatio
	if traceSamplingRatio == 0 {
		traceSamplingRatio = defaults.TraceSamplingRatio
	}
	metricExportInterval := cfg.MetricExportInterval
	if metricExportInterval == 0 {
		metricExportInterval = defaults.MetricExportInterval
	}
	traceExportTimeout := cfg.TraceExportTimeout
	if traceExportTimeout == 0 {
		traceExportTimeout = defaults.TraceExportTimeout
	}
	tracesEnabled := cfg.EnableTraces
	metricsEnabled := cfg.EnableMetrics
	logsEnabled := cfg.EnableLogs
	if !tracesEnabled && !metricsEnabled && !logsEnabled {
		tracesEnabled = defaults.EnableTraces
		metricsEnabled = defaults.EnableMetrics
		logsEnabled = defaults.EnableLogs
	}

	return dashboardTelemetrySummary{
		ServiceName:            serviceName,
		ServiceVersion:         serviceVersion,
		Environment:            environment,
		InstanceID:             instanceID,
		TracesEnabled:          tracesEnabled,
		MetricsEnabled:         metricsEnabled,
		LogsEnabled:            logsEnabled,
		TracesEndpoint:         tracesEndpoint,
		MetricsEndpoint:        metricsEndpoint,
		LogsEndpoint:           logsEndpoint,
		TraceSamplingRatio:     traceSamplingRatio,
		MetricExportInterval:   metricExportInterval.String(),
		TraceExportTimeout:     traceExportTimeout.String(),
		InsecureExporter:       cfg.Insecure,
		TracesEndpointPresent:  tracesEndpoint != "",
		MetricsEndpointPresent: metricsEndpoint != "",
		LogsEndpointPresent:    logsEndpoint != "",
	}
}

func (s *Server) dashboardGuardrailsSummary() dashboardGuardrailsSummary {
	warnings := make([]string, 0, 3)
	if s.Config.Observability.Telemetry.Insecure {
		warnings = append(warnings, "OTLP export is configured as insecure")
	}
	if securityEnabled := securityEnabledFromEnv(); !securityEnabled {
		warnings = append(warnings, "strict transport security is disabled outside production")
	}
	if securityAllowForwardedHeaders() {
		warnings = append(warnings, "trusted proxy headers are enabled; verify proxy CIDR allowlist")
	}

	return dashboardGuardrailsSummary{
		AdminAuthRequired:         true,
		ObservabilityRBAC:         true,
		AdminRateLimiting:         true,
		AdminCSRFProtection:       true,
		StrictTransportSecurity:   securityEnabledFromEnv(),
		TrustedProxyHeaders:       securityAllowForwardedHeaders(),
		SCIMBearerAuth:            s.SCIMService != nil && s.Config.Admin.SCIM.Enabled,
		SCIMUnknownFieldRejection: true,
		SCIMBodyLimitBytes:        1 << 20,
		TelemetrySecretsRedacted:  true,
		Warnings:                  warnings,
	}
}

func (s *Server) dashboardSCIMSummary(ctx context.Context) *dashboardSCIMSummary {
	summary := &dashboardSCIMSummary{
		Enabled:     true,
		BasePath:    normalizeMountedPath(s.Config.Admin.SCIM.BasePath),
		TokenPrefix: s.Config.Admin.SCIM.TokenPrefix,
	}

	tokens, err := s.SCIMService.ListSCIMTokens(ctx)
	if err != nil {
		summary.Warnings = append(summary.Warnings, "SCIM token inventory unavailable")
		return summary
	}
	mappings, err := s.SCIMService.ListSCIMMappings(ctx)
	if err != nil {
		summary.Warnings = append(summary.Warnings, "SCIM mapping inventory unavailable")
	} else {
		summary.MappingCount = len(mappings)
	}

	now := time.Now().UTC()
	var latestUse time.Time
	for _, token := range tokens {
		if token == nil {
			continue
		}
		summary.TokenCount++
		if token.Active {
			summary.ActiveTokenCount++
		}
		if token.ExpiresAt != nil {
			if token.ExpiresAt.Before(now) {
				summary.ExpiredTokenCount++
			} else if token.ExpiresAt.Before(now.Add(7 * 24 * time.Hour)) {
				summary.ExpiringTokenCount++
			}
		}
		if token.LastUsedAt != nil && token.LastUsedAt.After(latestUse) {
			latestUse = token.LastUsedAt.UTC()
		}
		if hasSCIMPermission(token, "*") {
			summary.WildcardTokenCount++
		}
		if hasAnySCIMPermission(token, "users:write", "groups:write", "users:*", "groups:*", "*") {
			summary.WriteTokenCount++
		}
	}

	if !latestUse.IsZero() {
		summary.LastTokenUsedAt = latestUse.Format(time.RFC3339)
	}
	if summary.WildcardTokenCount > 0 {
		summary.Warnings = append(summary.Warnings, "one or more SCIM tokens have wildcard permissions")
	}
	if summary.ExpiredTokenCount > 0 {
		summary.Warnings = append(summary.Warnings, "expired SCIM tokens should be removed")
	}
	if summary.MappingCount == 0 {
		summary.Warnings = append(summary.Warnings, "SCIM is enabled without any attribute mappings")
	}

	return summary
}

func hasSCIMPermission(token *scim.SCIMToken, permission string) bool {
	if token == nil {
		return false
	}
	for _, candidate := range token.Permissions {
		if candidate == permission {
			return true
		}
	}
	return false
}

func hasAnySCIMPermission(token *scim.SCIMToken, permissions ...string) bool {
	for _, permission := range permissions {
		if hasSCIMPermission(token, permission) {
			return true
		}
	}
	return false
}

func securityEnabledFromEnv() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENVIRONMENT")))
	}
	return env == "prod" || env == "production"
}

func securityAllowForwardedHeaders() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ADMIN_TRUST_FORWARDED_HEADERS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Server) dashboardObservabilityEndpoints() []dashboardObservabilityEndpoint {
	return []dashboardObservabilityEndpoint{
		{Key: "otel-collector", Label: "OTel Collector", URL: strings.TrimSpace(s.Config.Observability.Endpoints.OTelCollector)},
		{Key: "prometheus", Label: "Prometheus", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Prometheus)},
		{Key: "grafana", Label: "Grafana", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Grafana)},
		{Key: "tempo", Label: "Tempo", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Tempo)},
		{Key: "loki", Label: "Loki", URL: strings.TrimSpace(s.Config.Observability.Endpoints.Loki)},
	}
}

func (s *Server) probeDashboardObservability(
	ctx context.Context,
	endpoint dashboardObservabilityEndpoint,
	timeout time.Duration,
) dashboardObservabilityProbe {
	result := dashboardObservabilityProbe{
		Key:        endpoint.Key,
		Label:      endpoint.Label,
		URL:        endpoint.URL,
		Status:     "offline",
		StatusCode: 0,
		Message:    "endpoint not configured",
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	if endpoint.URL == "" {
		return result
	}

	started := time.Now()
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	resp, err := client.Do(req)
	result.ResponseTimeMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Message = err.Error()
		return result
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	result.StatusCode = resp.StatusCode
	result.Message = resp.Status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = "healthy"
		return result
	}

	result.Status = "degraded"
	return result
}

func normalizeAdminPath(adminPath string) string {
	trimmed := strings.TrimSpace(adminPath)
	if trimmed == "" {
		return "/aegion"
	}

	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}

	if len(trimmed) > 1 {
		trimmed = strings.TrimRight(trimmed, "/")
	}

	if trimmed == "" {
		return "/aegion"
	}

	return trimmed
}

func normalizeMountedPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "/scim/v2"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	if len(trimmed) > 1 {
		trimmed = strings.TrimRight(trimmed, "/")
	}
	return trimmed
}

func (s *Server) handleListSCIMTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.SCIMService.ListSCIMTokens(r.Context())
	if err != nil {
		http.Error(w, "failed to list SCIM tokens", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{"tokens": tokens}); err != nil {
		xlog.Default().Error("failed to encode SCIM tokens response", "error", err)
	}
}

func (s *Server) handleCreateSCIMToken(w http.ResponseWriter, r *http.Request) {
	operator := handler.OperatorFromContext(r.Context())
	if operator == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Permissions []string   `json:"permissions"`
		ExpiresAt   *time.Time `json:"expires_at"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	token, plainToken, err := s.SCIMService.CreateSCIMToken(r.Context(), req.Name, strings.TrimSpace(req.Description), req.Permissions, req.ExpiresAt, operator.ID)
	if err != nil {
		http.Error(w, "failed to create SCIM token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"token":       token,
		"plain_token": plainToken,
	}); err != nil {
		xlog.Default().Error("failed to encode SCIM token creation response", "error", err)
	}
}

func (s *Server) handleDeleteSCIMToken(w http.ResponseWriter, r *http.Request) {
	tokenID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		http.Error(w, "invalid token id", http.StatusBadRequest)
		return
	}
	if err := s.SCIMService.DeleteSCIMToken(r.Context(), tokenID); err != nil {
		http.Error(w, "failed to delete SCIM token", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSCIMMappings(w http.ResponseWriter, r *http.Request) {
	mappings, err := s.SCIMService.ListSCIMMappings(r.Context())
	if err != nil {
		http.Error(w, "failed to list SCIM mappings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{"mappings": mappings}); err != nil {
		xlog.Default().Error("failed to encode SCIM mappings response", "error", err)
	}
}

func (s *Server) handleCreateSCIMMapping(w http.ResponseWriter, r *http.Request) {
	var req scim.SCIMMapping
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	mapping, err := s.SCIMService.CreateSCIMMapping(r.Context(), &req)
	if err != nil {
		if errors.Is(err, scim.ErrRequiredMappingName) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to create SCIM mapping", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]any{"mapping": mapping}); err != nil {
		xlog.Default().Error("failed to encode SCIM mapping creation response", "error", err)
	}
}

func (s *Server) handleUpdateSCIMMapping(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		http.Error(w, "invalid mapping id", http.StatusBadRequest)
		return
	}

	var req scim.SCIMMapping
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.ID = id

	mapping, err := s.SCIMService.UpdateSCIMMapping(r.Context(), &req)
	if err != nil {
		if errors.Is(err, scim.ErrRequiredMappingName) || errors.Is(err, scim.ErrRequiredMappingID) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to update SCIM mapping", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{"mapping": mapping}); err != nil {
		xlog.Default().Error("failed to encode SCIM mapping update response", "error", err)
	}
}

func (s *Server) handleDeleteSCIMMapping(w http.ResponseWriter, r *http.Request) {
	mappingID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		http.Error(w, "invalid mapping id", http.StatusBadRequest)
		return
	}
	if err := s.SCIMService.DeleteSCIMMapping(r.Context(), mappingID); err != nil {
		http.Error(w, "failed to delete SCIM mapping", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) spaHandler() http.Handler {
	s.ensureRoutingAssets()
	// Mount the embedded SPA files
	return http.StripPrefix(s.adminPath, s.spaServer)
}

func (s *Server) spaFallback(w http.ResponseWriter, r *http.Request) {
	s.ensureRoutingAssets()
	// For SPA routes that don't match files, serve index.html
	// This allows client-side routing to work
	path := strings.TrimPrefix(r.URL.Path, s.adminPath)

	// Only serve SPA fallback for admin paths
	if strings.HasPrefix(r.URL.Path, s.adminPath) {
		// Check if this is an API call that shouldn't get the SPA
		if strings.HasPrefix(path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Serve index.html for SPA routing
		s.spaServer.ServeHTTP(w, &http.Request{
			Method: "GET",
			URL:    &url.URL{Path: "/index.html"},
			Header: r.Header,
		})
		return
	}

	// Regular 404 for non-admin paths
	http.NotFound(w, r)
}

// SPAFileServer handles serving static files with fallback to index.html
type SPAFileServer struct {
	fileServer http.Handler
}

func NewSPAFileServer() *SPAFileServer {
	return &SPAFileServer{
		fileServer: http.FileServer(http.FS(admin.GetSPAFiles())),
	}
}

func (spa *SPAFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Check if the file exists
	if _, err := admin.GetSPAFiles().Open(path); err != nil {
		// File doesn't exist, check if it's a potential route
		ext := filepath.Ext(path)
		if ext == "" || ext == ".html" {
			// Likely a client-side route, serve index.html
			r.URL.Path = "/index.html"
		} else {
			// Static asset that doesn't exist, return 404
			http.NotFound(w, r)
			return
		}
	}

	// Set appropriate cache headers
	ext := filepath.Ext(r.URL.Path)
	switch ext {
	case ".js", ".css":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // 1 year
	case ".html":
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600") // 1 hour
	}

	spa.fileServer.ServeHTTP(w, r)
}

func (s *Server) ensureRoutingAssets() {
	if s.adminPath == "" {
		s.adminPath = normalizeAdminPath(s.Config.Admin.Path)
		s.Config.Admin.Path = s.adminPath
	}
	if s.spaServer == nil {
		s.spaServer = NewSPAFileServer()
	}
}
