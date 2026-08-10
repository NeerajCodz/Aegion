package main

import (
	"context"
	"crypto/sha256"
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

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/courier"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/database"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	"github.com/aegion/aegion/internal/xlog"
	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
	mfaservice "github.com/aegion/aegion/modules/mfa/service"
	mfastore "github.com/aegion/aegion/modules/mfa/store"
	passkeysservice "github.com/aegion/aegion/modules/passkeys/service"
	passkeysstore "github.com/aegion/aegion/modules/passkeys/store"
	passwordservice "github.com/aegion/aegion/modules/password/service"
	passwordstore "github.com/aegion/aegion/modules/password/store"
	policygrpc "github.com/aegion/aegion/modules/policy/grpc"
	policystore "github.com/aegion/aegion/modules/policy/store"
)

// ServerConfig holds the server configuration.
type ServerConfig struct {
	Config         *config.Config
	ModulePlan     config.ModulePlan
	DB             *database.DB
	Log            *xlog.Logger
	WorkerManager  *workers.Manager
	AdminBootstrap bool
}

// Server represents the main Aegion server.
type Server struct {
	cfg            *config.Config
	db             *database.DB
	log            *xlog.Logger
	router         chi.Router
	registry       *registry.Registry
	modulePlan     config.ModulePlan
	moduleRoutes   ModuleRouteTable
	registryGRPC   *registryGRPCServer
	sessionManager sessionManager
	tokenGen       *authtoken.Generator
	flowService    *flows.Service
	policyChecker  policyChecker
	workerManager  *workers.Manager
	passwordAuth   passwordFlowService
	magicLinkAuth  magicLinkFlowService
	mfaAuth        mfaFlowService
	passkeyAuth    passkeyFlowService
	courier        *courier.Courier
	dbQueryRowFn   func(ctx context.Context, sql string, args ...any) pgx.Row
	dbQueryFn      func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	dbExecFn       func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	dbBeginFn      func(ctx context.Context) (pgx.Tx, error)
}

type policyChecker interface {
	Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error)
}

type sessionManager interface {
	Create(ctx context.Context, identityID uuid.UUID, method session.AuthMethod, device session.DeviceInfo) (*session.Session, error)
	GetFromRequest(ctx context.Context, r *http.Request) (*session.Session, error)
	Revoke(ctx context.Context, sessionID uuid.UUID) error
	RevokeAllForIdentity(ctx context.Context, identityID uuid.UUID) error
	AddAuthMethod(ctx context.Context, sessionID uuid.UUID, method session.AuthMethod) error
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

type mfaFlowService interface {
	StartTOTPEnrollment(ctx context.Context, identityID, accountName string) (*mfaservice.TOTPEnrollmentStartResponse, error)
	CompleteTOTPEnrollment(ctx context.Context, req *mfaservice.TOTPEnrollmentFinishRequest) (*mfaservice.TOTPEnrollmentFinishResponse, error)
	VerifyTOTP(ctx context.Context, identityID, code string) error
	VerifyBackupCode(ctx context.Context, identityID, code string) error
	HasEnrolledFactor(ctx context.Context, identityID string) (bool, error)
	RegenerateBackupCodes(ctx context.Context, identityID string) ([]string, error)
	RememberTrustedDevice(ctx context.Context, identityID, label string) (string, time.Time, error)
	ValidateTrustedDevice(ctx context.Context, identityID, token string) (bool, error)
	RevokeTrustedDevice(ctx context.Context, identityID, token string) error
	ResetIdentity(ctx context.Context, identityID string) error
}

type passkeyFlowService interface {
	BeginRegistration(identityID string) (*passkeysservice.RegistrationStartResponse, error)
	FinishRegistration(req *passkeysservice.RegistrationFinishRequest) error
	BeginAuthentication(identityID string) (*passkeysservice.AuthenticationStartResponse, error)
	FinishAuthentication(req *passkeysservice.AuthenticationFinishRequest) error
}

var newSessionManager = func(cfg session.ManagerConfig) sessionManager {
	return session.NewManager(cfg)
}

var pingDatabase = func(ctx context.Context, db *database.DB) error {
	if db == nil || db.Pool == nil {
		return errors.New("database unavailable")
	}
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
		HealthCheckInterval: cfg.Config.Server.Registry.HealthCheckInterval.Duration(),
		HealthCheckTimeout:  cfg.Config.Server.Registry.HealthCheckTimeout.Duration(),
	}, cfg.Log)

	// Initialize flow store and service
	flowStore := flows.NewPostgresFlowStore(cfg.DB.Pool)
	flowConfig := flows.DefaultConfig()
	if cfg.Config.Passkeys.Enabled {
		flowConfig.DefaultMethods = append(flowConfig.DefaultMethods, flows.AuthMethod{Method: "passkey"})
	}
	flowService := flows.NewService(flowStore, flowConfig)
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
			SMS: courier.SMSConfig{
				Enabled:      cfg.Config.Courier.SMS.Enabled,
				URL:          cfg.Config.Courier.SMS.URL,
				Method:       cfg.Config.Courier.SMS.Method,
				Headers:      cfg.Config.Courier.SMS.Headers,
				BodyTemplate: cfg.Config.Courier.SMS.BodyTemplate,
				Timeout:      cfg.Config.Courier.SMS.Timeout.Duration(),
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

	var mfaAuth mfaFlowService
	if cfg.Config.MFA.Enabled {
		mfaRepo := any(mfastore.New())
		if cfg.DB != nil && cfg.DB.Pool != nil {
			if repo, err := mfastore.NewPostgres(cfg.DB.Pool); err == nil {
				mfaRepo = repo
			}
		}
		mfaAuth = mfaservice.New(mfaRepo.(interface {
			SaveEnrollment(enrollment mfastore.Enrollment) error
			GetEnrollment(enrollmentID string) (mfastore.Enrollment, error)
			DeleteEnrollment(enrollmentID string) error
			UpsertTOTPFactor(factor mfastore.TOTPFactor) error
			GetTOTPFactor(identityID string) (mfastore.TOTPFactor, error)
			UpdateTOTPLastUsed(identityID string, usedAt time.Time) error
			ReplaceBackupCodes(identityID string, codes []mfastore.BackupCode) error
			ListBackupCodes(identityID string) ([]mfastore.BackupCode, error)
			MarkBackupCodeUsed(identityID, codeID string, usedAt time.Time) error
			SaveTrustedDevice(device mfastore.TrustedDevice) error
			GetTrustedDevice(identityID, tokenHash, tokenPrefix string) (mfastore.TrustedDevice, error)
			TouchTrustedDevice(identityID, deviceID string, touchedAt time.Time) error
			DeleteTrustedDevice(identityID, deviceID string, revokedAt time.Time) error
			DeleteAllIdentityData(identityID string) error
			ListFactorsByIdentity(identityID string) ([]mfastore.Factor, error)
		}), mfaservice.Config{
			Issuer:                 cfg.Config.MFA.Issuer,
			EnrollmentTTL:          cfg.Config.MFA.EnrollmentTTL.Duration(),
			TOTPPeriod:             cfg.Config.MFA.CodePeriod.Duration(),
			TOTPDigits:             cfg.Config.MFA.CodeDigits,
			TOTPAllowedTimeWindows: cfg.Config.MFA.AllowedTimeWindows,
			BackupCodeCount:        cfg.Config.MFA.BackupCodeCount,
			TrustedDeviceTTL:       cfg.Config.MFA.TrustedDeviceLifespan.Duration(),
			CipherKey:              deriveCipherKey(cfg.Config),
		})
	}

	var passkeyAuth passkeyFlowService
	if cfg.Config.Passkeys.Enabled {
		passkeyStore := any(passkeysstore.New())
		if cfg.DB != nil && cfg.DB.Pool != nil {
			if repo, err := passkeysstore.NewPostgres(cfg.DB.Pool); err == nil {
				passkeyStore = repo
			}
		}
		passkeyAuth = passkeysservice.New(passkeyStore.(interface {
			SaveChallenge(challenge passkeysstore.Challenge)
			ConsumeChallenge(challengeID string) (passkeysstore.Challenge, error)
			CreateCredential(credential passkeysstore.Credential) error
			GetCredential(credentialID string) (passkeysstore.Credential, error)
			ListCredentialsByIdentity(identityID string) []passkeysstore.Credential
			UpdateCredentialSignCount(credentialID string, signCount uint32) error
		}), passkeysservice.Config{
			RPID:               cfg.Config.Passkeys.RPID,
			RPOrigin:           cfg.Config.Passkeys.RPOrigin,
			ChallengeTTL:       cfg.Config.Passkeys.ChallengeTTL.Duration(),
			AllowedCredentials: cfg.Config.Passkeys.AllowedCredentials,
		})
	}

	var checker policyChecker
	if cfg.Config.Policy.Enabled {
		checker = policygrpc.NewServer(policystore.New(cfg.DB.Pool))
	}

	moduleRoutes, err := NewModuleRouteTable(cfg.ModulePlan)
	if err != nil {
		reg.Stop()
		return nil, err
	}

	s := &Server{
		cfg:            cfg.Config,
		db:             cfg.DB,
		log:            cfg.Log,
		registry:       reg,
		modulePlan:     cfg.ModulePlan,
		moduleRoutes:   moduleRoutes,
		tokenGen:       tokenGen,
		sessionManager: sessionMgr,
		flowService:    flowService,
		policyChecker:  checker,
		workerManager:  cfg.WorkerManager,
		passwordAuth:   passwordAuth,
		magicLinkAuth:  magicLinkAuth,
		mfaAuth:        mfaAuth,
		passkeyAuth:    passkeyAuth,
		courier:        courierSvc,
	}

	// Setup routes
	s.router = SetupRoutes(s)

	// Start registry
	reg.Start()
	s.log.Info("Service registry started")

	// Bootstrap admin if requested
	if cfg.AdminBootstrap {
		if err := s.bootstrapAdmin(ctx); err != nil {
			s.log.Warn("Admin bootstrap failed", "error", err)
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
		s.log.Info("Admin bootstrap skipped: no operator credentials configured")
		return nil
	}
	if config.IsPlaceholderValue(password) {
		return errors.New("admin bootstrap blocked: operator.password must be rotated from placeholder value")
	}

	s.log.Info("Admin bootstrap requested", "email", email)

	outcome, err := ensureBootstrapAdminOperator(ctx, s.db, email, password)
	if err != nil {
		return err
	}

	if !outcome.CreatedIdentity && !outcome.CreatedOperator {
		s.log.Info("Admin bootstrap skipped: operator already exists", "email", email)
		return nil
	}

	attrs := map[string]any{
		"email":            email,
		"identity_id":      outcome.IdentityID.String(),
		"created_identity": outcome.CreatedIdentity,
		"created_operator": outcome.CreatedOperator,
	}
	if outcome.OperatorID != uuid.Nil {
		attrs["operator_id"] = outcome.OperatorID.String()
	}
	s.log.LogWideEvent(ctx, "Admin bootstrap completed", attrs)

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

		hashedPassword, hashErr := platformcrypto.HashPassword(password)
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
		`, uuid.New(), identityID, email, hashedPassword); execErr != nil {
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
			hashedPassword, hashErr := platformcrypto.HashPassword(password)
			if hashErr != nil {
				return outcome, hashErr
			}
			if _, execErr := tx.Exec(ctx, `
				INSERT INTO pwd_credentials (id, identity_id, identifier, hash, created_at, updated_at)
				VALUES ($1, $2, $3, $4, NOW(), NOW())
			`, uuid.New(), identityID, email, hashedPassword); execErr != nil {
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

func deriveCipherKey(cfg *config.Config) []byte {
	if cfg == nil || len(cfg.Secrets.Cipher) == 0 || strings.TrimSpace(cfg.Secrets.Cipher[0]) == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(cfg.Secrets.Cipher[0])))
	return sum[:]
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

	s.log.Info("Background workers registered")
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("Shutting down server components")

	if err := s.shutdownGRPCControlPlane(ctx); err != nil {
		return err
	}
	if s.registry != nil {
		s.registry.Stop()
		s.log.Info("Service registry stopped")
	}

	return nil
}

// Registry returns the service registry.
func (s *Server) Registry() *registry.Registry {
	return s.registry
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
		ctx := r.Context()

		defer func() {
			// Use wide events pattern - one context-rich log per request
			s.log.LogWideEvent(ctx, "HTTP request", map[string]any{
				"http.method":      r.Method,
				"http.path":        r.URL.Path,
				"http.status":      ww.Status(),
				"latency_ms":       time.Since(start).Milliseconds(),
				"http.user_agent":  r.UserAgent(),
				"http.remote_addr": r.RemoteAddr,
				"outcome":          map[bool]string{true: "success", false: "error"}[ww.Status() < 400],
			})
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

	moduleCount := 0
	healthyCount := 0
	unhealthyModules := make([]map[string]string, 0)
	registryOK := s.registry != nil
	if s.registry != nil {
		moduleCount = s.registry.ModuleCount()
		healthyCount = s.registry.HealthyCount()
		for _, module := range s.registry.ListModules(nil) {
			if module.Status == registry.StatusHealthy {
				continue
			}
			unhealthyModules = append(unhealthyModules, map[string]string{
				"id":     module.ID,
				"name":   module.Name,
				"status": string(module.Status),
			})
		}
	}

	resp := map[string]interface{}{
		"status":        "ready",
		"database":      "ok",
		"registry":      registryOK,
		"module_count":  moduleCount,
		"healthy_count": healthyCount,
		"courier":       s.courier != nil,
	}
	if len(unhealthyModules) > 0 {
		resp["unhealthy_modules"] = unhealthyModules
	}
	if !registryOK {
		resp["status"] = "not ready"
		resp["reason"] = "registry unavailable"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	if moduleCount > 0 && healthyCount < moduleCount {
		resp["status"] = "not ready"
		resp["reason"] = "modules unhealthy"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
		return
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
