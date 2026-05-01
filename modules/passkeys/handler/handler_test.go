package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/passkeys/service"
)

type stubPasskeyService struct{}

func (s *stubPasskeyService) BeginRegistration(identityID string) (*service.RegistrationStartResponse, error) {
	return &service.RegistrationStartResponse{
		Challenge: "c1",
		RPID:      "example.com",
		RPOrigin:  "https://example.com",
		ExpiresIn: 300,
	}, nil
}

func (s *stubPasskeyService) FinishRegistration(req *service.RegistrationFinishRequest) error {
	return nil
}

func (s *stubPasskeyService) BeginAuthentication(identityID string) (*service.AuthenticationStartResponse, error) {
	return &service.AuthenticationStartResponse{
		Challenge:            "c2",
		AllowedCredentialIDs: []string{"cred-1"},
		ExpiresIn:            300,
	}, nil
}

func (s *stubPasskeyService) FinishAuthentication(req *service.AuthenticationFinishRequest) error {
	return nil
}

func TestRegisterRoutes(t *testing.T) {
	h := New(&stubPasskeyService{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("registration start", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/registration/start", strings.NewReader(`{"identity_id":"id-1"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed parsing body: %v", err)
		}
		if body["challenge"] == "" {
			t.Fatal("expected challenge")
		}
	})

	t.Run("registration finish", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/registration/finish", strings.NewReader(`{"identity_id":"id-1","challenge":"c1","credential_id":"cred-1","public_key":"pk"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("authentication start", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/authentication/start", strings.NewReader(`{"identity_id":"id-1"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("authentication finish", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/authentication/finish", strings.NewReader(`{"identity_id":"id-1","challenge":"c2","credential_id":"cred-1","signature":"sig","sign_count":1}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}
