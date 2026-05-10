package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/registry"
	coresession "github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/database"
)

type nilCreateSessionManager struct{}

func (nilCreateSessionManager) Create(context.Context, uuid.UUID, coresession.AuthMethod, coresession.DeviceInfo) (*coresession.Session, error) {
	return nil, nil
}
func (nilCreateSessionManager) GetFromRequest(context.Context, *http.Request) (*coresession.Session, error) {
	return nil, coresession.ErrSessionNotFound
}
func (nilCreateSessionManager) Revoke(context.Context, uuid.UUID) error               { return nil }
func (nilCreateSessionManager) RevokeAllForIdentity(context.Context, uuid.UUID) error { return nil }
func (nilCreateSessionManager) AddAuthMethod(context.Context, uuid.UUID, coresession.AuthMethod) error {
	return nil
}
func (nilCreateSessionManager) SetCookie(http.ResponseWriter, *coresession.Session) {}
func (nilCreateSessionManager) ClearCookie(http.ResponseWriter)                     {}

func TestAuthFlowHelperEdgeBranches(t *testing.T) {
	s, _ := newFlowServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/self-service/login", nil)

	unknown := &flows.Flow{Type: flows.FlowType("unknown")}
	result, err := s.executeFlowSubmission(context.Background(), rec, req, unknown, flowSubmitInput{})
	if err != nil {
		t.Fatalf("executeFlowSubmission unknown type should not error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for unknown flow type, got %+v", result)
	}

	if id, ok := pendingMFALoginIdentity(nil); ok || id != nil {
		t.Fatalf("expected nil pending mfa identity for nil flow")
	}
	if method, ok := pendingMFALoginMethod(nil); ok || method != "" {
		t.Fatalf("expected empty pending mfa method for nil flow")
	}

	flow := &flows.Flow{Context: map[string]any{}}
	flow.Context["pending_login_identity_id"] = 123
	if id, ok := pendingMFALoginIdentity(flow); ok || id != nil {
		t.Fatalf("expected invalid pending identity context type to fail")
	}
	flow.Context["pending_login_identity_id"] = "not-a-uuid"
	if id, ok := pendingMFALoginIdentity(flow); ok || id != nil {
		t.Fatalf("expected invalid pending identity uuid to fail")
	}

	flow.Context["pending_login_method"] = 123
	if method, ok := pendingMFALoginMethod(flow); ok || method != "" {
		t.Fatalf("expected invalid pending method context type to fail")
	}
	flow.Context["pending_login_method"] = " "
	if method, ok := pendingMFALoginMethod(flow); ok || method != "" {
		t.Fatalf("expected blank pending method to fail")
	}

	if _, _, ok := s.pendingMFALogin(flow); ok {
		t.Fatal("expected pendingMFALogin to fail with missing identity/method")
	}

	clearPendingMFALogin(nil)

	readReq := httptest.NewRequest(http.MethodGet, "/", nil)
	var nilServer *Server
	if got := nilServer.readMFATrustedDeviceCookie(readReq); got != "" {
		t.Fatalf("expected empty cookie value for nil server, got %q", got)
	}

	writeRec := httptest.NewRecorder()
	nilServer.writeMFATrustedDeviceCookie(writeRec, "token", time.Now().UTC().Add(time.Hour))
	s.writeMFATrustedDeviceCookie(writeRec, "", time.Now().UTC().Add(time.Hour))
	if len(writeRec.Result().Cookies()) != 0 {
		t.Fatalf("expected no cookies when write preconditions are not met")
	}

	if amr := sessionAMRValues(nil); amr != nil {
		t.Fatalf("expected nil AMR values for empty methods, got %+v", amr)
	}
}

func TestMFAFlowErrorBranches(t *testing.T) {
	t.Run("finish primary auth bubbles mfa and auth-context errors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/self-service/login", nil)
		rec := httptest.NewRecorder()

		s, _ := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{}
		s.mfaAuth = &stubMFAFlowService{hasFactorErr: errors.New("has-factor-failed")}
		flow, _ := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if _, err := s.finishPrimaryAuthentication(context.Background(), rec, req, flow, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected mfa has-factor error")
		}

		s, _ = newFlowServer(t)
		s.cfg.MFA.TrustedDeviceCookieName = "aegion_mfa_trusted_device"
		s.sessionManager = &stubRouteSessionManager{addAuthMethodErr: errors.New("add-auth-failed")}
		s.mfaAuth = &stubMFAFlowService{hasFactor: true, validateTrustedDevice: true}
		flow, _ = s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		reqWithCookie := httptest.NewRequest(http.MethodPost, "http://example.com/self-service/login", nil)
		reqWithCookie.AddCookie(&http.Cookie{Name: "aegion_mfa_trusted_device", Value: "trusted-token"})
		if _, err := s.finishPrimaryAuthentication(context.Background(), httptest.NewRecorder(), reqWithCookie, flow, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil || !strings.Contains(err.Error(), "add-auth-failed") {
			t.Fatalf("expected add-auth-failed error, got %v", err)
		}

		s, store := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{}
		flow, _ = s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		store.updateErr = errors.New("update-flow-failed")
		if _, err := s.finishPrimaryAuthentication(context.Background(), httptest.NewRecorder(), req, flow, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil || !strings.Contains(err.Error(), "update-flow-failed") {
			t.Fatalf("expected update-flow-failed error, got %v", err)
		}

		s, _ = newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{}
		flow, _ = s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				if len(dest) != 1 {
					return errors.New("unexpected destination length")
				}
				if v, ok := dest[0].(*bool); ok {
					*v = false
					return nil
				}
				return errors.New("expected bool destination")
			}}
		}
		if _, err := s.finishPrimaryAuthentication(context.Background(), httptest.NewRecorder(), req, flow, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected inactive identity rejection")
		} else {
			expectFlowHTTPError(t, err, http.StatusUnauthorized)
		}
	})

	t.Run("ensureSecondFactorOrTrustedDevice errors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/self-service/login", nil)

		s, _ := newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{hasFactor: true, validateTrustedDeviceErr: errors.New("trusted-device-check-failed")}
		s.cfg.MFA.TrustedDeviceCookieName = "aegion_mfa_trusted_device"
		req.AddCookie(&http.Cookie{Name: "aegion_mfa_trusted_device", Value: "token"})
		flow, _ := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if _, _, err := s.ensureSecondFactorOrTrustedDevice(context.Background(), req, flow, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected trusted device validation error")
		}

		s, store := newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{hasFactor: true}
		store.createErr = errors.New("create-login-flow-failed")
		if _, _, err := s.ensureSecondFactorOrTrustedDevice(context.Background(), req, nil, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected create login flow failure")
		}

		s, store = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{hasFactor: true}
		flow, _ = s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		store.updateErr = errors.New("update-login-flow-failed")
		if _, _, err := s.ensureSecondFactorOrTrustedDevice(context.Background(), req, flow, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected prepare mfa flow update error")
		}
	})

	t.Run("completePendingMFALogin errors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/self-service/login", nil)
		rec := httptest.NewRecorder()
		identityID := uuid.New()
		flow := &flows.Flow{}

		s, _ := newFlowServer(t)
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{"totp_code": "123456"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected mfa unavailable error")
		}

		s, _ = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{}
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected missing totp/backup error")
		}

		s, _ = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{verifyTOTPErr: errors.New("totp-invalid")}
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{"totp_code": "000000"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected invalid totp error")
		}

		s, _ = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{verifyBackupErr: errors.New("backup-invalid")}
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{"backup_code": "BAD-CODE"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected invalid backup error")
		}

		s, _ = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{}
		s.sessionManager = &stubRouteSessionManager{createErr: errors.New("create-session-failed")}
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{"totp_code": "123456"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected create session error")
		}

		s, _ = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{}
		s.sessionManager = &stubRouteSessionManager{addAuthMethodErr: errors.New("add-auth-failed")}
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{"totp_code": "123456"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected add auth method error")
		}

		s, _ = newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{trustedDeviceErr: errors.New("remember-device-failed")}
		s.sessionManager = &stubRouteSessionManager{}
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, flow, map[string]string{"totp_code": "123456", "remember_device": "true"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected remember trusted device error")
		}

		s, store := newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{}
		s.sessionManager = &stubRouteSessionManager{}
		pendingFlow, _ := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		store.updateErr = errors.New("apply-auth-context-failed")
		if _, err := s.completePendingMFALogin(context.Background(), rec, req, pendingFlow, map[string]string{"totp_code": "123456"}, identityID, coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected apply auth context update error")
		}
	})

	t.Run("prepare mfa login flow requires flow", func(t *testing.T) {
		s, _ := newFlowServer(t)
		if err := s.prepareMFALoginFlow(context.Background(), nil, uuid.New(), "person@example.com", coresession.AuthMethodPassword); err == nil {
			t.Fatal("expected flow is required error")
		}
	})
}

func TestCreateIdentityErrorBranches(t *testing.T) {
	ctx := context.Background()
	email := "person@example.com"

	t.Run("begin failure", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbBeginFn = func(context.Context) (pgx.Tx, error) { return nil, errors.New("begin-failed") }
		if _, err := s.createIdentity(ctx, email); err == nil {
			t.Fatal("expected begin failure")
		}
	})

	t.Run("schema resolution failure", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbBeginFn = func(context.Context) (pgx.Tx, error) { return &adminTestTx{}, nil }
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("schema-lookup-failed") }}
		}
		if _, err := s.createIdentity(ctx, email); err == nil {
			t.Fatal("expected schema lookup failure")
		}
	})

	t.Run("identity insert failure", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = schemaID
				return nil
			}}
		}
		s.dbBeginFn = func(context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "INSERT INTO core_identities") {
						return pgconn.CommandTag{}, errors.New("insert-failed")
					}
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}, nil
		}
		if _, err := s.createIdentity(ctx, email); err == nil {
			t.Fatal("expected identity insert failure")
		}
	})

	t.Run("primary email upsert failure", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = schemaID
				return nil
			}}
		}
		s.dbBeginFn = func(context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "core_identity_addresses") {
						return pgconn.CommandTag{}, errors.New("upsert-email-failed")
					}
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
			}, nil
		}
		if _, err := s.createIdentity(ctx, email); err == nil {
			t.Fatal("expected primary email upsert failure")
		}
	})

	t.Run("commit failure", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = schemaID
				return nil
			}}
		}
		s.dbBeginFn = func(context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				commitFn: func(context.Context) error { return errors.New("commit-failed") },
			}, nil
		}
		if _, err := s.createIdentity(ctx, email); err == nil {
			t.Fatal("expected commit failure")
		}
	})
}

func TestExternalAndMagicLinkErrorBranches(t *testing.T) {
	t.Run("external login flow validation and completion errors", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.sessionManager = &stubRouteSessionManager{}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login/methods/external/complete", mustJSONBody(t, map[string]any{
			"flow_id":     uuid.New().String(),
			"csrf_token":  "missing",
			"identity_id": uuid.New().String(),
			"method":      "social",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		s, store := newFlowServer(t)
		s.sessionManager = nilCreateSessionManager{}
		callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"identity_id": uuid.New().String(),
				"profile": map[string]any{
					"email": "person@example.com",
				},
			})
		}))
		defer callback.Close()
		registerTestModule(t, s, "social", registry.EndpointHTTP, callback.URL)
		flow, _ := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		store.updateErr = errors.New("complete-flow-failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login/methods/external/complete", mustJSONBody(t, map[string]any{
			"flow_id":    flow.ID.String(),
			"csrf_token": flow.CSRFToken,
			"method":     "social",
			"provider":   "github",
			"state":      "state-123",
			"code":       "code-123",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleCompleteExternalLogin(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, rec.Code, rec.Body.String())
		}
	})

	t.Run("magic link invalid recovery and verification identities", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "person@example.com",
			verifyLinkIdentityID: nil,
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/recovery/methods/link/verify?token=t", nil)
		s.handleMagicLinkRecoveryVerify(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/self-service/verification/methods/link/verify?token=t", nil)
		s.handleMagicLinkVerificationVerify(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("magic link response redirects to root when no flow payload", func(t *testing.T) {
		s, _ := newFlowServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=t", nil)
		s.respondMagicLinkVerification(rec, req, map[string]any{
			"status":  "authenticated",
			"message": "ok",
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "/" {
			t.Fatalf("expected redirect to /, got %q", got)
		}
	})
}

func TestSettingsAndPasskeyErrorBranches(t *testing.T) {
	t.Run("settings mfa endpoints require session and enabled service", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.mfaAuth = &stubMFAFlowService{}
		s.sessionManager = &stubRouteSessionManager{getErr: errors.New("no-session")}

		rec := httptest.NewRecorder()
		s.handleStartSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/start", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		s, _ = newFlowServer(t)
		rec = httptest.NewRecorder()
		s.handleFinishSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/finish", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		s.mfaAuth = &stubMFAFlowService{}
		s.sessionManager = &stubRouteSessionManager{getErr: errors.New("no-session")}
		rec = httptest.NewRecorder()
		s.handleFinishSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/finish", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		s, _ = newFlowServer(t)
		rec = httptest.NewRecorder()
		s.handleRegenerateSettingsBackupCodes(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/backup-codes/regenerate", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		s.mfaAuth = &stubMFAFlowService{}
		s.sessionManager = &stubRouteSessionManager{getErr: errors.New("no-session")}
		rec = httptest.NewRecorder()
		s.handleRegenerateSettingsBackupCodes(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/backup-codes/regenerate", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("passkey login endpoint error branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.db = &database.DB{}
		s.passkeyAuth = &stubPasskeyFlowService{}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", mustJSONBody(t, map[string]any{
			"flow_id":    "bad-flow-id",
			"csrf_token": "csrf",
			"identifier": "person@example.com",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", mustJSONBody(t, map[string]any{
			"flow_id":    uuid.New().String(),
			"csrf_token": "bad-csrf",
			"identifier": "person@example.com",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		flow, _ := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("lookup-failed") }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", mustJSONBody(t, map[string]any{
			"flow_id":    flow.ID.String(),
			"csrf_token": flow.CSRFToken,
			"identifier": "person@example.com",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		identityID := uuid.New()
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				return scanIdentityOrActive(identityID, dest...)
			}}
		}
		s.passkeyAuth = &stubPasskeyFlowService{beginAuthenticationErr: errors.New("begin-auth-failed")}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", mustJSONBody(t, map[string]any{
			"flow_id":    flow.ID.String(),
			"csrf_token": flow.CSRFToken,
			"identifier": "person@example.com",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("passkey finish endpoint error branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.db = &database.DB{}

		rec := httptest.NewRecorder()
		s.handleFinishLoginPasskey(rec, httptest.NewRequest(http.MethodGet, "/self-service/login/methods/passkey/finish", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		s.handleFinishLoginPasskey(rec, httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		flow, _ := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		identityID := uuid.New()
		s.passkeyAuth = &stubPasskeyFlowService{}
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				return scanIdentityOrActive(identityID, dest...)
			}}
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       "bad-flow-id",
			"csrf_token":    "csrf",
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    1,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow.ID.String(),
			"csrf_token":    "bad-csrf",
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    1,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
		}

		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("lookup-failed") }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow.ID.String(),
			"csrf_token":    flow.CSRFToken,
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    1,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				return scanIdentityOrActive(identityID, dest...)
			}}
		}
		s.sessionManager = &stubRouteSessionManager{createErr: errors.New("create-session-failed")}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow.ID.String(),
			"csrf_token":    flow.CSRFToken,
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    1,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s2, store := newFlowServer(t)
		s2.db = &database.DB{}
		s2.passkeyAuth = &stubPasskeyFlowService{}
		s2.sessionManager = nilCreateSessionManager{}
		s2.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				return scanIdentityOrActive(identityID, dest...)
			}}
		}
		flow2, _ := s2.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		store.updateErr = errors.New("complete-flow-failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow2.ID.String(),
			"csrf_token":    flow2.CSRFToken,
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    1,
		}))
		req.Header.Set("Content-Type", "application/json")
		s2.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, rec.Code, rec.Body.String())
		}
	})

	t.Run("passkey settings endpoints service/session error branches", func(t *testing.T) {
		s, _ := newFlowServer(t)
		rec := httptest.NewRecorder()
		s.handleStartSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/start", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		s.passkeyAuth = &stubPasskeyFlowService{}
		s.sessionManager = &stubRouteSessionManager{getErr: errors.New("no-session")}
		rec = httptest.NewRecorder()
		s.handleStartSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/start", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		s2, _ := newFlowServer(t)
		rec = httptest.NewRecorder()
		s2.handleFinishSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/finish", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		s2.passkeyAuth = &stubPasskeyFlowService{}
		s2.sessionManager = &stubRouteSessionManager{getErr: errors.New("no-session")}
		rec = httptest.NewRecorder()
		s2.handleFinishSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/finish", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})
}

func TestLookupIdentityByEmailStateFiltering(t *testing.T) {
	s := newHookedServer(t)
	var capturedQuery string
	s.dbQueryRowFn = func(_ context.Context, query string, _ ...any) pgx.Row {
		capturedQuery = query
		return adminTestRow{scanFn: func(dest ...any) error {
			if len(dest) != 1 {
				return errors.New("unexpected destination length")
			}
			if v, ok := dest[0].(*uuid.UUID); ok {
				*v = uuid.New()
				return nil
			}
			return errors.New("expected uuid destination")
		}}
	}

	if _, err := s.lookupIdentityByEmail(context.Background(), "person@example.com"); err != nil {
		t.Fatalf("lookupIdentityByEmail returned error: %v", err)
	}
	if !strings.Contains(capturedQuery, "AND i.state = 'active'") {
		t.Fatalf("expected active state filter in lookupIdentityByEmail query, got %q", capturedQuery)
	}

	if _, err := s.lookupAnyIdentityByEmail(context.Background(), "person@example.com"); err != nil {
		t.Fatalf("lookupAnyIdentityByEmail returned error: %v", err)
	}
	if strings.Contains(capturedQuery, "AND i.state = 'active'") {
		t.Fatalf("expected no active state filter in lookupAnyIdentityByEmail query, got %q", capturedQuery)
	}
}
