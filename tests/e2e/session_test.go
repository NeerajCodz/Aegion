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

// SessionResponse represents session data
type SessionResponse struct {
	ID         string    `json:"id"`
	Token      string    `json:"token"`
	IdentityID string    `json:"identity_id"`
	AAL        string    `json:"aal"`
	ExpiresAt  time.Time `json:"expires_at"`
	Active     bool      `json:"active"`
	Identity   struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"identity"`
}

// SessionListResponse represents a list of sessions
type SessionListResponse struct {
	Sessions []SessionResponse `json:"sessions"`
	Total    int               `json:"total"`
}

// createSessionTestServer creates a mock server for session tests
func createSessionTestServer(t *testing.T, suite *TestSuite) *httptest.Server {
	t.Helper()

	r := chi.NewRouter()

	// Session validation endpoint
	r.Get("/api/v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")

		// Query session from database
		var session SessionResponse
		var identityEmail, identityName string
		err := suite.Pool.QueryRow(context.Background(),
			`SELECT s.id, s.token, s.identity_id, s.aal, s.expires_at, s.active,
			        i.email, i.name
			 FROM sessions s
			 JOIN identities i ON s.identity_id = i.id
			 WHERE s.id = $1`, sessionID).Scan(
			&session.ID, &session.Token, &session.IdentityID, &session.AAL,
			&session.ExpiresAt, &session.Active, &identityEmail, &identityName)

		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
			return
		}

		session.Identity.ID = session.IdentityID
		session.Identity.Email = identityEmail
		session.Identity.Name = identityName

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	})

	// Session refresh endpoint
	r.Post("/api/v1/sessions/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")

		// Update session expiry
		newExpiry := time.Now().Add(24 * time.Hour)
		_, err := suite.Pool.Exec(context.Background(),
			`UPDATE sessions SET expires_at = $1, updated_at = NOW() WHERE id = $2`,
			newExpiry, sessionID)

		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"expires_at": newExpiry,
		})
	})

	// Session revocation endpoint
	r.Delete("/api/v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")

		_, err := suite.Pool.Exec(context.Background(),
			`UPDATE sessions SET active = false, updated_at = NOW() WHERE id = $1`,
			sessionID)

		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	})

	// List sessions by identity endpoint
	r.Get("/api/v1/identities/{id}/sessions", func(w http.ResponseWriter, r *http.Request) {
		identityID := chi.URLParam(r, "id")

		rows, err := suite.Pool.Query(context.Background(),
			`SELECT s.id, s.token, s.identity_id, s.aal, s.expires_at, s.active,
			        i.email, i.name
			 FROM sessions s
			 JOIN identities i ON s.identity_id = i.id
			 WHERE s.identity_id = $1 AND s.active = true
			 ORDER BY s.created_at DESC`, identityID)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
			return
		}
		defer rows.Close()

		var sessions []SessionResponse
		for rows.Next() {
			var session SessionResponse
			var identityEmail, identityName string
			err := rows.Scan(&session.ID, &session.Token, &session.IdentityID,
				&session.AAL, &session.ExpiresAt, &session.Active,
				&identityEmail, &identityName)
			if err != nil {
				continue
			}

			session.Identity.ID = session.IdentityID
			session.Identity.Email = identityEmail
			session.Identity.Name = identityName
			sessions = append(sessions, session)
		}

		response := SessionListResponse{
			Sessions: sessions,
			Total:    len(sessions),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	return httptest.NewServer(r)
}

func TestSessionValidation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createSessionTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "session@example.com", "Session User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	// Create test session
	session, err := fixtures.CreateSession(ctx, identity.ID)
	if err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}

	t.Run("validate active session", func(t *testing.T) {
		resp, err := client.Get("/api/v1/sessions/" + session.ID.String())
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var sessionResp SessionResponse
		if err := ParseJSONResponse(resp, &sessionResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if sessionResp.ID != session.ID.String() {
			t.Errorf("Expected session ID %s, got %s", session.ID.String(), sessionResp.ID)
		}

		if !sessionResp.Active {
			t.Error("Expected session to be active")
		}

		if sessionResp.Identity.Email != identity.Email {
			t.Errorf("Expected identity email %s, got %s", identity.Email, sessionResp.Identity.Email)
		}
	})

	t.Run("validate non-existent session", func(t *testing.T) {
		fakeSessionID := uuid.New().String()
		resp, err := client.Get("/api/v1/sessions/" + fakeSessionID)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

func TestSessionRefresh(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createSessionTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity and session
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "refresh@example.com", "Refresh User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	session, err := fixtures.CreateSession(ctx, identity.ID)
	if err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}

	t.Run("refresh valid session", func(t *testing.T) {
		originalExpiry := session.ExpiresAt

		resp, err := client.Post("/api/v1/sessions/"+session.ID.String()+"/refresh", nil)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var refreshResp map[string]interface{}
		if err := ParseJSONResponse(resp, &refreshResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !refreshResp["success"].(bool) {
			t.Error("Expected refresh to succeed")
		}

		// Verify the expiry was updated
		expiresAtStr, ok := refreshResp["expires_at"].(string)
		if !ok {
			t.Fatal("Expected expires_at in response")
		}

		newExpiry, err := time.Parse(time.RFC3339, expiresAtStr)
		if err != nil {
			t.Fatalf("Failed to parse expires_at: %v", err)
		}

		if !newExpiry.After(originalExpiry) {
			t.Error("Expected new expiry to be after original expiry")
		}
	})

	t.Run("refresh non-existent session", func(t *testing.T) {
		fakeSessionID := uuid.New().String()
		resp, err := client.Post("/api/v1/sessions/"+fakeSessionID+"/refresh", nil)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

func TestSessionRevocation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createSessionTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity and session
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "revoke@example.com", "Revoke User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	session, err := fixtures.CreateSession(ctx, identity.ID)
	if err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}

	t.Run("revoke valid session", func(t *testing.T) {
		resp, err := client.Delete("/api/v1/sessions/" + session.ID.String())
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var revokeResp map[string]bool
		if err := ParseJSONResponse(resp, &revokeResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !revokeResp["success"] {
			t.Error("Expected revocation to succeed")
		}

		// Verify session is inactive
		var active bool
		err = suite.Pool.QueryRow(ctx, "SELECT active FROM sessions WHERE id = $1", session.ID).Scan(&active)
		if err != nil {
			t.Fatalf("Failed to query session: %v", err)
		}

		if active {
			t.Error("Expected session to be inactive after revocation")
		}
	})

	t.Run("revoke non-existent session", func(t *testing.T) {
		fakeSessionID := uuid.New().String()
		resp, err := client.Delete("/api/v1/sessions/" + fakeSessionID)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

func TestSessionListByIdentity(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createSessionTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "list@example.com", "List User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	// Create multiple sessions for the identity
	session1, _ := fixtures.CreateSession(ctx, identity.ID)
	session2, _ := fixtures.CreateSession(ctx, identity.ID)

	t.Run("list sessions for identity", func(t *testing.T) {
		resp, err := client.Get("/api/v1/identities/" + identity.ID.String() + "/sessions")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var listResp SessionListResponse
		if err := ParseJSONResponse(resp, &listResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if listResp.Total != 2 {
			t.Errorf("Expected 2 sessions, got %d", listResp.Total)
		}

		if len(listResp.Sessions) != 2 {
			t.Errorf("Expected 2 sessions in list, got %d", len(listResp.Sessions))
		}

		// Check that both sessions are present
		sessionIDs := make(map[string]bool)
		for _, session := range listResp.Sessions {
			sessionIDs[session.ID] = true

			if session.Identity.Email != identity.Email {
				t.Errorf("Expected identity email %s, got %s", identity.Email, session.Identity.Email)
			}

			if !session.Active {
				t.Error("Expected all sessions to be active")
			}
		}

		if !sessionIDs[session1.ID.String()] {
			t.Error("Expected session1 to be in list")
		}
		if !sessionIDs[session2.ID.String()] {
			t.Error("Expected session2 to be in list")
		}
	})

	t.Run("list sessions for non-existent identity", func(t *testing.T) {
		fakeIdentityID := uuid.New().String()
		resp, err := client.Get("/api/v1/identities/" + fakeIdentityID + "/sessions")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var listResp SessionListResponse
		if err := ParseJSONResponse(resp, &listResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if listResp.Total != 0 {
			t.Errorf("Expected 0 sessions, got %d", listResp.Total)
		}
	})
}

func TestConcurrentSessions(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	server := createSessionTestServer(t, suite)
	defer server.Close()

	client := NewAPIClient(server.URL)
	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create test identity
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	identity, err := fixtures.CreateIdentityWithPassword(ctx, "concurrent@example.com", "Concurrent User", string(passwordHash))
	if err != nil {
		t.Fatalf("Failed to create test identity: %v", err)
	}

	t.Run("multiple concurrent sessions", func(t *testing.T) {
		// Create multiple sessions concurrently
		const numSessions = 5
		sessionCh := make(chan *Session, numSessions)
		errCh := make(chan error, numSessions)

		for i := 0; i < numSessions; i++ {
			go func() {
				session, err := fixtures.CreateSession(ctx, identity.ID)
				if err != nil {
					errCh <- err
					return
				}
				sessionCh <- session
			}()
		}

		// Collect results
		var sessions []*Session
		for i := 0; i < numSessions; i++ {
			select {
			case session := <-sessionCh:
				sessions = append(sessions, session)
			case err := <-errCh:
				t.Fatalf("Failed to create session: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for session creation")
			}
		}

		if len(sessions) != numSessions {
			t.Errorf("Expected %d sessions, got %d", numSessions, len(sessions))
		}

		// Verify all sessions are listed
		resp, err := client.Get("/api/v1/identities/" + identity.ID.String() + "/sessions")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var listResp SessionListResponse
		if err := ParseJSONResponse(resp, &listResp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if listResp.Total != numSessions {
			t.Errorf("Expected %d sessions in list, got %d", numSessions, listResp.Total)
		}

		// Test concurrent session operations
		for _, session := range sessions {
			go func(s *Session) {
				// Test session validation
				resp, err := client.Get("/api/v1/sessions/" + s.ID.String())
				if err != nil {
					t.Errorf("Failed to validate session: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected status 200 for session validation, got %d", resp.StatusCode)
				}
			}(session)
		}

		time.Sleep(100 * time.Millisecond) // Allow goroutines to complete
	})

	t.Run("concurrent session refresh", func(t *testing.T) {
		// Create a session
		session, err := fixtures.CreateSession(ctx, identity.ID)
		if err != nil {
			t.Fatalf("Failed to create test session: %v", err)
		}

		// Refresh the same session concurrently
		const numRefreshes = 3
		respCh := make(chan *http.Response, numRefreshes)
		errCh := make(chan error, numRefreshes)

		for i := 0; i < numRefreshes; i++ {
			go func() {
				resp, err := client.Post("/api/v1/sessions/"+session.ID.String()+"/refresh", nil)
				if err != nil {
					errCh <- err
					return
				}
				respCh <- resp
			}()
		}

		// Collect results
		successCount := 0
		for i := 0; i < numRefreshes; i++ {
			select {
			case resp := <-respCh:
				if resp.StatusCode == http.StatusOK {
					successCount++
				}
			case err := <-errCh:
				t.Errorf("Refresh request failed: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for refresh response")
			}
		}

		if successCount != numRefreshes {
			t.Errorf("Expected %d successful refreshes, got %d", numRefreshes, successCount)
		}
	})
}
