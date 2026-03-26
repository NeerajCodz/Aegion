package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// RecoveryFlowResponse represents a recovery flow
type RecoveryFlowResponse struct {
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

// RecoveryRequest represents a recovery submission
type RecoveryRequest struct {
	Method string `json:"method"`
	Email  string `json:"email"`
	Code   string `json:"code,omitempty"`
	Token  string `json:"token,omitempty"`
}

// RecoveryCodeResponse represents a recovery code validation response
type RecoveryCodeResponse struct {
	Valid     bool   `json:"valid"`
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Message   string `json:"message,omitempty"`
}

// PasswordResetRequest represents a password reset request
type PasswordResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// createRecoveryTestServer creates a mock server for recovery tests
func createRecoveryTestServer(t *testing.T, suite *TestSuite) *httptest.Server {
	t.Helper()

	r := chi.NewRouter()

	// Recovery flow initiation endpoint
	r.Get("/api/v1/self-service/recovery", func(w http.ResponseWriter, r *http.Request) {
		flowID := uuid.New().String()
		now := time.Now()
		expires := now.Add(time.Hour)

		flow := RecoveryFlowResponse{
			ID:        flowID,
			Type:      "browser",
			State:     "choose_method",
			CSRFToken: uuid.New().String(),
			IssuedAt:  now.Format(time.RFC3339),
			ExpiresAt: expires.Format(time.RFC3339),
		}

		flow.UI.Action = "/api/v1/self-service/recovery?flow=" + flowID
		flow.UI.Method = "POST"
		flow.UI.Nodes = []struct {
			Type       string                 `json:"type"`
			Group      string                 `json:"group"`
			Attributes map[string]interface{} `json:"attributes"`
			Messages   []struct {
				ID   int    `json:"id"`
				Text string `json:"text"`
				Type string `json:"type"`
			} `json:"messages"`
		}{
			{
				Type:  "input",
				Group: "default",
				Attributes: map[string]interface{}{
					"name":        "email",
					"type":        "email",
					"placeholder": "Enter your email address",
					"required":    true,
				},
			},
			{
				Type:  "input",
				Group: "default",
				Attributes: map[string]interface{}{
					"name":  "method",
					"type":  "hidden",
					"value": "link",
				},
			},
		}

		// Store flow in database
		_, err := suite.Pool.Exec(context.Background(),
			`INSERT INTO flows (id, type, state, csrf_token, issued_at, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			flowID, "recovery", "choose_method", flow.CSRFToken, now, expires)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create flow"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(flow)
	})

	// Recovery submission endpoint
	r.Post("/api/v1/self-service/recovery", func(w http.ResponseWriter, r *http.Request) {
		flowID := r.URL.Query().Get("flow")
		if flowID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "flow parameter required"})
			return
		}

		var req RecoveryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		// Verify flow exists and is valid
		var flowExists bool
		err := suite.Pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM flows WHERE id = $1 AND type = 'recovery' AND expires_at > NOW())`,
			flowID).Scan(&flowExists)

		if err != nil || !flowExists {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired flow"})
			return
		}

		// Check if identity exists
		var identityID string
		err = suite.Pool.QueryRow(context.Background(),
			`SELECT id FROM identities WHERE email = $1`, req.Email).Scan(&identityID)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "email not found"})
			return
		}

		// Generate recovery code
		recoveryCode := "RECOVERY123"
		recoveryToken := uuid.New().String()
		expiresAt := time.Now().Add(15 * time.Minute)

		// Store recovery code
		_, err = suite.Pool.Exec(context.Background(),
			`INSERT INTO recovery_codes (id, identity_id, code, token, expires_at, used)
			 VALUES ($1, $2, $3, $4, $5, false)`,
			uuid.New(), identityID, recoveryCode, recoveryToken, expiresAt)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create recovery code"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Recovery code sent to email",
			"code":    recoveryCode, // In real implementation, this would be sent via email
		})
	})

	// Recovery code validation endpoint
	r.Post("/api/v1/self-service/recovery/validate", func(w http.ResponseWriter, r *http.Request) {
		var req RecoveryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		var token string
		var expiresAt time.Time
		err := suite.Pool.QueryRow(context.Background(),
			`SELECT token, expires_at FROM recovery_codes 
			 WHERE code = $1 AND used = false AND expires_at > NOW()`,
			req.Code).Scan(&token, &expiresAt)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(RecoveryCodeResponse{
				Valid:   false,
				Message: "Invalid or expired recovery code",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RecoveryCodeResponse{
			Valid:     true,
			Token:     token,
			ExpiresAt: expiresAt.Format(time.RFC3339),
			Message:   "Recovery code is valid",
		})
	})

	// Password reset endpoint
	r.Post("/api/v1/self-service/recovery/reset", func(w http.ResponseWriter, r *http.Request) {
		var req PasswordResetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		// Validate token and get identity
		var identityID string
		err := suite.Pool.QueryRow(context.Background(),
			`SELECT identity_id FROM recovery_codes 
			 WHERE token = $1 AND used = false AND expires_at > NOW()`,
			req.Token).Scan(&identityID)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired token"})
			return
		}

		// Hash new password
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
			return
		}

		// Update password
		_, err = suite.Pool.Exec(context.Background(),
			`UPDATE identity_credentials 
			 SET password_hash = $1, updated_at = NOW()
			 WHERE identity_id = $2 AND type = 'password'`,
			string(passwordHash), identityID)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to update password"})
			return
		}

		// Mark recovery code as used
		_, err = suite.Pool.Exec(context.Background(),
			`UPDATE recovery_codes SET used = true WHERE token = $1`, req.Token)

		if err != nil {
			t.Logf("Failed to mark recovery code as used: %v", err)
		}

		// Invalidate all active sessions for this identity
		_, err = suite.Pool.Exec(context.Background(),
			`UPDATE sessions SET active = false WHERE identity_id = $1`, identityID)

		if err != nil {
			t.Logf("Failed to invalidate sessions: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Password reset successfully",
		})
	})

	return httptest.NewServer(r)
}

func TestRecoveryFlowInitiation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRecoveryTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)

	t.Run("initiate recovery flow", func(t *testing.T) {
		resp, err := client.Get("/api/v1/self-service/recovery")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var flow RecoveryFlowResponse
		if err := ParseJSONResponse(resp, &flow); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if flow.ID == "" {
			t.Error("Expected flow ID to be set")
		}

		if flow.Type != "browser" {
			t.Errorf("Expected flow type 'browser', got %s", flow.Type)
		}

		if flow.State != "choose_method" {
			t.Errorf("Expected flow state 'choose_method', got %s", flow.State)
		}

		if flow.CSRFToken == "" {
			t.Error("Expected CSRF token to be set")
		}

		if !strings.Contains(flow.UI.Action, flow.ID) {
			t.Error("Expected UI action to contain flow ID")
		}

		if len(flow.UI.Nodes) == 0 {
			t.Error("Expected UI nodes to be present")
		}

		// Check for email input node
		hasEmailNode := false
		for _, node := range flow.UI.Nodes {
			if node.Attributes["name"] == "email" {
				hasEmailNode = true
				break
			}
		}
		if !hasEmailNode {
			t.Error("Expected email input node to be present")
		}
	})
}

func TestRecoveryCodeValidation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRecoveryTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "recovery@example.com", "Recovery User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	// Create recovery flow and get code
	flowResp, _ := client.Get("/api/v1/self-service/recovery")
	var flow RecoveryFlowResponse
	ParseJSONResponse(flowResp, &flow)

	// Submit recovery request
	recoveryReq := RecoveryRequest{
		Method: "link",
		Email:  identity.Email,
	}

	submitResp, _ := client.Post("/api/v1/self-service/recovery?flow="+flow.ID, recoveryReq)
	var submitResult map[string]interface{}
	ParseJSONResponse(submitResp, &submitResult)

	recoveryCode := submitResult["code"].(string)

	t.Run("validate correct recovery code", func(t *testing.T) {
		validateReq := RecoveryRequest{
			Code: recoveryCode,
		}

		resp, err := client.Post("/api/v1/self-service/recovery/validate", validateReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var validateResp RecoveryCodeResponse
		if err := ParseJSONResponse(resp, &validateResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !validateResp.Valid {
			t.Error("Expected recovery code to be valid")
		}

		if validateResp.Token == "" {
			t.Error("Expected recovery token to be returned")
		}

		if validateResp.ExpiresAt == "" {
			t.Error("Expected expiry time to be returned")
		}
	})

	t.Run("validate incorrect recovery code", func(t *testing.T) {
		validateReq := RecoveryRequest{
			Code: "INVALID123",
		}

		resp, err := client.Post("/api/v1/self-service/recovery/validate", validateReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var validateResp RecoveryCodeResponse
		if err := ParseJSONResponse(resp, &validateResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if validateResp.Valid {
			t.Error("Expected recovery code to be invalid")
		}

		if validateResp.Message == "" {
			t.Error("Expected error message to be returned")
		}
	})

	t.Run("validate expired recovery code", func(t *testing.T) {
		// Create an expired recovery code
		expiredCode := "EXPIRED123"
		expiredToken := uuid.New().String()
		pastTime := time.Now().Add(-1 * time.Hour)

		_, err = suite.Pool.Exec(ctx,
			`INSERT INTO recovery_codes (id, identity_id, code, token, expires_at, used)
			 VALUES ($1, $2, $3, $4, $5, false)`,
			uuid.New(), identity.ID, expiredCode, expiredToken, pastTime)

		if err != nil {
			t.Fatalf("Failed to create expired recovery code: %v", err)
		}

		validateReq := RecoveryRequest{
			Code: expiredCode,
		}

		resp, err := client.Post("/api/v1/self-service/recovery/validate", validateReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var validateResp RecoveryCodeResponse
		if err := ParseJSONResponse(resp, &validateResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if validateResp.Valid {
			t.Error("Expected expired recovery code to be invalid")
		}
	})
}

func TestPasswordReset(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRecoveryTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity
	oldPassword := "oldpassword123"
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte(oldPassword), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "reset@example.com", "Reset User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	// Create active session
	session, err := fixtures.CreateSession(ctx, identity.ID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Get recovery token through the flow
	flowResp, _ := client.Get("/api/v1/self-service/recovery")
	var flow RecoveryFlowResponse
	ParseJSONResponse(flowResp, &flow)

	recoveryReq := RecoveryRequest{
		Method: "link",
		Email:  identity.Email,
	}

	submitResp, _ := client.Post("/api/v1/self-service/recovery?flow="+flow.ID, recoveryReq)
	var submitResult map[string]interface{}
	ParseJSONResponse(submitResp, &submitResult)

	recoveryCode := submitResult["code"].(string)

	// Validate code to get token
	validateReq := RecoveryRequest{Code: recoveryCode}
	validateResp, _ := client.Post("/api/v1/self-service/recovery/validate", validateReq)
	var validateResult RecoveryCodeResponse
	ParseJSONResponse(validateResp, &validateResult)

	recoveryToken := validateResult.Token

	t.Run("successful password reset", func(t *testing.T) {
		newPassword := "newpassword456"
		resetReq := PasswordResetRequest{
			Token:       recoveryToken,
			NewPassword: newPassword,
		}

		resp, err := client.Post("/api/v1/self-service/recovery/reset", resetReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var resetResp map[string]interface{}
		if err := ParseJSONResponse(resp, &resetResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !resetResp["success"].(bool) {
			t.Error("Expected password reset to succeed")
		}

		// Verify password was updated
		var newHash string
		err = suite.Pool.QueryRow(ctx,
			`SELECT password_hash FROM identity_credentials 
			 WHERE identity_id = $1 AND type = 'password'`,
			identity.ID).Scan(&newHash)

		if err != nil {
			t.Fatalf("Failed to query password hash: %v", err)
		}

		// Verify new password works
		err = bcrypt.CompareHashAndPassword([]byte(newHash), []byte(newPassword))
		if err != nil {
			t.Error("New password does not match hash")
		}

		// Verify old password doesn't work
		err = bcrypt.CompareHashAndPassword([]byte(newHash), []byte(oldPassword))
		if err == nil {
			t.Error("Old password should not match new hash")
		}

		// Verify session was invalidated
		var sessionActive bool
		err = suite.Pool.QueryRow(ctx,
			`SELECT active FROM sessions WHERE id = $1`, session.ID).Scan(&sessionActive)

		if err != nil {
			t.Fatalf("Failed to query session: %v", err)
		}

		if sessionActive {
			t.Error("Expected session to be invalidated after password reset")
		}
	})

	t.Run("reset with invalid token", func(t *testing.T) {
		invalidToken := uuid.New().String()
		resetReq := PasswordResetRequest{
			Token:       invalidToken,
			NewPassword: "newpassword789",
		}

		resp, err := client.Post("/api/v1/self-service/recovery/reset", resetReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var resetResp map[string]string
		if err := ParseJSONResponse(resp, &resetResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(resetResp["error"], "invalid or expired token") {
			t.Errorf("Expected invalid token error, got: %s", resetResp["error"])
		}
	})

	t.Run("reset with empty password", func(t *testing.T) {
		// Get a new recovery token
		flowResp2, _ := client.Get("/api/v1/self-service/recovery")
		var flow2 RecoveryFlowResponse
		ParseJSONResponse(flowResp2, &flow2)

		recoveryReq2 := RecoveryRequest{
			Method: "link",
			Email:  identity.Email,
		}

		submitResp2, _ := client.Post("/api/v1/self-service/recovery?flow="+flow2.ID, recoveryReq2)
		var submitResult2 map[string]interface{}
		ParseJSONResponse(submitResp2, &submitResult2)

		recoveryCode2 := submitResult2["code"].(string)
		validateReq2 := RecoveryRequest{Code: recoveryCode2}
		validateResp2, _ := client.Post("/api/v1/self-service/recovery/validate", validateReq2)
		var validateResult2 RecoveryCodeResponse
		ParseJSONResponse(validateResp2, &validateResult2)

		resetReq := PasswordResetRequest{
			Token:       validateResult2.Token,
			NewPassword: "",
		}

		resp, err := client.Post("/api/v1/self-service/recovery/reset", resetReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		// Should still succeed but we could add validation
		if resp.StatusCode == http.StatusOK {
			t.Log("Password reset succeeded with empty password - consider adding validation")
		}
	})
}

func TestRecoveryWithInvalidToken(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createRecoveryTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "invalid@example.com", "Invalid User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	t.Run("recovery with non-existent email", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/recovery")
		var flow RecoveryFlowResponse
		ParseJSONResponse(flowResp, &flow)

		recoveryReq := RecoveryRequest{
			Method: "link",
			Email:  "nonexistent@example.com",
		}

		resp, err := client.Post("/api/v1/self-service/recovery?flow="+flow.ID, recoveryReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var errorResp map[string]string
		if err := ParseJSONResponse(resp, &errorResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(errorResp["error"], "email not found") {
			t.Errorf("Expected 'email not found' error, got: %s", errorResp["error"])
		}
	})

	t.Run("recovery with invalid flow ID", func(t *testing.T) {
		fakeFlowID := uuid.New().String()
		recoveryReq := RecoveryRequest{
			Method: "link",
			Email:  identity.Email,
		}

		resp, err := client.Post("/api/v1/self-service/recovery?flow="+fakeFlowID, recoveryReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var errorResp map[string]string
		if err := ParseJSONResponse(resp, &errorResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(errorResp["error"], "invalid or expired flow") {
			t.Errorf("Expected 'invalid or expired flow' error, got: %s", errorResp["error"])
		}
	})

	t.Run("recovery with missing flow parameter", func(t *testing.T) {
		recoveryReq := RecoveryRequest{
			Method: "link",
			Email:  identity.Email,
		}

		resp, err := client.Post("/api/v1/self-service/recovery", recoveryReq)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var errorResp map[string]string
		if err := ParseJSONResponse(resp, &errorResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(errorResp["error"], "flow parameter required") {
			t.Errorf("Expected 'flow parameter required' error, got: %s", errorResp["error"])
		}
	})

	t.Run("recovery with malformed request", func(t *testing.T) {
		flowResp, _ := client.Get("/api/v1/self-service/recovery")
		var flow RecoveryFlowResponse
		ParseJSONResponse(flowResp, &flow)

		// Send malformed JSON
		resp, err := client.PostRaw("/api/v1/self-service/recovery?flow="+flow.ID, "invalid json")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("validation with malformed request", func(t *testing.T) {
		// Send malformed JSON to validation endpoint
		resp, err := client.PostRaw("/api/v1/self-service/recovery/validate", "invalid json")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("password reset with malformed request", func(t *testing.T) {
		// Send malformed JSON to reset endpoint
		resp, err := client.PostRaw("/api/v1/self-service/recovery/reset", "invalid json")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}