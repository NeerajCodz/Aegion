package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aegion/aegion/core/courier"
	"github.com/aegion/aegion/core/flows"
	coresession "github.com/aegion/aegion/core/session"
	platformconfig "github.com/aegion/aegion/internal/platform/config"
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	magiclinkservice "github.com/aegion/aegion/modules/magic_link/service"
	magiclinkstore "github.com/aegion/aegion/modules/magic_link/store"
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
	Status  string
	Message string
}

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
	return s != nil && (s.passwordAuth != nil || s.magicLinkAuth != nil)
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

func (s *Server) executeLoginFlow(ctx context.Context, w http.ResponseWriter, r *http.Request, _ *flows.Flow, values map[string]string) (*flowExecutionResult, error) {
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

		if _, err := s.createSession(ctx, w, r, identityID, coresession.AuthMethodPassword); err != nil {
			return nil, err
		}

		return &flowExecutionResult{Status: "authenticated", Message: "login successful"}, nil
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
		if _, err := s.createSession(r.Context(), w, r, *identityID, coresession.AuthMethodMagicLink); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session", err)
			return
		}
		s.respondMagicLinkVerification(w, r, map[string]any{"status": "authenticated", "message": "login successful"})
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
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
