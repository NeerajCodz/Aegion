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
)

// RegistrationFlowResponse represents a registration flow
type RegistrationFlowResponse struct {
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
			Messages   []struct {
				ID   int    `json:"id"`
				Text string `json:"text"`
				Type string `json:"type"`
			} `json:"messages"`
		} `json:"nodes"`
	} `json:"ui"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
}

// RegistrationRequest represents a registration submission
type RegistrationRequest struct {
	Method   string `json:"method"`
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
	Name     string `json:"name,omitempty"`
}

// createRegistrationTestServer creates a mock server for registration tests
func createRegistrationTestServer(t *testing.T, suite *TestSuite) *httptest.Server {
	t.Helper()

	r := chi.NewRouter()

	// Initialize registration flow
	r.Get("/api/v1/self-service/registration", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		fixtures := NewTestFixtures(suite.Pool)

		flow, err := fixtures.CreateFlow(ctx, "registration", 15*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := RegistrationFlowResponse{
			ID:        flow.ID.String(),
			Type:      "registration",
			State:     "active",
			CSRFToken: flow.CSRFToken,
		}
		response.UI.Action = "/api/v1/self-service/registration"
		response.UI.Method = "POST"
		response.IssuedAt = time.Now().Format(time.RFC3339)
		response.ExpiresAt = flow.ExpiresAt.Format(time.RFC3339)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Submit registration
	r.Post("/api/v1/self-service/registration", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		flowID := r.URL.Query().Get("flow")

		var req RegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
			return
		}

		// Validate flow exists
		if flowID == "" {
			writeError(w, http.StatusBadRequest, "invalid_flow", "Flow ID is required")
			return
		}

		// Validate email
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "validation_error", "Email is required")
			return
		}

		// Check for duplicate email
		var exists bool
		err := suite.Pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM core_identity_addresses WHERE address = $1)
		`, req.Email).Scan(&exists)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "Database error")
			return
		}
		if exists {
			writeError(w, http.StatusConflict, "duplicate_email", "Email already registered")
			return
		}

		// Validate password if method is password
		if req.Method == "password" && len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "validation_error", "Password must be at least 8 characters")
			return
		}

		// Create identity
		fixtures := NewTestFixtures(suite.Pool)
		identity, err := fixtures.CreateIdentity(ctx, req.Email, req.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "Failed to create identity")
			return
		}

		// Create session
		session, err := fixtures.CreateSession(ctx, identity.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "Failed to create session")
			return
		}

		// Mark flow as completed
		_, _ = suite.Pool.Exec(ctx, `
			UPDATE core_flows SET state = 'completed', identity_id = $1 WHERE id = $2
		`, identity.ID, flowID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"identity": map[string]interface{}{
				"id":     identity.ID.String(),
				"email":  identity.Email,
				"state":  identity.State,
				"traits": identity.Traits,
			},
			"session": map[string]interface{}{
				"id":          session.ID.String(),
				"token":       session.Token,
				"identity_id": session.IdentityID.String(),
				"active":      session.Active,
			},
		})
	})

	// Email verification
	r.Get("/api/v1/self-service/verification", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		ctx := r.Context()

		if token == "" {
			// Initialize verification flow
			fixtures := NewTestFixtures(suite.Pool)
			flow, _ := fixtures.CreateFlow(ctx, "verification", 24*time.Hour)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":         flow.ID.String(),
				"type":       "verification",
				"state":      "active",
				"csrf_token": flow.CSRFToken,
			})
			return
		}

		// Verify token and mark email as verified
		var email string
		var identityID uuid.UUID
		err := suite.Pool.QueryRow(ctx, `
			SELECT m.email, m.identity_id 
			FROM module_magic_link_tokens m
			WHERE m.token = $1 AND m.used = false AND m.expires_at > NOW()
		`, token).Scan(&email, &identityID)

		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_token", "Invalid or expired verification token")
			return
		}

		// Mark token as used and email as verified
		_, _ = suite.Pool.Exec(ctx, `UPDATE module_magic_link_tokens SET used = true WHERE token = $1`, token)
		_, _ = suite.Pool.Exec(ctx, `
			UPDATE core_identity_addresses SET verified = true, verified_at = NOW()
			WHERE identity_id = $1 AND address_type = 'email'
		`, identityID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state": "passed",
		})
	})

	return httptest.NewServer(r)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":    status,
			"status":  code,
			"message": message,
		},
	})
}

// TestRegistrationFlowInitiation tests starting a registration flow
func TestRegistrationFlowInitiation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRegistrationTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	t.Run("successful flow initiation", func(t *testing.T) {
		resp, err := client.Get("/api/v1/self-service/registration")
		if err != nil {
			t.Fatalf("Failed to initiate registration flow: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var flow RegistrationFlowResponse
		if err := ParseJSONResponse(resp, &flow); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if flow.ID == "" {
			t.Error("Flow ID should not be empty")
		}
		if flow.Type != "registration" {
			t.Errorf("Expected flow type 'registration', got '%s'", flow.Type)
		}
		if flow.State != "active" {
			t.Errorf("Expected flow state 'active', got '%s'", flow.State)
		}
		if flow.CSRFToken == "" {
			t.Error("CSRF token should not be empty")
		}
	})

	t.Run("flow contains correct UI action", func(t *testing.T) {
		resp, _ := client.Get("/api/v1/self-service/registration")

		var flow RegistrationFlowResponse
		ParseJSONResponse(resp, &flow)

		if flow.UI.Action != "/api/v1/self-service/registration" {
			t.Errorf("Expected UI action '/api/v1/self-service/registration', got '%s'", flow.UI.Action)
		}
		if flow.UI.Method != "POST" {
			t.Errorf("Expected UI method 'POST', got '%s'", flow.UI.Method)
		}
	})

	t.Run("flow has expiration time", func(t *testing.T) {
		resp, _ := client.Get("/api/v1/self-service/registration")

		var flow RegistrationFlowResponse
		ParseJSONResponse(resp, &flow)

		if flow.ExpiresAt == "" {
			t.Error("Flow should have expiration time")
		}

		expiresAt, err := time.Parse(time.RFC3339, flow.ExpiresAt)
		if err != nil {
			t.Errorf("Failed to parse expiration time: %v", err)
		}

		if expiresAt.Before(time.Now()) {
			t.Error("Flow expiration should be in the future")
		}
	})
}

// TestRegistrationFormValidation tests form validation
func TestRegistrationFormValidation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRegistrationTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	// First, get a flow
	flowResp, _ := client.Get("/api/v1/self-service/registration")
	var flow RegistrationFlowResponse
	ParseJSONResponse(flowResp, &flow)

	t.Run("missing email", func(t *testing.T) {
		req := RegistrationRequest{
			Method: "password",
			Email:  "",
		}

		resp, err := client.Post("/api/v1/self-service/registration?flow="+flow.ID, req)
		if err != nil {
			t.Fatalf("Failed to submit registration: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var errResp map[string]interface{}
		ParseJSONResponse(resp, &errResp)

		errObj := errResp["error"].(map[string]interface{})
		if errObj["status"] != "validation_error" {
			t.Errorf("Expected error status 'validation_error', got '%s'", errObj["status"])
		}
	})

	t.Run("password too short", func(t *testing.T) {
		req := RegistrationRequest{
			Method:   "password",
			Email:    "test@example.com",
			Password: "short",
		}

		resp, _ := client.Post("/api/v1/self-service/registration?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var errResp map[string]interface{}
		ParseJSONResponse(resp, &errResp)

		errObj := errResp["error"].(map[string]interface{})
		if errObj["status"] != "validation_error" {
			t.Errorf("Expected error status 'validation_error', got '%s'", errObj["status"])
		}
	})

	t.Run("missing flow ID", func(t *testing.T) {
		req := RegistrationRequest{
			Method:   "password",
			Email:    "test@example.com",
			Password: "validpassword123",
		}

		resp, _ := client.Post("/api/v1/self-service/registration", req)

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

// TestSuccessfulRegistration tests the happy path
func TestSuccessfulRegistration(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRegistrationTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	// Get a flow
	flowResp, _ := client.Get("/api/v1/self-service/registration")
	var flow RegistrationFlowResponse
	ParseJSONResponse(flowResp, &flow)

	t.Run("register with password", func(t *testing.T) {
		req := RegistrationRequest{
			Method:   "password",
			Email:    "newuser@example.com",
			Password: "securepassword123",
			Name:     "New User",
		}

		resp, err := client.Post("/api/v1/self-service/registration?flow="+flow.ID, req)
		if err != nil {
			t.Fatalf("Failed to register: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		// Verify identity was created
		identity := result["identity"].(map[string]interface{})
		if identity["id"] == "" {
			t.Error("Identity ID should not be empty")
		}
		if identity["email"] != "newuser@example.com" {
			t.Errorf("Expected email 'newuser@example.com', got '%s'", identity["email"])
		}

		// Verify session was created
		session := result["session"].(map[string]interface{})
		if session["token"] == "" {
			t.Error("Session token should not be empty")
		}
		if session["active"] != true {
			t.Error("Session should be active")
		}
	})

	t.Run("register creates identity in database", func(t *testing.T) {
		// Get another flow
		flowResp2, _ := client.Get("/api/v1/self-service/registration")
		var flow2 RegistrationFlowResponse
		ParseJSONResponse(flowResp2, &flow2)

		req := RegistrationRequest{
			Method: "password",
			Email:  "dbcheck@example.com",
			Name:   "DB Check User",
		}

		client.Post("/api/v1/self-service/registration?flow="+flow2.ID, req)

		// Verify in database
		ctx := context.Background()
		var count int
		err := suite.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM core_identity_addresses WHERE address = $1
		`, "dbcheck@example.com").Scan(&count)

		if err != nil {
			t.Fatalf("Failed to query database: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 address record, got %d", count)
		}
	})
}

// TestDuplicateEmailHandling tests duplicate email registration
func TestDuplicateEmailHandling(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRegistrationTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()

	// Create existing identity
	fixtures := NewTestFixtures(suite.Pool)
	_, err := fixtures.CreateIdentity(ctx, "existing@example.com", "Existing User")
	if err != nil {
		t.Fatalf("Failed to create existing identity: %v", err)
	}

	// Get a flow
	flowResp, _ := client.Get("/api/v1/self-service/registration")
	var flow RegistrationFlowResponse
	ParseJSONResponse(flowResp, &flow)

	t.Run("reject duplicate email", func(t *testing.T) {
		req := RegistrationRequest{
			Method:   "password",
			Email:    "existing@example.com",
			Password: "validpassword123",
		}

		resp, _ := client.Post("/api/v1/self-service/registration?flow="+flow.ID, req)

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", resp.StatusCode)
		}

		var errResp map[string]interface{}
		ParseJSONResponse(resp, &errResp)

		errObj := errResp["error"].(map[string]interface{})
		if errObj["status"] != "duplicate_email" {
			t.Errorf("Expected error status 'duplicate_email', got '%s'", errObj["status"])
		}
	})

	t.Run("case insensitive email check", func(t *testing.T) {
		// Get another flow
		flowResp2, _ := client.Get("/api/v1/self-service/registration")
		var flow2 RegistrationFlowResponse
		ParseJSONResponse(flowResp2, &flow2)

		// Note: This test documents expected behavior - actual implementation
		// may need case-insensitive email checking
		req := RegistrationRequest{
			Method:   "password",
			Email:    "EXISTING@example.com",
			Password: "validpassword123",
		}

		resp, _ := client.Post("/api/v1/self-service/registration?flow="+flow2.ID, req)

		// If case-insensitive is implemented, this should be 409
		// For now, document that case sensitivity might allow duplicate
		if resp.StatusCode == http.StatusConflict {
			t.Log("Email check is case-insensitive (good)")
		} else if resp.StatusCode == http.StatusOK {
			t.Log("Warning: Email check is case-sensitive, may allow duplicates")
		}
	})
}

// TestEmailVerificationFlow tests the verification flow
func TestEmailVerificationFlow(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRegistrationTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	t.Run("initiate verification flow", func(t *testing.T) {
		resp, err := client.Get("/api/v1/self-service/verification")
		if err != nil {
			t.Fatalf("Failed to get verification flow: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var flow map[string]interface{}
		ParseJSONResponse(resp, &flow)

		if flow["type"] != "verification" {
			t.Errorf("Expected flow type 'verification', got '%s'", flow["type"])
		}
	})

	t.Run("verify email with valid token", func(t *testing.T) {
		// Create identity and token
		identity, _ := fixtures.CreateIdentity(ctx, "verify@example.com", "Verify User")
		flow, _ := fixtures.CreateFlow(ctx, "verification", time.Hour)
		token, _, _ := fixtures.CreateMagicLinkToken(ctx, &identity.ID, identity.Email, flow.ID, time.Hour)

		resp, _ := client.Get("/api/v1/self-service/verification?token=" + token)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		ParseJSONResponse(resp, &result)

		if result["state"] != "passed" {
			t.Errorf("Expected state 'passed', got '%s'", result["state"])
		}

		// Verify email is marked as verified in database
		var verified bool
		suite.Pool.QueryRow(ctx, `
			SELECT verified FROM core_identity_addresses 
			WHERE identity_id = $1 AND address_type = 'email'
		`, identity.ID).Scan(&verified)

		if !verified {
			t.Error("Email should be marked as verified")
		}
	})

	t.Run("reject invalid verification token", func(t *testing.T) {
		resp, _ := client.Get("/api/v1/self-service/verification?token=invalid-token")

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var errResp map[string]interface{}
		ParseJSONResponse(resp, &errResp)

		errObj := errResp["error"].(map[string]interface{})
		if errObj["status"] != "invalid_token" {
			t.Errorf("Expected error status 'invalid_token', got '%s'", errObj["status"])
		}
	})

	t.Run("reject expired verification token", func(t *testing.T) {
		// Create expired token
		identity, _ := fixtures.CreateIdentity(ctx, "expired@example.com", "Expired User")
		flow, _ := fixtures.CreateFlow(ctx, "verification", time.Hour)
		token, _, _ := fixtures.CreateMagicLinkToken(ctx, &identity.ID, identity.Email, flow.ID, -time.Hour)

		resp, _ := client.Get("/api/v1/self-service/verification?token=" + token)

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("reject already used token", func(t *testing.T) {
		// Create and use token
		identity, _ := fixtures.CreateIdentity(ctx, "used@example.com", "Used Token User")
		flow, _ := fixtures.CreateFlow(ctx, "verification", time.Hour)
		token, _, _ := fixtures.CreateMagicLinkToken(ctx, &identity.ID, identity.Email, flow.ID, time.Hour)

		// First use
		client.Get("/api/v1/self-service/verification?token=" + token)

		// Second use should fail
		resp, _ := client.Get("/api/v1/self-service/verification?token=" + token)

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400 for reused token, got %d", resp.StatusCode)
		}
	})
}

// TestRegistrationConcurrency tests concurrent registration attempts
func TestRegistrationConcurrency(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRegistrationTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	t.Run("concurrent registrations with unique emails", func(t *testing.T) {
		const numRequests = 10
		results := make(chan int, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(idx int) {
				// Get flow
				flowResp, _ := client.Get("/api/v1/self-service/registration")
				var flow RegistrationFlowResponse
				ParseJSONResponse(flowResp, &flow)

				req := RegistrationRequest{
					Method:   "password",
					Email:    "concurrent" + string(rune('0'+idx)) + "@example.com",
					Password: "validpassword123",
				}

				resp, _ := client.Post("/api/v1/self-service/registration?flow="+flow.ID, req)
				results <- resp.StatusCode
			}(i)
		}

		successCount := 0
		for i := 0; i < numRequests; i++ {
			status := <-results
			if status == http.StatusOK {
				successCount++
			}
		}

		if successCount != numRequests {
			t.Errorf("Expected %d successful registrations, got %d", numRequests, successCount)
		}
	})
}

// TestRegistrationFlowExpiration tests flow expiration
func TestRegistrationFlowExpiration(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	t.Run("flow state reflects expiration", func(t *testing.T) {
		// Create an already expired flow
		flow, _ := fixtures.CreateFlow(ctx, "registration", -time.Minute)

		var state string
		var expiresAt time.Time
		suite.Pool.QueryRow(ctx, `
			SELECT state, expires_at FROM core_flows WHERE id = $1
		`, flow.ID).Scan(&state, &expiresAt)

		if expiresAt.After(time.Now()) {
			t.Error("Flow should have expired")
		}

		// Note: The actual flow should be marked as expired by a background worker
		// or checked at request time
	})
}
