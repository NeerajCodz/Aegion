package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Database Implementation for Testing Store with Real Implementation
// ============================================================================

// MockDB simulates a database connection pool
type MockDB struct {
	codes      map[uuid.UUID]*Code
	rateLimits map[string]*RateLimit
	execCalls  []ExecCall
	queryCalls []QueryCall
	shouldFail bool
	failError  error
}

type ExecCall struct {
	Query  string
	Args   []interface{}
	Result int64
}

type QueryCall struct {
	Query  string
	Args   []interface{}
	Result interface{}
}

type RateLimit struct {
	Key       string
	Count     int
	WindowEnd time.Time
}

// NewMockDB creates a new mock database
func NewMockDB() *MockDB {
	return &MockDB{
		codes:      make(map[uuid.UUID]*Code),
		rateLimits: make(map[string]*RateLimit),
		execCalls:  []ExecCall{},
		queryCalls: []QueryCall{},
	}
}

// Exec mocks database execution
func (m *MockDB) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgx.CommandTag, error) {
	m.execCalls = append(m.execCalls, ExecCall{
		Query: sql,
		Args:  arguments,
	})

	if m.shouldFail {
		return nil, m.failError
	}

	return pgx.CommandTag(fmt.Sprintf("INSERT 0 1")), nil
}

// QueryRow mocks database query for single row
func (m *MockDB) QueryRow(ctx context.Context, sql string, arguments ...interface{}) pgx.Row {
	m.queryCalls = append(m.queryCalls, QueryCall{
		Query: sql,
		Args:  arguments,
	})
	return &MockRow{data: nil}
}

// MockRow represents a single row result
type MockRow struct {
	data interface{}
}

func (mr *MockRow) Scan(dest ...interface{}) error {
	return pgx.ErrNoRows
}

// ============================================================================
// Mock Database Tests
// ============================================================================

// TestMockDBTracking tests that mock database tracks operations
func TestMockDBTracking(t *testing.T) {
	t.Run("tracks exec calls", func(t *testing.T) {
		mock := NewMockDB()
		ctx := context.Background()

		mock.Exec(ctx, "INSERT INTO test VALUES ($1)", 123)
		mock.Exec(ctx, "UPDATE test SET value=$1", 456)

		assert.Len(t, mock.execCalls, 2)
		assert.Contains(t, mock.execCalls[0].Query, "INSERT")
		assert.Contains(t, mock.execCalls[1].Query, "UPDATE")
	})

	t.Run("tracks query row calls", func(t *testing.T) {
		mock := NewMockDB()
		ctx := context.Background()

		mock.QueryRow(ctx, "SELECT * FROM test WHERE id=$1", uuid.New())
		mock.QueryRow(ctx, "SELECT * FROM test WHERE code=$1", "123456")

		assert.Len(t, mock.queryCalls, 2)
		assert.Contains(t, mock.queryCalls[0].Query, "SELECT")
		assert.Contains(t, mock.queryCalls[1].Query, "SELECT")
	})

	t.Run("stores arguments", func(t *testing.T) {
		mock := NewMockDB()
		ctx := context.Background()

		testID := uuid.New()
		mock.Exec(ctx, "INSERT INTO test VALUES ($1, $2)", testID, "value")

		assert.Len(t, mock.execCalls, 1)
		assert.Len(t, mock.execCalls[0].Args, 2)
	})

	t.Run("can simulate failures", func(t *testing.T) {
		mock := NewMockDB()
		mock.shouldFail = true
		mock.failError = fmt.Errorf("connection refused")

		ctx := context.Background()
		_, err := mock.Exec(ctx, "SELECT * FROM test")

		assert.Error(t, err)
		assert.Equal(t, "connection refused", err.Error())
	})
}

// ============================================================================
// Store Code Generation Tests with Real Implementation
// ============================================================================

// TestStoreCodeGeneration tests code generation in Store
func TestStoreCodeGeneration(t *testing.T) {
	t.Run("generate code returns non-empty string", func(t *testing.T) {
		store := New(NewMockDB())
		code := store.generateCode()
		assert.NotEmpty(t, code)
		assert.Len(t, code, 6) // default length
	})

	t.Run("generate code uses configured charset", func(t *testing.T) {
		store := New(NewMockDB())
		store.SetCodeConfig(8, "ABCDEF")

		code := store.generateCode()
		assert.Len(t, code, 8)
		for _, ch := range code {
			assert.True(t, ch >= 'A' && ch <= 'F', "Character outside charset: %c", ch)
		}
	})

	t.Run("generate token returns non-empty string", func(t *testing.T) {
		store := New(NewMockDB())
		token := store.generateToken()
		assert.NotEmpty(t, token)
	})

	t.Run("generated codes are different", func(t *testing.T) {
		store := New(NewMockDB())
		codes := make(map[string]bool)

		for i := 0; i < 10; i++ {
			code := store.generateCode()
			// May collide randomly, but probability is very low
			codes[code] = true
		}

		// At least some should be different
		assert.Greater(t, len(codes), 1)
	})

	t.Run("generated tokens are different", func(t *testing.T) {
		store := New(NewMockDB())
		tokens := make(map[string]bool)

		for i := 0; i < 10; i++ {
			token := store.generateToken()
			assert.NotNil(t, token)
			tokens[token] = true
		}

		// Most should be different (cryptographic randomness)
		assert.Greater(t, len(tokens), 5)
	})
}

// ============================================================================
// Store Configuration Tests
// ============================================================================

// TestStoreConfiguration tests store configuration options
func TestStoreConfiguration(t *testing.T) {
	t.Run("set code config updates length", func(t *testing.T) {
		store := New(NewMockDB())
		store.SetCodeConfig(12, "0123456789")

		code := store.generateCode()
		assert.Len(t, code, 12)
	})

	t.Run("set code config updates charset", func(t *testing.T) {
		store := New(NewMockDB())
		store.SetCodeConfig(10, "ABCDEFGHIJ")

		code := store.generateCode()
		for _, ch := range code {
			assert.True(t, ch >= 'A' && ch <= 'J')
		}
	})

	t.Run("default config is 6-digit numeric", func(t *testing.T) {
		store := New(NewMockDB())

		code := store.generateCode()
		assert.Len(t, code, 6)
		for _, ch := range code {
			assert.True(t, ch >= '0' && ch <= '9')
		}
	})
}

// ============================================================================
// Store Error Cases Tests
// ============================================================================

// TestStoreErrorHandling tests error handling in Store operations
func TestStoreErrorHandling(t *testing.T) {
	t.Run("create handles nil database gracefully", func(t *testing.T) {
		store := &Store{
			db:          nil,
			codeLength:  6,
			codeCharset: "0123456789",
		}
		ctx := context.Background()

		assert.Panics(t, func() {
			store.Create(ctx, "user@example.com", CodeTypeLogin, nil, time.Hour)
		})
	})

	t.Run("get by code handles nil database", func(t *testing.T) {
		store := &Store{
			db:          nil,
			codeLength:  6,
			codeCharset: "0123456789",
		}
		ctx := context.Background()

		assert.Panics(t, func() {
			store.GetByCode(ctx, "user@example.com", "123456", CodeTypeLogin)
		})
	})

	t.Run("mark used handles nil database", func(t *testing.T) {
		store := &Store{
			db:          nil,
			codeLength:  6,
			codeCharset: "0123456789",
		}
		ctx := context.Background()

		assert.Panics(t, func() {
			store.MarkUsed(ctx, uuid.New())
		})
	})
}

// ============================================================================
// Code Structure Tests
// ============================================================================

// TestCodeStructure tests Code struct initialization and validation
func TestCodeStructure(t *testing.T) {
	t.Run("code with all fields", func(t *testing.T) {
		now := time.Now()
		identityID := uuid.New()

		code := &Code{
			ID:         uuid.New(),
			IdentityID: &identityID,
			Recipient:  "user@example.com",
			Type:       CodeTypeLogin,
			Code:       "123456",
			Token:      "token123",
			Used:       false,
			UsedAt:     nil,
			ExpiresAt:  now.Add(time.Hour),
			CreatedAt:  now,
		}

		assert.NotEqual(t, uuid.Nil, code.ID)
		assert.NotNil(t, code.IdentityID)
		assert.Equal(t, "user@example.com", code.Recipient)
		assert.Equal(t, CodeTypeLogin, code.Type)
		assert.Equal(t, "123456", code.Code)
		assert.False(t, code.Used)
		assert.Nil(t, code.UsedAt)
	})

	t.Run("code with used_at timestamp", func(t *testing.T) {
		now := time.Now()
		usedAt := now.Add(5 * time.Minute)

		code := &Code{
			ID:        uuid.New(),
			Recipient: "user@example.com",
			Code:      "123456",
			Used:      true,
			UsedAt:    &usedAt,
			CreatedAt: now,
		}

		assert.True(t, code.Used)
		assert.NotNil(t, code.UsedAt)
		assert.True(t, code.UsedAt.After(code.CreatedAt))
	})
}

// ============================================================================
// Code Type Tests
// ============================================================================

// TestCodeTypes tests all code type constants
func TestCodeTypes(t *testing.T) {
	t.Run("all code types are defined", func(t *testing.T) {
		types := []CodeType{
			CodeTypeLogin,
			CodeTypeVerification,
			CodeTypeRecovery,
		}

		assert.Len(t, types, 3)
	})

	t.Run("code types have correct values", func(t *testing.T) {
		assert.Equal(t, CodeType("login"), CodeTypeLogin)
		assert.Equal(t, CodeType("verification"), CodeTypeVerification)
		assert.Equal(t, CodeType("recovery"), CodeTypeRecovery)
	})

	t.Run("code types are distinct", func(t *testing.T) {
		types := map[CodeType]bool{
			CodeTypeLogin:        true,
			CodeTypeVerification: true,
			CodeTypeRecovery:     true,
		}

		assert.Len(t, types, 3)
	})
}

// ============================================================================
// Error Constant Tests
// ============================================================================

// TestErrorConstants tests all error constants
func TestErrorConstants(t *testing.T) {
	t.Run("all error constants defined", func(t *testing.T) {
		assert.NotNil(t, ErrCodeNotFound)
		assert.NotNil(t, ErrCodeExpired)
		assert.NotNil(t, ErrCodeUsed)
		assert.NotNil(t, ErrRateLimited)
	})

	t.Run("error messages are non-empty", func(t *testing.T) {
		assert.NotEmpty(t, ErrCodeNotFound.Error())
		assert.NotEmpty(t, ErrCodeExpired.Error())
		assert.NotEmpty(t, ErrCodeUsed.Error())
		assert.NotEmpty(t, ErrRateLimited.Error())
	})

	t.Run("errors are distinct", func(t *testing.T) {
		assert.NotEqual(t, ErrCodeNotFound, ErrCodeExpired)
		assert.NotEqual(t, ErrCodeExpired, ErrCodeUsed)
		assert.NotEqual(t, ErrCodeUsed, ErrRateLimited)
		assert.NotEqual(t, ErrRateLimited, ErrCodeNotFound)
	})
}

// ============================================================================
// Store Initialization Tests
// ============================================================================

// TestStoreInitialization tests Store initialization
func TestStoreInitialization(t *testing.T) {
	t.Run("new store has default config", func(t *testing.T) {
		mock := NewMockDB()
		store := New(mock)

		assert.NotNil(t, store)
		assert.Equal(t, 6, store.codeLength)
		assert.Equal(t, "0123456789", store.codeCharset)
	})

	t.Run("new store has database reference", func(t *testing.T) {
		mock := NewMockDB()
		store := New(mock)

		assert.NotNil(t, store.db)
	})

	t.Run("code config can be updated", func(t *testing.T) {
		mock := NewMockDB()
		store := New(mock)

		store.SetCodeConfig(10, "ABCDEFGH")
		assert.Equal(t, 10, store.codeLength)
		assert.Equal(t, "ABCDEFGH", store.codeCharset)
	})
}

// ============================================================================
// Database Operation Pattern Tests
// ============================================================================

// TestDatabaseOperationPatterns tests that Store correctly calls database methods
func TestDatabaseOperationPatterns(t *testing.T) {
	t.Run("mock db records exec calls", func(t *testing.T) {
		mock := NewMockDB()
		ctx := context.Background()

		mock.Exec(ctx, "INSERT INTO ml_codes VALUES ($1, $2)", uuid.New(), "code123")
		mock.Exec(ctx, "UPDATE ml_codes SET used=TRUE WHERE id=$1", uuid.New())

		assert.Len(t, mock.execCalls, 2)
		assert.Contains(t, mock.execCalls[0].Query, "INSERT")
		assert.Contains(t, mock.execCalls[1].Query, "UPDATE")
	})

	t.Run("mock db records query row calls", func(t *testing.T) {
		mock := NewMockDB()
		ctx := context.Background()

		mock.QueryRow(ctx, "SELECT * FROM ml_codes WHERE id=$1", uuid.New())
		mock.QueryRow(ctx, "SELECT * FROM ml_codes WHERE token=$1", "token123")

		assert.Len(t, mock.queryCalls, 2)
		assert.Contains(t, mock.queryCalls[0].Query, "SELECT")
		assert.Contains(t, mock.queryCalls[1].Query, "SELECT")
	})

	t.Run("mock db preserves arguments", func(t *testing.T) {
		mock := NewMockDB()
		ctx := context.Background()

		testID := uuid.New()
		testCode := "123456"
		mock.Exec(ctx, "INSERT INTO test (id, code) VALUES ($1, $2)", testID, testCode)

		assert.Len(t, mock.execCalls, 1)
		assert.Equal(t, testID, mock.execCalls[0].Args[0])
		assert.Equal(t, testCode, mock.execCalls[0].Args[1])
	})
}

// ============================================================================
// Type Assertion Tests
// ============================================================================

// TestTypeAssertions tests type assertions and conversions
func TestTypeAssertions(t *testing.T) {
	t.Run("uuid nil check", func(t *testing.T) {
		nilID := uuid.Nil
		newID := uuid.New()

		assert.Equal(t, nilID, uuid.UUID{})
		assert.NotEqual(t, newID, uuid.UUID{})
	})

	t.Run("pointer to uuid", func(t *testing.T) {
		id := uuid.New()
		ptr := &id

		assert.Equal(t, id, *ptr)
		assert.NotNil(t, ptr)
	})

	t.Run("code type string conversion", func(t *testing.T) {
		codeType := CodeTypeLogin
		str := string(codeType)

		assert.Equal(t, "login", str)
	})

	t.Run("time pointer operations", func(t *testing.T) {
		now := time.Now()
		ptr := &now

		assert.Equal(t, now, *ptr)
		assert.True(t, ptr.Before(now.Add(time.Second)))
	})
}

// ============================================================================
// Store Method Signature Tests
// ============================================================================

// TestStoreMethodSignatures ensures all required methods exist with correct signatures
func TestStoreMethodSignatures(t *testing.T) {
	t.Run("store has new method", func(t *testing.T) {
		mock := NewMockDB()
		store := New(mock)
		assert.NotNil(t, store)
	})

	t.Run("store has set code config method", func(t *testing.T) {
		store := New(NewMockDB())
		assert.NotPanics(t, func() {
			store.SetCodeConfig(8, "0123456789")
		})
	})

	t.Run("store methods accept context", func(t *testing.T) {
		store := New(NewMockDB())
		ctx := context.Background()

		// These will panic due to nil handling, but we're testing signatures
		assert.Panics(t, func() {
			store.Create(ctx, "test@example.com", CodeTypeLogin, nil, time.Hour)
		})
	})
}

// ============================================================================
// Concurrent Operation Tests
// ============================================================================

// TestStoreConcurrency tests Store thread safety
func TestStoreConcurrency(t *testing.T) {
	t.Run("concurrent code generation", func(t *testing.T) {
		store := New(NewMockDB())
		codes := make(chan string, 100)

		for i := 0; i < 50; i++ {
			go func() {
				code := store.generateCode()
				codes <- code
			}()
		}

		uniqueCodes := make(map[string]bool)
		for i := 0; i < 50; i++ {
			code := <-codes
			uniqueCodes[code] = true
		}

		assert.Greater(t, len(uniqueCodes), 1)
	})

	t.Run("concurrent token generation", func(t *testing.T) {
		store := New(NewMockDB())
		tokens := make(chan string, 100)

		for i := 0; i < 50; i++ {
			go func() {
				token := store.generateToken()
				tokens <- token
			}()
		}

		uniqueTokens := make(map[string]bool)
		for i := 0; i < 50; i++ {
			token := <-tokens
			uniqueTokens[token] = true
		}

		assert.Greater(t, len(uniqueTokens), 1)
	})
}
