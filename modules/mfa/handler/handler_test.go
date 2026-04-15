package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/mfa/service"
)

type stubMFAService struct {
	startResp  *service.TOTPEnrollmentStartResponse
	finishResp *service.TOTPEnrollmentFinishResponse
	trustToken string
	trustExp   time.Time
}

func (s *stubMFAService) StartTOTPEnrollment(_ context.Context, identityID, accountName string) (*service.TOTPEnrollmentStartResponse, error) {
	return s.startResp, nil
}

func (s *stubMFAService) CompleteTOTPEnrollment(_ context.Context, req *service.TOTPEnrollmentFinishRequest) (*service.TOTPEnrollmentFinishResponse, error) {
	return s.finishResp, nil
}

func (s *stubMFAService) VerifyTOTP(_ context.Context, identityID, code string) error {
	return nil
}

func (s *stubMFAService) VerifyBackupCode(_ context.Context, identityID, code string) error {
	return nil
}

func (s *stubMFAService) RegenerateBackupCodes(_ context.Context, identityID string) ([]string, error) {
	return []string{"ABCD-EFGH-IJKL"}, nil
}

func (s *stubMFAService) RememberTrustedDevice(_ context.Context, identityID, label string) (string, time.Time, error) {
	return s.trustToken, s.trustExp, nil
}

func (s *stubMFAService) RevokeTrustedDevice(_ context.Context, identityID, token string) error {
	return nil
}

func TestRegisterRoutes(t *testing.T) {
	h := New(&stubMFAService{
		startResp: &service.TOTPEnrollmentStartResponse{
			EnrollmentID: "enroll-1",
			Secret:       "SECRET",
			OTPAuthURL:   "otpauth://totp/Aegion:user@example.com?secret=SECRET",
			ExpiresIn:    600,
		},
		finishResp: &service.TOTPEnrollmentFinishResponse{
			BackupCodes: []string{"AAAA-BBBB-CCCC"},
		},
		trustToken: "trusted-token",
		trustExp:   time.Unix(1700000000, 0).UTC(),
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("totp start", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mfa/totp/start", strings.NewReader(`{"identity_id":"id-1","account_name":"user@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["enrollment_id"] != "enroll-1" {
			t.Fatalf("unexpected response body: %+v", body)
		}
	})

	t.Run("totp finish", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mfa/totp/finish", strings.NewReader(`{"identity_id":"id-1","enrollment_id":"enroll-1","code":"123456"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("verify totp", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mfa/totp/verify", strings.NewReader(`{"identity_id":"id-1","code":"123456"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("backup regenerate", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mfa/backup/regenerate", strings.NewReader(`{"identity_id":"id-1"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("trusted device", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mfa/trusted-device", strings.NewReader(`{"identity_id":"id-1","label":"browser"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}
