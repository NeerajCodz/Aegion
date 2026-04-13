package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/core/flows"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
)

type stubPasswordFlowService struct {
	validateErr error
	verifyErr   error
	verifyID    uuid.UUID

	registerErr error
	registers   []struct {
		IdentityID uuid.UUID
		Identifier string
		Password   string
	}

	changeErr error
	changes   []struct {
		IdentityID  uuid.UUID
		OldPassword string
		NewPassword string
	}

	resetErr error
	resets   []struct {
		IdentityID  uuid.UUID
		NewPassword string
	}
}

func (s *stubPasswordFlowService) ValidatePassword(ctx context.Context, password, identifier string) error {
	return s.validateErr
}

func (s *stubPasswordFlowService) Register(ctx context.Context, identityID uuid.UUID, identifier, password string) error {
	s.registers = append(s.registers, struct {
		IdentityID uuid.UUID
		Identifier string
		Password   string
	}{IdentityID: identityID, Identifier: identifier, Password: password})
	return s.registerErr
}

func (s *stubPasswordFlowService) Verify(ctx context.Context, identifier, password string) (uuid.UUID, error) {
	if s.verifyID == uuid.Nil {
		s.verifyID = uuid.New()
	}
	return s.verifyID, s.verifyErr
}

func (s *stubPasswordFlowService) ChangePassword(ctx context.Context, identityID uuid.UUID, oldPassword, newPassword string) error {
	s.changes = append(s.changes, struct {
		IdentityID  uuid.UUID
		OldPassword string
		NewPassword string
	}{IdentityID: identityID, OldPassword: oldPassword, NewPassword: newPassword})
	return s.changeErr
}

func (s *stubPasswordFlowService) ResetPassword(ctx context.Context, identityID uuid.UUID, newPassword string) error {
	s.resets = append(s.resets, struct {
		IdentityID  uuid.UUID
		NewPassword string
	}{IdentityID: identityID, NewPassword: newPassword})
	return s.resetErr
}

type stubMagicLinkFlowService struct {
	loginSends        []string
	verificationSends []struct {
		Email      string
		IdentityID uuid.UUID
	}
	recoverySends []struct {
		Email      string
		IdentityID *uuid.UUID
	}

	verifyLinkRecipient  string
	verifyLinkIdentityID *uuid.UUID
	verifyLinkErr        error

	verifyCodeIdentityID *uuid.UUID
	verifyCodeErr        error
}

func (s *stubMagicLinkFlowService) SendLoginCode(ctx context.Context, email string) error {
	s.loginSends = append(s.loginSends, email)
	return nil
}

func (s *stubMagicLinkFlowService) VerifyMagicLink(ctx context.Context, token string) (string, *uuid.UUID, error) {
	return s.verifyLinkRecipient, s.verifyLinkIdentityID, s.verifyLinkErr
}

func (s *stubMagicLinkFlowService) VerifyMagicLinkForType(ctx context.Context, token string, expectedType magiclinkstore.CodeType) (string, *uuid.UUID, error) {
	return s.verifyLinkRecipient, s.verifyLinkIdentityID, s.verifyLinkErr
}

func (s *stubMagicLinkFlowService) SendVerificationCode(ctx context.Context, email string, identityID uuid.UUID) error {
	s.verificationSends = append(s.verificationSends, struct {
		Email      string
		IdentityID uuid.UUID
	}{Email: email, IdentityID: identityID})
	return nil
}

func (s *stubMagicLinkFlowService) VerifyVerificationCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error) {
	return s.verifyCodeIdentityID, s.verifyCodeErr
}

func (s *stubMagicLinkFlowService) SendRecoveryCodeIfIdentityExists(ctx context.Context, email string, identityID *uuid.UUID) error {
	s.recoverySends = append(s.recoverySends, struct {
		Email      string
		IdentityID *uuid.UUID
	}{Email: email, IdentityID: identityID})
	return nil
}

func (s *stubMagicLinkFlowService) VerifyRecoveryCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error) {
	return s.verifyCodeIdentityID, s.verifyCodeErr
}

func TestHandleSubmitLoginExecutesPasswordAuthentication(t *testing.T) {
	s, store := newFlowServer(t)
	auth := &stubPasswordFlowService{verifyID: uuid.New()}
	sm := &stubRouteSessionManager{}
	s.passwordAuth = auth
	s.sessionManager = sm

	flow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
	if err != nil {
		t.Fatalf("create login flow: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
		"flow_id":    flow.ID.String(),
		"csrf_token": flow.CSRFToken,
		"identifier": "person@example.com",
		"password":   "correct horse battery staple",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleSubmitLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if len(sm.created) != 1 || sm.created[0].IdentityID != auth.verifyID {
		t.Fatalf("expected session for %s, got %+v", auth.verifyID, sm.created)
	}
	if sm.setCookieCount != 1 {
		t.Fatalf("expected session cookie to be set once, got %d", sm.setCookieCount)
	}
	if got := store.flows[flow.ID].State; got != flows.StateCompleted {
		t.Fatalf("expected flow to be completed, got %s", got)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "authenticated" {
		t.Fatalf("expected authenticated status, got %q", body["status"])
	}
}

func TestHandleSubmitRegistrationCreatesIdentityAndSession(t *testing.T) {
	s := newHookedServer(t)
	store := newRouteFlowStore()
	s.flowService = flows.NewService(store, flows.DefaultConfig())
	auth := &stubPasswordFlowService{}
	magic := &stubMagicLinkFlowService{}
	sm := &stubRouteSessionManager{}
	s.passwordAuth = auth
	s.magicLinkAuth = magic
	s.sessionManager = sm

	schemaID := uuid.New()
	insertedIdentityID := uuid.Nil
	s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
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
	s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &adminTestTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT INTO core_identities") {
					insertedIdentityID = args[0].(uuid.UUID)
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}, nil
	}

	flow, err := s.flowService.CreateRegistrationFlow(context.Background(), "http://example.com/registration")
	if err != nil {
		t.Fatalf("create registration flow: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/registration", mustJSONBody(t, map[string]any{
		"flow_id":               flow.ID.String(),
		"csrf_token":            flow.CSRFToken,
		"email":                 "new.user@example.com",
		"password":              "Passw0rd!withLength",
		"password_confirmation": "Passw0rd!withLength",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleSubmitRegistration(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if insertedIdentityID == uuid.Nil {
		t.Fatal("expected identity insert to run")
	}
	if len(auth.registers) != 1 || auth.registers[0].IdentityID != insertedIdentityID {
		t.Fatalf("expected password registration for %s, got %+v", insertedIdentityID, auth.registers)
	}
	if len(sm.created) != 1 || sm.created[0].IdentityID != insertedIdentityID {
		t.Fatalf("expected session for %s, got %+v", insertedIdentityID, sm.created)
	}
	if len(magic.verificationSends) != 1 || magic.verificationSends[0].IdentityID != insertedIdentityID {
		t.Fatalf("expected verification send for %s, got %+v", insertedIdentityID, magic.verificationSends)
	}
}

func TestHandleMagicLinkRecoveryVerifyCreatesResetFlow(t *testing.T) {
	s, store := newFlowServer(t)
	identityID := uuid.New()
	s.magicLinkAuth = &stubMagicLinkFlowService{
		verifyLinkRecipient:  "recover@example.com",
		verifyLinkIdentityID: &identityID,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/self-service/recovery/methods/link/verify?token=test-token", nil)
	req.Header.Set("Accept", "application/json")
	s.handleMagicLinkRecoveryVerify(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	var found *flows.Flow
	for _, flow := range store.flows {
		if flow.Type == flows.TypeSettings {
			found = flow
			break
		}
	}
	if found == nil {
		t.Fatal("expected a settings flow to be created")
	}
	if found.IdentityID == nil || *found.IdentityID != identityID {
		t.Fatalf("expected recovery flow identity %s, got %+v", identityID, found.IdentityID)
	}
	if recoveryVerified, ok := found.GetContext("recovery_verified"); !ok || recoveryVerified != true {
		t.Fatalf("expected recovery_verified context, got %v (ok=%v)", recoveryVerified, ok)
	}
}

func TestHandleSubmitSettingsUsesRecoveryReset(t *testing.T) {
	s, store := newFlowServer(t)
	passwords := &stubPasswordFlowService{}
	s.passwordAuth = passwords

	identityID := uuid.New()
	flow, err := s.flowService.CreateSettingsFlow(context.Background(), "http://example.com/settings", identityID)
	if err != nil {
		t.Fatalf("create settings flow: %v", err)
	}
	flow.AddContext("recovery_verified", true)
	if err := s.flowService.UpdateFlow(context.Background(), flow); err != nil {
		t.Fatalf("update settings flow: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/settings", mustJSONBody(t, map[string]any{
		"flow_id":          flow.ID.String(),
		"csrf_token":       flow.CSRFToken,
		"new_password":     "AnEvenBetterPassword123!",
		"password_confirm": "AnEvenBetterPassword123!",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleSubmitSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if len(passwords.resets) != 1 || passwords.resets[0].IdentityID != identityID {
		t.Fatalf("expected recovery reset for %s, got %+v", identityID, passwords.resets)
	}
	if len(passwords.changes) != 0 {
		t.Fatalf("expected no change-password call during recovery reset, got %+v", passwords.changes)
	}
	if got := store.flows[flow.ID].State; got != flows.StateCompleted {
		t.Fatalf("expected completed flow, got %s", got)
	}
}

func TestHandleSubmitVerificationMarksEmailVerified(t *testing.T) {
	s := newHookedServer(t)
	store := newRouteFlowStore()
	s.flowService = flows.NewService(store, flows.DefaultConfig())

	identityID := uuid.New()
	s.magicLinkAuth = &stubMagicLinkFlowService{
		verifyCodeIdentityID: &identityID,
	}
	var verifiedEmail string
	s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "UPDATE core_identity_addresses") {
			verifiedEmail = args[1].(string)
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}

	flow, err := s.flowService.CreateVerificationFlow(context.Background(), "http://example.com/verification", &identityID)
	if err != nil {
		t.Fatalf("create verification flow: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/verification", mustJSONBody(t, map[string]any{
		"flow_id":    flow.ID.String(),
		"csrf_token": flow.CSRFToken,
		"email":      "verify@example.com",
		"code":       "123456",
	}))
	req.Header.Set("Content-Type", "application/json")
	s.handleSubmitVerification(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if verifiedEmail != "verify@example.com" {
		t.Fatalf("expected verification update for verify@example.com, got %q", verifiedEmail)
	}
}
