// Package e2e provides end-to-end tests for the Aegion identity platform.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestSuite holds the shared test infrastructure
type TestSuite struct {
	Container  *postgres.PostgresContainer
	Pool       *pgxpool.Pool
	Server     *httptest.Server
	Router     chi.Router
	BaseURL    string
	HTTPClient *http.Client
	mu         sync.Mutex
}

var (
	suite     *TestSuite
	suiteOnce sync.Once
)

// SetupTestSuite initializes the test suite with PostgreSQL container
func SetupTestSuite(t *testing.T) *TestSuite {
	t.Helper()

	suiteOnce.Do(func() {
		ctx := context.Background()

		// Start PostgreSQL container
		container, err := postgres.Run(ctx,
			"postgres:16-alpine",
			postgres.WithDatabase("aegion_test"),
			postgres.WithUsername("aegion"),
			postgres.WithPassword("aegion_test_password"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("Failed to start PostgreSQL container: %v", err)
		}

		// Get connection string
		connStr, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("Failed to get connection string: %v", err)
		}

		// Create connection pool
		pool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			t.Fatalf("Failed to create connection pool: %v", err)
		}

		// Run migrations
		if err := runMigrations(ctx, pool); err != nil {
			t.Fatalf("Failed to run migrations: %v", err)
		}

		suite = &TestSuite{
			Container: container,
			Pool:      pool,
			HTTPClient: &http.Client{
				Timeout: 30 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		}
	})

	return suite
}

// TeardownTestSuite cleans up the test infrastructure
func TeardownTestSuite(t *testing.T) {
	t.Helper()

	if suite == nil {
		return
	}

	ctx := context.Background()

	if suite.Server != nil {
		suite.Server.Close()
	}

	if suite.Pool != nil {
		suite.Pool.Close()
	}

	if suite.Container != nil {
		if err := suite.Container.Terminate(ctx); err != nil {
			t.Errorf("Failed to terminate container: %v", err)
		}
	}
}

// runMigrations executes database migrations
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrations := []string{
		// Core identity schemas
		`CREATE TABLE IF NOT EXISTS core_identity_schemas (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(255) NOT NULL UNIQUE,
			schema JSONB NOT NULL DEFAULT '{}',
			is_default BOOLEAN DEFAULT false,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Core identities
		`CREATE TABLE IF NOT EXISTS core_identities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			schema_id UUID REFERENCES core_identity_schemas(id),
			traits JSONB NOT NULL DEFAULT '{}',
			state VARCHAR(50) NOT NULL DEFAULT 'active',
			is_anonymous BOOLEAN DEFAULT false,
			metadata_public JSONB DEFAULT '{}',
			metadata_admin JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			deleted_at TIMESTAMPTZ
		)`,

		// Core identity addresses
		`CREATE TABLE IF NOT EXISTS core_identity_addresses (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
			address_type VARCHAR(50) NOT NULL,
			address VARCHAR(255) NOT NULL,
			verified BOOLEAN DEFAULT false,
			verified_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(address_type, address)
		)`,

		// Core sessions
		`CREATE TABLE IF NOT EXISTS core_sessions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			token VARCHAR(255) NOT NULL UNIQUE,
			identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
			aal VARCHAR(10) NOT NULL DEFAULT 'aal1',
			issued_at TIMESTAMPTZ DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL,
			authenticated_at TIMESTAMPTZ DEFAULT NOW(),
			logout_token VARCHAR(255),
			devices JSONB DEFAULT '[]',
			active BOOLEAN DEFAULT true,
			is_impersonation BOOLEAN DEFAULT false,
			impersonator_id UUID REFERENCES core_identities(id),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Session auth methods
		`CREATE TABLE IF NOT EXISTS core_session_auth_methods (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			session_id UUID NOT NULL REFERENCES core_sessions(id) ON DELETE CASCADE,
			method VARCHAR(50) NOT NULL,
			aal_contribution VARCHAR(10) NOT NULL,
			completed_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Core flows
		`CREATE TABLE IF NOT EXISTS core_flows (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			type VARCHAR(50) NOT NULL,
			state VARCHAR(50) NOT NULL DEFAULT 'active',
			identity_id UUID REFERENCES core_identities(id),
			session_id UUID REFERENCES core_sessions(id),
			request_url TEXT,
			return_to TEXT,
			ui JSONB,
			context JSONB DEFAULT '{}',
			csrf_token VARCHAR(255) NOT NULL,
			issued_at TIMESTAMPTZ DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Continuity containers
		`CREATE TABLE IF NOT EXISTS core_continuity_containers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID REFERENCES core_identities(id),
			name VARCHAR(255) NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}',
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Courier messages
		`CREATE TABLE IF NOT EXISTS core_courier_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			type VARCHAR(50) NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			recipient VARCHAR(255) NOT NULL,
			subject TEXT,
			body TEXT NOT NULL,
			template_type VARCHAR(100),
			template_data JSONB DEFAULT '{}',
			send_count INTEGER DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Admin operators
		`CREATE TABLE IF NOT EXISTS admin_operators (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
			role VARCHAR(50) NOT NULL DEFAULT 'operator',
			permissions JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(identity_id)
		)`,

		// Admin audit logs
		`CREATE TABLE IF NOT EXISTS admin_audit_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			operator_id UUID REFERENCES admin_operators(id),
			action VARCHAR(100) NOT NULL,
			resource_type VARCHAR(100),
			resource_id UUID,
			old_value JSONB,
			new_value JSONB,
			client_ip VARCHAR(45),
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Password credentials
		`CREATE TABLE IF NOT EXISTS module_password_credentials (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
			password_hash VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(identity_id)
		)`,

		// Magic link tokens
		`CREATE TABLE IF NOT EXISTS module_magic_link_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID REFERENCES core_identities(id) ON DELETE CASCADE,
			email VARCHAR(255) NOT NULL,
			token VARCHAR(255) NOT NULL UNIQUE,
			otp_code VARCHAR(10),
			flow_id UUID REFERENCES core_flows(id) ON DELETE CASCADE,
			used BOOLEAN DEFAULT false,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Recovery tokens
		`CREATE TABLE IF NOT EXISTS core_recovery_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
			token VARCHAR(255) NOT NULL UNIQUE,
			flow_id UUID REFERENCES core_flows(id) ON DELETE CASCADE,
			used BOOLEAN DEFAULT false,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Recovery codes (for recovery tests)
		`CREATE TABLE IF NOT EXISTS recovery_codes (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			identity_id UUID NOT NULL REFERENCES core_identities(id) ON DELETE CASCADE,
			code VARCHAR(20) NOT NULL,
			token VARCHAR(255) NOT NULL UNIQUE,
			used BOOLEAN DEFAULT false,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Admin users (for admin tests)
		`CREATE TABLE IF NOT EXISTS admin_users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username VARCHAR(100) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			role VARCHAR(50) NOT NULL DEFAULT 'operator',
			active BOOLEAN DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Admin sessions (for admin tests)
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
			token VARCHAR(255) NOT NULL UNIQUE,
			role VARCHAR(50) NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			active BOOLEAN DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Flows table (for test flows)
		`CREATE TABLE IF NOT EXISTS flows (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			type VARCHAR(50) NOT NULL,
			state VARCHAR(50) NOT NULL,
			csrf_token VARCHAR(255),
			issued_at TIMESTAMPTZ DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Create simplified aliases for testing
		`CREATE VIEW IF NOT EXISTS identities AS SELECT 
			id, email, name, state, created_at, updated_at 
			FROM core_identities`,

		`CREATE VIEW IF NOT EXISTS identity_credentials AS SELECT 
			id, identity_id, 'password' as type, password_hash, created_at, updated_at
			FROM module_password_credentials`,

		`CREATE VIEW IF NOT EXISTS sessions AS SELECT 
			id, token, identity_id, aal, expires_at, active, authenticated_at as created_at, expires_at as updated_at
			FROM core_sessions`,

		// Create indexes
		`CREATE INDEX IF NOT EXISTS idx_identities_state ON core_identities(state)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_identity ON core_sessions(identity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON core_sessions(token)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON core_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_flows_type_state ON core_flows(type, state)`,
		`CREATE INDEX IF NOT EXISTS idx_flows_expires ON core_flows(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_addresses_address ON core_identity_addresses(address)`,

		// Insert default schema
		`INSERT INTO core_identity_schemas (id, name, schema, is_default) 
		 VALUES ('00000000-0000-0000-0000-000000000001', 'default', '{"type": "object", "properties": {"email": {"type": "string", "format": "email"}, "name": {"type": "string"}}, "required": ["email"]}', true)
		 ON CONFLICT (name) DO NOTHING`,
	}

	for _, migration := range migrations {
		if _, err := pool.Exec(ctx, migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// CleanupDatabase clears all test data
func (ts *TestSuite) CleanupDatabase(t *testing.T) {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ctx := context.Background()
	tables := []string{
		"admin_sessions",
		"admin_users",
		"recovery_codes",
		"flows",
		"admin_audit_logs",
		"admin_operators",
		"core_recovery_tokens",
		"module_magic_link_tokens",
		"module_password_credentials",
		"core_courier_messages",
		"core_continuity_containers",
		"core_session_auth_methods",
		"core_flows",
		"core_sessions",
		"core_identity_addresses",
		"core_identities",
	}

	for _, table := range tables {
		_, err := ts.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Errorf("Failed to cleanup table %s: %v", table, err)
		}
	}
}

// APIClient provides helper methods for making API requests
type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
	SessionID  string
	CSRFToken  string
	Cookies    []*http.Cookie
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Request makes an HTTP request to the API
func (c *APIClient) Request(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if c.SessionID != "" {
		req.Header.Set("X-Session-Token", c.SessionID)
	}

	if c.CSRFToken != "" {
		req.Header.Set("X-CSRF-Token", c.CSRFToken)
	}

	for _, cookie := range c.Cookies {
		req.AddCookie(cookie)
	}

	return c.HTTPClient.Do(req)
}

// Get performs a GET request
func (c *APIClient) Get(path string) (*http.Response, error) {
	return c.Request(http.MethodGet, path, nil)
}

// Post performs a POST request
func (c *APIClient) Post(path string, body interface{}) (*http.Response, error) {
	return c.Request(http.MethodPost, path, body)
}

// Put performs a PUT request
func (c *APIClient) Put(path string, body interface{}) (*http.Response, error) {
	return c.Request(http.MethodPut, path, body)
}

// Delete performs a DELETE request
func (c *APIClient) Delete(path string) (*http.Response, error) {
	return c.Request(http.MethodDelete, path, nil)
}

// PostWithHeaders performs a POST request with custom headers
func (c *APIClient) PostWithHeaders(path string, body interface{}, headers map[string]string) (*http.Response, error) {
	return c.RequestWithHeaders(http.MethodPost, path, body, headers)
}

// GetWithHeaders performs a GET request with custom headers
func (c *APIClient) GetWithHeaders(path string, headers map[string]string) (*http.Response, error) {
	return c.RequestWithHeaders(http.MethodGet, path, nil, headers)
}

// PutWithHeaders performs a PUT request with custom headers
func (c *APIClient) PutWithHeaders(path string, body interface{}, headers map[string]string) (*http.Response, error) {
	return c.RequestWithHeaders(http.MethodPut, path, body, headers)
}

// DeleteWithHeaders performs a DELETE request with custom headers
func (c *APIClient) DeleteWithHeaders(path string, headers map[string]string) (*http.Response, error) {
	return c.RequestWithHeaders(http.MethodDelete, path, nil, headers)
}

// PostRaw performs a POST request with raw string body
func (c *APIClient) PostRaw(path, body string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add stored cookies
	for _, cookie := range c.Cookies {
		req.AddCookie(cookie)
	}

	return c.HTTPClient.Do(req)
}

// RequestWithHeaders performs a request with custom headers
func (c *APIClient) RequestWithHeaders(method, path string, body interface{}, headers map[string]string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Set content type for body requests
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add stored cookies
	for _, cookie := range c.Cookies {
		req.AddCookie(cookie)
	}

	// Add CSRF token if available
	if c.CSRFToken != "" {
		req.Header.Set("X-CSRF-Token", c.CSRFToken)
	}

	return c.HTTPClient.Do(req)
}

// ParseJSONResponse parses a JSON response into the provided struct
func ParseJSONResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(body))
	}
	return nil
}

// SaveCookies saves cookies from a response
func (c *APIClient) SaveCookies(resp *http.Response) {
	c.Cookies = append(c.Cookies, resp.Cookies()...)
}

// TestFixtures provides helper methods for creating test data
type TestFixtures struct {
	pool *pgxpool.Pool
}

// NewTestFixtures creates a new fixtures helper
func NewTestFixtures(pool *pgxpool.Pool) *TestFixtures {
	return &TestFixtures{pool: pool}
}

// CreateIdentity creates a test identity
func (f *TestFixtures) CreateIdentity(ctx context.Context, email, name string) (*Identity, error) {
	id := uuid.New()
	schemaID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	traits := map[string]interface{}{
		"email": email,
		"name":  name,
	}
	traitsJSON, _ := json.Marshal(traits)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO core_identities (id, schema_id, traits, state)
		VALUES ($1, $2, $3, 'active')
	`, id, schemaID, traitsJSON)
	if err != nil {
		return nil, err
	}

	// Create address
	_, err = f.pool.Exec(ctx, `
		INSERT INTO core_identity_addresses (identity_id, address_type, address, verified)
		VALUES ($1, 'email', $2, false)
	`, id, email)
	if err != nil {
		return nil, err
	}

	return &Identity{
		ID:     id,
		Email:  email,
		Name:   name,
		State:  "active",
		Traits: traits,
	}, nil
}

// CreateIdentityWithPassword creates an identity with password
func (f *TestFixtures) CreateIdentityWithPassword(ctx context.Context, email, name, passwordHash string) (*Identity, error) {
	identity, err := f.CreateIdentity(ctx, email, name)
	if err != nil {
		return nil, err
	}

	_, err = f.pool.Exec(ctx, `
		INSERT INTO module_password_credentials (identity_id, password_hash)
		VALUES ($1, $2)
	`, identity.ID, passwordHash)
	if err != nil {
		return nil, err
	}

	return identity, nil
}

// CreateVerifiedIdentity creates an identity with verified email
func (f *TestFixtures) CreateVerifiedIdentity(ctx context.Context, email, name string) (*Identity, error) {
	identity, err := f.CreateIdentity(ctx, email, name)
	if err != nil {
		return nil, err
	}

	_, err = f.pool.Exec(ctx, `
		UPDATE core_identity_addresses 
		SET verified = true, verified_at = NOW()
		WHERE identity_id = $1 AND address_type = 'email'
	`, identity.ID)
	if err != nil {
		return nil, err
	}

	return identity, nil
}

// CreateSession creates a test session
func (f *TestFixtures) CreateSession(ctx context.Context, identityID uuid.UUID) (*Session, error) {
	id := uuid.New()
	token := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO core_sessions (id, token, identity_id, aal, expires_at, active)
		VALUES ($1, $2, $3, 'aal1', $4, true)
	`, id, token, identityID, expiresAt)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:         id,
		Token:      token,
		IdentityID: identityID,
		AAL:        "aal1",
		ExpiresAt:  expiresAt,
		Active:     true,
	}, nil
}

// CreateFlow creates a test flow
func (f *TestFixtures) CreateFlow(ctx context.Context, flowType string, expiresIn time.Duration) (*Flow, error) {
	id := uuid.New()
	csrfToken := uuid.New().String()
	expiresAt := time.Now().Add(expiresIn)

	ui := map[string]interface{}{
		"action": fmt.Sprintf("/api/v1/self-service/%s", flowType),
		"method": "POST",
		"nodes":  []interface{}{},
	}
	uiJSON, _ := json.Marshal(ui)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO core_flows (id, type, state, csrf_token, ui, expires_at)
		VALUES ($1, $2, 'active', $3, $4, $5)
	`, id, flowType, csrfToken, uiJSON, expiresAt)
	if err != nil {
		return nil, err
	}

	return &Flow{
		ID:        id,
		Type:      flowType,
		State:     "active",
		CSRFToken: csrfToken,
		ExpiresAt: expiresAt,
	}, nil
}

// CreateRecoveryToken creates a recovery token for an identity
func (f *TestFixtures) CreateRecoveryToken(ctx context.Context, identityID, flowID uuid.UUID, expiresIn time.Duration) (string, error) {
	token := uuid.New().String()
	expiresAt := time.Now().Add(expiresIn)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO core_recovery_tokens (identity_id, token, flow_id, expires_at)
		VALUES ($1, $2, $3, $4)
	`, identityID, token, flowID, expiresAt)
	if err != nil {
		return "", err
	}

	return token, nil
}

// CreateMagicLinkToken creates a magic link token
func (f *TestFixtures) CreateMagicLinkToken(ctx context.Context, identityID *uuid.UUID, email string, flowID uuid.UUID, expiresIn time.Duration) (string, string, error) {
	token := uuid.New().String()
	otpCode := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	expiresAt := time.Now().Add(expiresIn)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO module_magic_link_tokens (identity_id, email, token, otp_code, flow_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, identityID, email, token, otpCode, flowID, expiresAt)
	if err != nil {
		return "", "", err
	}

	return token, otpCode, nil
}

// CreateOperator creates an admin operator
func (f *TestFixtures) CreateOperator(ctx context.Context, identityID uuid.UUID, role string, permissions map[string]interface{}) (*Operator, error) {
	id := uuid.New()
	permJSON, _ := json.Marshal(permissions)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO admin_operators (id, identity_id, role, permissions)
		VALUES ($1, $2, $3, $4)
	`, id, identityID, role, permJSON)
	if err != nil {
		return nil, err
	}

	return &Operator{
		ID:          id,
		IdentityID:  identityID,
		Role:        role,
		Permissions: permissions,
	}, nil
}

// Identity represents a test identity
type Identity struct {
	ID        uuid.UUID
	Email     string
	Name      string
	State     string
	Traits    map[string]interface{}
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Session represents a test session
type Session struct {
	ID         uuid.UUID
	Token      string
	IdentityID uuid.UUID
	AAL        string
	ExpiresAt  time.Time
	Active     bool
}

// Flow represents a test flow
type Flow struct {
	ID        uuid.UUID
	Type      string
	State     string
	CSRFToken string
	ExpiresAt time.Time
}

// Operator represents a test operator
type Operator struct {
	ID          uuid.UUID
	IdentityID  uuid.UUID
	Role        string
	Permissions map[string]interface{}
}

// Test setup and teardown

func TestMain(m *testing.M) {
	code := m.Run()

	// Cleanup
	if suite != nil {
		ctx := context.Background()
		if suite.Pool != nil {
			suite.Pool.Close()
		}
		if suite.Container != nil {
			_ = suite.Container.Terminate(ctx)
		}
	}

	os.Exit(code)
}

// TestDatabaseSetup verifies the database is properly initialized
func TestDatabaseSetup(t *testing.T) {
	suite := SetupTestSuite(t)

	ctx := context.Background()

	// Test connection
	var result int
	err := suite.Pool.QueryRow(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if result != 1 {
		t.Errorf("Expected 1, got %d", result)
	}
}

// TestContainerManagement verifies container lifecycle
func TestContainerManagement(t *testing.T) {
	suite := SetupTestSuite(t)

	ctx := context.Background()

	// Verify container is running
	state, err := suite.Container.State(ctx)
	if err != nil {
		t.Fatalf("Failed to get container state: %v", err)
	}

	if !state.Running {
		t.Error("Container should be running")
	}
}

// TestMigrationsApplied verifies all tables exist
func TestMigrationsApplied(t *testing.T) {
	suite := SetupTestSuite(t)

	ctx := context.Background()

	tables := []string{
		"core_identity_schemas",
		"core_identities",
		"core_identity_addresses",
		"core_sessions",
		"core_session_auth_methods",
		"core_flows",
		"core_continuity_containers",
		"core_courier_messages",
		"admin_operators",
		"admin_audit_logs",
		"module_password_credentials",
		"module_magic_link_tokens",
		"core_recovery_tokens",
	}

	for _, table := range tables {
		var exists bool
		err := suite.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = $1
			)
		`, table).Scan(&exists)

		if err != nil {
			t.Errorf("Failed to check table %s: %v", table, err)
			continue
		}

		if !exists {
			t.Errorf("Table %s does not exist", table)
		}
	}
}

// TestDefaultSchemaExists verifies the default identity schema exists
func TestDefaultSchemaExists(t *testing.T) {
	suite := SetupTestSuite(t)

	ctx := context.Background()

	var name string
	var isDefault bool
	err := suite.Pool.QueryRow(ctx, `
		SELECT name, is_default FROM core_identity_schemas WHERE id = $1
	`, uuid.MustParse("00000000-0000-0000-0000-000000000001")).Scan(&name, &isDefault)

	if err != nil {
		t.Fatalf("Failed to query default schema: %v", err)
	}

	if name != "default" {
		t.Errorf("Expected schema name 'default', got '%s'", name)
	}

	if !isDefault {
		t.Error("Schema should be marked as default")
	}
}

// TestFixtureCreation verifies fixture helpers work correctly
func TestFixtureCreation(t *testing.T) {
	suite := SetupTestSuite(t)
	defer suite.CleanupDatabase(t)

	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	t.Run("CreateIdentity", func(t *testing.T) {
		identity, err := fixtures.CreateIdentity(ctx, "test@example.com", "Test User")
		if err != nil {
			t.Fatalf("Failed to create identity: %v", err)
		}

		if identity.ID == uuid.Nil {
			t.Error("Identity ID should not be nil")
		}
		if identity.Email != "test@example.com" {
			t.Errorf("Expected email 'test@example.com', got '%s'", identity.Email)
		}
	})

	t.Run("CreateSession", func(t *testing.T) {
		identity, _ := fixtures.CreateIdentity(ctx, "session@example.com", "Session User")
		session, err := fixtures.CreateSession(ctx, identity.ID)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		if session.Token == "" {
			t.Error("Session token should not be empty")
		}
		if !session.Active {
			t.Error("Session should be active")
		}
	})

	t.Run("CreateFlow", func(t *testing.T) {
		flow, err := fixtures.CreateFlow(ctx, "login", 15*time.Minute)
		if err != nil {
			t.Fatalf("Failed to create flow: %v", err)
		}

		if flow.Type != "login" {
			t.Errorf("Expected flow type 'login', got '%s'", flow.Type)
		}
		if flow.State != "active" {
			t.Errorf("Expected flow state 'active', got '%s'", flow.State)
		}
	})
}

// TestAPIClient verifies the API client works correctly
func TestAPIClient(t *testing.T) {
	// Create a test server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/test":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/echo":
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewAPIClient(server.URL)

	t.Run("GET request", func(t *testing.T) {
		resp, err := client.Get("/test")
		if err != nil {
			t.Fatalf("Failed to make GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := ParseJSONResponse(resp, &result); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("Expected status 'ok', got '%s'", result["status"])
		}
	})

	t.Run("POST request", func(t *testing.T) {
		body := map[string]string{"key": "value"}
		resp, err := client.Post("/echo", body)
		if err != nil {
			t.Fatalf("Failed to make POST request: %v", err)
		}

		var result map[string]string
		if err := ParseJSONResponse(resp, &result); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if result["key"] != "value" {
			t.Errorf("Expected key 'value', got '%s'", result["key"])
		}
	})
}

// TestCleanupDatabase verifies database cleanup works
func TestCleanupDatabase(t *testing.T) {
	suite := SetupTestSuite(t)

	ctx := context.Background()
	fixtures := NewTestFixtures(suite.Pool)

	// Create some data
	_, _ = fixtures.CreateIdentity(ctx, "cleanup@example.com", "Cleanup User")

	// Verify data exists
	var count int
	suite.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM core_identities").Scan(&count)
	if count == 0 {
		t.Error("Expected at least one identity before cleanup")
	}

	// Cleanup
	suite.CleanupDatabase(t)

	// Verify data is gone
	suite.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM core_identities").Scan(&count)
	if count != 0 {
		t.Errorf("Expected 0 identities after cleanup, got %d", count)
	}
}
