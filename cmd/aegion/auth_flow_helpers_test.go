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
	coresession "github.com/aegion/aegion/core/session"
	platformconfig "github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	passwordservice "github.com/aegion/aegion/modules/password/service"
)

func TestAuthFlowHelpersContextAndMethods(t *testing.T) {
	if got := (*flowHTTPError)(nil).Error(); got != "flow execution failed" {
		t.Fatalf("nil flowHTTPError.Error() = %q", got)
	}
	if got := (&flowHTTPError{Message: "boom"}).Error(); got != "boom" {
		t.Fatalf("flowHTTPError.Error() = %q", got)
	}

	if (&Server{}).selfServiceAuthEnabled() {
		t.Fatal("expected self service auth disabled with no auth modules configured")
	}
	if !(&Server{passwordAuth: &stubPasswordFlowService{}}).selfServiceAuthEnabled() {
		t.Fatal("expected self service auth enabled when password module exists")
	}
	if !(&Server{magicLinkAuth: &stubMagicLinkFlowService{}}).selfServiceAuthEnabled() {
		t.Fatal("expected self service auth enabled when magic-link module exists")
	}

	if got := passwordHIBPBaseURL(""); got != "" {
		t.Fatalf("passwordHIBPBaseURL(empty) = %q", got)
	}
	if got := passwordHIBPBaseURL("https://hibp.example.com/"); got != "https://hibp.example.com/range/" {
		t.Fatalf("passwordHIBPBaseURL(https host) = %q", got)
	}
	if got := passwordHIBPBaseURL("hibp.example.com/"); got != "https://hibp.example.com/range/" {
		t.Fatalf("passwordHIBPBaseURL(host only) = %q", got)
	}

	if got, err := parseExternalAuthMethod("social"); err != nil || got != coresession.AuthMethodSocial {
		t.Fatalf("parseExternalAuthMethod(social) = %q, %v", got, err)
	}
	if got, err := parseExternalAuthMethod("SSO"); err != nil || got != coresession.AuthMethodSAML {
		t.Fatalf("parseExternalAuthMethod(SSO) = %q, %v", got, err)
	}
	if _, err := parseExternalAuthMethod("password"); err == nil {
		t.Fatal("expected parseExternalAuthMethod to reject unsupported method")
	}

	if got := authMethodToAMR(coresession.AuthMethodPassword); got != "pwd" {
		t.Fatalf("authMethodToAMR(password) = %q", got)
	}
	if got := authMethodToAMR(coresession.AuthMethodPasskey); got != "hwk" {
		t.Fatalf("authMethodToAMR(passkey) = %q", got)
	}
	if got := authMethodToAMR(coresession.AuthMethodSocial); got != "federated" {
		t.Fatalf("authMethodToAMR(social) = %q", got)
	}
	if got := authMethodToAMR("unknown"); got != "" {
		t.Fatalf("authMethodToAMR(unknown) = %q", got)
	}

	methods := []coresession.SessionAuthMethod{
		{Method: coresession.AuthMethodPassword},
		{Method: coresession.AuthMethodPassword},
		{Method: coresession.AuthMethodTOTP},
		{Method: coresession.AuthMethodSAML},
		{Method: coresession.AuthMethod("invalid")},
	}
	amr := sessionAMRValues(methods)
	if len(amr) != 3 || amr[0] != "federated" || amr[1] != "otp" || amr[2] != "pwd" {
		t.Fatalf("sessionAMRValues = %#v", amr)
	}

	if got := authCompletionContext(nil, true); got != nil {
		t.Fatalf("authCompletionContext(nil) = %#v", got)
	}

	authenticatedAt := time.Now().UTC().Add(-time.Minute)
	session := &coresession.Session{
		ID:              uuid.New(),
		IdentityID:      uuid.New(),
		AAL:             coresession.AAL1,
		AuthenticatedAt: authenticatedAt,
		AuthMethods: []coresession.SessionAuthMethod{
			{Method: coresession.AuthMethodPassword},
			{Method: coresession.AuthMethodSAML},
		},
	}
	ctxMap := authCompletionContext(session, true)
	if ctxMap["aal"] != "aal1" || ctxMap["acr"] != "aal1" || ctxMap["trusted_device"] != "true" || ctxMap["reauth_required"] != "false" {
		t.Fatalf("authCompletionContext values = %#v", ctxMap)
	}
	if ctxMap["sid"] != session.ID.String() || ctxMap["amr"] == "" || ctxMap["auth_time"] == "" {
		t.Fatalf("authCompletionContext missing expected fields: %#v", ctxMap)
	}
	if _, err := time.Parse(time.RFC3339, ctxMap["auth_time"]); err != nil {
		t.Fatalf("auth_time is not RFC3339: %v", err)
	}
}

func TestAuthFlowHelpersSessionMutationAndContextMerge(t *testing.T) {
	applySessionAuthMethod(nil, coresession.AuthMethodPassword)

	session := &coresession.Session{
		ID:              uuid.New(),
		IdentityID:      uuid.New(),
		AAL:             coresession.AAL1,
		AuthenticatedAt: time.Now().UTC().Add(-time.Hour),
		AuthMethods:     []coresession.SessionAuthMethod{{Method: coresession.AuthMethodPassword}},
	}
	applySessionAuthMethod(session, coresession.AuthMethodPassword)
	if len(session.AuthMethods) != 1 {
		t.Fatalf("duplicate auth method should not be appended: %#v", session.AuthMethods)
	}
	applySessionAuthMethod(session, coresession.AuthMethodTOTP)
	if len(session.AuthMethods) != 2 {
		t.Fatalf("expected totp method append, got %#v", session.AuthMethods)
	}
	if session.AAL != coresession.AAL2 {
		t.Fatalf("expected AAL2 after second factor, got %s", session.AAL)
	}
	if session.AuthenticatedAt.IsZero() {
		t.Fatal("expected AuthenticatedAt to be refreshed for second factor")
	}

	flow, err := flows.NewFlow(flows.TypeLogin, "https://app.example.com/login", time.Minute)
	if err != nil {
		t.Fatalf("failed to create flow: %v", err)
	}
	store := newRouteFlowStore()
	store.flows[flow.ID] = flow
	s := &Server{flowService: flows.NewService(store, flows.DefaultConfig())}

	err = s.applyAuthContextToFlow(context.Background(), flow, session, map[string]string{
		"acr":            "aal2",
		"trusted_device": "true",
	})
	if err != nil {
		t.Fatalf("applyAuthContextToFlow returned error: %v", err)
	}
	if flow.IdentityID == nil || *flow.IdentityID != session.IdentityID {
		t.Fatalf("flow identity was not set: %#v", flow.IdentityID)
	}
	if flow.SessionID == nil || *flow.SessionID != session.ID {
		t.Fatalf("flow session was not set: %#v", flow.SessionID)
	}
	if got, ok := flow.GetContext("acr"); !ok || got != "aal2" {
		t.Fatalf("missing acr context value: %v %v", got, ok)
	}
	if got, ok := flow.GetContext("amr_values"); !ok || got == nil {
		t.Fatalf("missing amr_values context: %v %v", got, ok)
	}

	if err := (*Server)(nil).applyAuthContextToFlow(context.Background(), flow, session, nil); err != nil {
		t.Fatalf("nil server should return nil error, got %v", err)
	}
	if err := (&Server{}).applyAuthContextToFlow(context.Background(), nil, session, nil); err != nil {
		t.Fatalf("nil flow should return nil error, got %v", err)
	}

	payload := map[string]any{"existing": "value"}
	mergeAuthContext(payload, map[string]string{
		"auth_time": "2026-01-01T00:00:00Z",
		"empty":     "   ",
	})
	if payload["auth_time"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("expected auth_time merge, got %#v", payload)
	}
	if _, ok := payload["empty"]; ok {
		t.Fatalf("expected empty context values to be skipped: %#v", payload)
	}
	mergeAuthContext(nil, map[string]string{"x": "y"})
	mergeAuthContext(payload, nil)

	if !acceptsJSON(func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "application/json")
		return req
	}()) {
		t.Fatal("acceptsJSON should detect json accept header")
	}
	if acceptsJSON(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("acceptsJSON should be false without json accept header")
	}

	values := map[string]string{"id": "  user@example.com  ", "email": "fallback@example.com"}
	if got := normalizedFlowValue(values, "missing", "id", "email"); got != "user@example.com" {
		t.Fatalf("normalizedFlowValue = %q", got)
	}
	if got := normalizedFlowValue(nil, "id"); got != "" {
		t.Fatalf("normalizedFlowValue(nil) = %q", got)
	}
	if got := normalizedEmailValue(values, "id"); got != "user@example.com" {
		t.Fatalf("normalizedEmailValue = %q", got)
	}
}

func TestAuthFlowHelpersErrorMapping(t *testing.T) {
	s := &Server{}

	if got := s.mapPasswordError(nil); got != nil {
		t.Fatalf("mapPasswordError(nil) = %v", got)
	}
	if got := s.mapPasswordError(passwordservice.ErrPasswordTooShort); got == nil {
		t.Fatal("expected mapPasswordError to map password policy errors")
	}
	if got := s.mapPasswordError(passwordservice.ErrInvalidCredentials); got == nil {
		t.Fatal("expected mapPasswordError to map invalid credentials")
	}
	if got := s.mapPasswordError(passwordservice.ErrIdentityNotFound); got == nil {
		t.Fatal("expected mapPasswordError to map identity not found")
	}
	plainErr := errors.New("password boom")
	if got := s.mapPasswordError(plainErr); !errors.Is(got, plainErr) {
		t.Fatalf("expected passthrough error, got %v", got)
	}

	if got := s.mapMagicLinkError(nil); got != nil {
		t.Fatalf("mapMagicLinkError(nil) = %v", got)
	}
	if got := s.mapMagicLinkError(magiclinkservice.ErrInvalidCode); got == nil {
		t.Fatal("expected invalid code to be mapped")
	}
	if got := s.mapMagicLinkError(magiclinkservice.ErrRateLimited); got == nil {
		t.Fatal("expected rate limited to be mapped")
	}
	if got := s.mapMagicLinkError(magiclinkservice.ErrRecipientEmpty); got == nil {
		t.Fatal("expected recipient empty to be mapped")
	}
	if got := s.mapMagicLinkError(plainErr); !errors.Is(got, plainErr) {
		t.Fatalf("expected passthrough error, got %v", got)
	}

	rec := httptest.NewRecorder()
	s.writeFlowExecutionError(rec, &flowHTTPError{Status: http.StatusBadRequest, Message: "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mapped flow error, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.writeFlowExecutionError(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for generic flow error, got %d", rec.Code)
	}
}

func TestAuthFlowHelpersParseSubmission(t *testing.T) {
	flowID := uuid.New()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/self-service/login?flow="+flowID.String(), nil)
	req.Header.Set("X-CSRF-Token", "csrf-header")
	input, err := parseFlowSubmitRequest(rec, req)
	if err != nil {
		t.Fatalf("parseFlowSubmitRequest query fallback returned error: %v", err)
	}
	if input.FlowID != flowID || input.CSRFToken != "csrf-header" {
		t.Fatalf("unexpected parsed flow input: %#v", input)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/self-service/login", strings.NewReader(`{"flow_id":"`+flowID.String()+`","csrf_token":"csrf-json","identifier":"user@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	input, err = parseFlowSubmitRequest(rec, req)
	if err != nil {
		t.Fatalf("parseFlowSubmitRequest json returned error: %v", err)
	}
	if input.Values["identifier"] != "user@example.com" {
		t.Fatalf("expected identifier in parsed values, got %#v", input.Values)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/self-service/login", nil)
	if _, err := parseFlowSubmitRequest(rec, req); err == nil {
		t.Fatal("expected missing flow id error")
	}
}

func TestAuthFlowHelpersRuntimeAndIdentityUtilities(t *testing.T) {
	hasher := runtimePasswordHasher{}
	hash, err := hasher.Hash("Passw0rd!WithLength")
	if err != nil {
		t.Fatalf("runtimePasswordHasher.Hash returned error: %v", err)
	}
	if strings.TrimSpace(hash) == "" {
		t.Fatal("runtimePasswordHasher.Hash returned empty hash")
	}

	match, err := hasher.Verify("Passw0rd!WithLength", hash)
	if err != nil || !match {
		t.Fatalf("runtimePasswordHasher.Verify should accept correct password, got match=%v err=%v", match, err)
	}
	match, err = hasher.Verify("wrong-password", hash)
	if err != nil || match {
		t.Fatalf("runtimePasswordHasher.Verify should reject wrong password, got match=%v err=%v", match, err)
	}

	if err := (magicLinkCourierAdapter{}).SendMagicLinkEmail(context.Background(), "person@example.com", "https://app.example.com", "123456"); err != nil {
		t.Fatalf("magicLinkCourierAdapter with nil courier should be no-op, got %v", err)
	}

	if got := publicBaseURL(nil); got != "http://localhost:8080" {
		t.Fatalf("publicBaseURL(nil) = %q", got)
	}
	cfg := &platformconfig.Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 9443
	cfg.Server.TLS.Enabled = true
	if got := publicBaseURL(cfg); got != "https://localhost:9443" {
		t.Fatalf("publicBaseURL(tls) = %q", got)
	}

	identityID := uuid.New()
	s := &Server{
		db: &database.DB{},
		dbQueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*string)) = "  Person@Example.COM "
				return nil
			}}
		},
	}
	email, err := s.primaryEmailByIdentity(context.Background(), identityID)
	if err != nil {
		t.Fatalf("primaryEmailByIdentity returned error: %v", err)
	}
	if email != "person@example.com" {
		t.Fatalf("primaryEmailByIdentity normalized email = %q", email)
	}

	s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return adminTestRow{scanFn: func(dest ...any) error { return errors.New("query failed") }}
	}
	if _, err := s.primaryEmailByIdentity(context.Background(), identityID); err == nil {
		t.Fatal("expected primaryEmailByIdentity error for failed query")
	}

	deleted := false
	s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		deleted = true
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := s.deleteIdentity(context.Background(), identityID); err != nil {
		t.Fatalf("deleteIdentity returned error: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleteIdentity to execute update query")
	}

	if err := (&Server{}).deleteIdentity(context.Background(), identityID); err != nil {
		t.Fatalf("deleteIdentity without db access should no-op, got %v", err)
	}
}
