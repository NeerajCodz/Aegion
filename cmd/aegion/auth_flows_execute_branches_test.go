package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	passwordservice "github.com/aegion/aegion/modules/password/service"
)

func expectFlowHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	var flowErr *flowHTTPError
	if !errors.As(err, &flowErr) {
		t.Fatalf("expected flowHTTPError, got %T (%v)", err, err)
	}
	if flowErr.Status != status {
		t.Fatalf("expected flowHTTPError status %d, got %d (%v)", status, flowErr.Status, flowErr)
	}
}

func TestExecuteLoginFlowBranches(t *testing.T) {
	s, _ := newFlowServer(t)
	flow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
	if err != nil {
		t.Fatalf("create login flow: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/self-service/login", nil)

	_, err = s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{
		"identifier": "person@example.com",
		"password":   "secret",
	})
	expectFlowHTTPError(t, err, http.StatusServiceUnavailable)

	s.passwordAuth = &stubPasswordFlowService{}
	_, err = s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{
		"password": "secret",
	})
	expectFlowHTTPError(t, err, http.StatusBadRequest)

	s.passwordAuth = &stubPasswordFlowService{verifyErr: passwordservice.ErrInvalidCredentials}
	_, err = s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{
		"identifier": "person@example.com",
		"password":   "secret",
	})
	expectFlowHTTPError(t, err, http.StatusUnauthorized)

	s.passwordAuth = nil
	_, err = s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{
		"identifier": "person@example.com",
	})
	expectFlowHTTPError(t, err, http.StatusBadRequest)

	s.magicLinkAuth = &stubMagicLinkFlowService{}
	_, err = s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{})
	expectFlowHTTPError(t, err, http.StatusBadRequest)

	s.magicLinkAuth = &stubMagicLinkFlowService{loginSendErr: magiclinkservice.ErrRateLimited}
	_, err = s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{
		"identifier": "person@example.com",
	})
	expectFlowHTTPError(t, err, http.StatusTooManyRequests)

	s.magicLinkAuth = &stubMagicLinkFlowService{}
	result, err := s.executeLoginFlow(context.Background(), rec, req, flow, map[string]string{
		"identifier": "person@example.com",
	})
	if err != nil {
		t.Fatalf("executeLoginFlow(challenge_sent) error = %v", err)
	}
	if result == nil || result.Status != "challenge_sent" {
		t.Fatalf("expected challenge_sent result, got %+v", result)
	}
}

func TestExecuteSettingsAndVerificationFlowBranches(t *testing.T) {
	t.Run("settings", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		flow, err := s.flowService.CreateSettingsFlow(context.Background(), "http://example.com/settings", identityID, uuid.New())
		if err != nil {
			t.Fatalf("create settings flow: %v", err)
		}

		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{"new_password": "secret"})
		expectFlowHTTPError(t, err, http.StatusServiceUnavailable)

		s.passwordAuth = &stubPasswordFlowService{}
		_, err = s.executeSettingsFlow(context.Background(), nil, map[string]string{"new_password": "secret"})
		expectFlowHTTPError(t, err, http.StatusUnauthorized)

		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{
			"new_password":     "secret",
			"password_confirm": "different",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		flow.AddContext("recovery_verified", true)
		s.passwordAuth = &stubPasswordFlowService{resetErr: passwordservice.ErrPasswordTooShort}
		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{"new_password": "short"})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		s.passwordAuth = &stubPasswordFlowService{}
		s.sessionManager = &stubRouteSessionManager{revokeAllErr: errors.New("revoke failed")}
		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{"new_password": "LongEnoughPassword123!"})
		if err == nil {
			t.Fatal("expected revoke all sessions error during recovery reset")
		}

		delete(flow.Context, "recovery_verified")
		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{"new_password": "LongEnoughPassword123!"})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		s.passwordAuth = &stubPasswordFlowService{changeErr: passwordservice.ErrInvalidCredentials}
		_, err = s.executeSettingsFlow(context.Background(), flow, map[string]string{
			"new_password":     "LongEnoughPassword123!",
			"current_password": "wrong",
		})
		expectFlowHTTPError(t, err, http.StatusUnauthorized)

		s.passwordAuth = &stubPasswordFlowService{}
		result, err := s.executeSettingsFlow(context.Background(), flow, map[string]string{
			"new_password":     "LongEnoughPassword123!",
			"current_password": "current-secret",
		})
		if err != nil {
			t.Fatalf("executeSettingsFlow(success) error = %v", err)
		}
		if result == nil || result.Status != "updated" {
			t.Fatalf("expected updated result, got %+v", result)
		}
	})

	t.Run("verification", func(t *testing.T) {
		s, _ := newFlowServer(t)
		identityID := uuid.New()
		flow, err := s.flowService.CreateVerificationFlow(context.Background(), "http://example.com/verification", &identityID)
		if err != nil {
			t.Fatalf("create verification flow: %v", err)
		}

		_, err = s.executeVerificationFlow(context.Background(), flow, map[string]string{
			"email": "person@example.com",
			"code":  "123456",
		})
		expectFlowHTTPError(t, err, http.StatusServiceUnavailable)

		s.magicLinkAuth = &stubMagicLinkFlowService{}
		s.dbQueryRowFn = func(context.Context, string, ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("query failed") }}
		}
		_, err = s.executeVerificationFlow(context.Background(), flow, map[string]string{"code": "123456"})
		if err == nil {
			t.Fatal("expected primaryEmailByIdentity lookup failure")
		}
		s.dbQueryRowFn = nil

		_, err = s.executeVerificationFlow(context.Background(), nil, map[string]string{})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		_, err = s.executeVerificationFlow(context.Background(), nil, map[string]string{"email": "person@example.com"})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		s.magicLinkAuth = &stubMagicLinkFlowService{verifyCodeErr: magiclinkservice.ErrInvalidCode}
		_, err = s.executeVerificationFlow(context.Background(), flow, map[string]string{
			"email": "person@example.com",
			"code":  "123456",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		s.magicLinkAuth = &stubMagicLinkFlowService{}
		_, err = s.executeVerificationFlow(context.Background(), flow, map[string]string{
			"email": "person@example.com",
			"code":  "123456",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		otherIdentity := uuid.New()
		s.magicLinkAuth = &stubMagicLinkFlowService{verifyCodeIdentityID: &otherIdentity}
		_, err = s.executeVerificationFlow(context.Background(), flow, map[string]string{
			"email": "person@example.com",
			"code":  "123456",
		})
		expectFlowHTTPError(t, err, http.StatusBadRequest)

		s.magicLinkAuth = &stubMagicLinkFlowService{verifyCodeIdentityID: &identityID}
		s.dbExecFn = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec failed")
		}
		_, err = s.executeVerificationFlow(context.Background(), flow, map[string]string{
			"email": "person@example.com",
			"code":  "123456",
		})
		if err == nil {
			t.Fatal("expected markEmailVerified failure")
		}

		s.dbExecFn = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		result, err := s.executeVerificationFlow(context.Background(), flow, map[string]string{
			"email": "person@example.com",
			"code":  "123456",
		})
		if err != nil {
			t.Fatalf("executeVerificationFlow(success) error = %v", err)
		}
		if result == nil || result.Status != "verified" {
			t.Fatalf("expected verified result, got %+v", result)
		}
	})
}
