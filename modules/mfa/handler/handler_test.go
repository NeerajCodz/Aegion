package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/mfa/service"
	"github.com/google/uuid"
)

type stubMFAService struct {
	startTOTPEnrollmentFn   func(context.Context, string, string) (*service.TOTPEnrollmentStartResponse, error)
	completeEnrollmentFn    func(context.Context, *service.TOTPEnrollmentFinishRequest) (*service.TOTPEnrollmentFinishResponse, error)
	verifyTOTPFn            func(context.Context, string, string) error
	verifyBackupCodeFn      func(context.Context, string, string) error
	regenerateBackupCodesFn func(context.Context, string) ([]string, error)
	rememberTrustedDeviceFn func(context.Context, string, string) (string, time.Time, error)
	revokeTrustedDeviceFn   func(context.Context, string, string) error
}

func (s *stubMFAService) StartTOTPEnrollment(ctx context.Context, identityID, accountName string) (*service.TOTPEnrollmentStartResponse, error) {
	if s.startTOTPEnrollmentFn != nil {
		return s.startTOTPEnrollmentFn(ctx, identityID, accountName)
	}
	return &service.TOTPEnrollmentStartResponse{
		EnrollmentID: "enroll-1",
		Secret:       "SECRET",
		OTPAuthURL:   "otpauth://totp/Aegion:user@example.com?secret=SECRET",
		ExpiresIn:    600,
	}, nil
}

func (s *stubMFAService) CompleteTOTPEnrollment(ctx context.Context, req *service.TOTPEnrollmentFinishRequest) (*service.TOTPEnrollmentFinishResponse, error) {
	if s.completeEnrollmentFn != nil {
		return s.completeEnrollmentFn(ctx, req)
	}
	return &service.TOTPEnrollmentFinishResponse{BackupCodes: []string{"AAAA-BBBB-CCCC"}}, nil
}

func (s *stubMFAService) VerifyTOTP(ctx context.Context, identityID, code string) error {
	if s.verifyTOTPFn != nil {
		return s.verifyTOTPFn(ctx, identityID, code)
	}
	return nil
}

func (s *stubMFAService) VerifyBackupCode(ctx context.Context, identityID, code string) error {
	if s.verifyBackupCodeFn != nil {
		return s.verifyBackupCodeFn(ctx, identityID, code)
	}
	return nil
}

func (s *stubMFAService) RegenerateBackupCodes(ctx context.Context, identityID string) ([]string, error) {
	if s.regenerateBackupCodesFn != nil {
		return s.regenerateBackupCodesFn(ctx, identityID)
	}
	return []string{"ABCD-EFGH-IJKL"}, nil
}

func (s *stubMFAService) RememberTrustedDevice(ctx context.Context, identityID, label string) (string, time.Time, error) {
	if s.rememberTrustedDeviceFn != nil {
		return s.rememberTrustedDeviceFn(ctx, identityID, label)
	}
	return "trusted-token", time.Unix(1700000000, 0).UTC(), nil
}

func (s *stubMFAService) RevokeTrustedDevice(ctx context.Context, identityID, token string) error {
	if s.revokeTrustedDeviceFn != nil {
		return s.revokeTrustedDeviceFn(ctx, identityID, token)
	}
	return nil
}

func newMFARequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

const testIdentitySigningSecret = "mfa-test-identity-signing-secret"

func newAuthenticatedMFARequest(t *testing.T, method, path, body, identityID string) *http.Request {
	t.Helper()
	req := newMFARequest(method, path, body)
	req.Header.Set(identityIDHeader, identityID)
	req.Header.Set(identitySessionIDHeader, uuid.NewString())
	req.Header.Set(identityAALHeader, "aal1")
	signature, err := platformcrypto.SignIdentityHeaders(
		[]byte(testIdentitySigningSecret),
		req.Header,
		signedIdentityHeaders,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("sign identity headers: %v", err)
	}
	req.Header.Set(identitySignatureHeader, signature)
	return req
}

func TestMFAHandlersSuccessPaths(t *testing.T) {
	h := New(&stubMFAService{}, WithIdentitySigningSecret([]byte(testIdentitySigningSecret)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	identityID := uuid.NewString()

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"totp start", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/start", `{"account_name":"user@example.com"}`, identityID), http.StatusOK},
		{"totp finish", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/finish", `{"enrollment_id":"enroll-1","code":"123456"}`, identityID), http.StatusOK},
		{"totp verify", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/verify", `{"code":"123456"}`, identityID), http.StatusOK},
		{"backup verify", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/backup/verify", `{"code":"AAAA-BBBB-CCCC"}`, identityID), http.StatusOK},
		{"backup regenerate", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/backup/regenerate", `{}`, identityID), http.StatusOK},
		{"trusted device remember", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/trusted-device", `{"label":"browser"}`, identityID), http.StatusOK},
		{"trusted device revoke", newAuthenticatedMFARequest(t, http.MethodDelete, "/api/v1/mfa/trusted-device", `{"token":"trusted-token"}`, identityID), http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMFAHandlersRequireVerifiedCoreIdentity(t *testing.T) {
	subjectID := uuid.NewString()
	calledIdentityID := ""
	h := New(&stubMFAService{
		verifyTOTPFn: func(_ context.Context, identityID, _ string) error {
			calledIdentityID = identityID
			return nil
		},
	}, WithIdentitySigningSecret([]byte(testIdentitySigningSecret)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("uses verified signed subject", func(t *testing.T) {
		req := newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/verify", `{"code":"123456"}`, subjectID)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		if calledIdentityID != subjectID {
			t.Fatalf("service identity = %q, want signed subject %q", calledIdentityID, subjectID)
		}
	})

	t.Run("rejects body identity authority", func(t *testing.T) {
		req := newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/verify", `{"identity_id":"`+uuid.NewString()+`","code":"123456"}`, subjectID)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects unsigned identity headers", func(t *testing.T) {
		req := newMFARequest(http.MethodPost, "/api/v1/mfa/totp/verify", `{"code":"123456"}`)
		req.Header.Set(identityIDHeader, subjectID)
		req.Header.Set(identitySessionIDHeader, uuid.NewString())
		req.Header.Set(identityAALHeader, "aal1")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects tampered signed identity", func(t *testing.T) {
		req := newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/verify", `{"code":"123456"}`, subjectID)
		req.Header.Set(identityIDHeader, uuid.NewString())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestMFAHandlersMethodAndValidationErrors(t *testing.T) {
	h := New(&stubMFAService{}, WithIdentitySigningSecret([]byte(testIdentitySigningSecret)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"totp start method", newMFARequest(http.MethodGet, "/api/v1/mfa/totp/start", ""), http.StatusMethodNotAllowed},
		{"totp start body", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/start", `{"identity_id":"id-1"}{"extra":1}`), http.StatusBadRequest},
		{"totp finish method", newMFARequest(http.MethodGet, "/api/v1/mfa/totp/finish", ""), http.StatusMethodNotAllowed},
		{"totp finish body", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/finish", `{"identity_id":"id-1"}{"extra":1}`), http.StatusBadRequest},
		{"totp verify method", newMFARequest(http.MethodGet, "/api/v1/mfa/totp/verify", ""), http.StatusMethodNotAllowed},
		{"totp verify body", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/verify", `{"identity_id":"id-1"}{"extra":1}`), http.StatusBadRequest},
		{"backup verify method", newMFARequest(http.MethodGet, "/api/v1/mfa/backup/verify", ""), http.StatusMethodNotAllowed},
		{"backup verify body", newMFARequest(http.MethodPost, "/api/v1/mfa/backup/verify", `{"identity_id":"id-1"}{"extra":1}`), http.StatusBadRequest},
		{"backup regenerate method", newMFARequest(http.MethodGet, "/api/v1/mfa/backup/regenerate", ""), http.StatusMethodNotAllowed},
		{"backup regenerate body", newMFARequest(http.MethodPost, "/api/v1/mfa/backup/regenerate", `{"identity_id":"id-1"}{"extra":1}`), http.StatusBadRequest},
		{"trusted device invalid body", newMFARequest(http.MethodPost, "/api/v1/mfa/trusted-device", `{"identity_id":"id-1"}{"extra":1}`), http.StatusBadRequest},
		{"trusted device invalid method", newMFARequest(http.MethodPatch, "/api/v1/mfa/trusted-device", `{}`), http.StatusMethodNotAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMFAHandlersServiceErrors(t *testing.T) {
	boom := errors.New("boom")
	h := New(&stubMFAService{
		startTOTPEnrollmentFn: func(context.Context, string, string) (*service.TOTPEnrollmentStartResponse, error) { return nil, boom },
		completeEnrollmentFn: func(context.Context, *service.TOTPEnrollmentFinishRequest) (*service.TOTPEnrollmentFinishResponse, error) {
			return nil, boom
		},
		verifyTOTPFn:            func(context.Context, string, string) error { return boom },
		verifyBackupCodeFn:      func(context.Context, string, string) error { return boom },
		regenerateBackupCodesFn: func(context.Context, string) ([]string, error) { return nil, boom },
		rememberTrustedDeviceFn: func(context.Context, string, string) (string, time.Time, error) { return "", time.Time{}, boom },
		revokeTrustedDeviceFn:   func(context.Context, string, string) error { return boom },
	}, WithIdentitySigningSecret([]byte(testIdentitySigningSecret)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	identityID := uuid.NewString()

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"totp start", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/start", `{"account_name":"user@example.com"}`, identityID)},
		{"totp finish", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/finish", `{"enrollment_id":"e","code":"123456"}`, identityID)},
		{"totp verify", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/totp/verify", `{"code":"123456"}`, identityID)},
		{"backup verify", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/backup/verify", `{"code":"A"}`, identityID)},
		{"backup regenerate", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/backup/regenerate", `{}`, identityID)},
		{"trusted remember", newAuthenticatedMFARequest(t, http.MethodPost, "/api/v1/mfa/trusted-device", `{"label":"browser"}`, identityID)},
		{"trusted revoke", newAuthenticatedMFARequest(t, http.MethodDelete, "/api/v1/mfa/trusted-device", `{"token":"trusted-token"}`, identityID)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, tc.req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMFAJSONHelpers(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":"b"}{"extra":1}`))
	var dst map[string]string
	if err := decodeJSONBody(rec, req, &dst); err == nil {
		t.Fatal("expected decodeJSONBody to reject trailing JSON")
	}

	rec = httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"status": "ok"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected content type application/json, got %q", ct)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode json payload: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}

	rec = httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "  invalid  ")
	var errPayload map[string]map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &errPayload); err != nil {
		t.Fatalf("failed to decode error payload: %v", err)
	}
	if errPayload["error"]["message"] != "invalid" {
		t.Fatalf("expected trimmed error message, got %#v", errPayload)
	}
}

func TestMFARegisterRoutesNilMux(t *testing.T) {
	h := New(&stubMFAService{})
	h.RegisterRoutes(nil)
}
