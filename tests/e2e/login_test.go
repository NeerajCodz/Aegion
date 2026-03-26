package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// LoginFlowResponse represents a login flow
type LoginFlowResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	State     string `json:"state"`
	CSRFToken string `json:"csrf_token"`
	UI        struct {
		Action string `json:"action"`
		Method string `json:"method"`
		Nodes  []struct {
			Type       string                 `json:"type"`
			Group      string                 `json:"group"`
			Attributes map[string]interface{} `json:"attributes"`
		} `json:"nodes"`
	} `json:"ui"`
	Refresh   bool   `json:"refresh"`
	RequestAL string `json:"requested_aal"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
}

// LoginRequest represents a login submission
type LoginRequest struct {
	Method     string `json:"method"`
	Identifier string `json:"identifier,omitempty"`
	Password   string `json:"password,omitempty"`
	Code       string `json:"code,omitempty"`
	Token      string `json:"token,omitempty"`
}

// createLoginTestServer creates a mock server for login tests
func createLoginTestServer(t *testing.T, suite *TestSuite) *httptest.Server {
	t.Helper()

	r := chi.NewRouter()

	// Initialize login flow
	r.Get("/api/v1/self-service/login", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		fixtures := NewTestFixtures(suite.Pool)

		flow, err := fixtures.CreateFlow(ctx, "login", 15*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		refresh := r.URL.Query().Get("refresh") == "true"
		requestedAAL := r.URL.Query().Get("aal")
		if requestedAAL == "" {
			requestedAAL = "aal1"
		}

		response := LoginFlowResponse{
			ID:        flow.ID.String(),
			Type:      "login",
			State:     "active",
			CSRFToken: flow.CSRFToken,
			Refresh:   refresh,
			RequestAL: requestedAAL,
		}
		response.UI.Action = "/api/v1/self-service/login"
		response.UI.Method = "POST"
		response.IssuedAt = time.Now().Format(time.RFC3339)
		response.ExpiresAt = flow.ExpiresAt.Format(time.RFC3339)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Submit login
	r.Post("/api/v1/self-service/login", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		flowID := r.URL.Query().Get("flow")

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
			return
		}

		if flowID == "" {
			writeError(w, http.StatusBadRequest, "invalid_flow", "Flow ID is required")
			return
		}

		// Verify flow exists and is active
		var flowState string
		var flowExpiresAt time.Time
		err := suite.Pool.QueryRow(ctx, `
			SELECT state, expires_at FROM core_flows WHERE id = $1
		`, flowID).Scan(&flowState, &flowExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_flow", "Flow not found")
			return
		}
		if flowState != "active" {
			writeError(w, http.StatusBadRequest, "invalid_flow", "Flow is not active")
			return
		}
		if flowExpiresAt.Before(time.Now()) {
			writeError(w, http.StatusGone, "flow_expired", "Flow has expired")
			return
		}

		switch req.Method {
		case "password":
			handlePasswordLogin(ctx, w, suite, req, flowID)
		case "code":
			handleOTPLogin(ctx, w, suite, req, flowID)
		case "link":
			handleMagicLinkLogin(ctx, w, suite, req, flowID)
		default:
			writeError(w, http.StatusBadRequest, "invalid_method", "Invalid login method")
		}
	})

	// Magic link callback
	r.Get("/api/v1/self-service/login/link", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := r.URL.Query().Get("token")

		if token == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Token is required")
			return
		}

		// Find and validate token
		var identityID uuid.UUID
		var flowID uuid.UUID
		err := suite.Pool.QueryRow(ctx, `
			SELECT identity_id, flow_id FROM module_magic_link_tokens
			WHERE token = $1 AND used = false AND expires_at > NOW()
		`, token).Scan(&identityID, &flowID)

		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_token", "Invalid or expired magic link")
			return
		}

		// Mark token as used
		suite.Pool.Exec(ctx, `UPDATE module_magic_link_tokens SET used = true WHERE token = $1`, token)

		// Create session
		fixtures := NewTestFixtures(suite.Pool)
		session, _ := fixtures.CreateSession(ctx, identityID)

		// Update flow
		suite.Pool.Exec(ctx, `
			UPDATE core_flows SET state = 'completed', identity_id = $1, session_id = $2 WHERE id = $3
		`, identityID, session.ID, flowID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session": map[string]interface{}{
				"id":          session.ID.String(),
				"token":       session.Token,
				"identity_id": session.IdentityID.String(),
				"active":      session.Active,
			},
		})
	})

	// Whoami endpoint
	r.Get("/api/v1/sessions/whoami", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sessionToken := r.Header.Get("X-Session-Token")

		if sessionToken == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "No session token provided")
			return
		}

		var session Session
		var identityID uuid.UUID
		err := suite.Pool.QueryRow(ctx, `
			SELECT id, token, identity_id, aal, expires_at, active
			FROM core_sessions
			WHERE token = $1 AND active = true AND expires_at > NOW()
		`, sessionToken).Scan(&session.ID, &session.Token, &identityID, &session.AAL, &session.ExpiresAt, &session.Active)

		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid or expired session")
			return
		}

		// Get identity
		var traits json.RawMessage
		var state string
		suite.Pool.QueryRow(ctx, `
			SELECT traits, state FROM core_identities WHERE id = $1
		`, identityID).Scan(&traits, &state)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session": map[string]interface{}{
				"id":          session.ID.String(),
				"active":      session.Active,
				"aal":         session.AAL,
				"expires_at":  session.ExpiresAt.Format(time.RFC3339),
				"identity_id": identityID.String(),
			},
			"identity": map[string]interface{}{
				"id":     identityID.String(),
				"traits": traits,
				"state":  state,
			},
		})
	})

	return httptest.NewServer(r)
}

func handlePasswordLogin(ctx context.Context, w http.ResponseWriter, suite *TestSuite, req LoginRequest, flowID string) {
	if req.Identifier == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Email/identifier is required")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Password is required")
		return
	}

	// Find identity by email
	var identityID uuid.UUID
	err := suite.Pool.QueryRow(ctx, `
		SELECT identity_id FROM core_identity_addresses 
		WHERE address = $1 AND address_type = 'email'
	`, req.Identifier).Scan(&identityID)

	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
		return
	}

	// Check password
	var passwordHash string
	err = suite.Pool.QueryRow(ctx, `
		SELECT password_hash FROM module_password_credentials WHERE identity_id = $1
	`, identityID).Scan(&passwordHash)

	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
		return
	}

	// Create session
	fixtures := NewTestFixtures(suite.Pool)
	session, err := fixtures.CreateSession(ctx, identityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Failed to create session")
		return
	}

	// Update flow
	suite.Pool.Exec(ctx, `
		UPDATE core_flows SET state = 'completed', identity_id = $1, session_id = $2 WHERE id = $3
	`, identityID, session.ID, flowID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session": map[string]interface{}{
			"id":          session.ID.String(),
			"token":       session.Token,
			"identity_id": session.IdentityID.String(),
			"active":      session.Active,
			"aal":         session.AAL,
		},
	})
}

func handleOTPLogin(ctx context.Context, w http.ResponseWriter, suite *TestSuite, req LoginRequest, flowID string) {
	if req.Identifier == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Email/identifier is required")
		return
	}

	if req.Code == "" {
		// Initiate OTP - create token and return
		var identityID uuid.UUID
		err := suite.Pool.QueryRow(ctx, `
			SELECT identity_id FROM core_identity_addresses 
			WHERE address = $1 AND address_type = 'email'
		`, req.Identifier).Scan(&identityID)

		if err != nil {
			// Don't reveal if email exists
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"state":   "code_sent",
				"message": "If the email exists, a code has been sent",
			})
			return
		}

		// Create OTP token
		fixtures := NewTestFixtures(suite.Pool)
		flowUUID, _ := uuid.Parse(flowID)
		_, otpCode, _ := fixtures.CreateMagicLinkToken(ctx, &identityID, req.Identifier, flowUUID, 10*time.Minute)

		// In real implementation, send email with OTP
		_ = otpCode

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":   "code_sent",
			"message": "Code sent to email",
		})
		return
	}

	// Verify OTP code
	var identityID uuid.UUID
	err := suite.Pool.QueryRow(ctx, `
		SELECT identity_id FROM module_magic_link_tokens
		WHERE otp_code = $1 AND email = $2 AND used = false AND expires_at > NOW()
	`, req.Code, req.Identifier).Scan(&identityID)

	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_code", "Invalid or expired code")
		return
	}

	// Mark token as used
	suite.Pool.Exec(ctx, `
		UPDATE module_magic_link_tokens SET used = true 
		WHERE otp_code = $1 AND email = $2
	`, req.Code, req.Identifier)

	// Create session
	fixtures := NewTestFixtures(suite.Pool)
	session, _ := fixtures.CreateSession(ctx, identityID)

	// Update flow
	suite.Pool.Exec(ctx, `
		UPDATE core_flows SET state = 'completed', identity_id = $1, session_id = $2 WHERE id = $3
	`, identityID, session.ID, flowID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session": map[string]interface{}{
			"id":          session.ID.String(),
			"token":       session.Token,
			"identity_id": session.IdentityID.String(),
			"active":      session.Active,
		},
	})
}

func handleMagicLinkLogin(ctx context.Context, w http.ResponseWriter, suite *TestSuite, req LoginRequest, flowID string) {
	if req.Identifier == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Email/identifier is required")
		return
	}

	var identityID uuid.UUID
	err := suite.Pool.QueryRow(ctx, `
		SELECT identity_id FROM core_identity_addresses 
		WHERE address = $1 AND address_type = 'email'
	`, req.Identifier).Scan(&identityID)

	if err != nil {
		// Don't reveal if email exists
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":   "link_sent",
			"message": "If the email exists, a magic link has been sent",
		})
		return
	}

	// Create magic link token
	fixtures := NewTestFixtures(suite.Pool)
	flowUUID, _ := uuid.Parse(flowID)
	token, _, _ := fixtures.CreateMagicLinkToken(ctx, &identityID, req.Identifier, flowUUID, 15*time.Minute)

	// In real implementation, send email with link
	_ = token

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"state":   "link_sent",
		"message": "Magic link sent to email",
	})
}

// TestLoginFlowInitiation tests starting a login flow
func TestLoginFlowInitiation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	t.Run("successful flow initiation", func(t *testing.T) {
		resp, err := client.Get("/api/v1/self-service/login")
		if err != nil {
			t.Fatalf("Failed to initiate login flow: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var flow LoginFlowResponse
		if err := ParseJSONResponse(resp, &flow); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if flow.ID == "" {
			t.Error("Flow ID should not be empty")
		}
		if flow.Type != "login" {
			t.Errorf("Expected flow type 'login', got '%s'", flow.Type)
		}
		if flow.CSRFToken == "" {
			t.Error("CSRF token should not be empty")
		}
	})

	t.Run("flow with refresh parameter", func(t *testing.T) {
		resp, _ := client.Get("/api/v1/self-service/login?refresh=true")

		var flow LoginFlowResponse
		ParseJSONResponse(resp, &flow)

		if !flow.Refresh {
			t.Error("Flow should have refresh=true")
		}
	})

	t.Run("flow with AAL parameter", func(t *testing.T) {
		resp, _ := client.Get("/api/v1/self-service/login?aal=aal2")

		var flow LoginFlowResponse
		ParseJSONResponse(resp, &flow)

		if flow.RequestAL != "aal2" {
			t.Errorf("Expected requested_aal 'aal2', got '%s'", flow.RequestAL)
		}
	})
}

// TestPasswordLogin tests password-based login
func TestPasswordLogin(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create a test user with password
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
	identity, _ := fixtures.CreateIdentityWithPassword(ctx, "login@example.com", "Test User", string(passwordHash))
	_ = identity

	t.Run("successful password login", func(t *testing.T) {
		// Get flow
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "password",
			Identifier: "login@example.com",
			Password:   "correctpassword",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		session := result["session"].(map[string]interface{})
		if session["token"] == "" {
			t.Error("Session token should not be empty")
		}
		if session["active"] != true {
			t.Error("Session should be active")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "password",
			Identifier: "login@example.com",
			Password:   "wrongpassword",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		var errResp map[string]interface{}
		ParseJSONResponse(resp, &errResp)

		errObj := errResp["error"].(map[string]interface{})
		if errObj["status"] != "invalid_credentials" {
			t.Errorf("Expected error status 'invalid_credentials', got '%s'", errObj["status"])
		}
	})

	t.Run("nonexistent user", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "password",
			Identifier: "nonexistent@example.com",
			Password:   "anypassword",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("missing password", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "password",
			Identifier: "login@example.com",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

// TestMagicLinkLogin tests magic link login
func TestMagicLinkLogin(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test user
	_, _ = fixtures.CreateVerifiedIdentity(ctx, "magic@example.com", "Magic User")

	t.Run("initiate magic link", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "link",
			Identifier: "magic@example.com",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		if result["state"] != "link_sent" {
			t.Errorf("Expected state 'link_sent', got '%s'", result["state"])
		}
	})

	t.Run("complete magic link login", func(t *testing.T) {
		// Create identity and token directly
		identity, _ := fixtures.CreateIdentity(ctx, "magiclink@example.com", "Magic Link User")
		flow, _ := fixtures.CreateFlow(ctx, "login", 15*time.Minute)
		token, _, _ := fixtures.CreateMagicLinkToken(ctx, &identity.ID, identity.Email, flow.ID, 15*time.Minute)

		resp, _ := client.Get("/api/v1/self-service/login/link?token=" + token)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		session := result["session"].(map[string]interface{})
		if session["token"] == "" {
			t.Error("Session token should not be empty")
		}
	})

	t.Run("invalid magic link token", func(t *testing.T) {
		resp, _ := client.Get("/api/v1/self-service/login/link?token=invalid-token")

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("magic link for nonexistent email", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "link",
			Identifier: "nonexistent@example.com",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		// Should return success to not reveal email existence
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

// TestOTPLogin tests OTP code login
func TestOTPLogin(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test user
	_, _ = fixtures.CreateVerifiedIdentity(ctx, "otp@example.com", "OTP User")

	t.Run("initiate OTP code", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "code",
			Identifier: "otp@example.com",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		if result["state"] != "code_sent" {
			t.Errorf("Expected state 'code_sent', got '%s'", result["state"])
		}
	})

	t.Run("complete OTP login", func(t *testing.T) {
		identity, _ := fixtures.CreateIdentity(ctx, "otpverify@example.com", "OTP Verify User")
		flow, _ := fixtures.CreateFlow(ctx, "login", 15*time.Minute)
		_, otpCode, _ := fixtures.CreateMagicLinkToken(ctx, &identity.ID, identity.Email, flow.ID, 10*time.Minute)

		req := LoginRequest{
			Method:     "code",
			Identifier: "otpverify@example.com",
			Code:       otpCode,
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID.String(), req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		session := result["session"].(map[string]interface{})
		if session["token"] == "" {
			t.Error("Session token should not be empty")
		}
	})

	t.Run("invalid OTP code", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "code",
			Identifier: "otp@example.com",
			Code:       "000000",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})
}

// TestSessionCreation tests that sessions are created correctly
func TestSessionCreation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	_, _ = fixtures.CreateIdentityWithPassword(ctx, "session@example.com", "Session Test", string(passwordHash))

	t.Run("session is stored in database", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "password",
			Identifier: "session@example.com",
			Password:   "password123",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		session := result["session"].(map[string]interface{})
		sessionID := session["id"].(string)

		// Verify session in database
		var active bool
		err := suite.Pool.QueryRow(ctx, `
			SELECT active FROM core_sessions WHERE id = $1
		`, sessionID).Scan(&active)

		if err != nil {
			t.Fatalf("Session not found in database: %v", err)
		}
		if !active {
			t.Error("Session should be active in database")
		}
	})

	t.Run("session can be used for whoami", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/login")
		var flow LoginFlowResponse
		ParseJSONResponse(flowResp, &flow)

		req := LoginRequest{
			Method:     "password",
			Identifier: "session@example.com",
			Password:   "password123",
		}

		loginResp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

		var loginResult map[string]interface{}
		ParseJSONResponse(loginResp, &loginResult)

		sessionObj := loginResult["session"].(map[string]interface{})
		sessionToken := sessionObj["token"].(string)

		// Use session token
		client.SessionID = sessionToken
		whoamiResp, _ := client.Get("/api/v1/sessions/whoami")

		if whoamiResp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", whoamiResp.StatusCode)
		}

		var whoamiResult map[string]interface{}
		ParseJSONResponse(whoamiResp, &whoamiResult)

		session := whoamiResult["session"].(map[string]interface{})
		if session["active"] != true {
			t.Error("Session should be active")
		}
	})
}

// TestInvalidCredentials tests various invalid credential scenarios
func TestInvalidCredentials(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	testCases := []struct {
		name       string
		identifier string
		password   string
	}{
		{"empty identifier", "", "password"},
		{"empty password", "user@example.com", ""},
		{"both empty", "", ""},
		{"sql injection attempt", "'; DROP TABLE users; --", "password"},
		{"very long identifier", string(make([]byte, 1000)), "password"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flowResp, _ := client.Get("/api/v1/self-service/login")
			var flow LoginFlowResponse
			ParseJSONResponse(flowResp, &flow)

			req := LoginRequest{
				Method:     "password",
				Identifier: tc.identifier,
				Password:   tc.password,
			}

			resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

			// Should return 400 or 401, not 500
			if resp.StatusCode >= 500 {
				t.Errorf("Expected client error, got server error %d", resp.StatusCode)
			}
		})
	}
}

// TestFlowExpiration tests login flow expiration
func TestFlowExpiration(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	t.Run("expired flow is rejected", func(t *testing.T) {
		// Create expired flow
		flow, _ := fixtures.CreateFlow(ctx, "login", -time.Minute)

		req := LoginRequest{
			Method:     "password",
			Identifier: "test@example.com",
			Password:   "password",
		}

		resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID.String(), req)

		if resp.StatusCode != http.StatusGone {
			t.Errorf("Expected status 410 (Gone), got %d", resp.StatusCode)
		}

		var errResp map[string]interface{}
		ParseJSONResponse(resp, &errResp)

		errObj := errResp["error"].(map[string]interface{})
		if errObj["status"] != "flow_expired" {
			t.Errorf("Expected error status 'flow_expired', got '%s'", errObj["status"])
		}
	})
}

// TestInvalidMethod tests invalid login method
func TestInvalidMethod(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createLoginTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	flowResp, _ := client.Get("/api/v1/self-service/login")
	var flow LoginFlowResponse
	ParseJSONResponse(flowResp, &flow)

	req := LoginRequest{
		Method:     "invalid_method",
		Identifier: "test@example.com",
	}

	resp, _ := client.Post("/api/v1/self-service/login?flow="+flow.ID, req)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}
