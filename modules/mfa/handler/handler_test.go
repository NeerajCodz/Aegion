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

	"github.com/aegion/aegion/modules/mfa/service"
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

func TestMFAHandlersSuccessPaths(t *testing.T) {
	h := New(&stubMFAService{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"totp start", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/start", `{"identity_id":"id-1","account_name":"user@example.com"}`), http.StatusOK},
		{"totp finish", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/finish", `{"identity_id":"id-1","enrollment_id":"enroll-1","code":"123456"}`), http.StatusOK},
		{"totp verify", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/verify", `{"identity_id":"id-1","code":"123456"}`), http.StatusOK},
		{"backup verify", newMFARequest(http.MethodPost, "/api/v1/mfa/backup/verify", `{"identity_id":"id-1","code":"AAAA-BBBB-CCCC"}`), http.StatusOK},
		{"backup regenerate", newMFARequest(http.MethodPost, "/api/v1/mfa/backup/regenerate", `{"identity_id":"id-1"}`), http.StatusOK},
		{"trusted device remember", newMFARequest(http.MethodPost, "/api/v1/mfa/trusted-device", `{"identity_id":"id-1","label":"browser"}`), http.StatusOK},
		{"trusted device revoke", newMFARequest(http.MethodDelete, "/api/v1/mfa/trusted-device", `{"identity_id":"id-1","token":"trusted-token"}`), http.StatusOK},
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

func TestMFAHandlersMethodAndValidationErrors(t *testing.T) {
	h := New(&stubMFAService{})
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
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"totp start", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/start", `{"identity_id":"id-1","account_name":"user@example.com"}`)},
		{"totp finish", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/finish", `{"identity_id":"id-1","enrollment_id":"e","code":"123456"}`)},
		{"totp verify", newMFARequest(http.MethodPost, "/api/v1/mfa/totp/verify", `{"identity_id":"id-1","code":"123456"}`)},
		{"backup verify", newMFARequest(http.MethodPost, "/api/v1/mfa/backup/verify", `{"identity_id":"id-1","code":"A"}`)},
		{"backup regenerate", newMFARequest(http.MethodPost, "/api/v1/mfa/backup/regenerate", `{"identity_id":"id-1"}`)},
		{"trusted remember", newMFARequest(http.MethodPost, "/api/v1/mfa/trusted-device", `{"identity_id":"id-1","label":"browser"}`)},
		{"trusted revoke", newMFARequest(http.MethodDelete, "/api/v1/mfa/trusted-device", `{"identity_id":"id-1","token":"trusted-token"}`)},
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
