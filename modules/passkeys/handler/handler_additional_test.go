package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/passkeys/service"
)

type passkeyBehaviorService struct {
	beginRegResp  *service.RegistrationStartResponse
	beginRegErr   error
	finishRegErr  error
	beginAuthResp *service.AuthenticationStartResponse
	beginAuthErr  error
	finishAuthErr error
}

func (s *passkeyBehaviorService) BeginRegistration(identityID string) (*service.RegistrationStartResponse, error) {
	return s.beginRegResp, s.beginRegErr
}
func (s *passkeyBehaviorService) FinishRegistration(req *service.RegistrationFinishRequest) error {
	return s.finishRegErr
}
func (s *passkeyBehaviorService) BeginAuthentication(identityID string) (*service.AuthenticationStartResponse, error) {
	return s.beginAuthResp, s.beginAuthErr
}
func (s *passkeyBehaviorService) FinishAuthentication(req *service.AuthenticationFinishRequest) error {
	return s.finishAuthErr
}

func TestPasskeyHandlerAdditionalBranches(t *testing.T) {
	svc := &passkeyBehaviorService{
		beginRegResp:  &service.RegistrationStartResponse{Challenge: "c1"},
		beginAuthResp: &service.AuthenticationStartResponse{Challenge: "c2"},
	}
	h := New(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("method and decode validation branches", func(t *testing.T) {
		cases := []struct {
			method string
			path   string
			body   string
			code   int
		}{
			{method: http.MethodGet, path: "/api/v1/passkeys/registration/start", body: "", code: http.StatusMethodNotAllowed},
			{method: http.MethodPost, path: "/api/v1/passkeys/registration/start", body: "{", code: http.StatusBadRequest},
			{method: http.MethodGet, path: "/api/v1/passkeys/registration/finish", body: "", code: http.StatusMethodNotAllowed},
			{method: http.MethodPost, path: "/api/v1/passkeys/registration/finish", body: "{", code: http.StatusBadRequest},
			{method: http.MethodGet, path: "/api/v1/passkeys/authentication/start", body: "", code: http.StatusMethodNotAllowed},
			{method: http.MethodPost, path: "/api/v1/passkeys/authentication/start", body: "{", code: http.StatusBadRequest},
			{method: http.MethodGet, path: "/api/v1/passkeys/authentication/finish", body: "", code: http.StatusMethodNotAllowed},
			{method: http.MethodPost, path: "/api/v1/passkeys/authentication/finish", body: "{", code: http.StatusBadRequest},
		}

		for _, tc := range cases {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("%s %s expected %d, got %d", tc.method, tc.path, tc.code, rec.Code)
			}
		}
	})

	t.Run("service error branches", func(t *testing.T) {
		svc.beginRegErr = errors.New("begin reg failed")
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/registration/start", strings.NewReader(`{"identity_id":"id-1"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		svc.beginRegErr = nil
		svc.finishRegErr = errors.New("finish reg failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/registration/finish", strings.NewReader(`{"identity_id":"id-1","challenge":"c1","credential_id":"cred-1","public_key":"pk"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		svc.finishRegErr = nil
		svc.beginAuthErr = errors.New("begin auth failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/authentication/start", strings.NewReader(`{"identity_id":"id-1"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		svc.beginAuthErr = nil
		svc.finishAuthErr = errors.New("finish auth failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/authentication/finish", strings.NewReader(`{"identity_id":"id-1","challenge":"c2","credential_id":"cred-1","signature":"sig","sign_count":1}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}

