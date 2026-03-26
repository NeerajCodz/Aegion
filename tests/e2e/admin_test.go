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

// AdminCredentials represents admin login credentials
type AdminCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AdminSession represents an admin session
type AdminSession struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
}

// IdentityRequest represents identity management request
type IdentityRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password,omitempty"`
	Active   *bool  `json:"active,omitempty"`
}

// AdminSessionRequest represents admin session operation request
type AdminSessionRequest struct {
	IdentityID string `json:"identity_id"`
	Action     string `json:"action"` // "revoke", "refresh", "list"
}

// PermissionRequest represents permission check request
type PermissionRequest struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

// createAdminTestServer creates a mock server for admin tests
func createAdminTestServer(t *testing.T, suite *TestSuite) *httptest.Server {
	t.Helper()

	r := chi.NewRouter()

	// Admin authentication middleware
	adminAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if !strings.HasPrefix(token, "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid token"})
				return
			}

			token = strings.TrimPrefix(token, "Bearer ")

			// Verify admin token
			var userID, role string
			err := suite.Pool.QueryRow(context.Background(),
				`SELECT user_id, role FROM admin_sessions 
				 WHERE token = $1 AND expires_at > NOW() AND active = true`,
				token).Scan(&userID, &role)

			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired token"})
				return
			}

			if role != "admin" && role != "operator" {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "insufficient permissions"})
				return
			}

			// Add user info to context
			ctx := r.Context()
			ctx = context.WithValue(ctx, "userID", userID)
			ctx = context.WithValue(ctx, "role", role)
			r = r.WithContext(ctx)

			next(w, r)
		}
	}

	// Admin login endpoint
	r.Post("/api/v1/admin/login", func(w http.ResponseWriter, r *http.Request) {
		var creds AdminCredentials
		if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		// Verify admin credentials
		var userID, passwordHash, role string
		err := suite.Pool.QueryRow(context.Background(),
			`SELECT id, password_hash, role FROM admin_users 
			 WHERE username = $1 AND active = true`,
			creds.Username).Scan(&userID, &passwordHash, &role)

		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
			return
		}

		// Verify password
		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(creds.Password)); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
			return
		}

		// Create admin session
		token := uuid.New().String()
		expiresAt := time.Now().Add(8 * time.Hour)

		_, err = suite.Pool.Exec(context.Background(),
			`INSERT INTO admin_sessions (id, user_id, token, expires_at, active, role)
			 VALUES ($1, $2, $3, $4, true, $5)`,
			uuid.New(), userID, token, expiresAt, role)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to create session"})
			return
		}

		session := AdminSession{
			Token:     token,
			ExpiresAt: expiresAt,
			UserID:    userID,
			Role:      role,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	})

	// Identity management endpoints
	r.Route("/api/v1/admin/identities", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return adminAuth(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})

		// Create identity
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			var req IdentityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
				return
			}

			// Check if email already exists
			var exists bool
			err := suite.Pool.QueryRow(context.Background(),
				`SELECT EXISTS(SELECT 1 FROM identities WHERE email = $1)`,
				req.Email).Scan(&exists)

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
				return
			}

			if exists {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"error": "email already exists"})
				return
			}

			// Create identity
			identityID := uuid.New()
			_, err = suite.Pool.Exec(context.Background(),
				`INSERT INTO identities (id, email, name, created_at, updated_at)
				 VALUES ($1, $2, $3, NOW(), NOW())`,
				identityID, req.Email, req.Name)

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to create identity"})
				return
			}

			// Create password credential if provided
			if req.Password != "" {
				passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
					return
				}

				_, err = suite.Pool.Exec(context.Background(),
					`INSERT INTO identity_credentials (id, identity_id, type, password_hash, created_at, updated_at)
					 VALUES ($1, $2, 'password', $3, NOW(), NOW())`,
					uuid.New(), identityID, string(passwordHash))

				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": "failed to create credentials"})
					return
				}
			}

			identity := Identity{
				ID:    identityID,
				Email: req.Email,
				Name:  req.Name,
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(identity)
		})

		// Get identity
		r.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
			identityID := chi.URLParam(r, "id")

			var identity Identity
			err := suite.Pool.QueryRow(context.Background(),
				`SELECT id, email, name, created_at, updated_at FROM identities WHERE id = $1`,
				identityID).Scan(&identity.ID, &identity.Email, &identity.Name,
				&identity.CreatedAt, &identity.UpdatedAt)

			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "identity not found"})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(identity)
		})

		// Update identity
		r.Put("/{id}", func(w http.ResponseWriter, r *http.Request) {
			identityID := chi.URLParam(r, "id")

			var req IdentityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
				return
			}

			// Update identity
			_, err := suite.Pool.Exec(context.Background(),
				`UPDATE identities SET email = $1, name = $2, updated_at = NOW() 
				 WHERE id = $3`,
				req.Email, req.Name, identityID)

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to update identity"})
				return
			}

			// Update password if provided
			if req.Password != "" {
				passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
					return
				}

				_, err = suite.Pool.Exec(context.Background(),
					`UPDATE identity_credentials SET password_hash = $1, updated_at = NOW()
					 WHERE identity_id = $2 AND type = 'password'`,
					string(passwordHash), identityID)

				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{"error": "failed to update password"})
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
		})

		// Delete identity
		r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
			identityID := chi.URLParam(r, "id")

			// Check admin role
			role := r.Context().Value("role").(string)
			if role != "admin" {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "admin role required"})
				return
			}

			_, err := suite.Pool.Exec(context.Background(),
				`DELETE FROM identities WHERE id = $1`, identityID)

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to delete identity"})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
		})
	})

	// Admin session management
	r.Post("/api/v1/admin/sessions", adminAuth(func(w http.ResponseWriter, r *http.Request) {
		var req AdminSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		switch req.Action {
		case "revoke":
			_, err := suite.Pool.Exec(context.Background(),
				`UPDATE sessions SET active = false WHERE identity_id = $1`,
				req.IdentityID)

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to revoke sessions"})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Sessions revoked successfully",
			})

		case "list":
			rows, err := suite.Pool.Query(context.Background(),
				`SELECT id, token, aal, expires_at, active FROM sessions 
				 WHERE identity_id = $1 ORDER BY created_at DESC`,
				req.IdentityID)

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "failed to list sessions"})
				return
			}
			defer rows.Close()

			var sessions []map[string]interface{}
			for rows.Next() {
				var id, token, aal string
				var expiresAt time.Time
				var active bool

				err := rows.Scan(&id, &token, &aal, &expiresAt, &active)
				if err != nil {
					continue
				}

				sessions = append(sessions, map[string]interface{}{
					"id":         id,
					"token":      token,
					"aal":        aal,
					"expires_at": expiresAt,
					"active":     active,
				})
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"sessions": sessions,
				"total":    len(sessions),
			})

		default:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid action"})
		}
	}))

	// Permission check endpoint
	r.Post("/api/v1/admin/permissions/check", adminAuth(func(w http.ResponseWriter, r *http.Request) {
		var req PermissionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		role := r.Context().Value("role").(string)
		userID := r.Context().Value("userID").(string)

		// Simple permission logic
		allowed := false
		switch role {
		case "admin":
			allowed = true // Admins can do everything
		case "operator":
			// Operators have limited permissions
			if req.Resource == "identities" && (req.Action == "read" || req.Action == "update") {
				allowed = true
			}
			if req.Resource == "sessions" && (req.Action == "read" || req.Action == "revoke") {
				allowed = true
			}
		}

		response := map[string]interface{}{
			"allowed":  allowed,
			"resource": req.Resource,
			"action":   req.Action,
			"user_id":  userID,
			"role":     role,
		}

		if !allowed {
			response["reason"] = "insufficient permissions for this action"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))

	return httptest.NewServer(r)
}

func TestAdminAuthentication(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createAdminTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()

	// Create admin user
	adminPassword := "admin123"
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	adminUserID := uuid.New()

	_, err := suite.Pool.Exec(ctx,
		`INSERT INTO admin_users (id, username, password_hash, role, active, created_at)
		 VALUES ($1, $2, $3, $4, true, NOW())`,
		adminUserID, "admin", string(passwordHash), "admin")

	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	t.Run("successful admin login", func(t *testing.T) {
		creds := AdminCredentials{
			Username: "admin",
			Password: adminPassword,
		}

		resp, err := client.Post("/api/v1/admin/login", creds)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var session AdminSession
		if err := ParseJSONResponse(resp, &session); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if session.Token == "" {
			t.Error("Expected admin token to be set")
		}

		if session.UserID != adminUserID.String() {
			t.Errorf("Expected user ID %s, got %s", adminUserID.String(), session.UserID)
		}

		if session.Role != "admin" {
			t.Errorf("Expected role 'admin', got %s", session.Role)
		}

		if session.ExpiresAt.Before(time.Now()) {
			t.Error("Expected expiry time to be in the future")
		}
	})

	t.Run("login with invalid credentials", func(t *testing.T) {
		creds := AdminCredentials{
			Username: "admin",
			Password: "wrongpassword",
		}

		resp, err := client.Post("/api/v1/admin/login", creds)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		var errorResp map[string]string
		if err := ParseJSONResponse(resp, &errorResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(errorResp["error"], "invalid credentials") {
			t.Errorf("Expected 'invalid credentials' error, got: %s", errorResp["error"])
		}
	})

	t.Run("login with non-existent user", func(t *testing.T) {
		creds := AdminCredentials{
			Username: "nonexistent",
			Password: "password",
		}

		resp, err := client.Post("/api/v1/admin/login", creds)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})
}

func TestIdentityManagement(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createAdminTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()

	// Create and login admin user
	adminPassword := "admin123"
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	adminUserID := uuid.New()

	_, err := suite.Pool.Exec(ctx,
		`INSERT INTO admin_users (id, username, password_hash, role, active, created_at)
		 VALUES ($1, $2, $3, $4, true, NOW())`,
		adminUserID, "admin", string(passwordHash), "admin")

	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	// Login to get admin token
	creds := AdminCredentials{Username: "admin", Password: adminPassword}
	loginResp, _ := client.Post("/api/v1/admin/login", creds)
	var session AdminSession
	ParseJSONResponse(loginResp, &session)

	// Set auth header for subsequent requests
	authHeaders := map[string]string{
		"Authorization": "Bearer " + session.Token,
	}

	t.Run("create identity", func(t *testing.T) {
		identityReq := IdentityRequest{
			Email:    "newuser@example.com",
			Name:     "New User",
			Password: "password123",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/identities", identityReq, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var identity Identity
		if err := ParseJSONResponse(resp, &identity); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if identity.Email != identityReq.Email {
			t.Errorf("Expected email %s, got %s", identityReq.Email, identity.Email)
		}

		if identity.Name != identityReq.Name {
			t.Errorf("Expected name %s, got %s", identityReq.Name, identity.Name)
		}

		if identity.ID == uuid.Nil {
			t.Error("Expected identity ID to be set")
		}

		// Verify password credential was created
		var credExists bool
		err = suite.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM identity_credentials 
			 WHERE identity_id = $1 AND type = 'password')`,
			identity.ID).Scan(&credExists)

		if err != nil {
			t.Fatalf("Failed to check credentials: %v", err)
		}

		if !credExists {
			t.Error("Expected password credential to be created")
		}
	})

	t.Run("create duplicate email", func(t *testing.T) {
		// First create an identity
		identityReq1 := IdentityRequest{
			Email: "duplicate@example.com",
			Name:  "First User",
		}
		client.PostWithHeaders("/api/v1/admin/identities", identityReq1, authHeaders)

		// Try to create another with same email
		identityReq2 := IdentityRequest{
			Email: "duplicate@example.com",
			Name:  "Second User",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/identities", identityReq2, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", resp.StatusCode)
		}

		var errorResp map[string]string
		if err := ParseJSONResponse(resp, &errorResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(errorResp["error"], "email already exists") {
			t.Errorf("Expected 'email already exists' error, got: %s", errorResp["error"])
		}
	})

	t.Run("get identity", func(t *testing.T) {
		// Create identity first
		identityReq := IdentityRequest{
			Email: "getuser@example.com",
			Name:  "Get User",
		}
		createResp, _ := client.PostWithHeaders("/api/v1/admin/identities", identityReq, authHeaders)
		var createdIdentity Identity
		ParseJSONResponse(createResp, &createdIdentity)

		// Get the identity
		resp, err := client.GetWithHeaders("/api/v1/admin/identities/"+createdIdentity.ID.String(), authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var identity Identity
		if err := ParseJSONResponse(resp, &identity); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if identity.ID != createdIdentity.ID {
			t.Errorf("Expected ID %s, got %s", createdIdentity.ID.String(), identity.ID.String())
		}

		if identity.Email != identityReq.Email {
			t.Errorf("Expected email %s, got %s", identityReq.Email, identity.Email)
		}
	})

	t.Run("update identity", func(t *testing.T) {
		// Create identity first
		identityReq := IdentityRequest{
			Email: "updateuser@example.com",
			Name:  "Update User",
		}
		createResp, _ := client.PostWithHeaders("/api/v1/admin/identities", identityReq, authHeaders)
		var createdIdentity Identity
		ParseJSONResponse(createResp, &createdIdentity)

		// Update the identity
		updateReq := IdentityRequest{
			Email:    "updated@example.com",
			Name:     "Updated User",
			Password: "newpassword123",
		}

		resp, err := client.PutWithHeaders("/api/v1/admin/identities/"+createdIdentity.ID.String(), updateReq, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify the update
		var email, name string
		err = suite.Pool.QueryRow(ctx,
			`SELECT email, name FROM identities WHERE id = $1`,
			createdIdentity.ID).Scan(&email, &name)

		if err != nil {
			t.Fatalf("Failed to query identity: %v", err)
		}

		if email != updateReq.Email {
			t.Errorf("Expected updated email %s, got %s", updateReq.Email, email)
		}

		if name != updateReq.Name {
			t.Errorf("Expected updated name %s, got %s", updateReq.Name, name)
		}
	})

	t.Run("delete identity", func(t *testing.T) {
		// Create identity first
		identityReq := IdentityRequest{
			Email: "deleteuser@example.com",
			Name:  "Delete User",
		}
		createResp, _ := client.PostWithHeaders("/api/v1/admin/identities", identityReq, authHeaders)
		var createdIdentity Identity
		ParseJSONResponse(createResp, &createdIdentity)

		// Delete the identity
		resp, err := client.DeleteWithHeaders("/api/v1/admin/identities/"+createdIdentity.ID.String(), authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Verify deletion
		var exists bool
		err = suite.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM identities WHERE id = $1)`,
			createdIdentity.ID).Scan(&exists)

		if err != nil {
			t.Fatalf("Failed to check identity existence: %v", err)
		}

		if exists {
			t.Error("Expected identity to be deleted")
		}
	})

	t.Run("unauthorized access", func(t *testing.T) {
		// Try to access without auth header
		resp, err := client.Get("/api/v1/admin/identities/" + uuid.New().String())
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})
}

func TestSessionManagementByAdmin(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createAdminTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create admin user and login
	adminPassword := "admin123"
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	adminUserID := uuid.New()

	_, err := suite.Pool.Exec(ctx,
		`INSERT INTO admin_users (id, username, password_hash, role, active, created_at)
		 VALUES ($1, $2, $3, $4, true, NOW())`,
		adminUserID, "admin", string(passwordHash), "admin")

	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	creds := AdminCredentials{Username: "admin", Password: adminPassword}
	loginResp, _ := client.Post("/api/v1/admin/login", creds)
	var adminSession AdminSession
	ParseJSONResponse(loginResp, &adminSession)

	authHeaders := map[string]string{
		"Authorization": "Bearer " + adminSession.Token,
	}

	// Create test identity and sessions
	userPasswordHash, _ := bcrypt.GenerateFromPassword([]byte("userpass"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "user@example.com", "Test User", string(userPasswordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	_, _ = fixtures.CreateSession(ctx, identity.ID)
	_, _ = fixtures.CreateSession(ctx, identity.ID)

	t.Run("list sessions for identity", func(t *testing.T) {
		req := AdminSessionRequest{
			IdentityID: identity.ID.String(),
			Action:     "list",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/sessions", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var listResp map[string]interface{}
		if err := ParseJSONResponse(resp, &listResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		sessions := listResp["sessions"].([]interface{})
		total := int(listResp["total"].(float64))

		if total != 2 {
			t.Errorf("Expected 2 sessions, got %d", total)
		}

		if len(sessions) != 2 {
			t.Errorf("Expected 2 sessions in list, got %d", len(sessions))
		}

		// Verify session data
		for _, sessionData := range sessions {
			session := sessionData.(map[string]interface{})
			if session["active"] != true {
				t.Error("Expected all sessions to be active")
			}
		}
	})

	t.Run("revoke sessions for identity", func(t *testing.T) {
		req := AdminSessionRequest{
			IdentityID: identity.ID.String(),
			Action:     "revoke",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/sessions", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var revokeResp map[string]interface{}
		if err := ParseJSONResponse(resp, &revokeResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !revokeResp["success"].(bool) {
			t.Error("Expected session revocation to succeed")
		}

		// Verify sessions are inactive
		var activeCount int
		err = suite.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sessions WHERE identity_id = $1 AND active = true`,
			identity.ID).Scan(&activeCount)

		if err != nil {
			t.Fatalf("Failed to count active sessions: %v", err)
		}

		if activeCount != 0 {
			t.Errorf("Expected 0 active sessions, got %d", activeCount)
		}
	})

	t.Run("invalid session action", func(t *testing.T) {
		req := AdminSessionRequest{
			IdentityID: identity.ID.String(),
			Action:     "invalid",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/sessions", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})
}

func TestOperatorPermissions(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createAdminTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()

	// Create operator user
	operatorPassword := "operator123"
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte(operatorPassword), bcrypt.DefaultCost)
	operatorUserID := uuid.New()

	_, err := suite.Pool.Exec(ctx,
		`INSERT INTO admin_users (id, username, password_hash, role, active, created_at)
		 VALUES ($1, $2, $3, $4, true, NOW())`,
		operatorUserID, "operator", string(passwordHash), "operator")

	if err != nil {
		t.Fatalf("Failed to create operator user: %v", err)
	}

	// Login as operator
	creds := AdminCredentials{Username: "operator", Password: operatorPassword}
	loginResp, _ := client.Post("/api/v1/admin/login", creds)
	var operatorSession AdminSession
	ParseJSONResponse(loginResp, &operatorSession)

	authHeaders := map[string]string{
		"Authorization": "Bearer " + operatorSession.Token,
	}

	t.Run("operator permissions - read identities", func(t *testing.T) {
		req := PermissionRequest{
			Resource: "identities",
			Action:   "read",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/permissions/check", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var permResp map[string]interface{}
		if err := ParseJSONResponse(resp, &permResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !permResp["allowed"].(bool) {
			t.Error("Expected operator to have read permissions for identities")
		}

		if permResp["role"].(string) != "operator" {
			t.Errorf("Expected role 'operator', got %s", permResp["role"].(string))
		}
	})

	t.Run("operator permissions - update identities", func(t *testing.T) {
		req := PermissionRequest{
			Resource: "identities",
			Action:   "update",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/permissions/check", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var permResp map[string]interface{}
		ParseJSONResponse(resp, &permResp)

		if !permResp["allowed"].(bool) {
			t.Error("Expected operator to have update permissions for identities")
		}
	})

	t.Run("operator permissions - delete identities (denied)", func(t *testing.T) {
		req := PermissionRequest{
			Resource: "identities",
			Action:   "delete",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/permissions/check", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var permResp map[string]interface{}
		ParseJSONResponse(resp, &permResp)

		if permResp["allowed"].(bool) {
			t.Error("Expected operator to not have delete permissions for identities")
		}

		if permResp["reason"] == nil {
			t.Error("Expected reason for denied permission")
		}
	})

	t.Run("operator permissions - session operations", func(t *testing.T) {
		req := PermissionRequest{
			Resource: "sessions",
			Action:   "revoke",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/permissions/check", req, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var permResp map[string]interface{}
		ParseJSONResponse(resp, &permResp)

		if !permResp["allowed"].(bool) {
			t.Error("Expected operator to have revoke permissions for sessions")
		}
	})

	t.Run("operator cannot delete identity", func(t *testing.T) {
		// Try to delete an identity - should fail
		fakeIdentityID := uuid.New().String()
		resp, err := client.DeleteWithHeaders("/api/v1/admin/identities/"+fakeIdentityID, authHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}

		var errorResp map[string]string
		if err := ParseJSONResponse(resp, &errorResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !strings.Contains(errorResp["error"], "admin role required") {
			t.Errorf("Expected 'admin role required' error, got: %s", errorResp["error"])
		}
	})

	// Test admin permissions for comparison
	t.Run("create admin user for permission comparison", func(t *testing.T) {
		adminPassword := "admin123"
		adminPasswordHash, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		adminUserID := uuid.New()

		_, err := suite.Pool.Exec(ctx,
			`INSERT INTO admin_users (id, username, password_hash, role, active, created_at)
			 VALUES ($1, $2, $3, $4, true, NOW())`,
			adminUserID, "admin", string(adminPasswordHash), "admin")

		if err != nil {
			t.Fatalf("Failed to create admin user: %v", err)
		}

		// Login as admin
		adminCreds := AdminCredentials{Username: "admin", Password: adminPassword}
		adminLoginResp, _ := client.Post("/api/v1/admin/login", adminCreds)
		var adminSession AdminSession
		ParseJSONResponse(adminLoginResp, &adminSession)

		adminAuthHeaders := map[string]string{
			"Authorization": "Bearer " + adminSession.Token,
		}

		// Test admin permissions
		req := PermissionRequest{
			Resource: "identities",
			Action:   "delete",
		}

		resp, err := client.PostWithHeaders("/api/v1/admin/permissions/check", req, adminAuthHeaders)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var permResp map[string]interface{}
		ParseJSONResponse(resp, &permResp)

		if !permResp["allowed"].(bool) {
			t.Error("Expected admin to have delete permissions for identities")
		}

		if permResp["role"].(string) != "admin" {
			t.Errorf("Expected role 'admin', got %s", permResp["role"].(string))
		}
	})
}
