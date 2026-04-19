package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/internal/platform/database"
	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
	passwordservice "github.com/aegion/aegion/modules/password/service"
)

func TestExecuteRegistrationFlowAdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("validation and dependency branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/registration", nil)

		_, err := s.executeRegistrationFlow(ctx, rec, req, nil, map[string]string{
			"email":    "person@example.com",
			"password": "secret",
		})
		expectFlowHTTPError(t, err, http.StatusServiceUnavailable)

		s.passwordAuth = &stubPasswordFlowService{}
		_, err = s.executeRegistrationFlow(ctx, rec, req, nil, map[string]string{
			"password": "secret",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		_, err = s.executeRegistrationFlow(ctx, rec, req, nil, map[string]string{
			"email": "person@example.com",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		_, err = s.executeRegistrationFlow(ctx, rec, req, nil, map[string]string{
			"email":            "person@example.com",
			"password":         "secret",
			"password_confirm": "different",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)
	})

	t.Run("identity lookup and password policy branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.passwordAuth = &stubPasswordFlowService{}
		s.db = &database.DB{}

		lookupErr := errors.New("lookup failed")
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return lookupErr }}
		}
		_, err := s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "secret",
		})
		if !errors.Is(err, lookupErr) {
			t.Fatalf("expected lookup error, got %v", err)
		}

		existingIdentityID := uuid.New()
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = existingIdentityID
				return nil
			}}
		}
		_, err = s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "secret",
		})
		expectFlowHTTPError(t, err, http.StatusConflict)

		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		s.passwordAuth = &stubPasswordFlowService{validateErr: passwordservice.ErrPasswordTooShort}
		_, err = s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "short",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)
	})

	t.Run("identity creation and registration branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.passwordAuth = &stubPasswordFlowService{}

		_, err := s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "Passw0rd!WithLength",
		})
		if err == nil || !strings.Contains(err.Error(), "database unavailable") {
			t.Fatalf("expected database unavailable error, got %v", err)
		}

		s = newHookedServer(t)
		s.passwordAuth = &stubPasswordFlowService{registerErr: passwordservice.ErrPasswordTooShort}
		s.db = &database.DB{}
		schemaID := uuid.New()
		s.dbQueryRowFn = func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "JOIN core_identity_addresses"):
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
		}
		s.dbBeginFn = func(context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}, nil
		}
		deleteCalled := false
		s.dbExecFn = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "UPDATE core_identities") {
				deleteCalled = true
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		_, err = s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "Passw0rd!WithLength",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)
		if !deleteCalled {
			t.Fatal("expected registration rollback to delete created identity")
		}
	})

	t.Run("session creation and success paths", func(t *testing.T) {
		setupServer := func() *Server {
			s := newHookedServer(t)
			s.passwordAuth = &stubPasswordFlowService{}
			s.db = &database.DB{}
			schemaID := uuid.New()
			s.dbQueryRowFn = func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "JOIN core_identity_addresses"):
					return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				case strings.Contains(sql, "FROM core_identity_schemas"):
					return adminTestRow{scanFn: func(dest ...any) error {
						*(dest[0].(*uuid.UUID)) = schemaID
						return nil
					}}
				default:
					return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				}
			}
			s.dbBeginFn = func(context.Context) (pgx.Tx, error) {
				return &adminTestTx{
					execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
						return pgconn.NewCommandTag("UPDATE 1"), nil
					},
				}, nil
			}
			return s
		}

		s := setupServer()
		_, err := s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "Passw0rd!WithLength",
		})
		if err == nil || !strings.Contains(err.Error(), "session manager unavailable") {
			t.Fatalf("expected session manager unavailable error, got %v", err)
		}

		s = setupServer()
		s.sessionManager = &stubRouteSessionManager{}
		result, err := s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "Passw0rd!WithLength",
		})
		if err != nil {
			t.Fatalf("executeRegistrationFlow success returned error: %v", err)
		}
		if result == nil || result.Status != "registered" || result.Message != "registration successful" {
			t.Fatalf("unexpected registration result: %+v", result)
		}

		s = setupServer()
		s.sessionManager = &stubRouteSessionManager{}
		s.magicLinkAuth = &stubMagicLinkFlowService{}
		result, err = s.executeRegistrationFlow(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/self-service/registration", nil), nil, map[string]string{
			"email":    "person@example.com",
			"password": "Passw0rd!WithLength",
		})
		if err != nil {
			t.Fatalf("executeRegistrationFlow verification path returned error: %v", err)
		}
		if result == nil || !strings.Contains(result.Message, "verification email sent") {
			t.Fatalf("expected verification message, got %+v", result)
		}
	})
}

func TestExecuteRecoveryFlowAdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("service and request validation branches", func(t *testing.T) {
		s, _ := newFlowServer(t)

		_, err := s.executeRecoveryFlow(ctx, nil, map[string]string{"email": "person@example.com"})
		expectFlowHTTPError(t, err, http.StatusServiceUnavailable)

		s.magicLinkAuth = &stubMagicLinkFlowService{}
		_, err = s.executeRecoveryFlow(ctx, nil, map[string]string{})
		expectFlowHTTPError(t, err, http.StatusBadRequest)
	})

	t.Run("lookup and send branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.magicLinkAuth = &stubMagicLinkFlowService{}
		s.db = &database.DB{}

		lookupErr := errors.New("lookup failed")
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return lookupErr }}
		}
		_, err := s.executeRecoveryFlow(ctx, nil, map[string]string{"email": "person@example.com"})
		if !errors.Is(err, lookupErr) {
			t.Fatalf("expected lookup error, got %v", err)
		}

		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		s.magicLinkAuth = &stubMagicLinkFlowService{recoverySendErr: magiclinkservice.ErrRateLimited}
		_, err = s.executeRecoveryFlow(ctx, nil, map[string]string{"email": "person@example.com"})
		expectFlowHTTPError(t, err, http.StatusTooManyRequests)

		s.magicLinkAuth = &stubMagicLinkFlowService{}
		result, err := s.executeRecoveryFlow(ctx, nil, map[string]string{"email": "person@example.com"})
		if err != nil {
			t.Fatalf("executeRecoveryFlow success returned error: %v", err)
		}
		if result == nil || result.Status != "challenge_sent" {
			t.Fatalf("unexpected recovery result: %+v", result)
		}
		if len(s.magicLinkAuth.(*stubMagicLinkFlowService).recoverySends) != 1 {
			t.Fatalf("expected one recovery send call, got %+v", s.magicLinkAuth.(*stubMagicLinkFlowService).recoverySends)
		}
		if s.magicLinkAuth.(*stubMagicLinkFlowService).recoverySends[0].IdentityID != nil {
			t.Fatalf("expected nil identity when lookup returns no rows, got %+v", s.magicLinkAuth.(*stubMagicLinkFlowService).recoverySends[0].IdentityID)
		}
	})
}

func TestHandleCompleteExternalLoginAdditionalBranches(t *testing.T) {
	t.Run("rejects unsupported method and payload errors", func(t *testing.T) {
		s, _ := newFlowServer(t)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/self-service/login/methods/external/complete", nil)
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login/methods/external/complete", nil)
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("flow and identity validation branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{}

		regFlow, err := s.flowService.CreateRegistrationFlow(context.Background(), "http://example.com/registration")
		if err != nil {
			t.Fatalf("create registration flow: %v", err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login/methods/external/complete", mustJSONBody(t, map[string]any{
			"flow_id":     regFlow.ID.String(),
			"csrf_token":  regFlow.CSRFToken,
			"identity_id": uuid.New().String(),
			"method":      "social",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		loginFlow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create login flow: %v", err)
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login/methods/external/complete", mustJSONBody(t, map[string]any{
			"flow_id":     loginFlow.ID.String(),
			"csrf_token":  loginFlow.CSRFToken,
			"identity_id": "not-a-uuid",
			"method":      "social",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("returns execution failure when session creation fails", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{createErr: errors.New("session create failed")}

		flow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create login flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login/methods/external/complete", mustJSONBody(t, map[string]any{
			"flow_id":     flow.ID.String(),
			"csrf_token":  flow.CSRFToken,
			"identity_id": uuid.New().String(),
			"method":      "social",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d body=%s", http.StatusInternalServerError, rec.Code, rec.Body.String())
		}
	})
}

func TestHandleMagicLinkVerifyAdditionalBranches(t *testing.T) {
	t.Run("rejects when magic link auth is disabled", func(t *testing.T) {
		s, _ := newFlowServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeLogin)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("maps verification and login identity resolution errors", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.magicLinkAuth = &stubMagicLinkFlowService{verifyLinkErr: magiclinkservice.ErrRateLimited}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeLogin)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected %d, got %d", http.StatusTooManyRequests, rec.Code)
		}

		s.magicLinkAuth = &stubMagicLinkFlowService{verifyLinkRecipient: "person@example.com"}
		s.db = &database.DB{}
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("lookup failed") }}
		}
		rec = httptest.NewRecorder()
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeLogin)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		rec = httptest.NewRecorder()
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeLogin)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("login flow fails when session cannot be created", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "person@example.com",
			verifyLinkIdentityID: &identityID,
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeLogin)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("recovery flow creation and update error branches", func(t *testing.T) {
		s, store := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "recover@example.com",
			verifyLinkIdentityID: &identityID,
		}
		store.createErr = errors.New("create flow failed")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/recovery/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeRecovery)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s, store = newFlowServer(t)
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "recover@example.com",
			verifyLinkIdentityID: &identityID,
		}
		store.updateErr = errors.New("update flow failed")
		rec = httptest.NewRecorder()
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeRecovery)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("verification email update failure branch", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "verify@example.com",
			verifyLinkIdentityID: &identityID,
		}
		s.db = &database.DB{}
		s.dbExecFn = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("mark verified failed")
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/verification/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeVerification)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("responds with browser redirect for recovery flow", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "recover@example.com",
			verifyLinkIdentityID: &identityID,
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/recovery/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeTypeRecovery)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
		}
		if !strings.HasPrefix(rec.Header().Get("Location"), "/ui/settings?flow=") || !strings.Contains(rec.Header().Get("Location"), "&recovery=1") {
			t.Fatalf("expected recovery redirect with flow query, got %q", rec.Header().Get("Location"))
		}
	})
}

