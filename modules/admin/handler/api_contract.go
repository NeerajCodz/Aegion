package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/admin/store"
)

type authLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authLoginResponse struct {
	Token    string       `json:"token"`
	Operator OperatorView `json:"operator"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req authLoginRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || strings.TrimSpace(req.Password) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Email and password are required")
		return
	}

	op, err := h.service.Store().AuthenticateOperatorByEmail(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Authentication failed")
		return
	}

	profile, err := h.service.Store().GetIdentityProfile(r.Context(), op.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load operator profile")
		return
	}
	if normalizeIdentityState(profile.State) != "active" {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
		return
	}

	token, err := h.generateAPIKeyToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create session token")
		return
	}
	prefix := h.lookupPrefix(token)
	tokenHash := store.HashAPIKeyToken(token)

	now := time.Now().UTC()
	expiresAt := now.Add(h.config.SessionTokenExpiry)

	key := &store.APIKey{
		ID:         uuid.New(),
		OperatorID: op.ID,
		Name:       fmt.Sprintf("ui_session_%d", now.Unix()),
		KeyHash:    tokenHash,
		KeyPrefix:  prefix,
		Permissions: map[string]interface{}{
			"session": true,
		},
		ExpiresAt: &expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.service.Store().CreateAPIKey(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create session token")
		return
	}

	effectivePermissions, err := h.service.GetEffectivePermissions(r.Context(), op.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve operator permissions")
		return
	}

	operatorView := operatorToView(op, profile, effectivePermissions)
	writeJSON(w, http.StatusOK, authLoginResponse{
		Token:    token,
		Operator: operatorView,
	})
}

func (h *Handler) generateAPIKeyToken() (string, error) {
	entropy := h.config.APIKeyEntropyBytes
	if entropy < 16 {
		entropy = 16
	}
	b := make([]byte, entropy)
	if err := platformcrypto.FillRandomBytes(b); err != nil {
		return "", err
	}
	return h.config.APIKeyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func (h *Handler) lookupPrefix(token string) string {
	start := len(h.config.APIKeyPrefix)
	end := start + h.config.APIKeyPrefixLen
	if len(token) < end {
		return ""
	}
	return token[start:end]
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	authMethod, _ := r.Context().Value(contextKeyAuthMethod).(string)
	if authMethod == "api_key" {
		keyIDRaw, _ := r.Context().Value(contextKeyAuthKeyID).(string)
		keyID, err := uuid.Parse(strings.TrimSpace(keyIDRaw))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke session token")
			return
		}

		if err := h.revokeAPIKey(r.Context(), keyID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke session token")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type apiKeyRevoker interface {
	DeleteAPIKey(ctx context.Context, id uuid.UUID) error
}

func (h *Handler) revokeAPIKey(ctx context.Context, keyID uuid.UUID) error {
	revoker, ok := h.service.Store().(apiKeyRevoker)
	if !ok {
		return errors.New("api key revocation unavailable")
	}

	err := revoker.DeleteAPIKey(ctx, keyID)
	if err != nil && !errors.Is(err, store.ErrAPIKeyNotFound) {
		return err
	}

	return nil
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	profile, err := h.service.Store().GetIdentityProfile(r.Context(), operator.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load operator profile")
		return
	}

	effectivePermissions, err := h.service.GetEffectivePermissions(r.Context(), operator.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve operator permissions")
		return
	}

	writeJSON(w, http.StatusOK, operatorToView(operator, profile, effectivePermissions))
}

func (h *Handler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	stats := DashboardStatsResponse{
		SystemStatus: "healthy",
	}

	db := h.dbConn()
	if !isDBAvailable(db) {
		writeJSON(w, http.StatusOK, stats)
		return
	}

	if err := db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_identities
		WHERE deleted_at IS NULL
	`).Scan(&stats.TotalIdentities); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load identity stats")
		return
	}

	if err := db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_sessions
		WHERE active = TRUE
		  AND expires_at > NOW()
	`).Scan(&stats.ActiveSessions); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load session stats")
		return
	}

	if err := db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_identities
		WHERE deleted_at IS NULL
		  AND created_at >= NOW() - INTERVAL '24 hours'
	`).Scan(&stats.IdentitiesLast24h); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load signup stats")
		return
	}

	var (
		totalIdentities int64
		aal2Identities  int64
	)
	if err := db.QueryRow(r.Context(), `
		SELECT
			COUNT(DISTINCT ci.id) AS total,
			COUNT(DISTINCT CASE WHEN cs.aal = 'aal2' THEN ci.id END) AS aal2
		FROM core_identities ci
		LEFT JOIN core_sessions cs
			ON cs.identity_id = ci.id
		   AND cs.active = TRUE
		   AND cs.expires_at > NOW()
		WHERE ci.deleted_at IS NULL
	`).Scan(&totalIdentities, &aal2Identities); err == nil && totalIdentities > 0 {
		stats.MFAAdoptionRate = float64(aal2Identities) * 100 / float64(totalIdentities)
	}

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_identities
		WHERE deleted_at IS NULL
		  AND created_at >= NOW() - INTERVAL '7 days'
	`).Scan(&stats.IdentitiesLast7d)

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM core_identities
		WHERE deleted_at IS NULL
		  AND created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&stats.IdentitiesLast30d)

	var passkeyCount int64
	if err := db.QueryRow(r.Context(), `
		SELECT COUNT(DISTINCT identity_id)
		FROM passkeys_credentials
	`).Scan(&passkeyCount); err == nil && stats.TotalIdentities > 0 {
		stats.PasskeyAdoptionRate = float64(passkeyCount) * 100 / float64(stats.TotalIdentities)
	}

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM adm_ip_bans
		WHERE expires_at IS NULL OR expires_at > NOW()
	`).Scan(&stats.ActiveIPBans)

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM adm_operators
	`).Scan(&stats.TotalOperators)
	stats.ActiveOperators = stats.TotalOperators

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM adm_roles
	`).Scan(&stats.TotalRoles)

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM oauth2_clients
	`).Scan(&stats.TotalOAuth2Clients)

	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM oauth2_tokens
		WHERE revoked_at IS NULL AND expires_at > NOW()
	`).Scan(&stats.ActiveOAuth2Tokens)

	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) DashboardTimeSeries(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	rangeParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	if rangeParam == "" {
		rangeParam = "24h"
	}
	switch rangeParam {
	case "24h", "7d", "30d", "90d":
	default:
		rangeParam = "24h"
	}

	now := time.Now().UTC()
	var points []TimeSeriesPoint
	pointsByTime := make(map[string]*TimeSeriesPoint)

	switch rangeParam {
	case "24h":
		start := now.Add(-23 * time.Hour).Truncate(time.Hour)
		for i := 0; i < 24; i++ {
			t := start.Add(time.Duration(i) * time.Hour)
			ts := t.Format(time.RFC3339)
			pt := TimeSeriesPoint{Timestamp: ts}
			points = append(points, pt)
			pointsByTime[t.Format("2006-01-02 15:00")] = &points[len(points)-1]
		}
	case "7d":
		start := now.AddDate(0, 0, -6).Truncate(24 * time.Hour)
		for i := 0; i < 7; i++ {
			t := start.AddDate(0, 0, i)
			ts := t.Format(time.RFC3339)
			pt := TimeSeriesPoint{Timestamp: ts}
			points = append(points, pt)
			pointsByTime[t.Format("2006-01-02")] = &points[len(points)-1]
		}
	case "30d":
		start := now.AddDate(0, 0, -29).Truncate(24 * time.Hour)
		for i := 0; i < 30; i++ {
			t := start.AddDate(0, 0, i)
			ts := t.Format(time.RFC3339)
			pt := TimeSeriesPoint{Timestamp: ts}
			points = append(points, pt)
			pointsByTime[t.Format("2006-01-02")] = &points[len(points)-1]
		}
	case "90d":
		start := now.AddDate(0, 0, -89).Truncate(24 * time.Hour)
		for i := 0; i < 90; i++ {
			t := start.AddDate(0, 0, i)
			ts := t.Format(time.RFC3339)
			pt := TimeSeriesPoint{Timestamp: ts}
			points = append(points, pt)
			pointsByTime[t.Format("2006-01-02")] = &points[len(points)-1]
		}
	}

	var intervalStr string
	var dateTrunc string
	var timeFormat string
	if rangeParam == "24h" {
		intervalStr = "24 hours"
		dateTrunc = "hour"
		timeFormat = "2006-01-02 15:00"
	} else if rangeParam == "7d" {
		intervalStr = "7 days"
		dateTrunc = "day"
		timeFormat = "2006-01-02"
	} else if rangeParam == "30d" {
		intervalStr = "30 days"
		dateTrunc = "day"
		timeFormat = "2006-01-02"
	} else {
		intervalStr = "90 days"
		dateTrunc = "day"
		timeFormat = "2006-01-02"
	}

	if isDBAvailable(h.dbConn()) {
		rows, err := h.dbConn().Query(r.Context(), fmt.Sprintf(`
			SELECT DATE_TRUNC('%s', created_at) AS bucket, COUNT(*)
			FROM core_identities
			WHERE deleted_at IS NULL
			  AND created_at >= NOW() - INTERVAL '%s'
			GROUP BY bucket
			ORDER BY bucket ASC
		`, dateTrunc, intervalStr))
		if err == nil && rows != nil {
			for rows.Next() {
				var (
					bucket time.Time
					count  int64
				)
				if err := rows.Scan(&bucket, &count); err == nil {
					key := bucket.UTC().Format(timeFormat)
					if pt, ok := pointsByTime[key]; ok {
						pt.NewIdentities = count
					}
				}
			}
			rows.Close()
		}

		rows, err = h.dbConn().Query(r.Context(), fmt.Sprintf(`
			SELECT DATE_TRUNC('%s', created_at) AS bucket, COUNT(*)
			FROM core_sessions
			WHERE created_at >= NOW() - INTERVAL '%s'
			GROUP BY bucket
			ORDER BY bucket ASC
		`, dateTrunc, intervalStr))
		if err == nil && rows != nil {
			for rows.Next() {
				var (
					bucket time.Time
					count  int64
				)
				if err := rows.Scan(&bucket, &count); err == nil {
					key := bucket.UTC().Format(timeFormat)
					if pt, ok := pointsByTime[key]; ok {
						pt.ActiveSessions = count
					}
				}
			}
			rows.Close()
		}

		rows, err = h.dbConn().Query(r.Context(), fmt.Sprintf(`
			SELECT DATE_TRUNC('%s', created_at) AS bucket,
			       COUNT(CASE WHEN action NOT LIKE '%%fail%%' AND action NOT LIKE '%%denied%%' AND action NOT LIKE '%%reject%%' THEN 1 END) AS successes,
			       COUNT(CASE WHEN action LIKE '%%fail%%' OR action LIKE '%%denied%%' OR action LIKE '%%reject%%' THEN 1 END) AS failures
			FROM adm_audit_log
			WHERE created_at >= NOW() - INTERVAL '%s'
			GROUP BY bucket
			ORDER BY bucket ASC
		`, dateTrunc, intervalStr))
		if err == nil && rows != nil {
			for rows.Next() {
				var (
					bucket    time.Time
					successes int64
					failures  int64
				)
				if err := rows.Scan(&bucket, &successes, &failures); err == nil {
					key := bucket.UTC().Format(timeFormat)
					if pt, ok := pointsByTime[key]; ok {
						pt.AuthSuccesses = successes
						pt.AuthFailures = failures
					}
				}
			}
			rows.Close()
		}
	}

	writeJSON(w, http.StatusOK, DashboardTimeSeriesResponse{
		Range:  rangeParam,
		Points: points,
	})
}

func (h *Handler) DashboardAuthBreakdown(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	resp := AuthBreakdownResponse{}

	db := h.dbConn()
	if isDBAvailable(db) {
		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM passkeys_credentials
		`).Scan(&resp.PasskeysCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM pwd_credentials
		`).Scan(&resp.PasswordsCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM soc_identities
		`).Scan(&resp.SocialOIDCCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM sso_identities
		`).Scan(&resp.EnterpriseSSOCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM mfa_factors
			WHERE factor_type = 'totp'
		`).Scan(&resp.MFATOTPCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM mfa_factors
			WHERE factor_type = 'webauthn'
		`).Scan(&resp.MFAWebAuthnCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM mfa_factors
			WHERE factor_type = 'backup_codes'
		`).Scan(&resp.MFABackupCodesCount)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DashboardSecurityPosture(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	resp := SecurityPostureResponse{
		RiskScore:        15,
		RiskLevel:        "strong",
		ThreatIndicators: []ThreatIndicator{},
	}

	db := h.dbConn()
	if isDBAvailable(db) {
		var totalIdentities, aal2Identities int64
		if err := db.QueryRow(r.Context(), `
			SELECT
				COUNT(DISTINCT ci.id) AS total,
				COUNT(DISTINCT CASE WHEN cs.aal = 'aal2' THEN ci.id END) AS aal2
			FROM core_identities ci
			LEFT JOIN core_sessions cs
				ON cs.identity_id = ci.id
			   AND cs.active = TRUE
			   AND cs.expires_at > NOW()
			WHERE ci.deleted_at IS NULL
		`).Scan(&totalIdentities, &aal2Identities); err == nil && totalIdentities > 0 {
			resp.MFACoveragePct = float64(aal2Identities) * 100 / float64(totalIdentities)
		}

		var passkeysCount int64
		if err := db.QueryRow(r.Context(), `
			SELECT COUNT(DISTINCT identity_id)
			FROM passkeys_credentials
		`).Scan(&passkeysCount); err == nil && totalIdentities > 0 {
			resp.PasskeyCoveragePct = float64(passkeysCount) * 100 / float64(totalIdentities)
		}

		var activeSessions int64
		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM core_sessions
			WHERE active = TRUE
			  AND expires_at > NOW()
		`).Scan(&activeSessions)

		if totalIdentities > 0 {
			resp.SessionPressureRatio = float64(activeSessions) / float64(totalIdentities)
		}

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM adm_audit_log
			WHERE (action LIKE '%fail%' OR action LIKE '%denied%' OR action LIKE '%reject%')
			  AND created_at >= NOW() - INTERVAL '24 hours'
		`).Scan(&resp.FailedLoginsLast24h)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM adm_ip_bans
			WHERE expires_at IS NULL OR expires_at > NOW()
		`).Scan(&resp.ActiveIPBansCount)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM adm_scim_api_tokens
			WHERE (permissions->>'wildcard')::boolean = true OR (scopes::text LIKE '%*%')
		`).Scan(&resp.WildcardSCIMTokens)

		_ = db.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM oauth2_clients
			WHERE secret_rotated_at IS NULL
			  AND created_at < NOW() - INTERVAL '90 days'
		`).Scan(&resp.UnrotatedOAuthSecrets)
	}
	score := 15

	if resp.MFACoveragePct < 50 {
		score += 25
		resp.ThreatIndicators = append(resp.ThreatIndicators, ThreatIndicator{
			ID:          "mfa-low-adoption",
			Severity:    "warning",
			Title:       "Low MFA Adoption Rate",
			Description: fmt.Sprintf("MFA coverage is currently %.1f%% across active identities.", resp.MFACoveragePct),
			ActionURL:   "/settings",
			ActionLabel: "Enforce MFA",
		})
	}

	if resp.FailedLoginsLast24h > 10 {
		score += 20
		resp.ThreatIndicators = append(resp.ThreatIndicators, ThreatIndicator{
			ID:          "high-failed-logins",
			Severity:    "warning",
			Title:       "Elevated Authentication Failures",
			Description: fmt.Sprintf("%d failed login attempts detected in the last 24 hours.", resp.FailedLoginsLast24h),
			ActionURL:   "/logs",
			ActionLabel: "Review Audit Logs",
		})
	}

	if resp.ActiveIPBansCount > 0 {
		resp.ThreatIndicators = append(resp.ThreatIndicators, ThreatIndicator{
			ID:          "active-ip-threats",
			Severity:    "info",
			Title:       "Active IP Threat Bans",
			Description: fmt.Sprintf("%d IP addresses are currently blocked by security policies.", resp.ActiveIPBansCount),
			ActionURL:   "/security",
			ActionLabel: "Manage IP Bans",
		})
	}

	if resp.WildcardSCIMTokens > 0 {
		score += 15
		resp.ThreatIndicators = append(resp.ThreatIndicators, ThreatIndicator{
			ID:          "wildcard-scim-tokens",
			Severity:    "warning",
			Title:       "Wildcard SCIM Tokens Active",
			Description: fmt.Sprintf("%d SCIM token(s) have unrestricted wildcard permissions.", resp.WildcardSCIMTokens),
			ActionURL:   "/scim",
			ActionLabel: "Review SCIM Tokens",
		})
	}

	if resp.UnrotatedOAuthSecrets > 0 {
		score += 10
		resp.ThreatIndicators = append(resp.ThreatIndicators, ThreatIndicator{
			ID:          "unrotated-oauth-secrets",
			Severity:    "info",
			Title:       "Stale OAuth2 Client Credentials",
			Description: fmt.Sprintf("%d OAuth2 client secret(s) have not been rotated in over 90 days.", resp.UnrotatedOAuthSecrets),
			ActionURL:   "/oauth2",
			ActionLabel: "Rotate Secrets",
		})
	}

	if score > 100 {
		score = 100
	}
	resp.RiskScore = score

	if score >= 60 {
		resp.RiskLevel = "critical"
	} else if score >= 30 {
		resp.RiskLevel = "moderate"
	} else {
		resp.RiskLevel = "strong"
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	if OperatorFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	settings, err := h.readSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load settings")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	settings, err := h.readSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load current settings")
		return
	}

	if v, ok := req["session_lifetime_hours"]; ok {
		i, ok := toInt(v)
		if !ok || i < 1 || i > 720 {
			writeError(w, http.StatusBadRequest, "invalid_settings", "session_lifetime_hours must be between 1 and 720")
			return
		}
		settings.SessionLifetimeHours = i
	}

	if v, ok := req["mfa_required"]; ok {
		b, ok := v.(bool)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_settings", "mfa_required must be a boolean")
			return
		}
		settings.MFARequired = b
	}

	if v, ok := req["password_min_length"]; ok {
		i, ok := toInt(v)
		if !ok || i < 6 || i > 128 {
			writeError(w, http.StatusBadRequest, "invalid_settings", "password_min_length must be between 6 and 128")
			return
		}
		settings.PasswordMinLength = i
	}

	if v, ok := req["max_login_attempts"]; ok {
		i, ok := toInt(v)
		if !ok || i < 1 || i > 20 {
			writeError(w, http.StatusBadRequest, "invalid_settings", "max_login_attempts must be between 1 and 20")
			return
		}
		settings.MaxLoginAttempts = i
	}

	if v, ok := req["lockout_duration_minutes"]; ok {
		i, ok := toInt(v)
		if !ok || i < 1 || i > 1440 {
			writeError(w, http.StatusBadRequest, "invalid_settings", "lockout_duration_minutes must be between 1 and 1440")
			return
		}
		settings.LockoutDurationMinutes = i
	}

	if v, ok := req["allowed_domains"]; ok {
		domains, ok := toStringSlice(v)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_settings", "allowed_domains must be a string array")
			return
		}
		settings.AllowedDomains = domains
	}

	if err := h.upsertSetting(r.Context(), operator.IdentityID, "admin.settings", settings); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to persist settings")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)
	identityFilter := strings.TrimSpace(r.URL.Query().Get("identity_id"))

	where := "WHERE cs.active = TRUE AND cs.expires_at > NOW()"
	args := []interface{}{}
	arg := 1
	if identityFilter != "" {
		where += " AND cs.identity_id = $" + strconv.Itoa(arg)
		args = append(args, identityFilter)
		arg++
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM core_sessions cs " + where
	if err := h.dbConn().QueryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to count sessions")
		return
	}

	query := `
		SELECT
			cs.id,
			cs.identity_id,
			COALESCE(
				NULLIF((cs.devices->0->>'user_agent'), ''),
				''
			) AS user_agent,
			COALESCE(
				NULLIF((cs.devices->0->>'ip_address'), ''),
				''
			) AS ip_address,
			cs.created_at,
			cs.expires_at,
			cs.authenticated_at
		FROM core_sessions cs
	` + where + `
		ORDER BY cs.created_at DESC
		LIMIT $` + strconv.Itoa(arg) + ` OFFSET $` + strconv.Itoa(arg+1)
	args = append(args, perPage, offset)

	rows, err := h.dbConn().Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list sessions")
		return
	}
	defer rows.Close()

	data := make([]map[string]interface{}, 0, perPage)
	for rows.Next() {
		var (
			id              uuid.UUID
			identityID      uuid.UUID
			userAgent       string
			ipAddress       string
			createdAt       time.Time
			expiresAt       time.Time
			authenticatedAt time.Time
		)
		if err := rows.Scan(&id, &identityID, &userAgent, &ipAddress, &createdAt, &expiresAt, &authenticatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read sessions")
			return
		}

		data = append(data, map[string]interface{}{
			"id":             id.String(),
			"identity_id":    identityID.String(),
			"user_agent":     userAgent,
			"ip_address":     ipAddress,
			"created_at":     createdAt.Format(time.RFC3339),
			"expires_at":     expiresAt.Format(time.RFC3339),
			"last_active_at": authenticatedAt.Format(time.RFC3339),
			"is_current":     false,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list sessions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":        data,
		"total":       total,
		"page":        page,
		"per_page":    perPage,
		"total_pages": paginationTotalPages(total, perPage),
	})
}

func (h *Handler) readSettings(ctx context.Context) (*SystemSettingsResponse, error) {
	defaults := &SystemSettingsResponse{
		SessionLifetimeHours:   24,
		MFARequired:            false,
		PasswordMinLength:      8,
		MaxLoginAttempts:       5,
		LockoutDurationMinutes: 15,
		AllowedDomains:         []string{},
	}

	var raw []byte
	err := h.dbConn().QueryRow(ctx, `
		SELECT value
		FROM core_system_config
		WHERE key = 'admin.settings'
	`).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaults, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(raw, defaults); err != nil {
		return defaults, nil
	}

	return defaults, nil
}

func (h *Handler) upsertSetting(ctx context.Context, actorIdentityID uuid.UUID, key string, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}

	_, err = h.dbConn().Exec(ctx, `
		INSERT INTO core_system_config (key, value, updated_by, updated_at)
		VALUES ($1, $2::jsonb, $3, NOW())
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = NOW()
	`, key, string(raw), actorIdentityID)
	return err
}

func operatorToView(op *store.Operator, profile *store.IdentityProfile, effectivePermissions []string) OperatorView {
	view := OperatorView{
		ID:                   op.ID.String(),
		Role:                 op.Role,
		Status:               "active",
		Permissions:          op.Permissions,
		EffectivePermissions: effectivePermissions,
		CreatedAt:            op.CreatedAt.Format(time.RFC3339),
		UpdatedAt:            op.UpdatedAt.Format(time.RFC3339),
	}

	if profile != nil {
		view.Email = profile.Email
		view.Name = profile.Name
		if profile.State != "" {
			view.Status = normalizeIdentityState(profile.State)
		}
		if profile.LastLoginAt != nil {
			last := profile.LastLoginAt.Format(time.RFC3339)
			view.LastLoginAt = &last
		}
	}

	return view
}

func normalizeIdentityState(state string) string {
	switch strings.ToLower(state) {
	case "active":
		return "active"
	case "inactive", "banned", "blocked", "deleted":
		return "inactive"
	default:
		return "active"
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func toStringSlice(v interface{}) ([]string, bool) {
	raw, ok := v.([]interface{})
	if !ok {
		return nil, false
	}

	out := make([]string, 0, len(raw))
	for _, it := range raw {
		str, ok := it.(string)
		if !ok {
			return nil, false
		}
		str = strings.TrimSpace(str)
		if str != "" {
			out = append(out, str)
		}
	}

	return out, true
}

func paginationTotalPages(total int64, perPage int) int {
	if perPage <= 0 {
		return 1
	}
	pages := int(total) / perPage
	if int(total)%perPage > 0 {
		pages++
	}
	if pages == 0 {
		return 1
	}
	return pages
}
