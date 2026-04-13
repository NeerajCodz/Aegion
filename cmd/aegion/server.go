package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/courier"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/logger"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
	passwordservice "github.com/aegion/aegion/modules/password/service"
	passwordstore "github.com/aegion/aegion/modules/password/store"
	policygrpc "github.com/aegion/aegion/modules/policy/grpc"
	policystore "github.com/aegion/aegion/modules/policy/store"
)

// ServerConfig holds the server configuration.
type ServerConfig struct {
	Config         *config.Config
	ConfigPath     string
	DB             *database.DB
	Log            *logger.Logger
	WorkerManager  *workers.Manager
	AdminBootstrap bool
}

// Server represents the main Aegion server.
type Server struct {
	cfg            *config.Config
	db             *database.DB
	log            *logger.Logger
	router         chi.Router
	registry       *registry.Registry
	orchestrator   moduleOrchestrator
	sessionManager sessionManager
	tokenGen       *authtoken.Generator
	flowService    *flows.Service
	policyChecker  policyChecker
	workerManager  *workers.Manager
	passwordAuth   passwordFlowService
	magicLinkAuth  magicLinkFlowService
	courier        *courier.Courier
	dbQueryRowFn   func(ctx context.Context, sql string, args ...any) pgx.Row
	dbQueryFn      func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	dbExecFn       func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	dbBeginFn      func(ctx context.Context) (pgx.Tx, error)
}

type policyChecker interface {
	Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error)
}

type moduleOrchestrator interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	RestartModule(ctx context.Context, moduleID string) error
}

type sessionManager interface {
	Create(ctx context.Context, identityID uuid.UUID, method session.AuthMethod, device session.DeviceInfo) (*session.Session, error)
	GetFromRequest(ctx context.Context, r *http.Request) (*session.Session, error)
	Revoke(ctx context.Context, sessionID uuid.UUID) error
	SetCookie(w http.ResponseWriter, session *session.Session)
	ClearCookie(w http.ResponseWriter)
}

type passwordFlowService interface {
	ValidatePassword(ctx context.Context, password, identifier string) error
	Register(ctx context.Context, identityID uuid.UUID, identifier, password string) error
	Verify(ctx context.Context, identifier, password string) (uuid.UUID, error)
	ChangePassword(ctx context.Context, identityID uuid.UUID, oldPassword, newPassword string) error
	ResetPassword(ctx context.Context, identityID uuid.UUID, newPassword string) error
}

type magicLinkFlowService interface {
	SendLoginCode(ctx context.Context, email string) error
	VerifyMagicLink(ctx context.Context, token string) (string, *uuid.UUID, error)
	VerifyMagicLinkForType(ctx context.Context, token string, expectedType magiclinkstore.CodeType) (string, *uuid.UUID, error)
	SendVerificationCode(ctx context.Context, email string, identityID uuid.UUID) error
	VerifyVerificationCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error)
	SendRecoveryCodeIfIdentityExists(ctx context.Context, email string, identityID *uuid.UUID) error
	VerifyRecoveryCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error)
}

var newModuleOrchestrator = func(cfg orchestrator.Config) (moduleOrchestrator, error) {
	return orchestrator.New(cfg)
}

var newSessionManager = func(cfg session.ManagerConfig) sessionManager {
	return session.NewManager(cfg)
}

var pingDatabase = func(ctx context.Context, db *database.DB) error {
	return db.Pool.Ping(ctx)
}

var beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
	return db.Pool.Begin(ctx)
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(dest ...any) error {
	return r.err
}

func (s *Server) hasDatabaseAccess() bool {
	if s == nil || s.db == nil {
		return false
	}
	if s.db.Pool != nil {
		return true
	}
	return s.dbQueryRowFn != nil || s.dbQueryFn != nil || s.dbExecFn != nil || s.dbBeginFn != nil
}

func (s *Server) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if s.dbQueryRowFn != nil {
		return s.dbQueryRowFn(ctx, sql, args...)
	}
	if s.db == nil || s.db.Pool == nil {
		return errorRow{err: errors.New("database unavailable")}
	}
	return s.db.Pool.QueryRow(ctx, sql, args...)
}

func (s *Server) query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if s.dbQueryFn != nil {
		return s.dbQueryFn(ctx, sql, args...)
	}
	if s.db == nil || s.db.Pool == nil {
		return nil, errors.New("database unavailable")
	}
	return s.db.Pool.Query(ctx, sql, args...)
}

func (s *Server) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if s.dbExecFn != nil {
		return s.dbExecFn(ctx, sql, args...)
	}
	if s.db == nil || s.db.Pool == nil {
		return pgconn.CommandTag{}, errors.New("database unavailable")
	}
	return s.db.Pool.Exec(ctx, sql, args...)
}

func (s *Server) begin(ctx context.Context) (pgx.Tx, error) {
	if s.dbBeginFn != nil {
		return s.dbBeginFn(ctx)
	}
	if s.db == nil || s.db.Pool == nil {
		return nil, errors.New("database unavailable")
	}
	return s.db.Pool.Begin(ctx)
}

type bootstrapAdminOutcome struct {
	IdentityID      uuid.UUID
	OperatorID      uuid.UUID
	CreatedIdentity bool
	CreatedOperator bool
}

var ensureBootstrapAdminOperator = bootstrapAdminOperator

// NewServer creates and initializes a new server instance.
func NewServer(ctx context.Context, cfg *ServerConfig) (*Server, error) {
	// Initialize auth token generator
	var internalSecret string
	if len(cfg.Config.Secrets.Internal) > 0 {
		internalSecret = cfg.Config.Secrets.Internal[0]
	} else if len(cfg.Config.Secrets.Cookie) > 0 {
		internalSecret = cfg.Config.Secrets.Cookie[0]
	} else if len(cfg.Config.Secrets.Cipher) > 0 {
		internalSecret = cfg.Config.Secrets.Cipher[0]
	} else {
		return nil, errors.New("internal auth secret is not configured")
	}

	tokenGen, err := authtoken.NewGenerator(authtoken.GeneratorConfig{
		Secret: []byte(internalSecret),
		TTL:    5 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	// Initialize service registry
	reg := registry.New(registry.Config{
		HealthCheckInterval: cfg.Config.Server.InternalNet.HealthCheckInt.Duration(),
		HealthCheckTimeout:  cfg.Config.Server.InternalNet.HealthCheckTimeout.Duration(),
	})

	// Initialize flow store and service
	flowStore := flows.NewPostgresFlowStore(cfg.DB.Pool)
	flowService := flows.NewService(flowStore, flows.DefaultConfig())

	sessionSecret := []byte(internalSecret)
	if len(cfg.Config.Secrets.Cookie) > 0 {
		sessionSecret = []byte(cfg.Config.Secrets.Cookie[0])
	}
	sessionMgr := newSessionManager(session.ManagerConfig{
		DB:           cfg.DB.Pool,
		CookieSecret: sessionSecret,
		CookieConfig: session.CookieConfig{
			Name:     cfg.Config.Sessions.Cookie.Name,
			Path:     cfg.Config.Sessions.Cookie.Path,
			SameSite: parseSameSite(cfg.Config.Sessions.Cookie.SameSite),
			Secure:   cfg.Config.Sessions.Cookie.Secure,
			HTTPOnly: cfg.Config.Sessions.Cookie.HTTPOnly,
		},
		Lifespan:    cfg.Config.Sessions.Lifespan.Duration(),
		IdleTimeout: cfg.Config.Sessions.IdleTimeout.Duration(),
	})

	var courierSvc *courier.Courier
	if cfg.DB != nil {
		courierSvc = courier.New(courier.Config{
			DB: cfg.DB.Pool,
			SMTP: courier.SMTPConfig{
				Host:        cfg.Config.Courier.SMTP.Host,
				Port:        cfg.Config.Courier.SMTP.Port,
				FromAddress: cfg.Config.Courier.SMTP.FromAddress,
				FromName:    cfg.Config.Courier.SMTP.FromName,
				Username:    cfg.Config.Courier.SMTP.Auth.Username,
				Password:    cfg.Config.Courier.SMTP.Auth.Password,
				AuthEnabled: cfg.Config.Courier.SMTP.Auth.Enabled,
			},
			CodeExpiry: cfg.Config.MagicLink.CodeLifespan.Duration(),
			LinkExpiry: cfg.Config.MagicLink.LinkLifespan.Duration(),
		})
	}

	var passwordAuth passwordFlowService
	if cfg.Config.Password.Enabled {
		passwordAuth = passwordservice.New(
			passwordstore.New(cfg.DB.Pool),
			runtimePasswordHasher{},
			passwordservice.Config{
				MinLength:               cfg.Config.Password.MinLength,
				RequireUppercase:        cfg.Config.Password.RequireUppercase,
				RequireLowercase:        cfg.Config.Password.RequireLowercase,
				RequireNumber:           cfg.Config.Password.RequireNumber,
				RequireSpecial:          cfg.Config.Password.RequireSpecial,
				HIBPEnabled:             cfg.Config.Password.HIBPEnabled,
				HIBPBaseURL:             passwordHIBPBaseURL(cfg.Config.Password.HIBPHost),
				HIBPTimeout:             cfg.Config.Password.HIBPTimeout.Duration(),
				HIBPIgnoreNetworkErrors: cfg.Config.Password.HIBPIgnoreNetworkErrors,
				HIBPMinBreachCount:      cfg.Config.Password.HIBPMinBreachCount,
				HistoryCount:            cfg.Config.Password.HistoryCount,
			},
		)
	}

	var magicLinkAuth magicLinkFlowService
	if cfg.Config.MagicLink.Enabled {
		magicLinkAuth = magiclinkservice.New(
			magiclinkstore.New(cfg.DB.Pool),
			magicLinkCourierAdapter{courier: courierSvc},
			magiclinkservice.Config{
				BaseURL:           publicBaseURL(cfg.Config),
				CodeLength:        cfg.Config.MagicLink.CodeLength,
				CodeCharset:       cfg.Config.MagicLink.CodeCharset,
				LinkLifespan:      cfg.Config.MagicLink.LinkLifespan.Duration(),
				CodeLifespan:      cfg.Config.MagicLink.CodeLifespan.Duration(),
				RateLimit:         cfg.Config.MagicLink.RateLimit,
				RateWindow:        cfg.Config.MagicLink.RateWindow.Duration(),
				RecoveryRateLimit: cfg.Config.MagicLink.RecoveryRateLimit,
			},
		)
	}

	var checker policyChecker
	if cfg.Config.Policy.Enabled {
		checker = policygrpc.NewServer(policystore.New(cfg.DB.Pool))
	}

	s := &Server{
		cfg:            cfg.Config,
		db:             cfg.DB,
		log:            cfg.Log,
		registry:       reg,
		tokenGen:       tokenGen,
		sessionManager: sessionMgr,
		flowService:    flowService,
		policyChecker:  checker,
		workerManager:  cfg.WorkerManager,
		passwordAuth:   passwordAuth,
		magicLinkAuth:  magicLinkAuth,
		courier:        courierSvc,
	}

	// Setup routes
	s.router = SetupRoutes(s)

	// Start registry
	reg.Start()
	s.log.Info().Msg("Service registry started")

	if cfg.ConfigPath != "" {
		moduleOrchestrator, err := newModuleOrchestrator(orchestrator.Config{
			ConfigPath:  cfg.ConfigPath,
			Registry:    reg,
			TokenSecret: []byte(internalSecret),
		})
		if err != nil {
			reg.Stop()
			return nil, err
		}
		if err := moduleOrchestrator.Start(ctx); err != nil {
			reg.Stop()
			return nil, err
		}
		s.orchestrator = moduleOrchestrator
		s.log.Info().Str("config_path", cfg.ConfigPath).Msg("Module orchestrator started")
	}

	// Bootstrap admin if requested
	if cfg.AdminBootstrap {
		if err := s.bootstrapAdmin(ctx); err != nil {
			s.log.Warn().Err(err).Msg("Admin bootstrap failed")
		}
	}

	// Register workers if manager is available
	if s.workerManager != nil {
		s.registerWorkers()
	}

	return s, nil
}

// bootstrapAdmin creates the initial admin user if not exists.
func (s *Server) bootstrapAdmin(ctx context.Context) error {
	email := strings.ToLower(strings.TrimSpace(s.cfg.Operator.Email))
	password := strings.TrimSpace(s.cfg.Operator.Password)
	if email == "" || password == "" {
		s.log.Info().Msg("Admin bootstrap skipped: no operator credentials configured")
		return nil
	}

	s.log.Info().
		Str("email", email).
		Msg("Admin bootstrap requested")

	outcome, err := ensureBootstrapAdminOperator(ctx, s.db, email, password)
	if err != nil {
		return err
	}

	if !outcome.CreatedIdentity && !outcome.CreatedOperator {
		s.log.Info().
			Str("email", email).
			Msg("Admin bootstrap skipped: operator already exists")
		return nil
	}

	logEvent := s.log.Info().
		Str("email", email).
		Str("identity_id", outcome.IdentityID.String()).
		Bool("created_identity", outcome.CreatedIdentity).
		Bool("created_operator", outcome.CreatedOperator)
	if outcome.OperatorID != uuid.Nil {
		logEvent = logEvent.Str("operator_id", outcome.OperatorID.String())
	}
	logEvent.Msg("Admin bootstrap completed")

	return nil
}

func bootstrapAdminOperator(ctx context.Context, db *database.DB, email, password string) (bootstrapAdminOutcome, error) {
	outcome := bootstrapAdminOutcome{}
	if db == nil || db.Pool == nil {
		return outcome, errors.New("database unavailable")
	}

	tx, err := beginBootstrapAdminTx(ctx, db)
	if err != nil {
		return outcome, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	var operatorCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM adm_operators`).Scan(&operatorCount); err != nil {
		return outcome, err
	}
	if operatorCount > 0 {
		return outcome, nil
	}

	var identityID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT ci.id
		FROM core_identities ci
		LEFT JOIN LATERAL (
			SELECT value
			FROM core_identity_addresses
			WHERE identity_id = ci.id
			  AND type = 'email'
			ORDER BY is_primary DESC, created_at ASC
			LIMIT 1
		) addr ON TRUE
		WHERE ci.deleted_at IS NULL
		  AND LOWER(COALESCE(addr.value, ci.traits->>'email', '')) = $1
		LIMIT 1
	`, email).Scan(&identityID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return outcome, err
		}

		schemaID, schemaErr := resolveBootstrapSchemaID(ctx, tx)
		if schemaErr != nil {
			return outcome, schemaErr
		}

		hashedPassword, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return outcome, hashErr
		}

		identityID = uuid.New()
		traits := map[string]interface{}{
			"email":        email,
			"display_name": bootstrapDisplayName(email),
			"name":         bootstrapDisplayName(email),
		}
		traitsJSON, marshalErr := json.Marshal(traits)
		if marshalErr != nil {
			return outcome, marshalErr
		}

		if _, execErr := tx.Exec(ctx, `
			INSERT INTO core_identities (id, schema_id, traits, state, created_at, updated_at)
			VALUES ($1, $2, $3::jsonb, 'active', NOW(), NOW())
		`, identityID, schemaID, string(traitsJSON)); execErr != nil {
			return outcome, execErr
		}

		if _, execErr := tx.Exec(ctx, `
			INSERT INTO core_identity_addresses (id, identity_id, type, value, is_primary, verified, created_at, updated_at)
			VALUES ($1, $2, 'email', $3, TRUE, FALSE, NOW(), NOW())
		`, uuid.New(), identityID, email); execErr != nil {
			return outcome, execErr
		}

		if _, execErr := tx.Exec(ctx, `
			INSERT INTO pwd_credentials (id, identity_id, identifier, hash, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())
		`, uuid.New(), identityID, email, string(hashedPassword)); execErr != nil {
			return outcome, execErr
		}

		outcome.CreatedIdentity = true
	}

	if !outcome.CreatedIdentity {
		var credentialCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM pwd_credentials WHERE identity_id = $1`, identityID).Scan(&credentialCount); err != nil {
			return outcome, err
		}
		if credentialCount == 0 {
			hashedPassword, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if hashErr != nil {
				return outcome, hashErr
			}
			if _, execErr := tx.Exec(ctx, `
				INSERT INTO pwd_credentials (id, identity_id, identifier, hash, created_at, updated_at)
				VALUES ($1, $2, $3, $4, NOW(), NOW())
			`, uuid.New(), identityID, email, string(hashedPassword)); execErr != nil {
				return outcome, execErr
			}
		}
	}

	operatorID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO adm_operators (id, identity_id, role, permissions, created_at, updated_at)
		VALUES ($1, $2, 'super_admin', '{}'::jsonb, NOW(), NOW())
	`, operatorID, identityID); err != nil {
		return outcome, err
	}

	if err := tx.Commit(ctx); err != nil {
		return outcome, err
	}

	outcome.IdentityID = identityID
	outcome.OperatorID = operatorID
	outcome.CreatedOperator = true
	return outcome, nil
}

func resolveBootstrapSchemaID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var schemaID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM core_identity_schemas
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1
	`).Scan(&schemaID)
	if err == nil {
		return schemaID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}

	schemaID = uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO core_identity_schemas (id, name, is_default, schema, created_at, updated_at)
		VALUES ($1, 'default', TRUE, '{}'::jsonb, NOW(), NOW())
	`, schemaID)
	if err != nil {
		return uuid.Nil, err
	}

	return schemaID, nil
}

func bootstrapDisplayName(email string) string {
	localPart := strings.TrimSpace(email)
	if idx := strings.Index(localPart, "@"); idx > 0 {
		localPart = localPart[:idx]
	}
	if localPart == "" {
		return email
	}
	return localPart
}

// registerWorkers registers background workers with the manager.
func (s *Server) registerWorkers() {
	if s.courier != nil {
		s.workerManager.Register(workers.NewCourierDispatchWorker(workers.CourierDispatchConfig{
			DB:       s.db.Pool,
			Log:      s.log,
			Courier:  s.courier,
			Interval: 30 * time.Second,
		}))
	}

	// Register session cleanup worker
	s.workerManager.Register(workers.NewSessionCleanupWorker(workers.SessionCleanupConfig{
		DB:       s.db.Pool,
		Log:      s.log,
		Interval: 15 * time.Minute,
	}))

	// Register flow cleanup worker
	s.workerManager.Register(workers.NewFlowCleanupWorker(workers.FlowCleanupConfig{
		DB:       s.db.Pool,
		Log:      s.log,
		Interval: 10 * time.Minute,
	}))

	s.log.Info().Msg("Background workers registered")
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info().Msg("Shutting down server components")

	var shutdownErr error

	if s.orchestrator != nil {
		if err := s.orchestrator.Stop(ctx); err != nil {
			s.log.Error().Err(err).Msg("Failed to stop module orchestrator")
			shutdownErr = err
		} else {
			s.log.Info().Msg("Module orchestrator stopped")
		}
	}

	// Stop registry
	if s.registry != nil {
		s.registry.Stop()
		s.log.Info().Msg("Service registry stopped")
	}

	return shutdownErr
}

// Registry returns the service registry.
func (s *Server) Registry() *registry.Registry {
	return s.registry
}

// Orchestrator returns the module orchestrator.
func (s *Server) Orchestrator() moduleOrchestrator {
	return s.orchestrator
}

// FlowService returns the flow service.
func (s *Server) FlowService() *flows.Service {
	return s.flowService
}

// TokenGenerator returns the auth token generator.
func (s *Server) TokenGenerator() *authtoken.Generator {
	return s.tokenGen
}

// Middleware

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			s.log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("duration", time.Since(start)).
				Str("request_id", middleware.GetReqID(r.Context())).
				Msg("HTTP request")
		}()

		next.ServeHTTP(ww, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))

		allowAll := false
		explicitAllowed := false
		for _, configuredOrigin := range s.cfg.Server.CORS.AllowedOrigins {
			configuredOrigin = strings.TrimSpace(configuredOrigin)
			if configuredOrigin == "" {
				continue
			}
			if configuredOrigin == "*" {
				allowAll = true
				continue
			}
			if configuredOrigin == origin {
				explicitAllowed = true
			}
		}

		allowOrigin := ""
		switch {
		case explicitAllowed:
			allowOrigin = origin
		case allowAll && !s.cfg.Server.CORS.AllowCredentials:
			allowOrigin = "*"
		}

		if allowOrigin != "" {
			if allowOrigin != "*" {
				appendVaryHeader(w.Header(), "Origin")
			}
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Set("Access-Control-Allow-Methods", joinStrings(s.cfg.Server.CORS.AllowedMethods))
			w.Header().Set("Access-Control-Allow-Headers", joinStrings(s.cfg.Server.CORS.AllowedHeaders))
			if s.cfg.Server.CORS.AllowCredentials && allowOrigin != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

func appendVaryHeader(header http.Header, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	if err := pingDatabase(r.Context(), s.db); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","reason":"database unavailable"}`))
		return
	}

	// Check registry health
	registryOK := s.registry != nil && s.registry.ModuleCount() >= 0

	resp := map[string]interface{}{
		"status":        "ready",
		"database":      "ok",
		"registry":      registryOK,
		"orchestrator":  s.orchestrator != nil,
		"module_count":  s.registry.ModuleCount(),
		"healthy_count": s.registry.HealthyCount(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"alive"}`))
}

func (s *Server) handleNotImplemented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"not implemented"}`))
}
