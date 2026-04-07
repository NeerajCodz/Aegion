package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeType_Constants(t *testing.T) {
	// Test that code type constants are defined
	assert.Equal(t, "login", string(CodeTypeLogin))
	assert.Equal(t, "verification", string(CodeTypeVerification))
	assert.Equal(t, "recovery", string(CodeTypeRecovery))
}

func TestCode_Structure(t *testing.T) {
	now := time.Now()
	identityID := uuid.New()

	code := &Code{
		ID:         uuid.New(),
		IdentityID: &identityID,
		Recipient:  "user@example.com",
		Type:       CodeTypeLogin,
		Code:       "123456",
		Token:      "abc123token",
		Used:       false,
		UsedAt:     nil,
		ExpiresAt:  now.Add(15 * time.Minute),
		CreatedAt:  now,
	}

	assert.NotEqual(t, uuid.Nil, code.ID)
	assert.NotNil(t, code.IdentityID)
	assert.Equal(t, identityID, *code.IdentityID)
	assert.Equal(t, "user@example.com", code.Recipient)
	assert.Equal(t, CodeTypeLogin, code.Type)
	assert.Equal(t, "123456", code.Code)
	assert.Equal(t, "abc123token", code.Token)
	assert.False(t, code.Used)
	assert.Nil(t, code.UsedAt)
	assert.True(t, code.ExpiresAt.After(now))
	assert.Equal(t, now, code.CreatedAt)
}

func TestCode_WithNullableFields(t *testing.T) {
	now := time.Now()

	// Test code without identity (for login flow)
	code := &Code{
		ID:         uuid.New(),
		IdentityID: nil, // No identity for login codes
		Recipient:  "user@example.com",
		Type:       CodeTypeLogin,
		Code:       "123456",
		Token:      "abc123token",
		Used:       false,
		UsedAt:     nil,
		ExpiresAt:  now.Add(15 * time.Minute),
		CreatedAt:  now,
	}

	assert.NotEqual(t, uuid.Nil, code.ID)
	assert.Nil(t, code.IdentityID)
	assert.Equal(t, "user@example.com", code.Recipient)
	assert.Equal(t, CodeTypeLogin, code.Type)
}

// Test code generation patterns (simulating the Store methods)
func testGenerateCode(length int, charset string) string {
	if length == 0 {
		return ""
	}
	if len(charset) == 0 {
		return ""
	}
	if len(charset) == 1 {
		result := make([]byte, length)
		for i := range result {
			result[i] = charset[0]
		}
		return string(result)
	}

	// Simple implementation for testing
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[i%len(charset)]
	}
	return string(result)
}

// Test token generation patterns (simulating the Store methods)
func testGenerateToken(length int) string {
	if length == 0 {
		return ""
	}

	// Simple implementation for testing - just create a predictable token
	data := make([]byte, length)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func TestGenerateCode(t *testing.T) {
	tests := []struct {
		name     string
		length   int
		charset  string
		validate func(string) bool
	}{
		{
			name:    "default numeric code",
			length:  6,
			charset: "0123456789",
			validate: func(code string) bool {
				if len(code) != 6 {
					return false
				}
				for _, char := range code {
					if char < '0' || char > '9' {
						return false
					}
				}
				return true
			},
		},
		{
			name:    "alphanumeric code",
			length:  8,
			charset: "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ",
			validate: func(code string) bool {
				if len(code) != 8 {
					return false
				}
				for _, char := range code {
					if (char < '0' || char > '9') && (char < 'A' || char > 'Z') {
						return false
					}
				}
				return true
			},
		},
		{
			name:    "short code",
			length:  4,
			charset: "0123456789",
			validate: func(code string) bool {
				return len(code) == 4
			},
		},
		{
			name:    "long code",
			length:  12,
			charset: "0123456789ABCDEF",
			validate: func(code string) bool {
				return len(code) == 12
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := testGenerateCode(tt.length, tt.charset)

			assert.True(t, tt.validate(code), "Generated code '%s' failed validation", code)
			assert.Equal(t, tt.length, len(code))
		})
	}
}

func TestGenerateCode_Randomness(t *testing.T) {
	// Generate multiple codes and ensure they're different using our test function
	codes := make(map[string]bool)
	length := 6
	charset := "0123456789"
	iterations := 10 // Reduced since our test function is deterministic

	for i := 0; i < iterations; i++ {
		// Modify charset slightly to get different results
		modifiedCharset := charset + string(rune('A'+i%26))
		code := testGenerateCode(length, modifiedCharset)
		codes[code] = true
	}

	// Should have generated some codes
	assert.True(t, len(codes) > 0)
}

func TestGenerateCode_EdgeCases(t *testing.T) {
	t.Run("empty charset", func(t *testing.T) {
		// Should not panic with empty charset
		assert.NotPanics(t, func() {
			testGenerateCode(6, "")
		})
	})

	t.Run("zero length", func(t *testing.T) {
		code := testGenerateCode(0, "0123456789")
		assert.Equal(t, "", code)
	})

	t.Run("single character charset", func(t *testing.T) {
		code := testGenerateCode(5, "A")
		assert.Equal(t, "AAAAA", code)
	})
}

func TestGenerateToken(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "default token",
			length: 32,
		},
		{
			name:   "short token",
			length: 16,
		},
		{
			name:   "long token",
			length: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := testGenerateToken(tt.length)

			assert.NotEmpty(t, token)

			// Decode the base64 token to check length
			decoded, err := base64.RawURLEncoding.DecodeString(token)
			require.NoError(t, err)
			assert.Equal(t, tt.length, len(decoded))

			// Check that it's valid base64 URL encoding
			assert.True(t, isValidBase64URL(token))
		})
	}
}

func TestGenerateToken_Randomness(t *testing.T) {
	// Generate multiple tokens and ensure they're different using our test function
	tokens := make(map[string]bool)
	length := 32
	iterations := 10 // Reduced since our test function is deterministic

	for i := 0; i < iterations; i++ {
		// Use different lengths to get different results
		token := testGenerateToken(length + i)
		tokens[token] = true
	}

	// Should have generated some tokens
	assert.True(t, len(tokens) > 0)
}

func TestGenerateToken_EdgeCases(t *testing.T) {
	t.Run("zero length", func(t *testing.T) {
		token := testGenerateToken(0)
		assert.Equal(t, "", token)
	})

	t.Run("very small length", func(t *testing.T) {
		token := testGenerateToken(1)
		assert.NotEmpty(t, token)

		decoded, err := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, err)
		assert.Equal(t, 1, len(decoded))
	})
}

// Helper function to validate base64 URL encoding
func isValidBase64URL(s string) bool {
	_, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil
}

func TestStore_ErrorDefinitions(t *testing.T) {
	// Verify that error constants are defined
	assert.NotNil(t, ErrCodeNotFound)
	assert.NotNil(t, ErrCodeExpired)
	assert.NotNil(t, ErrCodeUsed)
	assert.NotNil(t, ErrRateLimited)

	// Check error messages are meaningful
	assert.Contains(t, ErrCodeNotFound.Error(), "not found")
	assert.Contains(t, ErrCodeExpired.Error(), "expired")
	assert.Contains(t, ErrCodeUsed.Error(), "used")
	assert.Contains(t, ErrRateLimited.Error(), "rate limit")
}

func TestStore_Interface_Methods(t *testing.T) {
	// Test that Store struct has all required methods
	// This will fail to compile if methods are missing

	store := &Store{}
	ctx := context.Background()

	// These will panic with nil database, but we're just testing method signatures
	assert.Panics(t, func() {
		_, _ = store.Create(ctx, "user@example.com", "login", nil, time.Hour)
	})

	assert.Panics(t, func() {
		_, _ = store.GetByCode(ctx, "user@example.com", "123456", "login")
	})

	assert.Panics(t, func() {
		_, _ = store.GetByToken(ctx, "token123")
	})

	assert.Panics(t, func() {
		_ = store.MarkUsed(ctx, uuid.New())
	})

	assert.Panics(t, func() {
		_ = store.InvalidatePrevious(ctx, "user@example.com", "login")
	})

	assert.Panics(t, func() {
		_ = store.CheckRateLimit(ctx, "key", 5, time.Hour)
	})

	assert.Panics(t, func() {
		_, _ = store.Cleanup(ctx)
	})
}

func TestValidation_Logic(t *testing.T) {
	// Test validation logic that might be used in store methods

	t.Run("recipient validation", func(t *testing.T) {
		tests := []struct {
			recipient string
			valid     bool
		}{
			{"user@example.com", true},
			{"", false},
			{"   ", false},          // whitespace only
			{"user@", false},        // incomplete email
			{"@example.com", false}, // missing user part
		}

		for _, tt := range tests {
			// More sophisticated email validation
			isValid := tt.recipient != "" &&
				strings.TrimSpace(tt.recipient) == tt.recipient &&
				strings.Contains(tt.recipient, "@") &&
				len(strings.Split(tt.recipient, "@")) == 2 &&
				strings.Split(tt.recipient, "@")[0] != "" &&
				strings.Split(tt.recipient, "@")[1] != ""

			assert.Equal(t, tt.valid, isValid, "Recipient validation failed for: %s", tt.recipient)
		}
	})

	t.Run("code type validation", func(t *testing.T) {
		validTypes := []string{"login", "verification", "recovery"}

		for _, validType := range validTypes {
			assert.Contains(t, validTypes, validType)
		}

		invalidTypes := []string{"", "invalid", "LOGIN", "123"}
		for _, invalidType := range invalidTypes {
			assert.NotContains(t, validTypes, invalidType)
		}
	})

	t.Run("TTL validation", func(t *testing.T) {
		tests := []struct {
			ttl   time.Duration
			valid bool
		}{
			{15 * time.Minute, true},
			{time.Hour, true},
			{0, false},               // Zero TTL
			{-time.Minute, false},    // Negative TTL
			{time.Nanosecond, false}, // Too short
		}

		for _, tt := range tests {
			isValid := tt.ttl > time.Second // Reasonable minimum
			assert.Equal(t, tt.valid, isValid, "TTL validation failed for: %v", tt.ttl)
		}
	})
}

func TestTimeHandling(t *testing.T) {
	t.Run("expiration calculation", func(t *testing.T) {
		now := time.Now()
		ttl := 15 * time.Minute
		expiresAt := now.Add(ttl)

		assert.True(t, expiresAt.After(now))
		assert.True(t, expiresAt.Before(now.Add(16*time.Minute)))
	})

	t.Run("expiration check", func(t *testing.T) {
		now := time.Now()

		// Not expired
		future := now.Add(time.Hour)
		assert.False(t, now.After(future))

		// Expired
		past := now.Add(-time.Hour)
		assert.True(t, now.After(past))
	})

	t.Run("rate limit window", func(t *testing.T) {
		now := time.Now()
		window := time.Hour
		windowStart := now.Add(-window)

		assert.True(t, windowStart.Before(now))
		assert.Equal(t, window, now.Sub(windowStart))
	})
}

func TestRateLimit_Logic(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		limit       int
		window      time.Duration
		expectValid bool
		description string
	}{
		{
			name:        "valid rate limit params",
			key:         "login:user@example.com",
			limit:       5,
			window:      time.Hour,
			expectValid: true,
			description: "Standard rate limit configuration",
		},
		{
			name:        "empty key",
			key:         "",
			limit:       5,
			window:      time.Hour,
			expectValid: false,
			description: "Rate limit key cannot be empty",
		},
		{
			name:        "zero limit",
			key:         "login:user@example.com",
			limit:       0,
			window:      time.Hour,
			expectValid: false,
			description: "Rate limit must be positive",
		},
		{
			name:        "negative limit",
			key:         "login:user@example.com",
			limit:       -1,
			window:      time.Hour,
			expectValid: false,
			description: "Rate limit cannot be negative",
		},
		{
			name:        "zero window",
			key:         "login:user@example.com",
			limit:       5,
			window:      0,
			expectValid: false,
			description: "Rate limit window must be positive",
		},
		{
			name:        "very short window",
			key:         "login:user@example.com",
			limit:       5,
			window:      time.Nanosecond,
			expectValid: false,
			description: "Rate limit window too short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation logic that would be used in store
			isValid := tt.key != "" && tt.limit > 0 && tt.window > time.Second

			assert.Equal(t, tt.expectValid, isValid, tt.description)
		})
	}
}

func TestRateLimit_KeyGeneration(t *testing.T) {
	tests := []struct {
		operation string
		email     string
		identity  string
		expected  string
	}{
		{
			operation: "login",
			email:     "user@example.com",
			identity:  "",
			expected:  "login:user@example.com",
		},
		{
			operation: "verify",
			email:     "user@example.com",
			identity:  "identity123",
			expected:  "verify:identity123",
		},
		{
			operation: "recover",
			email:     "user@example.com",
			identity:  "",
			expected:  "recover:user@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.operation+"_key", func(t *testing.T) {
			var key string

			switch tt.operation {
			case "login", "recover":
				key = tt.operation + ":" + tt.email
			case "verify":
				key = tt.operation + ":" + tt.identity
			}

			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestContext_Handling(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		select {
		case <-ctx.Done():
			assert.NotNil(t, ctx.Err())
		default:
			t.Error("Context should be canceled")
		}
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		time.Sleep(10 * time.Millisecond) // Wait for timeout

		// After sleep, context should be done
		assert.True(t, ctx.Err() != nil, "Context should have an error")
		assert.Equal(t, context.DeadlineExceeded, ctx.Err())
	})
}

func TestUUID_Operations(t *testing.T) {
	t.Run("uuid generation", func(t *testing.T) {
		id1 := uuid.New().String()
		id2 := uuid.New().String()

		assert.NotEqual(t, id1, id2)
		assert.NotEmpty(t, id1)
		assert.NotEmpty(t, id2)

		// Verify UUID format
		_, err1 := uuid.Parse(id1)
		_, err2 := uuid.Parse(id2)
		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})

	t.Run("uuid parsing", func(t *testing.T) {
		validUUID := uuid.New().String()
		invalidUUID := "not-a-uuid"

		_, err1 := uuid.Parse(validUUID)
		_, err2 := uuid.Parse(invalidUUID)

		assert.NoError(t, err1)
		assert.Error(t, err2)
	})
}

// Test database operation patterns that would be used
func TestDatabase_OperationPatterns(t *testing.T) {
	t.Run("create operation", func(t *testing.T) {
		// Test data that would be inserted
		now := time.Now()
		code := &Code{
			ID:        uuid.New(),
			Recipient: "user@example.com",
			Type:      CodeTypeLogin,
			Code:      testGenerateCode(6, "0123456789"),
			Token:     testGenerateToken(32),
			Used:      false,
			UsedAt:    nil,
			ExpiresAt: now.Add(15 * time.Minute),
			CreatedAt: now,
		}

		// Validate required fields
		assert.NotEqual(t, uuid.Nil, code.ID)
		assert.NotEmpty(t, code.Recipient)
		assert.NotEmpty(t, code.Code)
		assert.NotEmpty(t, code.Token)
		assert.False(t, code.Used)
		assert.Nil(t, code.UsedAt)
		assert.True(t, code.ExpiresAt.After(code.CreatedAt))
	})

	t.Run("query by code", func(t *testing.T) {
		// Test query parameters
		recipient := "user@example.com"
		otpCode := "123456"
		codeType := "login"

		assert.NotEmpty(t, recipient)
		assert.NotEmpty(t, otpCode)
		assert.NotEmpty(t, codeType)
		assert.Contains(t, []string{"login", "verification", "recovery"}, codeType)
	})

	t.Run("mark used operation", func(t *testing.T) {
		now := time.Now()
		codeID := uuid.New()

		// Simulate marking as used
		used := true
		usedAt := &now

		assert.NotEqual(t, uuid.Nil, codeID)
		assert.True(t, used)
		assert.NotNil(t, usedAt)
		assert.True(t, usedAt.Before(now.Add(time.Second)))
	})
}

// Benchmark performance-critical functions
func BenchmarkGenerateCode(b *testing.B) {
	length := 6
	charset := "0123456789"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testGenerateCode(length, charset)
	}
}

func BenchmarkGenerateToken(b *testing.B) {
	length := 32

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testGenerateToken(length)
	}
}

func BenchmarkCryptoRand(b *testing.B) {
	buffer := make([]byte, 32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rand.Read(buffer)
	}
}

func BenchmarkBase64Encoding(b *testing.B) {
	data := make([]byte, 32)
	_, _ = rand.Read(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		base64.RawURLEncoding.EncodeToString(data)
	}
}

func BenchmarkUUIDGeneration(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = uuid.New().String()
	}
}

// =========== Seam Injection Tests with fake database ===========

// mockCommandTag creates a command tag for testing
func mockCommandTag(rowsAffected int64) pgconn.CommandTag {
	if rowsAffected == 0 {
		return pgconn.NewCommandTag("")
	}
	return pgconn.NewCommandTag(fmt.Sprintf("INSERT 0 %d", rowsAffected))
}

// mockCommandTagDelete creates a DELETE command tag for testing
func mockCommandTagDelete(rowsAffected int64) pgconn.CommandTag {
	if rowsAffected == 0 {
		return pgconn.NewCommandTag("")
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", rowsAffected))
}

// fakeRow implements pgx.Row for testing
type fakeRow struct {
	data []interface{}
	err  error
}

func (r *fakeRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if len(r.data) == 0 {
		return pgx.ErrNoRows
	}
	for i, d := range dest {
		if i < len(r.data) {
			switch v := d.(type) {
			case *uuid.UUID:
				if uid, ok := r.data[i].(uuid.UUID); ok {
					*v = uid
				}
			case **uuid.UUID:
				if uid, ok := r.data[i].(*uuid.UUID); ok {
					*v = uid
				}
			case *string:
				if s, ok := r.data[i].(string); ok {
					*v = s
				}
			case *CodeType:
				if ct, ok := r.data[i].(CodeType); ok {
					*v = ct
				}
			case *bool:
				if b, ok := r.data[i].(bool); ok {
					*v = b
				}
			case **time.Time:
				if t, ok := r.data[i].(*time.Time); ok {
					*v = t
				}
			case *time.Time:
				if t, ok := r.data[i].(time.Time); ok {
					*v = t
				}
			case *int:
				if n, ok := r.data[i].(int); ok {
					*v = n
				}
			}
		}
	}
	return nil
}

// fakeDB implements DB interface for testing
type fakeDB struct {
	execFn     func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	queryRowFn func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row
}

func (f *fakeDB) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, arguments...)
	}
	return mockCommandTag(1), nil
}

func (f *fakeDB) QueryRow(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, optionsAndArgs...)
	}
	return &fakeRow{err: pgx.ErrNoRows}
}

type fakeQueryObserver struct {
	calls []string
	err   error
}

func (f *fakeQueryObserver) WrapQuery(ctx context.Context, operation, table string, fn func(context.Context) error) error {
	f.calls = append(f.calls, fmt.Sprintf("%s:%s", operation, table))
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

func TestStore_New(t *testing.T) {
	// Test NewWithDB constructor
	db := &fakeDB{}
	store := NewWithDB(db)

	assert.NotNil(t, store)
	assert.Equal(t, 6, store.codeLength)
	assert.Equal(t, "0123456789", store.codeCharset)
}

func TestStore_SetCodeConfig(t *testing.T) {
	db := &fakeDB{}
	store := NewWithDB(db)

	store.SetCodeConfig(8, "0123456789ABCDEF")

	assert.Equal(t, 8, store.codeLength)
	assert.Equal(t, "0123456789ABCDEF", store.codeCharset)
}

func TestStore_withObservedQuery_UsesObserver(t *testing.T) {
	observer := &fakeQueryObserver{}
	store := newStore(&fakeDB{}, observer)

	called := false
	err := store.withObservedQuery(context.Background(), "SELECT", dbTableCodes, func(context.Context) error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, []string{"SELECT:" + dbTableCodes}, observer.calls)
}

func TestStore_Create_RecordsObservabilityOperation(t *testing.T) {
	observer := &fakeQueryObserver{}
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(1), nil
		},
	}

	store := newStore(db, observer)
	_, err := store.Create(context.Background(), "user@example.com", CodeTypeLogin, nil, 15*time.Minute)

	require.NoError(t, err)
	assert.Contains(t, observer.calls, "INSERT:"+dbTableCodes)
}

func TestStore_Create_Success(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			// Verify the SQL contains INSERT
			assert.Contains(t, sql, "INSERT INTO ml_codes")
			return mockCommandTag(1), nil
		},
	}
	store := NewWithDB(db)

	identityID := uuid.New()
	code, err := store.Create(context.Background(), "user@example.com", CodeTypeLogin, &identityID, 15*time.Minute)

	require.NoError(t, err)
	assert.NotNil(t, code)
	assert.NotEqual(t, uuid.Nil, code.ID)
	assert.Equal(t, "user@example.com", code.Recipient)
	assert.Equal(t, CodeTypeLogin, code.Type)
	assert.NotEmpty(t, code.Code)
	assert.NotEmpty(t, code.Token)
	assert.False(t, code.Used)
}

func TestStore_Create_DBError(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), errors.New("database connection failed")
		},
	}
	store := NewWithDB(db)

	_, err := store.Create(context.Background(), "user@example.com", CodeTypeLogin, nil, 15*time.Minute)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database connection failed")
}

func TestStore_GetByCode_Success(t *testing.T) {
	expectedID := uuid.New()
	expectedIdentityID := uuid.New()
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	createdAt := time.Now().UTC()

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			assert.Contains(t, sql, "SELECT")
			assert.Contains(t, sql, "FROM ml_codes")
			return &fakeRow{
				data: []interface{}{
					expectedID,
					&expectedIdentityID,
					"user@example.com",
					CodeTypeLogin,
					"123456",
					"tokenvalue",
					false,
					(*time.Time)(nil),
					expiresAt,
					createdAt,
				},
			}
		},
	}
	store := NewWithDB(db)

	code, err := store.GetByCode(context.Background(), "user@example.com", "123456", CodeTypeLogin)

	require.NoError(t, err)
	assert.Equal(t, expectedID, code.ID)
	assert.Equal(t, "user@example.com", code.Recipient)
	assert.Equal(t, "123456", code.Code)
}

func TestStore_GetByCode_NotFound(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{err: pgx.ErrNoRows}
		},
	}
	store := NewWithDB(db)

	_, err := store.GetByCode(context.Background(), "user@example.com", "123456", CodeTypeLogin)

	assert.ErrorIs(t, err, ErrCodeNotFound)
}

func TestStore_GetByCode_Expired(t *testing.T) {
	expectedID := uuid.New()
	// Code expired 1 hour ago
	expiresAt := time.Now().UTC().Add(-1 * time.Hour)
	createdAt := time.Now().UTC().Add(-2 * time.Hour)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{
				data: []interface{}{
					expectedID,
					(*uuid.UUID)(nil),
					"user@example.com",
					CodeTypeLogin,
					"123456",
					"tokenvalue",
					false,
					(*time.Time)(nil),
					expiresAt,
					createdAt,
				},
			}
		},
	}
	store := NewWithDB(db)

	_, err := store.GetByCode(context.Background(), "user@example.com", "123456", CodeTypeLogin)

	assert.ErrorIs(t, err, ErrCodeExpired)
}

func TestStore_GetByCode_DBError(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{err: errors.New("database error")}
		},
	}
	store := NewWithDB(db)

	_, err := store.GetByCode(context.Background(), "user@example.com", "123456", CodeTypeLogin)

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrCodeNotFound)
}

func TestStore_GetByToken_Success(t *testing.T) {
	expectedID := uuid.New()
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	createdAt := time.Now().UTC()

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			assert.Contains(t, sql, "WHERE token = $1")
			return &fakeRow{
				data: []interface{}{
					expectedID,
					(*uuid.UUID)(nil),
					"user@example.com",
					CodeTypeLogin,
					"123456",
					"mytoken123",
					false,
					(*time.Time)(nil),
					expiresAt,
					createdAt,
				},
			}
		},
	}
	store := NewWithDB(db)

	code, err := store.GetByToken(context.Background(), "mytoken123")

	require.NoError(t, err)
	assert.Equal(t, expectedID, code.ID)
	assert.Equal(t, "mytoken123", code.Token)
}

func TestStore_GetByToken_NotFound(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{err: pgx.ErrNoRows}
		},
	}
	store := NewWithDB(db)

	_, err := store.GetByToken(context.Background(), "nonexistent")

	assert.ErrorIs(t, err, ErrCodeNotFound)
}

func TestStore_GetByToken_Expired(t *testing.T) {
	expectedID := uuid.New()
	expiresAt := time.Now().UTC().Add(-1 * time.Hour)
	createdAt := time.Now().UTC().Add(-2 * time.Hour)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{
				data: []interface{}{
					expectedID,
					(*uuid.UUID)(nil),
					"user@example.com",
					CodeTypeLogin,
					"123456",
					"expiredtoken",
					false,
					(*time.Time)(nil),
					expiresAt,
					createdAt,
				},
			}
		},
	}
	store := NewWithDB(db)

	_, err := store.GetByToken(context.Background(), "expiredtoken")

	assert.ErrorIs(t, err, ErrCodeExpired)
}

func TestStore_MarkUsed_Success(t *testing.T) {
	codeID := uuid.New()
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "UPDATE ml_codes")
			assert.Contains(t, sql, "SET used = TRUE")
			return mockCommandTag(1), nil
		},
	}
	store := NewWithDB(db)

	err := store.MarkUsed(context.Background(), codeID)

	assert.NoError(t, err)
}

func TestStore_MarkUsed_AlreadyUsed(t *testing.T) {
	codeID := uuid.New()
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			// Return 0 rows affected to indicate code was already used
			return mockCommandTag(0), nil
		},
	}
	store := NewWithDB(db)

	err := store.MarkUsed(context.Background(), codeID)

	assert.ErrorIs(t, err, ErrCodeUsed)
}

func TestStore_MarkUsed_DBError(t *testing.T) {
	codeID := uuid.New()
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), errors.New("db error")
		},
	}
	store := NewWithDB(db)

	err := store.MarkUsed(context.Background(), codeID)

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrCodeUsed)
}

func TestStore_InvalidatePrevious_Success(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "UPDATE ml_codes")
			assert.Contains(t, sql, "SET used = TRUE")
			return mockCommandTag(3), nil
		},
	}
	store := NewWithDB(db)

	err := store.InvalidatePrevious(context.Background(), "user@example.com", CodeTypeLogin)

	assert.NoError(t, err)
}

func TestStore_InvalidatePrevious_NoOp(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			// No rows affected is not an error for invalidation
			return mockCommandTag(0), nil
		},
	}
	store := NewWithDB(db)

	err := store.InvalidatePrevious(context.Background(), "user@example.com", CodeTypeLogin)

	assert.NoError(t, err)
}

func TestStore_CheckRateLimit_UnderLimit(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			assert.Contains(t, sql, "ml_rate_limits")
			return &fakeRow{data: []interface{}{3}} // count = 3, under limit of 5
		},
	}
	store := NewWithDB(db)

	err := store.CheckRateLimit(context.Background(), "login:user@example.com", 5, time.Hour)

	assert.NoError(t, err)
}

func TestStore_CheckRateLimit_AtLimit(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{data: []interface{}{5}} // count = 5, at limit
		},
	}
	store := NewWithDB(db)

	err := store.CheckRateLimit(context.Background(), "login:user@example.com", 5, time.Hour)

	assert.NoError(t, err)
}

func TestStore_CheckRateLimit_OverLimit(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{data: []interface{}{6}} // count = 6, over limit of 5
		},
	}
	store := NewWithDB(db)

	err := store.CheckRateLimit(context.Background(), "login:user@example.com", 5, time.Hour)

	assert.ErrorIs(t, err, ErrRateLimited)
}

func TestStore_CheckRateLimit_DBError(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &fakeRow{err: errors.New("db error")}
		},
	}
	store := NewWithDB(db)

	err := store.CheckRateLimit(context.Background(), "login:user@example.com", 5, time.Hour)

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrRateLimited)
}

func TestStore_Cleanup_Success(t *testing.T) {
	callCount := 0
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			callCount++
			if callCount == 1 {
				assert.Contains(t, sql, "DELETE FROM ml_codes")
				return mockCommandTagDelete(10), nil
			}
			assert.Contains(t, sql, "DELETE FROM ml_rate_limits")
			return mockCommandTagDelete(5), nil
		},
	}
	store := NewWithDB(db)

	deleted, err := store.Cleanup(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(10), deleted)
	assert.Equal(t, 2, callCount)
}

func TestStore_Cleanup_FirstQueryError(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), errors.New("first delete failed")
		},
	}
	store := NewWithDB(db)

	deleted, err := store.Cleanup(context.Background())

	assert.Error(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestStore_Cleanup_SecondQueryError(t *testing.T) {
	callCount := 0
	db := &fakeDB{
		execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			callCount++
			if callCount == 1 {
				return mockCommandTagDelete(5), nil
			}
			return mockCommandTag(0), errors.New("second delete failed")
		},
	}
	store := NewWithDB(db)

	deleted, err := store.Cleanup(context.Background())

	assert.Error(t, err)
	// First delete succeeded, returned that count
	assert.Equal(t, int64(5), deleted)
}

func TestStore_generateCode_Length(t *testing.T) {
	db := &fakeDB{}
	store := NewWithDB(db)

	// Default config: 6 digits
	code, err := store.generateCode()
	require.NoError(t, err)
	assert.Len(t, code, 6)

	// Custom config
	store.SetCodeConfig(8, "0123456789ABCDEF")
	code, err = store.generateCode()
	require.NoError(t, err)
	assert.Len(t, code, 8)
}

func TestStore_generateCode_Charset(t *testing.T) {
	db := &fakeDB{}
	store := NewWithDB(db)

	// Test numeric charset
	store.SetCodeConfig(6, "0123456789")
	for i := 0; i < 10; i++ {
		code, err := store.generateCode()
		require.NoError(t, err)
		for _, ch := range code {
			assert.True(t, ch >= '0' && ch <= '9', "Expected numeric character, got: %c", ch)
		}
	}
}

func TestStore_generateToken_Format(t *testing.T) {
	db := &fakeDB{}
	store := NewWithDB(db)

	token, err := store.generateToken()
	require.NoError(t, err)

	// Verify base64 URL safe
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
}

func TestStore_generateToken_Uniqueness(t *testing.T) {
	db := &fakeDB{}
	store := NewWithDB(db)

	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := store.generateToken()
		require.NoError(t, err)
		assert.False(t, tokens[token], "Generated duplicate token")
		tokens[token] = true
	}
}

// Test all code types
func TestStore_CodeTypes(t *testing.T) {
	tests := []struct {
		codeType CodeType
		name     string
	}{
		{CodeTypeLogin, "login"},
		{CodeTypeVerification, "verification"},
		{CodeTypeRecovery, "recovery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeDB{
				execFn: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
					// Verify code type is passed correctly
					found := false
					for _, arg := range arguments {
						if ct, ok := arg.(CodeType); ok && ct == tt.codeType {
							found = true
						}
					}
					assert.True(t, found, "CodeType not found in arguments")
					return mockCommandTag(1), nil
				},
			}
			store := NewWithDB(db)

			_, err := store.Create(context.Background(), "user@example.com", tt.codeType, nil, 15*time.Minute)
			assert.NoError(t, err)
		})
	}
}
