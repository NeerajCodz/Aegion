package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/core/flows"
	coresession "github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/database"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
	mfaservice "github.com/aegion/aegion/modules/mfa/service"
	passkeysservice "github.com/aegion/aegion/modules/passkeys/service"
)

type stubPasskeyFlowService struct {
	beginRegistrationResp *passkeysservice.RegistrationStartResponse
	beginRegistrationErr  error
	finishRegistrationErr error

	beginAuthenticationResp *passkeysservice.AuthenticationStartResponse
	beginAuthenticationErr  error
	finishAuthenticationErr error

	beginRegistrationIdentityIDs []string
	beginAuthenticationIdentity  []string
	finishAuthenticationReqs     []*passkeysservice.AuthenticationFinishRequest
	finishRegistrationReqs       []*passkeysservice.RegistrationFinishRequest
}

func (s *stubPasskeyFlowService) BeginRegistration(identityID string) (*passkeysservice.RegistrationStartResponse, error) {
	s.beginRegistrationIdentityIDs = append(s.beginRegistrationIdentityIDs, identityID)
	if s.beginRegistrationErr != nil {
		return nil, s.beginRegistrationErr
	}
	if s.beginRegistrationResp != nil {
		return s.beginRegistrationResp, nil
	}
	return &passkeysservice.RegistrationStartResponse{Challenge: "reg-challenge", ExpiresIn: 300}, nil
}

func (s *stubPasskeyFlowService) FinishRegistration(req *passkeysservice.RegistrationFinishRequest) error {
	if req != nil {
		reqCopy := *req
		s.finishRegistrationReqs = append(s.finishRegistrationReqs, &reqCopy)
	}
	return s.finishRegistrationErr
}

func (s *stubPasskeyFlowService) BeginAuthentication(identityID string) (*passkeysservice.AuthenticationStartResponse, error) {
	s.beginAuthenticationIdentity = append(s.beginAuthenticationIdentity, identityID)
	if s.beginAuthenticationErr != nil {
		return nil, s.beginAuthenticationErr
	}
	if s.beginAuthenticationResp != nil {
		return s.beginAuthenticationResp, nil
	}
	return &passkeysservice.AuthenticationStartResponse{
		Challenge:            "auth-challenge",
		AllowedCredentialIDs: []string{"cred-1"},
		ExpiresIn:            300,
	}, nil
}

func (s *stubPasskeyFlowService) FinishAuthentication(req *passkeysservice.AuthenticationFinishRequest) error {
	if req != nil {
		reqCopy := *req
		s.finishAuthenticationReqs = append(s.finishAuthenticationReqs, &reqCopy)
	}
	return s.finishAuthenticationErr
}

func TestHandleSubmitRecoveryExecutesAuthFlow(t *testing.T) {
	s, store := newFlowServer(t)
	magic := &stubMagicLinkFlowService{}
	s.magicLinkAuth = magic

	flow, err := s.flowService.CreateRecoveryFlow(context.Background(), "http://example.com/recovery")
	if err != nil {
		t.Fatalf("create recovery flow: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/recovery", mustJSONBody(t, map[string]any{
		"flow_id":    flow.ID.String(),
		"csrf_token": flow.CSRFToken,
		"email":      "recover@example.com",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleSubmitRecovery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if got := store.flows[flow.ID].State; got != flows.StateCompleted {
		t.Fatalf("expected recovery flow completion, got %s", got)
	}
	if len(magic.recoverySends) != 1 || magic.recoverySends[0].Email != "recover@example.com" {
		t.Fatalf("expected recovery code send call, got %+v", magic.recoverySends)
	}
}

func TestHandleMFASettingsEndpoints(t *testing.T) {
	t.Run("start enrollment handles method, availability, and success", func(t *testing.T) {
		s, _ := newFlowServer(t)
		rec := httptest.NewRecorder()
		s.handleStartSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodGet, "/self-service/settings/mfa/totp/start", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		s.handleStartSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/start", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		identityID := uuid.New()
		mfa := &stubMFAFlowService{
			startErr: errors.New("start failed"),
		}
		s.mfaAuth = mfa
		s.sessionManager = &stubRouteSessionManager{
			session: &coresession.Session{IdentityID: identityID},
		}
		rec = httptest.NewRecorder()
		s.handleStartSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/start", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		mfa.startErr = nil
		mfa.startResp = &mfaservice.TOTPEnrollmentStartResponse{EnrollmentID: "enroll-1", Secret: "secret", OTPAuthURL: "otpauth://aegion", ExpiresIn: 600}
		rec = httptest.NewRecorder()
		s.handleStartSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/start", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if len(mfa.startCalls) == 0 || mfa.startCalls[len(mfa.startCalls)-1].AccountName != identityID.String() {
			t.Fatalf("expected fallback account name to identity id, got %+v", mfa.startCalls)
		}
	})

	t.Run("finish enrollment validates body and records identity", func(t *testing.T) {
		s, _ := newFlowServer(t)
		mfa := &stubMFAFlowService{}
		identityID := uuid.New()
		s.mfaAuth = mfa
		s.sessionManager = &stubRouteSessionManager{
			session: &coresession.Session{IdentityID: identityID},
		}

		rec := httptest.NewRecorder()
		s.handleFinishSettingsTOTPEnrollment(rec, httptest.NewRequest(http.MethodGet, "/self-service/settings/mfa/totp/finish", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/finish", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishSettingsTOTPEnrollment(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		mfa.finishErr = errors.New("finish failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/finish", mustJSONBody(t, map[string]any{
			"enrollment_id": "enroll-1",
			"code":          "123456",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishSettingsTOTPEnrollment(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		mfa.finishErr = nil
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/totp/finish", mustJSONBody(t, map[string]any{
			"enrollment_id": "enroll-2",
			"code":          "222222",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishSettingsTOTPEnrollment(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if len(mfa.finishRequests) == 0 || mfa.finishRequests[len(mfa.finishRequests)-1].IdentityID != identityID.String() {
			t.Fatalf("expected finish request identity id %s, got %+v", identityID, mfa.finishRequests)
		}
	})

	t.Run("regenerate backup codes handles failures and success", func(t *testing.T) {
		s, _ := newFlowServer(t)
		rec := httptest.NewRecorder()
		s.handleRegenerateSettingsBackupCodes(rec, httptest.NewRequest(http.MethodGet, "/self-service/settings/mfa/backup-codes/regenerate", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		identityID := uuid.New()
		mfa := &stubMFAFlowService{regenerateBackupCodesErr: errors.New("regenerate failed")}
		s.mfaAuth = mfa
		s.sessionManager = &stubRouteSessionManager{
			session: &coresession.Session{IdentityID: identityID},
		}

		rec = httptest.NewRecorder()
		s.handleRegenerateSettingsBackupCodes(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/backup-codes/regenerate", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		mfa.regenerateBackupCodesErr = nil
		mfa.regeneratedBackupCodes = []string{"CODE-1", "CODE-2"}
		rec = httptest.NewRecorder()
		s.handleRegenerateSettingsBackupCodes(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/mfa/backup-codes/regenerate", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
	})
}

func TestHandlePasskeyLoginEndpoints(t *testing.T) {
	t.Run("start login covers unresolved and resolved identities", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.db = &database.DB{}
		rec := httptest.NewRecorder()
		s.handleStartLoginPasskey(rec, httptest.NewRequest(http.MethodGet, "/self-service/login/methods/passkey/start", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		s.handleStartLoginPasskey(rec, httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		passkeys := &stubPasskeyFlowService{}
		s.passkeyAuth = passkeys

		flow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create login flow: %v", err)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", mustJSONBody(t, map[string]any{
			"flow_id":    flow.ID.String(),
			"csrf_token": flow.CSRFToken,
			"identifier": "nobody@example.com",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		var unresolved map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &unresolved); err != nil {
			t.Fatalf("decode unresolved identity response: %v", err)
		}
		if unresolved["challenge"] != "" {
			t.Fatalf("expected empty challenge for unknown identity, got %+v", unresolved)
		}

		resolvedIdentity := uuid.New()
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = resolvedIdentity
				return nil
			}}
		}
		passkeys.beginAuthenticationResp = &passkeysservice.AuthenticationStartResponse{
			Challenge:            "resolved-challenge",
			AllowedCredentialIDs: []string{"cred-123"},
			ExpiresIn:            300,
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/start", mustJSONBody(t, map[string]any{
			"flow_id":    flow.ID.String(),
			"csrf_token": flow.CSRFToken,
			"identifier": "person@example.com",
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleStartLoginPasskey(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if len(passkeys.beginAuthenticationIdentity) == 0 || passkeys.beginAuthenticationIdentity[len(passkeys.beginAuthenticationIdentity)-1] != resolvedIdentity.String() {
			t.Fatalf("expected begin-auth for %s, got %+v", resolvedIdentity, passkeys.beginAuthenticationIdentity)
		}
	})

	t.Run("finish login covers unauthorized, mfa-step-up, and completion", func(t *testing.T) {
		s, store := newFlowServer(t)
		s.db = &database.DB{}
		passkeys := &stubPasskeyFlowService{}
		sm := &stubRouteSessionManager{}
		s.passkeyAuth = passkeys
		s.sessionManager = sm

		flow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create login flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow.ID.String(),
			"csrf_token":    flow.CSRFToken,
			"identifier":    "missing@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    1,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		identityID := uuid.New()
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = identityID
				return nil
			}}
		}
		passkeys.finishAuthenticationErr = errors.New("invalid signature")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow.ID.String(),
			"csrf_token":    flow.CSRFToken,
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    2,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		passkeys.finishAuthenticationErr = nil
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       flow.ID.String(),
			"csrf_token":    flow.CSRFToken,
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    3,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if len(sm.created) != 1 {
			t.Fatalf("expected session creation after passkey login, got %+v", sm.created)
		}
		if got := store.flows[flow.ID].State; got != flows.StateCompleted {
			t.Fatalf("expected completed login flow, got %s", got)
		}

		stepUpFlow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create step-up flow: %v", err)
		}
		s.mfaAuth = &stubMFAFlowService{hasFactor: true}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/passkey/finish", mustJSONBody(t, map[string]any{
			"flow_id":       stepUpFlow.ID.String(),
			"csrf_token":    stepUpFlow.CSRFToken,
			"identifier":    "person@example.com",
			"challenge":     "ch",
			"credential_id": "cred",
			"signature":     "sig",
			"sign_count":    4,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFinishLoginPasskey(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		var stepUp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &stepUp); err != nil {
			t.Fatalf("decode step-up response: %v", err)
		}
		if stepUp["status"] != "mfa_required" {
			t.Fatalf("expected mfa_required response, got %+v", stepUp)
		}
	})
}

func TestHandlePasskeySettingsEndpoints(t *testing.T) {
	s, _ := newFlowServer(t)
	passkeys := &stubPasskeyFlowService{}
	identityID := uuid.New()
	s.passkeyAuth = passkeys
	s.sessionManager = &stubRouteSessionManager{
		session: &coresession.Session{IdentityID: identityID},
	}

	rec := httptest.NewRecorder()
	s.handleStartSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodGet, "/self-service/settings/passkey/start", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	passkeys.beginRegistrationErr = errors.New("begin failed")
	rec = httptest.NewRecorder()
	s.handleStartSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/start", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}

	passkeys.beginRegistrationErr = nil
	rec = httptest.NewRecorder()
	s.handleStartSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if len(passkeys.beginRegistrationIdentityIDs) == 0 || passkeys.beginRegistrationIdentityIDs[len(passkeys.beginRegistrationIdentityIDs)-1] != identityID.String() {
		t.Fatalf("expected begin-registration identity %s, got %+v", identityID, passkeys.beginRegistrationIdentityIDs)
	}

	rec = httptest.NewRecorder()
	s.handleFinishSettingsPasskeyRegistration(rec, httptest.NewRequest(http.MethodGet, "/self-service/settings/passkey/finish", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/finish", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	s.handleFinishSettingsPasskeyRegistration(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}

	passkeys.finishRegistrationErr = errors.New("finish failed")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/finish", mustJSONBody(t, map[string]any{
		"challenge":     "challenge",
		"credential_id": "credential-id",
		"public_key":    "public-key",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleFinishSettingsPasskeyRegistration(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}

	passkeys.finishRegistrationErr = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/self-service/settings/passkey/finish", mustJSONBody(t, map[string]any{
		"challenge":     "challenge",
		"credential_id": "credential-id",
		"public_key":    "public-key",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleFinishSettingsPasskeyRegistration(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if len(passkeys.finishRegistrationReqs) == 0 || passkeys.finishRegistrationReqs[len(passkeys.finishRegistrationReqs)-1].IdentityID != identityID.String() {
		t.Fatalf("expected finish-registration identity %s, got %+v", identityID, passkeys.finishRegistrationReqs)
	}
}

func TestHandleMagicLinkVerificationAdditionalPaths(t *testing.T) {
	t.Run("login verification supports mfa browser redirect", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "person@example.com",
			verifyLinkIdentityID: &identityID,
		}
		s.sessionManager = &stubRouteSessionManager{}
		s.mfaAuth = &stubMFAFlowService{hasFactor: true}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=test-token", nil)
		s.handleMagicLinkLoginVerify(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
		}
		if !strings.HasPrefix(rec.Header().Get("Location"), "/ui/login?flow=") || !strings.Contains(rec.Header().Get("Location"), "&mfa=1") {
			t.Fatalf("expected mfa login redirect, got %q", rec.Header().Get("Location"))
		}
	})

	t.Run("verification wrapper updates email and supports json", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "verify@example.com",
			verifyLinkIdentityID: &identityID,
		}
		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/verification/methods/link/verify?token=test-token", nil)
		req.Header.Set("Accept", "application/json")
		s.handleMagicLinkVerificationVerify(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode verification response: %v", err)
		}
		if payload["status"] != "verified" {
			t.Fatalf("expected verified status, got %+v", payload)
		}
	})

	t.Run("missing token and unsupported type are rejected", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{
			verifyLinkRecipient:  "verify@example.com",
			verifyLinkIdentityID: &identityID,
		}

		rec := httptest.NewRecorder()
		s.handleMagicLinkLoginVerify(rec, httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/link/verify?token=test-token", nil)
		s.handleMagicLinkVerify(rec, req, magiclinkstore.CodeType("unknown"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
