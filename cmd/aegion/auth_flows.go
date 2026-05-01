package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aegion/aegion/core/courier"
	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/registry"
	coresession "github.com/aegion/aegion/core/session"
	platformconfig "github.com/aegion/aegion/internal/platform/config"
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/trustedproxy"
	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
	mfaservice "github.com/aegion/aegion/modules/mfa/service"
	passkeysservice "github.com/aegion/aegion/modules/passkeys/service"
	passwordservice "github.com/aegion/aegion/modules/password/service"
)

type runtimePasswordHasher struct{}

func (runtimePasswordHasher) Hash(password string) (string, error) {
	return platformcrypto.HashPassword(password)
}

func (runtimePasswordHasher) Verify(password, hash string) (bool, error) {
	return platformcrypto.VerifyPassword(password, hash)
}

type magicLinkCourierAdapter struct {
	courier *courier.Courier
}

func (a magicLinkCourierAdapter) SendMagicLinkEmail(ctx context.Context, to string, link string, code string) error {
	if a.courier == nil {
		return nil
	}
	_, err := a.courier.SendMagicLinkEmail(ctx, to, link, code)
	return err
}

type flowSubmitInput struct {
	FlowID    uuid.UUID
	CSRFToken string
	Values    map[string]string
}

type flowExecutionResult struct {
	Status         string
	Message        string
	KeepFlowActive bool
	FlowPayload    interface{}
	AuthContext    map[string]string
}

type externalLoginResolution struct {
	IdentityID uuid.UUID
	Identifier string
	Method     coresession.AuthMethod
}

type socialCallbackResponse struct {
	IdentityID string `json:"identity_id"`
	Profile    struct {
		Email string `json:"email"`
	} `json:"profile"`
}

type ssoCallbackResponse struct {
	Email        string `json:"email"`
	JITProvision bool   `json:"jit_provision"`
}

const (
	maxExternalCallbackBodyBytes int64         = 1 << 20
	externalCallbackTimeout      time.Duration = 5 * time.Second
)

type flowHTTPError struct {
	Status  int
	Message string
}

func (e *flowHTTPError) Error() string {
	if e == nil {
		return "flow execution failed"
	}
	return e.Message
}

func (s *Server) selfServiceAuthEnabled() bool {
	return s != nil && (s.passwordAuth != nil || s.magicLinkAuth != nil || s.passkeyAuth != nil)
}

func publicBaseURL(cfg *platformconfig.Config) string {
	if cfg == nil {
		return "http://localhost:8080"
	}

	host := strings.TrimSpace(cfg.Server.Host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}

	scheme := "http"
	if cfg.Server.TLS.Enabled || cfg.Server.CORS.AllowCredentials && cfg.Sessions.Cookie.Secure {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, host, cfg.Server.Port)
}

func passwordHIBPBaseURL(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/") + "/range/"
	}
	return "https://" + strings.TrimRight(host, "/") + "/range/"
}

func parseFlowSubmitRequest(w http.ResponseWriter, r *http.Request) (flowSubmitInput, error) {
	var payload flowSubmitPayload
	values := map[string]string{}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))

	if contentType == "application/json" {
		if err := decodeJSONBody(w, r, &values); err != nil {
			return flowSubmitInput{}, err
		}
		payload.FlowID = values["flow_id"]
		payload.Flow = values["flow"]
		payload.ID = values["id"]
		payload.CSRFToken = values["csrf_token"]
	} else {
		if err := r.ParseForm(); err != nil {
			return flowSubmitInput{}, err
		}
		for key, entries := range r.Form {
			if len(entries) == 0 {
				continue
			}
			values[key] = strings.TrimSpace(entries[len(entries)-1])
		}
		payload.FlowID = values["flow_id"]
		payload.Flow = values["flow"]
		payload.ID = values["id"]
		payload.CSRFToken = values["csrf_token"]
	}

	flowValue := strings.TrimSpace(payload.FlowID)
	if flowValue == "" {
		flowValue = strings.TrimSpace(payload.Flow)
	}
	if flowValue == "" {
		flowValue = strings.TrimSpace(payload.ID)
	}
	if flowValue == "" {
		flowValue = strings.TrimSpace(r.URL.Query().Get("flow"))
	}
	if flowValue == "" {
		flowValue = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if flowValue == "" {
		return flowSubmitInput{}, errors.New("missing flow id")
	}

	flowID, err := uuid.Parse(flowValue)
	if err != nil {
		return flowSubmitInput{}, err
	}

	csrfToken := strings.TrimSpace(payload.CSRFToken)
	if csrfToken == "" {
		csrfToken = strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	}
	if csrfToken == "" {
		return flowSubmitInput{}, errors.New("missing csrf token")
	}

	return flowSubmitInput{
		FlowID:    flowID,
		CSRFToken: csrfToken,
		Values:    values,
	}, nil
}

func parseFlowSubmitPayload(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, error) {
	input, err := parseFlowSubmitRequest(w, r)
	if err != nil {
		return uuid.Nil, "", err
	}
	return input.FlowID, input.CSRFToken, nil
}

func (s *Server) executeFlowSubmission(ctx context.Context, w http.ResponseWriter, r *http.Request, flow *flows.Flow, input flowSubmitInput) (*flowExecutionResult, error) {
	switch flow.Type {
	case flows.TypeLogin:
		return s.executeLoginFlow(ctx, w, r, flow, input.Values)
	case flows.TypeRegistration:
		return s.executeRegistrationFlow(ctx, w, r, flow, input.Values)
	case flows.TypeRecovery:
		return s.executeRecoveryFlow(ctx, flow, input.Values)
	case flows.TypeSettings:
		return s.executeSettingsFlow(ctx, flow, input.Values)
	case flows.TypeVerification:
		return s.executeVerificationFlow(ctx, flow, input.Values)
	default:
		return nil, nil
	}
}

func (s *Server) executeLoginFlow(ctx context.Context, w http.ResponseWriter, r *http.Request, flow *flows.Flow, values map[string]string) (*flowExecutionResult, error) {
	if pendingIdentityID, primaryMethod, ok := s.pendingMFALogin(flow); ok {
		return s.completePendingMFALogin(ctx, w, r, flow, values, pendingIdentityID, primaryMethod)
	}

	identifier := normalizedEmailValue(values, "identifier", "email")
	password := strings.TrimSpace(values["password"])

	if password != "" {
		if s.passwordAuth == nil {
			return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "password authentication is not enabled"}
		}
		if identifier == "" {
			return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "identifier is required"}
		}

		identityID, err := s.passwordAuth.Verify(ctx, identifier, password)
		if err != nil {
			switch {
			case errors.Is(err, passwordservice.ErrInvalidCredentials):
				return nil, &flowHTTPError{Status: http.StatusUnauthorized, Message: "invalid credentials"}
			default:
				return nil, err
			}
		}

		return s.finishPrimaryAuthentication(ctx, w, r, flow, identityID, identifier, coresession.AuthMethodPassword)
	}

	if s.magicLinkAuth == nil {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "password is required"}
	}
	if identifier == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "identifier is required"}
	}
	if err := s.magicLinkAuth.SendLoginCode(ctx, identifier); err != nil {
		return nil, s.mapMagicLinkError(err)
	}
	return &flowExecutionResult{Status: "challenge_sent", Message: "If an account exists, a sign-in link has been sent."}, nil
}

func (s *Server) executeRegistrationFlow(ctx context.Context, w http.ResponseWriter, r *http.Request, _ *flows.Flow, values map[string]string) (*flowExecutionResult, error) {
	if s.passwordAuth == nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "password registration is not enabled"}
	}

	email := normalizedEmailValue(values, "email", "identifier")
	password := strings.TrimSpace(values["password"])
	confirm := normalizedFlowValue(values, "password_confirm", "password_confirmation")

	if email == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "email is required"}
	}
	if password == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "password is required"}
	}
	if confirm != "" && password != confirm {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "password confirmation does not match"}
	}

	existingIdentityID, err := s.lookupIdentityByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existingIdentityID != nil {
		return nil, &flowHTTPError{Status: http.StatusConflict, Message: "an account with this email already exists"}
	}

	if err := s.passwordAuth.ValidatePassword(ctx, password, email); err != nil {
		return nil, s.mapPasswordError(err)
	}

	identityID, err := s.createIdentity(ctx, email)
	if err != nil {
		return nil, err
	}

	if err := s.passwordAuth.Register(ctx, identityID, email, password); err != nil {
		_ = s.deleteIdentity(ctx, identityID)
		return nil, s.mapPasswordError(err)
	}

	if _, err := s.createSession(ctx, w, r, identityID, coresession.AuthMethodPassword); err != nil {
		return nil, err
	}

	message := "registration successful"
	if s.magicLinkAuth != nil {
		if err := s.magicLinkAuth.SendVerificationCode(ctx, email, identityID); err == nil {
			message = "registration successful, verification email sent"
		}
	}

	return &flowExecutionResult{Status: "registered", Message: message}, nil
}

func (s *Server) executeRecoveryFlow(ctx context.Context, _ *flows.Flow, values map[string]string) (*flowExecutionResult, error) {
	if s.magicLinkAuth == nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "recovery is not enabled"}
	}

	email := normalizedEmailValue(values, "email", "identifier")
	if email == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "email is required"}
	}

	identityID, err := s.lookupIdentityByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	if err := s.magicLinkAuth.SendRecoveryCodeIfIdentityExists(ctx, email, identityID); err != nil {
		return nil, s.mapMagicLinkError(err)
	}

	return &flowExecutionResult{Status: "challenge_sent", Message: "If an account exists, a recovery link has been sent."}, nil
}

func (s *Server) executeSettingsFlow(ctx context.Context, flow *flows.Flow, values map[string]string) (*flowExecutionResult, error) {
	if s.passwordAuth == nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "password settings are not enabled"}
	}
	if flow == nil || flow.IdentityID == nil {
		return nil, &flowHTTPError{Status: http.StatusUnauthorized, Message: "active identity context is required"}
	}

	newPassword := strings.TrimSpace(values["new_password"])
	confirm := normalizedFlowValue(values, "new_password_confirm", "password_confirm", "password_confirmation")
	if newPassword == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "new password is required"}
	}
	if confirm != "" && newPassword != confirm {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "password confirmation does not match"}
	}

	if recoveryVerified, _ := flow.GetContext("recovery_verified"); recoveryVerified == true {
		if err := s.passwordAuth.ResetPassword(ctx, *flow.IdentityID, newPassword); err != nil {
			return nil, s.mapPasswordError(err)
		}
		if s.sessionManager != nil {
			if err := s.sessionManager.RevokeAllForIdentity(ctx, *flow.IdentityID); err != nil {
				return nil, err
			}
		}
		return &flowExecutionResult{Status: "updated", Message: "password reset complete"}, nil
	}

	currentPassword := strings.TrimSpace(values["current_password"])
	if currentPassword == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "current password is required"}
	}

	if err := s.passwordAuth.ChangePassword(ctx, *flow.IdentityID, currentPassword, newPassword); err != nil {
		return nil, s.mapPasswordError(err)
	}

	return &flowExecutionResult{Status: "updated", Message: "password updated"}, nil
}

func (s *Server) executeVerificationFlow(ctx context.Context, flow *flows.Flow, values map[string]string) (*flowExecutionResult, error) {
	if s.magicLinkAuth == nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "verification is not enabled"}
	}

	email := normalizedEmailValue(values, "email", "identifier")
	if email == "" && flow != nil && flow.IdentityID != nil {
		var err error
		email, err = s.primaryEmailByIdentity(ctx, *flow.IdentityID)
		if err != nil {
			return nil, err
		}
	}
	if email == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "email is required"}
	}

	code := strings.TrimSpace(values["code"])
	if code == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "verification code is required"}
	}

	identityID, err := s.magicLinkAuth.VerifyVerificationCode(ctx, email, code)
	if err != nil {
		return nil, s.mapMagicLinkError(err)
	}
	if identityID == nil {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "verification code is invalid"}
	}
	if flow != nil && flow.IdentityID != nil && *flow.IdentityID != *identityID {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "verification code does not match the active identity"}
	}

	if err := s.markEmailVerified(ctx, *identityID, email); err != nil {
		return nil, err
	}

	return &flowExecutionResult{Status: "verified", Message: "verification complete"}, nil
}

func (s *Server) createSession(ctx context.Context, w http.ResponseWriter, r *http.Request, identityID uuid.UUID, method coresession.AuthMethod) (*coresession.Session, error) {
	if s == nil || s.sessionManager == nil {
		return nil, errors.New("session manager unavailable")
	}
	session, err := s.sessionManager.Create(ctx, identityID, method, coresession.DeviceInfo{
		UserAgent: r.UserAgent(),
		IPAddress: extractRequestIP(r),
	})
	if err != nil {
		return nil, err
	}
	if session != nil {
		s.sessionManager.SetCookie(w, session)
	}
	return session, nil
}

func (s *Server) finishPrimaryAuthentication(ctx context.Context, w http.ResponseWriter, r *http.Request, flow *flows.Flow, identityID uuid.UUID, identifier string, method coresession.AuthMethod) (*flowExecutionResult, error) {
	trustedDeviceSatisfied, pendingFlow, err := s.ensureSecondFactorOrTrustedDevice(ctx, r, flow, identityID, identifier, method)
	if err != nil {
		return nil, err
	}
	if pendingFlow != nil {
		return &flowExecutionResult{
			Status:         "mfa_required",
			Message:        "multi-factor authentication required",
			KeepFlowActive: true,
			FlowPayload:    pendingFlow,
		}, nil
	}

	currentSession, err := s.createSession(ctx, w, r, identityID, method)
	if err != nil {
		return nil, err
	}
	if trustedDeviceSatisfied && currentSession != nil {
		if err := s.sessionManager.AddAuthMethod(ctx, currentSession.ID, coresession.AuthMethodTOTP); err != nil {
			return nil, err
		}
		applySessionAuthMethod(currentSession, coresession.AuthMethodTOTP)
	}
	authContext := authCompletionContext(currentSession, trustedDeviceSatisfied)
	if err := s.applyAuthContextToFlow(ctx, flow, currentSession, authContext); err != nil {
		return nil, err
	}

	return &flowExecutionResult{Status: "authenticated", Message: "login successful", AuthContext: authContext}, nil
}

func (s *Server) ensureSecondFactorOrTrustedDevice(ctx context.Context, r *http.Request, flow *flows.Flow, identityID uuid.UUID, identifier string, primaryMethod coresession.AuthMethod) (bool, *flows.Flow, error) {
	if s.mfaAuth == nil {
		return false, nil, nil
	}

	hasEnrolledFactor, err := s.mfaAuth.HasEnrolledFactor(ctx, identityID.String())
	if err != nil {
		return false, nil, err
	}
	if !hasEnrolledFactor {
		return false, nil, nil
	}

	if token := s.readMFATrustedDeviceCookie(r); token != "" {
		valid, err := s.mfaAuth.ValidateTrustedDevice(ctx, identityID.String(), token)
		if err != nil {
			return false, nil, err
		}
		if valid {
			return true, nil, nil
		}
	}

	if flow == nil {
		createdFlow, err := s.flowService.CreateLoginFlow(ctx, r.URL.String())
		if err != nil {
			return false, nil, err
		}
		flow = createdFlow
	}

	if err := s.prepareMFALoginFlow(ctx, flow, identityID, identifier, primaryMethod); err != nil {
		return false, nil, err
	}
	return false, flow, nil
}

func (s *Server) completePendingMFALogin(ctx context.Context, w http.ResponseWriter, r *http.Request, flow *flows.Flow, values map[string]string, identityID uuid.UUID, primaryMethod coresession.AuthMethod) (*flowExecutionResult, error) {
	if s.mfaAuth == nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "multi-factor authentication is not enabled"}
	}

	totpCode := normalizedFlowValue(values, "totp_code", "code")
	backupCode := normalizedFlowValue(values, "backup_code")
	rememberDevice := strings.EqualFold(normalizedFlowValue(values, "remember_device"), "true") ||
		strings.EqualFold(normalizedFlowValue(values, "remember_device"), "on") ||
		normalizedFlowValue(values, "remember_device") == "1"

	if totpCode == "" && backupCode == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "totp_code or backup_code is required"}
	}

	var secondFactorMethod coresession.AuthMethod
	switch {
	case totpCode != "":
		if err := s.mfaAuth.VerifyTOTP(ctx, identityID.String(), totpCode); err != nil {
			return nil, &flowHTTPError{Status: http.StatusUnauthorized, Message: "invalid totp code"}
		}
		secondFactorMethod = coresession.AuthMethodTOTP
	case backupCode != "":
		if err := s.mfaAuth.VerifyBackupCode(ctx, identityID.String(), backupCode); err != nil {
			return nil, &flowHTTPError{Status: http.StatusUnauthorized, Message: "invalid backup code"}
		}
		secondFactorMethod = coresession.AuthMethodBackup
	}

	currentSession, err := s.createSession(ctx, w, r, identityID, primaryMethod)
	if err != nil {
		return nil, err
	}
	if currentSession != nil {
		if err := s.sessionManager.AddAuthMethod(ctx, currentSession.ID, secondFactorMethod); err != nil {
			return nil, err
		}
		applySessionAuthMethod(currentSession, secondFactorMethod)
	}

	if rememberDevice {
		token, expiresAt, err := s.mfaAuth.RememberTrustedDevice(ctx, identityID.String(), r.UserAgent())
		if err != nil {
			return nil, err
		}
		s.writeMFATrustedDeviceCookie(w, token, expiresAt)
	}

	authContext := authCompletionContext(currentSession, false)
	clearPendingMFALogin(flow)
	if err := s.applyAuthContextToFlow(ctx, flow, currentSession, authContext); err != nil {
		return nil, err
	}

	return &flowExecutionResult{Status: "authenticated", Message: "login successful", AuthContext: authContext}, nil
}

func (s *Server) prepareMFALoginFlow(ctx context.Context, flow *flows.Flow, identityID uuid.UUID, identifier string, primaryMethod coresession.AuthMethod) error {
	if flow == nil {
		return errors.New("flow is required")
	}
	flow.AddContext("pending_login_identity_id", identityID.String())
	flow.AddContext("pending_login_method", string(primaryMethod))
	flow.AddContext("pending_login_identifier", strings.TrimSpace(identifier))
	flow.UI = &flows.UIState{
		Action: flow.RequestURL,
		Method: "POST",
		Nodes:  generateMFALoginFormNodes(flow.CSRFToken),
		Messages: []flows.Msg{
			{
				ID:   "login.mfa_required",
				Type: flows.MsgTypeInfo,
				Text: "Enter an authenticator code or a backup code to finish signing in.",
			},
		},
	}
	return s.flowService.UpdateFlow(ctx, flow)
}

func pendingMFALoginIdentity(flow *flows.Flow) (*uuid.UUID, bool) {
	if flow == nil {
		return nil, false
	}
	raw, ok := flow.GetContext("pending_login_identity_id")
	if !ok {
		return nil, false
	}
	text, ok := raw.(string)
	if !ok {
		return nil, false
	}
	parsed, err := uuid.Parse(strings.TrimSpace(text))
	if err != nil {
		return nil, false
	}
	return &parsed, true
}

func pendingMFALoginMethod(flow *flows.Flow) (coresession.AuthMethod, bool) {
	if flow == nil {
		return "", false
	}
	raw, ok := flow.GetContext("pending_login_method")
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return coresession.AuthMethod(strings.TrimSpace(text)), true
}

func (s *Server) pendingMFALogin(flow *flows.Flow) (uuid.UUID, coresession.AuthMethod, bool) {
	identityID, ok := pendingMFALoginIdentity(flow)
	if !ok || identityID == nil {
		return uuid.Nil, "", false
	}
	method, ok := pendingMFALoginMethod(flow)
	if !ok {
		return uuid.Nil, "", false
	}
	return *identityID, method, true
}

func clearPendingMFALogin(flow *flows.Flow) {
	if flow == nil {
		return
	}
	delete(flow.Context, "pending_login_identity_id")
	delete(flow.Context, "pending_login_method")
	delete(flow.Context, "pending_login_identifier")
}

func generateMFALoginFormNodes(csrfToken string) []flows.Node {
	nodes := []flows.Node{
		flows.NewHiddenNode("csrf_token", csrfToken),
		flows.WithPattern(flows.NewInputNode("totp_code", flows.InputTypeText, "Authenticator Code", false), "^[0-9]{6}$"),
		flows.NewInputNode("backup_code", flows.InputTypeText, "Backup Code", false),
		flows.NewInputNode("remember_device", flows.InputTypeCheckbox, "Remember this device", false),
		flows.NewSubmitNode("method", "Verify"),
	}
	return nodes
}

func (s *Server) readMFATrustedDeviceCookie(r *http.Request) string {
	if s == nil || s.cfg == nil {
		return ""
	}
	cookie, err := r.Cookie(strings.TrimSpace(s.cfg.MFA.TrustedDeviceCookieName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (s *Server) writeMFATrustedDeviceCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	if s == nil || s.cfg == nil || strings.TrimSpace(token) == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     strings.TrimSpace(s.cfg.MFA.TrustedDeviceCookieName),
		Value:    strings.TrimSpace(token),
		Path:     s.cfg.Sessions.Cookie.Path,
		Domain:   s.cfg.Sessions.Cookie.Domain,
		SameSite: parseSameSite(s.cfg.Sessions.Cookie.SameSite),
		Secure:   s.cfg.Sessions.Cookie.Secure,
		HttpOnly: true,
		Expires:  expiresAt.UTC(),
	})
}

func (s *Server) lookupIdentityByEmail(ctx context.Context, email string) (*uuid.UUID, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !s.hasDatabaseAccess() {
		return nil, nil
	}

	var identityID uuid.UUID
	err := s.queryRow(ctx, `
		SELECT i.id
		FROM core_identities i
		JOIN core_identity_addresses a ON a.identity_id = i.id
		WHERE a.type = 'email'
		  AND LOWER(a.value) = LOWER($1)
		  AND i.deleted_at IS NULL
		ORDER BY a.verified DESC, a.is_primary DESC, a.created_at DESC
		LIMIT 1
	`, email).Scan(&identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &identityID, nil
}

func (s *Server) primaryEmailByIdentity(ctx context.Context, identityID uuid.UUID) (string, error) {
	var email string
	err := s.queryRow(ctx, `
		SELECT COALESCE(
			(
				SELECT value
				FROM core_identity_addresses
				WHERE identity_id = $1
				  AND type = 'email'
				ORDER BY is_primary DESC, verified DESC, created_at ASC
				LIMIT 1
			),
			''
		)
	`, identityID).Scan(&email)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(email)), nil
}

func (s *Server) createIdentity(ctx context.Context, email string) (uuid.UUID, error) {
	if !s.hasDatabaseAccess() {
		return uuid.Nil, errors.New("database unavailable")
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	schemaID, err := s.resolveAdminSchemaID(ctx, "")
	if err != nil {
		return uuid.Nil, err
	}

	identityID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO core_identities (id, schema_id, traits, state, created_at, updated_at)
		VALUES ($1, $2, jsonb_build_object('email', $3), 'active', NOW(), NOW())
	`, identityID, schemaID, email); err != nil {
		return uuid.Nil, err
	}
	if err := upsertPrimaryIdentityEmail(ctx, tx, identityID, email); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	return identityID, nil
}

func (s *Server) deleteIdentity(ctx context.Context, identityID uuid.UUID) error {
	if !s.hasDatabaseAccess() {
		return nil
	}
	_, err := s.exec(ctx, `
		UPDATE core_identities
		SET state = 'inactive', deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, identityID)
	return err
}

func (s *Server) markEmailVerified(ctx context.Context, identityID uuid.UUID, email string) error {
	_, err := s.exec(ctx, `
		UPDATE core_identity_addresses
		SET verified = TRUE,
		    verified_at = COALESCE(verified_at, NOW()),
		    updated_at = NOW()
		WHERE identity_id = $1
		  AND type = 'email'
		  AND LOWER(value) = LOWER($2)
	`, identityID, strings.ToLower(strings.TrimSpace(email)))
	return err
}

func (s *Server) handleMagicLinkLoginVerify(w http.ResponseWriter, r *http.Request) {
	s.handleMagicLinkVerify(w, r, magiclinkstore.CodeTypeLogin)
}

func (s *Server) handleCompleteExternalLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	input, err := parseFlowSubmitRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow submission payload", err)
		return
	}

	flow, err := s.flowService.ValidateFlow(r.Context(), input.FlowID, input.CSRFToken)
	if err != nil {
		s.writeFlowValidationError(w, err)
		return
	}
	if flow.Type != flows.TypeLogin {
		writeError(w, http.StatusBadRequest, "flow type mismatch for external login completion", nil)
		return
	}

	resolution, err := s.resolveExternalLogin(r.Context(), r, input.Values)
	if err != nil {
		s.writeFlowExecutionError(w, err)
		return
	}

	result, err := s.finishPrimaryAuthentication(r.Context(), w, r, flow, resolution.IdentityID, resolution.Identifier, resolution.Method)
	if err != nil {
		s.writeFlowExecutionError(w, err)
		return
	}

	if result != nil && result.KeepFlowActive {
		response := map[string]any{
			"status":    result.Status,
			"flow_id":   input.FlowID.String(),
			"flow_type": string(flows.TypeLogin),
		}
		if result.Message != "" {
			response["message"] = result.Message
		}
		if result.FlowPayload != nil {
			response["flow"] = result.FlowPayload
		}
		mergeAuthContext(response, result.AuthContext)
		writeJSON(w, http.StatusOK, response)
		return
	}

	if err := s.flowService.CompleteFlow(r.Context(), input.FlowID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete flow", err)
		return
	}

	response := map[string]any{
		"status":    "authenticated",
		"flow_id":   input.FlowID.String(),
		"flow_type": string(flows.TypeLogin),
		"message":   "login successful",
	}
	if result != nil {
		if result.Status != "" {
			response["status"] = result.Status
		}
		if result.Message != "" {
			response["message"] = result.Message
		}
		mergeAuthContext(response, result.AuthContext)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) resolveExternalLogin(ctx context.Context, r *http.Request, values map[string]string) (*externalLoginResolution, error) {
	if strings.TrimSpace(normalizedFlowValue(values, "identity_id")) != "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "identity_id is not accepted for external login completion"}
	}

	method, err := resolveExternalAuthMethod(values)
	if err != nil {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}

	switch method {
	case coresession.AuthMethodSocial:
		return s.resolveExternalSocialLogin(ctx, values)
	case coresession.AuthMethodSAML:
		return s.resolveExternalSAMLLogin(ctx, r, values)
	default:
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "unsupported external auth method"}
	}
}

func resolveExternalAuthMethod(values map[string]string) (coresession.AuthMethod, error) {
	if methodValue := normalizedFlowValue(values, "method", "auth_method"); methodValue != "" {
		return parseExternalAuthMethod(methodValue)
	}
	if normalizedFlowValue(values, "saml_response", "SAMLResponse", "relay_state", "RelayState", "connection", "sso_connection") != "" {
		return coresession.AuthMethodSAML, nil
	}
	return coresession.AuthMethodSocial, nil
}

func (s *Server) resolveExternalSocialLogin(ctx context.Context, values map[string]string) (*externalLoginResolution, error) {
	provider := normalizedFlowValue(values, "provider", "social_provider")
	state := normalizedFlowValue(values, "state")
	code := normalizedFlowValue(values, "code")
	if provider == "" || state == "" || code == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "provider, state, and code are required for social login completion"}
	}

	callbackURL, err := s.moduleCallbackURL("social", "/self-service/social/"+url.PathEscape(provider)+"/callback", map[string]string{
		"state": state,
		"code":  code,
	})
	if err != nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "social authentication is not available"}
	}

	var callback socialCallbackResponse
	status, err := s.fetchExternalCallback(ctx, callbackURL, nil, &callback)
	if err != nil {
		return nil, &flowHTTPError{Status: http.StatusBadGateway, Message: "failed to verify social login callback"}
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, mapExternalCallbackStatus(status)
	}

	identityID, err := uuid.Parse(strings.TrimSpace(callback.IdentityID))
	if err != nil {
		return nil, &flowHTTPError{Status: http.StatusBadGateway, Message: "social callback did not return a valid identity"}
	}
	return &externalLoginResolution{
		IdentityID: identityID,
		Identifier: strings.ToLower(strings.TrimSpace(callback.Profile.Email)),
		Method:     coresession.AuthMethodSocial,
	}, nil
}

func (s *Server) resolveExternalSAMLLogin(ctx context.Context, r *http.Request, values map[string]string) (*externalLoginResolution, error) {
	connection := normalizedFlowValue(values, "connection", "sso_connection", "provider")
	relayState := normalizedFlowValue(values, "relay_state", "RelayState", "state")
	samlResponse := normalizedFlowValue(values, "saml_response", "SAMLResponse")
	if connection == "" || relayState == "" || samlResponse == "" {
		return nil, &flowHTTPError{Status: http.StatusBadRequest, Message: "connection, relay_state, and saml_response are required for SSO login completion"}
	}

	callbackURL, err := s.moduleCallbackURL("sso", "/self-service/sso/"+url.PathEscape(connection)+"/callback", map[string]string{
		"RelayState":   relayState,
		"SAMLResponse": samlResponse,
	})
	if err != nil {
		return nil, &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "sso authentication is not available"}
	}

	headers := map[string]string{
		"X-Forwarded-Proto": externalForwardedProto(s, r),
		"X-Forwarded-Host":  externalForwardedHost(s, r),
	}
	var callback ssoCallbackResponse
	status, err := s.fetchExternalCallback(ctx, callbackURL, headers, &callback)
	if err != nil {
		return nil, &flowHTTPError{Status: http.StatusBadGateway, Message: "failed to verify sso login callback"}
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, mapExternalCallbackStatus(status)
	}

	email := strings.ToLower(strings.TrimSpace(callback.Email))
	if email == "" {
		return nil, &flowHTTPError{Status: http.StatusUnauthorized, Message: "sso callback did not provide an email address"}
	}

	identityID, err := s.lookupIdentityByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if identityID == nil {
		if !callback.JITProvision {
			return nil, &flowHTTPError{Status: http.StatusUnauthorized, Message: "sso account is not linked"}
		}
		createdIdentityID, createErr := s.createIdentity(ctx, email)
		if createErr != nil {
			return nil, createErr
		}
		identityID = &createdIdentityID
	}

	if err := s.markEmailVerified(ctx, *identityID, email); err != nil {
		return nil, err
	}

	return &externalLoginResolution{
		IdentityID: *identityID,
		Identifier: email,
		Method:     coresession.AuthMethodSAML,
	}, nil
}

func (s *Server) moduleCallbackURL(moduleID, modulePath string, query map[string]string) (*url.URL, error) {
	if s == nil || s.registry == nil {
		return nil, errors.New("module registry unavailable")
	}

	module, err := s.registry.GetModule(moduleID)
	if err != nil {
		return nil, err
	}
	if module.Status != registry.StatusHealthy && module.Status != registry.StatusStarting {
		return nil, errors.New("module is not healthy")
	}

	for _, endpoint := range module.Endpoints {
		if endpoint.Type != registry.EndpointHTTP {
			continue
		}
		parsed, parseErr := url.Parse(endpoint.URL)
		if parseErr != nil {
			return nil, parseErr
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("module endpoint must use http or https")
		}
		if strings.TrimSpace(parsed.Host) == "" {
			return nil, errors.New("module endpoint is missing host")
		}
		parsed.Path = modulePath
		values := parsed.Query()
		for key, value := range query {
			if strings.TrimSpace(value) == "" {
				continue
			}
			values.Set(key, value)
		}
		parsed.RawQuery = values.Encode()
		return parsed, nil
	}
	return nil, errors.New("module has no http endpoint")
}

func (s *Server) fetchExternalCallback(ctx context.Context, callbackURL *url.URL, headers map[string]string, out interface{}) (status int, err error) {
	requestCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		requestCtx, cancel = context.WithTimeout(ctx, externalCallbackTimeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, callbackURL.String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, strings.TrimSpace(value))
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices && out != nil {
		if !strings.Contains(strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))), "application/json") {
			return resp.StatusCode, errors.New("callback response must be application/json")
		}
		payload, err := io.ReadAll(io.LimitReader(resp.Body, maxExternalCallbackBodyBytes+1))
		if err != nil {
			return resp.StatusCode, err
		}
		if int64(len(payload)) > maxExternalCallbackBodyBytes {
			return resp.StatusCode, errors.New("callback response exceeds size limit")
		}
		if err := json.Unmarshal(payload, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func mapExternalCallbackStatus(status int) error {
	switch {
	case status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound:
		return &flowHTTPError{Status: http.StatusUnauthorized, Message: "invalid external authentication callback"}
	case status == http.StatusServiceUnavailable:
		return &flowHTTPError{Status: http.StatusServiceUnavailable, Message: "external authentication is currently unavailable"}
	case status >= http.StatusInternalServerError:
		return &flowHTTPError{Status: http.StatusBadGateway, Message: "external authentication upstream failed"}
	default:
		return &flowHTTPError{Status: http.StatusBadGateway, Message: "failed to verify external authentication callback"}
	}
}

func externalForwardedProto(s *Server, r *http.Request) string {
	trustForwarded := s != nil && s.cfg != nil && s.cfg.Proxy.TrustForwardedHeaders
	return strings.ToLower(strings.TrimSpace(trustedproxy.ForwardedProto(r, trustForwarded, "AEGION_TRUSTED_PROXY_CIDRS")))
}

func externalForwardedHost(s *Server, r *http.Request) string {
	trustForwarded := s != nil && s.cfg != nil && s.cfg.Proxy.TrustForwardedHeaders
	return strings.TrimSpace(trustedproxy.ForwardedHost(r, trustForwarded, "AEGION_TRUSTED_PROXY_CIDRS"))
}

func (s *Server) handleMagicLinkRecoveryVerify(w http.ResponseWriter, r *http.Request) {
	s.handleMagicLinkVerify(w, r, magiclinkstore.CodeTypeRecovery)
}

func (s *Server) handleMagicLinkVerificationVerify(w http.ResponseWriter, r *http.Request) {
	s.handleMagicLinkVerify(w, r, magiclinkstore.CodeTypeVerification)
}

func (s *Server) handleMagicLinkVerify(w http.ResponseWriter, r *http.Request, codeType magiclinkstore.CodeType) {
	if s.magicLinkAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "magic link authentication is not enabled", nil)
		return
	}

	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing token", nil)
		return
	}

	recipient, identityID, err := s.magicLinkAuth.VerifyMagicLinkForType(r.Context(), token, codeType)
	if err != nil {
		s.writeFlowExecutionError(w, s.mapMagicLinkError(err))
		return
	}

	switch codeType {
	case magiclinkstore.CodeTypeLogin:
		if identityID == nil {
			resolved, resolveErr := s.lookupIdentityByEmail(r.Context(), recipient)
			if resolveErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve login identity", resolveErr)
				return
			}
			identityID = resolved
		}
		if identityID == nil {
			writeError(w, http.StatusBadRequest, "login link is invalid", nil)
			return
		}
		result, err := s.finishPrimaryAuthentication(r.Context(), w, r, nil, *identityID, recipient, coresession.AuthMethodMagicLink)
		if err != nil {
			s.writeFlowExecutionError(w, err)
			return
		}
		payload := map[string]any{"status": "authenticated", "message": "login successful"}
		if result != nil {
			if result.Status != "" {
				payload["status"] = result.Status
			}
			if result.Message != "" {
				payload["message"] = result.Message
			}
			if result.FlowPayload != nil {
				payload["flow"] = result.FlowPayload
			}
			mergeAuthContext(payload, result.AuthContext)
		}
		s.respondMagicLinkVerification(w, r, payload)
	case magiclinkstore.CodeTypeRecovery:
		if identityID == nil {
			writeError(w, http.StatusBadRequest, "recovery link is invalid", nil)
			return
		}
		flow, createErr := s.flowService.CreateSettingsFlow(r.Context(), r.URL.String(), *identityID)
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create recovery settings flow", createErr)
			return
		}
		flow.AddContext("recovery_verified", true)
		if err := s.flowService.UpdateFlow(r.Context(), flow); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist recovery settings flow", err)
			return
		}
		if acceptsJSON(r) {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "recovery_verified",
				"message": "recovery link verified",
				"flow":    flow,
			})
			return
		}
		http.Redirect(w, r, "/ui/settings?flow="+flow.ID.String()+"&recovery=1", http.StatusSeeOther)
	case magiclinkstore.CodeTypeVerification:
		if identityID == nil {
			writeError(w, http.StatusBadRequest, "verification link is invalid", nil)
			return
		}
		if err := s.markEmailVerified(r.Context(), *identityID, recipient); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify email address", err)
			return
		}
		s.respondMagicLinkVerification(w, r, map[string]any{"status": "verified", "message": "verification complete"})
	default:
		writeError(w, http.StatusBadRequest, "unsupported magic link type", nil)
	}
}

func (s *Server) respondMagicLinkVerification(w http.ResponseWriter, r *http.Request, payload map[string]any) {
	if acceptsJSON(r) {
		writeJSON(w, http.StatusOK, payload)
		return
	}
	if flowPayload, ok := payload["flow"]; ok {
		if flow, ok := flowPayload.(*flows.Flow); ok && flow != nil {
			http.Redirect(w, r, "/ui/login?flow="+flow.ID.String()+"&mfa=1", http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleStartSettingsTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.mfaAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "multi-factor authentication is not enabled", nil)
		return
	}
	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "active session required", err)
		return
	}
	accountName, err := s.primaryEmailByIdentity(r.Context(), currentSession.IdentityID)
	if err != nil || strings.TrimSpace(accountName) == "" {
		accountName = currentSession.IdentityID.String()
	}
	resp, err := s.mfaAuth.StartTOTPEnrollment(r.Context(), currentSession.IdentityID.String(), accountName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to start totp enrollment", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFinishSettingsTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.mfaAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "multi-factor authentication is not enabled", nil)
		return
	}
	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "active session required", err)
		return
	}
	var req mfaservice.TOTPEnrollmentFinishRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	req.IdentityID = currentSession.IdentityID.String()
	resp, err := s.mfaAuth.CompleteTOTPEnrollment(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to finish totp enrollment", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRegenerateSettingsBackupCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.mfaAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "multi-factor authentication is not enabled", nil)
		return
	}
	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "active session required", err)
		return
	}
	codes, err := s.mfaAuth.RegenerateBackupCodes(r.Context(), currentSession.IdentityID.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to regenerate backup codes", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "regenerated",
		"backup_codes": codes,
	})
}

func (s *Server) handleStartLoginPasskey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.passkeyAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "passkeys are not enabled", nil)
		return
	}
	var req struct {
		FlowID     string `json:"flow_id"`
		CSRFToken  string `json:"csrf_token"`
		Identifier string `json:"identifier"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	flowID, err := uuid.Parse(strings.TrimSpace(req.FlowID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}
	if _, err := s.flowService.ValidateFlow(r.Context(), flowID, strings.TrimSpace(req.CSRFToken)); err != nil {
		s.writeFlowValidationError(w, err)
		return
	}
	identityID, err := s.lookupIdentityByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Identifier)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve passkey identity", err)
		return
	}
	if identityID == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"challenge":              "",
			"allowed_credential_ids": []string{},
			"expires_in":             0,
		})
		return
	}
	resp, err := s.passkeyAuth.BeginAuthentication(identityID.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to start passkey authentication", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFinishLoginPasskey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.passkeyAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "passkeys are not enabled", nil)
		return
	}
	var req struct {
		FlowID       string `json:"flow_id"`
		CSRFToken    string `json:"csrf_token"`
		Identifier   string `json:"identifier"`
		Challenge    string `json:"challenge"`
		CredentialID string `json:"credential_id"`
		Signature    string `json:"signature"`
		SignCount    uint32 `json:"sign_count"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	flowID, err := uuid.Parse(strings.TrimSpace(req.FlowID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid flow id", err)
		return
	}
	flow, err := s.flowService.ValidateFlow(r.Context(), flowID, strings.TrimSpace(req.CSRFToken))
	if err != nil {
		s.writeFlowValidationError(w, err)
		return
	}
	identityID, err := s.lookupIdentityByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Identifier)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve passkey identity", err)
		return
	}
	if identityID == nil {
		writeError(w, http.StatusUnauthorized, "invalid passkey assertion", nil)
		return
	}
	if err := s.passkeyAuth.FinishAuthentication(&passkeysservice.AuthenticationFinishRequest{
		IdentityID:   identityID.String(),
		Challenge:    req.Challenge,
		CredentialID: req.CredentialID,
		Signature:    req.Signature,
		SignCount:    req.SignCount,
	}); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid passkey assertion", err)
		return
	}
	result, err := s.finishPrimaryAuthentication(r.Context(), w, r, flow, *identityID, strings.ToLower(strings.TrimSpace(req.Identifier)), coresession.AuthMethodPasskey)
	if err != nil {
		s.writeFlowExecutionError(w, err)
		return
	}
	if result != nil && result.KeepFlowActive {
		response := map[string]any{
			"status":  result.Status,
			"message": result.Message,
			"flow":    result.FlowPayload,
		}
		mergeAuthContext(response, result.AuthContext)
		writeJSON(w, http.StatusOK, response)
		return
	}
	if err := s.flowService.CompleteFlow(r.Context(), flow.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete flow", err)
		return
	}
	response := map[string]any{
		"status":  "authenticated",
		"message": "login successful",
	}
	if result != nil {
		if result.Status != "" {
			response["status"] = result.Status
		}
		if result.Message != "" {
			response["message"] = result.Message
		}
		mergeAuthContext(response, result.AuthContext)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStartSettingsPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.passkeyAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "passkeys are not enabled", nil)
		return
	}
	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "active session required", err)
		return
	}
	resp, err := s.passkeyAuth.BeginRegistration(currentSession.IdentityID.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to start passkey registration", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFinishSettingsPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if s.passkeyAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "passkeys are not enabled", nil)
		return
	}
	currentSession, err := s.sessionManager.GetFromRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "active session required", err)
		return
	}
	var req passkeysservice.RegistrationFinishRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	req.IdentityID = currentSession.IdentityID.String()
	if err := s.passkeyAuth.FinishRegistration(&req); err != nil {
		writeError(w, http.StatusBadRequest, "failed to finish passkey registration", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "registered",
	})
}

func authCompletionContext(currentSession *coresession.Session, trustedDevice bool) map[string]string {
	if currentSession == nil {
		return nil
	}

	authTime := currentSession.AuthenticatedAt.UTC()
	if authTime.IsZero() {
		authTime = time.Now().UTC()
	}

	return map[string]string{
		"auth_time":       authTime.Format(time.RFC3339),
		"acr":             string(currentSession.AAL),
		"aal":             string(currentSession.AAL),
		"amr":             strings.Join(sessionAMRValues(currentSession.AuthMethods), " "),
		"sid":             currentSession.ID.String(),
		"trusted_device":  strconv.FormatBool(trustedDevice),
		"reauth_required": "false",
	}
}

func sessionAMRValues(methods []coresession.SessionAuthMethod) []string {
	if len(methods) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(methods))
	amr := make([]string, 0, len(methods))
	for _, method := range methods {
		mapped := authMethodToAMR(method.Method)
		if mapped == "" {
			continue
		}
		if _, ok := seen[mapped]; ok {
			continue
		}
		seen[mapped] = struct{}{}
		amr = append(amr, mapped)
	}
	sort.Strings(amr)
	return amr
}

func authMethodToAMR(method coresession.AuthMethod) string {
	switch method {
	case coresession.AuthMethodPassword:
		return "pwd"
	case coresession.AuthMethodTOTP, coresession.AuthMethodMagicLink, coresession.AuthMethodSMS, coresession.AuthMethodBackup:
		return "otp"
	case coresession.AuthMethodWebAuthn, coresession.AuthMethodPasskey:
		return "hwk"
	case coresession.AuthMethodSocial, coresession.AuthMethodSAML:
		return "federated"
	default:
		return ""
	}
}

func parseExternalAuthMethod(raw string) (coresession.AuthMethod, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "social":
		return coresession.AuthMethodSocial, nil
	case "saml", "sso":
		return coresession.AuthMethodSAML, nil
	default:
		return "", errors.New("unsupported external auth method")
	}
}

func applySessionAuthMethod(currentSession *coresession.Session, method coresession.AuthMethod) {
	if currentSession == nil {
		return
	}

	for _, authMethod := range currentSession.AuthMethods {
		if authMethod.Method == method {
			return
		}
	}

	now := time.Now().UTC()
	currentSession.AuthMethods = append(currentSession.AuthMethods, coresession.SessionAuthMethod{
		Method:      method,
		CompletedAt: now,
	})
	if method == coresession.AuthMethodTOTP || method == coresession.AuthMethodWebAuthn || method == coresession.AuthMethodSMS || method == coresession.AuthMethodBackup {
		currentSession.AAL = coresession.AAL2
		currentSession.AuthenticatedAt = now
	}
}

func (s *Server) applyAuthContextToFlow(ctx context.Context, flow *flows.Flow, currentSession *coresession.Session, authContext map[string]string) error {
	if s == nil || s.flowService == nil || flow == nil || currentSession == nil {
		return nil
	}

	flow.SetIdentity(currentSession.IdentityID)
	flow.SetSession(currentSession.ID)
	for key, value := range authContext {
		flow.AddContext(key, value)
	}
	flow.AddContext("amr_values", sessionAMRValues(currentSession.AuthMethods))
	return s.flowService.UpdateFlow(ctx, flow)
}

func mergeAuthContext(payload map[string]any, authContext map[string]string) {
	if len(authContext) == 0 || payload == nil {
		return
	}
	for key, value := range authContext {
		if strings.TrimSpace(value) == "" {
			continue
		}
		payload[key] = value
	}
}

func acceptsJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept"))), "application/json")
}

func normalizedFlowValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if values == nil {
			break
		}
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func normalizedEmailValue(values map[string]string, keys ...string) string {
	return strings.ToLower(normalizedFlowValue(values, keys...))
}

func (s *Server) mapPasswordError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, passwordservice.ErrPasswordTooShort),
		errors.Is(err, passwordservice.ErrPasswordTooWeak),
		errors.Is(err, passwordservice.ErrPasswordBreached),
		errors.Is(err, passwordservice.ErrPasswordReused),
		errors.Is(err, passwordservice.ErrPasswordSimilar):
		return &flowHTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	case errors.Is(err, passwordservice.ErrInvalidCredentials):
		return &flowHTTPError{Status: http.StatusUnauthorized, Message: "invalid credentials"}
	case errors.Is(err, passwordservice.ErrIdentityNotFound):
		return &flowHTTPError{Status: http.StatusNotFound, Message: "identity not found"}
	default:
		return err
	}
}

func (s *Server) mapMagicLinkError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, magiclinkservice.ErrInvalidCode):
		return &flowHTTPError{Status: http.StatusBadRequest, Message: "invalid or expired code"}
	case errors.Is(err, magiclinkservice.ErrRateLimited):
		return &flowHTTPError{Status: http.StatusTooManyRequests, Message: "too many attempts, try again later"}
	case errors.Is(err, magiclinkservice.ErrRecipientEmpty):
		return &flowHTTPError{Status: http.StatusBadRequest, Message: "email is required"}
	default:
		return err
	}
}

func (s *Server) writeFlowExecutionError(w http.ResponseWriter, err error) {
	var httpErr *flowHTTPError
	if errors.As(err, &httpErr) {
		writeError(w, httpErr.Status, httpErr.Message, err)
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to execute self-service flow", err)
}
