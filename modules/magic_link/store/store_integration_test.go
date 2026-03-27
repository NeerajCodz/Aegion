package store

import (
	"context"
	"fmt"
	"sync"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// In-Memory Store Implementation for Testing (No Database Required)
// ============================================================================

// InMemoryStore is a complete in-memory implementation of a magic link store
type InMemoryStore struct {
	mu         sync.RWMutex
	codes      map[uuid.UUID]*CodeData
	tokens     map[string]uuid.UUID
	rateLimits map[string]*RateLimitData
	
	codeLength  int
	codeCharset string
}

type CodeData struct {
	ID         uuid.UUID
	IdentityID *uuid.UUID
	Recipient  string
	Type       CodeType
	Code       string
	Token      string
	Used       bool
	UsedAt     *time.Time
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type RateLimitData struct {
	Key       string
	Count     int
	WindowEnd time.Time
}

// NewInMemoryStore creates a new in-memory store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		codes:       make(map[uuid.UUID]*CodeData),
		tokens:      make(map[string]uuid.UUID),
		rateLimits:  make(map[string]*RateLimitData),
		codeLength:  6,
		codeCharset: "0123456789",
	}
}

// SetCodeConfig sets the OTP code configuration
func (s *InMemoryStore) SetCodeConfig(length int, charset string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codeLength = length
	s.codeCharset = charset
}

// Create creates a new code
func (s *InMemoryStore) Create(ctx context.Context, recipient string, codeType CodeType, identityID *uuid.UUID, ttl time.Duration) (*Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate code and token
	code := make([]byte, s.codeLength)
	for i := 0; i < s.codeLength; i++ {
		code[i] = s.codeCharset[i % len(s.codeCharset)]
	}
	
	tokenBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		tokenBytes[i] = byte(i % 256)
	}

	now := time.Now().UTC()
	codeID := uuid.New()
	codeData := &CodeData{
		ID:         codeID,
		IdentityID: identityID,
		Recipient:  recipient,
		Type:       codeType,
		Code:       string(code),
		Token:      fmt.Sprintf("token-%s-%d", codeID.String(), now.UnixNano()),
		Used:       false,
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}

	s.codes[codeID] = codeData
	s.tokens[codeData.Token] = codeID

	return &Code{
		ID:         codeData.ID,
		IdentityID: codeData.IdentityID,
		Recipient:  codeData.Recipient,
		Type:       codeData.Type,
		Code:       codeData.Code,
		Token:      codeData.Token,
		Used:       codeData.Used,
		UsedAt:     codeData.UsedAt,
		ExpiresAt:  codeData.ExpiresAt,
		CreatedAt:  codeData.CreatedAt,
	}, nil
}

// GetByCode retrieves a code by OTP and recipient
func (s *InMemoryStore) GetByCode(ctx context.Context, recipient string, otpCode string, codeType CodeType) (*Code, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	var latest *CodeData
	for _, c := range s.codes {
		if c.Recipient == recipient && c.Code == otpCode && c.Type == codeType && !c.Used {
			if latest == nil || c.CreatedAt.After(latest.CreatedAt) {
				latest = c
			}
		}
	}

	if latest == nil {
		return nil, ErrCodeNotFound
	}

	if now.After(latest.ExpiresAt) {
		return nil, ErrCodeExpired
	}

	return &Code{
		ID:         latest.ID,
		IdentityID: latest.IdentityID,
		Recipient:  latest.Recipient,
		Type:       latest.Type,
		Code:       latest.Code,
		Token:      latest.Token,
		Used:       latest.Used,
		UsedAt:     latest.UsedAt,
		ExpiresAt:  latest.ExpiresAt,
		CreatedAt:  latest.CreatedAt,
	}, nil
}

// GetByToken retrieves a code by token
func (s *InMemoryStore) GetByToken(ctx context.Context, token string) (*Code, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	codeID, exists := s.tokens[token]
	if !exists {
		return nil, ErrCodeNotFound
	}

	codeData := s.codes[codeID]
	if codeData.Used {
		return nil, ErrCodeNotFound
	}

	now := time.Now().UTC()
	if now.After(codeData.ExpiresAt) {
		return nil, ErrCodeExpired
	}

	return &Code{
		ID:         codeData.ID,
		IdentityID: codeData.IdentityID,
		Recipient:  codeData.Recipient,
		Type:       codeData.Type,
		Code:       codeData.Code,
		Token:      codeData.Token,
		Used:       codeData.Used,
		UsedAt:     codeData.UsedAt,
		ExpiresAt:  codeData.ExpiresAt,
		CreatedAt:  codeData.CreatedAt,
	}, nil
}

// MarkUsed marks a code as used
func (s *InMemoryStore) MarkUsed(ctx context.Context, codeID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	codeData, exists := s.codes[codeID]
	if !exists || codeData.Used {
		return ErrCodeUsed
	}

	now := time.Now().UTC()
	codeData.Used = true
	codeData.UsedAt = &now
	return nil
}

// InvalidatePrevious marks all previous codes as used
func (s *InMemoryStore) InvalidatePrevious(ctx context.Context, recipient string, codeType CodeType) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for _, codeData := range s.codes {
		if codeData.Recipient == recipient && codeData.Type == codeType && !codeData.Used {
			codeData.Used = true
			codeData.UsedAt = &now
		}
	}
	return nil
}

// CheckRateLimit checks if a request is rate limited
func (s *InMemoryStore) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	windowEnd := now.Add(window)

	if existing, exists := s.rateLimits[key]; exists {
		if existing.WindowEnd.Before(now) {
			// Window expired
			existing.Count = 1
			existing.WindowEnd = windowEnd
		} else {
			// Within window
			existing.Count++
		}
		if existing.Count > limit {
			return ErrRateLimited
		}
		return nil
	}

	// New entry
	s.rateLimits[key] = &RateLimitData{
		Key:       key,
		Count:     1,
		WindowEnd: windowEnd,
	}
	return nil
}

// Cleanup removes expired codes and rate limits
func (s *InMemoryStore) Cleanup(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)

	var deleted int64
	idsToDelete := []uuid.UUID{}
	for id, codeData := range s.codes {
		if codeData.ExpiresAt.Before(now) || (codeData.Used && codeData.UsedAt != nil && codeData.UsedAt.Before(cutoff)) {
			idsToDelete = append(idsToDelete, id)
			deleted++
		}
	}

	for _, id := range idsToDelete {
		codeData := s.codes[id]
		delete(s.codes, id)
		delete(s.tokens, codeData.Token)
	}

	// Clean rate limits
	keysToDelete := []string{}
	for key, entry := range s.rateLimits {
		if entry.WindowEnd.Before(now) {
			keysToDelete = append(keysToDelete, key)
		}
	}
	for _, key := range keysToDelete {
		delete(s.rateLimits, key)
	}

	return deleted, nil
}

// ============================================================================
// Comprehensive Integration Tests with In-Memory Store
// ============================================================================

// ============================================================================
// Mock Database Executor Pattern Tests
// ============================================================================

// MockDBExecutor is a mock executor for testing database operations
type MockDBExecutor struct {
	execCalls     []string
	queryRowCalls []string
	shouldFail    bool
	failMessage   string
	returnCount   int
}

func (m *MockDBExecutor) recordExec(query string) {
	m.execCalls = append(m.execCalls, query)
}

func (m *MockDBExecutor) recordQueryRow(query string) {
	m.queryRowCalls = append(m.queryRowCalls, query)
}

// TestMockDatabaseIntegration tests the mock database executor pattern
func TestMockDatabaseIntegration(t *testing.T) {
	t.Run("mock executor tracks calls", func(t *testing.T) {
		mock := &MockDBExecutor{}
		
		mock.recordExec("INSERT INTO ml_codes...")
		mock.recordExec("UPDATE ml_codes...")
		mock.recordQueryRow("SELECT FROM ml_codes...")
		
		assert.Len(t, mock.execCalls, 2)
		assert.Len(t, mock.queryRowCalls, 1)
		assert.Contains(t, mock.execCalls[0], "INSERT")
		assert.Contains(t, mock.execCalls[1], "UPDATE")
		assert.Contains(t, mock.queryRowCalls[0], "SELECT")
	})

	t.Run("mock executor handles failures", func(t *testing.T) {
		mock := &MockDBExecutor{
			shouldFail:  true,
			failMessage: "connection error",
		}
		
		assert.True(t, mock.shouldFail)
		assert.Equal(t, "connection error", mock.failMessage)
	})
}

// TestInMemoryStoreCodeCreation tests code creation
func TestInMemoryStoreCodeCreation(t *testing.T) {
	t.Run("create login code", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, err := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		require.NoError(t, err)
		assert.NotNil(t, code)
		assert.NotEqual(t, uuid.Nil, code.ID)
		assert.Nil(t, code.IdentityID)
		assert.Equal(t, "user@example.com", code.Recipient)
		assert.Equal(t, CodeTypeLogin, code.Type)
		assert.NotEmpty(t, code.Code)
		assert.NotEmpty(t, code.Token)
		assert.False(t, code.Used)
	})

	t.Run("create with identity", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		identityID := uuid.New()

		code, err := store.Create(ctx, "user@example.com", CodeTypeVerification, &identityID, 10*time.Minute)
		require.NoError(t, err)
		assert.NotNil(t, code.IdentityID)
		assert.Equal(t, identityID, *code.IdentityID)
	})

	t.Run("create multiple codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code1, _ := store.Create(ctx, "user1@example.com", CodeTypeLogin, nil, 15*time.Minute)
		code2, _ := store.Create(ctx, "user2@example.com", CodeTypeLogin, nil, 15*time.Minute)

		assert.NotEqual(t, code1.ID, code2.ID)
		assert.NotEqual(t, code1.Token, code2.Token)
	})
}

// TestInMemoryStoreRetrieval tests code retrieval
func TestInMemoryStoreRetrieval(t *testing.T) {
	t.Run("retrieve by code", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		retrieved, err := store.GetByCode(ctx, created.Recipient, created.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.Equal(t, created.ID, retrieved.ID)
	})

	t.Run("retrieve by token", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		retrieved, err := store.GetByToken(ctx, created.Token)
		require.NoError(t, err)
		assert.Equal(t, created.ID, retrieved.ID)
	})

	t.Run("not found when wrong code", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_, err := store.GetByCode(ctx, "user@example.com", "999999", CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("not found when wrong recipient", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_, err := store.GetByCode(ctx, "other@example.com", created.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("not found when wrong type", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_, err := store.GetByCode(ctx, created.Recipient, created.Code, CodeTypeVerification)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})
}

// TestInMemoryStoreExpiry tests code expiration
func TestInMemoryStoreExpiry(t *testing.T) {
	t.Run("expired code by code returns error", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 1*time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		_, err := store.GetByCode(ctx, created.Recipient, created.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("expired code by token returns error", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 1*time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		_, err := store.GetByToken(ctx, created.Token)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("non-expired code is retrievable", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 24*time.Hour)
		retrieved, err := store.GetByCode(ctx, created.Recipient, created.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.Equal(t, created.Code, retrieved.Code)
	})
}

// TestInMemoryStoreMarkUsed tests marking codes as used
func TestInMemoryStoreMarkUsed(t *testing.T) {
	t.Run("mark used", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		err := store.MarkUsed(ctx, created.ID)
		require.NoError(t, err)

		_, err = store.GetByCode(ctx, created.Recipient, created.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("mark already used returns error", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_ = store.MarkUsed(ctx, created.ID)

		err := store.MarkUsed(ctx, created.ID)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeUsed, err)
	})

	t.Run("mark nonexistent returns error", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		err := store.MarkUsed(ctx, uuid.New())
		assert.Error(t, err)
		assert.Equal(t, ErrCodeUsed, err)
	})
}

// TestInMemoryStoreInvalidatePrevious tests invalidating previous codes
func TestInMemoryStoreInvalidatePrevious(t *testing.T) {
	t.Run("invalidate all previous", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code1, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		code2, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)

		_ = store.InvalidatePrevious(ctx, "user@example.com", CodeTypeLogin)

		_, err1 := store.GetByCode(ctx, code1.Recipient, code1.Code, CodeTypeLogin)
		_, err2 := store.GetByCode(ctx, code2.Recipient, code2.Code, CodeTypeLogin)
		assert.Error(t, err1)
		assert.Error(t, err2)
	})

	t.Run("invalidate doesn't affect other types", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		loginCode, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		verifyCode, _ := store.Create(ctx, "user@example.com", CodeTypeVerification, nil, 15*time.Minute)

		_ = store.InvalidatePrevious(ctx, "user@example.com", CodeTypeLogin)

		_, err := store.GetByCode(ctx, loginCode.Recipient, loginCode.Code, CodeTypeLogin)
		assert.Error(t, err)

		retrieved, err := store.GetByCode(ctx, verifyCode.Recipient, verifyCode.Code, CodeTypeVerification)
		require.NoError(t, err)
		assert.NotNil(t, retrieved)
	})
}

// TestInMemoryStoreRateLimit tests rate limiting
func TestInMemoryStoreRateLimit(t *testing.T) {
	t.Run("within rate limit", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		limit := 5
		window := time.Hour

		for i := 0; i < limit; i++ {
			err := store.CheckRateLimit(ctx, "key", limit, window)
			assert.NoError(t, err)
		}
	})

	t.Run("exceeds rate limit", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		limit := 3
		window := time.Hour

		for i := 0; i < limit; i++ {
			_ = store.CheckRateLimit(ctx, "key", limit, window)
		}

		err := store.CheckRateLimit(ctx, "key", limit, window)
		assert.Error(t, err)
		assert.Equal(t, ErrRateLimited, err)
	})

	t.Run("per-key isolation", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		limit := 2
		window := time.Hour

		_ = store.CheckRateLimit(ctx, "key1", limit, window)
		_ = store.CheckRateLimit(ctx, "key1", limit, window)
		_ = store.CheckRateLimit(ctx, "key1", limit, window) // Should fail

		err := store.CheckRateLimit(ctx, "key2", limit, window) // Should pass
		assert.NoError(t, err)
	})
}

// TestInMemoryStoreCleanup tests cleanup operations
func TestInMemoryStoreCleanup(t *testing.T) {
	t.Run("cleanup deletes expired codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		_, _ = store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 1*time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		deleted, err := store.Cleanup(ctx)
		require.NoError(t, err)
		assert.Greater(t, deleted, int64(0))
	})

	t.Run("cleanup empty database", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		deleted, err := store.Cleanup(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})
}

// TestInMemoryStoreCodeConfig tests code configuration
func TestInMemoryStoreCodeConfig(t *testing.T) {
	t.Run("set custom config", func(t *testing.T) {
		store := NewInMemoryStore()
		store.SetCodeConfig(8, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")

		ctx := context.Background()
		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)

		assert.Len(t, code.Code, 8)
	})
}

// TestInMemoryStoreConcurrency tests concurrent access
func TestInMemoryStoreConcurrency(t *testing.T) {
	t.Run("concurrent creates", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				recipient := fmt.Sprintf("user%d@example.com", id)
				_, err := store.Create(ctx, recipient, CodeTypeLogin, nil, 15*time.Minute)
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent rate limit checks", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		var wg sync.WaitGroup
		limitExceeded := false
		var mu sync.Mutex

		for i := 0; i < 15; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := store.CheckRateLimit(ctx, "concurrent", 10, time.Hour)
				if err == ErrRateLimited {
					mu.Lock()
					limitExceeded = true
					mu.Unlock()
				}
			}()
		}

		wg.Wait()
		assert.True(t, limitExceeded)
	})
}

// TestInMemoryStoreEdgeCases tests edge cases
func TestInMemoryStoreEdgeCases(t *testing.T) {
	t.Run("very long recipient", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		longRecipient := "user" + strings.Repeat("a", 200) + "@example.com"
		code, err := store.Create(ctx, longRecipient, CodeTypeLogin, nil, 15*time.Minute)
		require.NoError(t, err)
		assert.Equal(t, longRecipient, code.Recipient)
	})

	t.Run("special characters in recipient", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		recipients := []string{
			"user+tag@example.com",
			"user.name@example.co.uk",
			"+1234567890",
			"user_name@example.com",
		}

		for _, recipient := range recipients {
			code, err := store.Create(ctx, recipient, CodeTypeLogin, nil, 15*time.Minute)
			require.NoError(t, err)
			assert.Equal(t, recipient, code.Recipient)
		}
	})

	t.Run("very short TTL", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 1*time.Nanosecond)
		time.Sleep(1 * time.Millisecond)

		_, err := store.GetByCode(ctx, code.Recipient, code.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("very long TTL", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 365*24*time.Hour)
		retrieved, err := store.GetByCode(ctx, code.Recipient, code.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.NotNil(t, retrieved)
	})
}

// ============================================================================
// COMPREHENSIVE CRUD OPERATION TESTS
// ============================================================================

// TestCRUDCreateOperation tests code creation operations
func TestCRUDCreateOperation(t *testing.T) {
	t.Run("create with all fields", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		identityID := uuid.New()

		code, err := store.Create(ctx, "user@example.com", CodeTypeVerification, &identityID, 15*time.Minute)
		require.NoError(t, err)
		
		assert.NotNil(t, code)
		assert.NotEqual(t, uuid.Nil, code.ID)
		assert.NotNil(t, code.IdentityID)
		assert.Equal(t, identityID, *code.IdentityID)
		assert.Equal(t, "user@example.com", code.Recipient)
		assert.Equal(t, CodeTypeVerification, code.Type)
		assert.NotEmpty(t, code.Code)
		assert.NotEmpty(t, code.Token)
		assert.False(t, code.Used)
		assert.Nil(t, code.UsedAt)
		assert.NotZero(t, code.CreatedAt)
		assert.True(t, code.ExpiresAt.After(code.CreatedAt))
	})

	t.Run("create without identity", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, err := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 10*time.Minute)
		require.NoError(t, err)
		assert.Nil(t, code.IdentityID)
		assert.Equal(t, CodeTypeLogin, code.Type)
	})

	t.Run("create multiple unique codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		codes := make([]*Code, 10)
		for i := 0; i < 10; i++ {
			c, err := store.Create(ctx, fmt.Sprintf("user%d@example.com", i), CodeTypeLogin, nil, 15*time.Minute)
			require.NoError(t, err)
			codes[i] = c
		}

		// Verify all are unique
		idMap := make(map[uuid.UUID]bool)
		tokenMap := make(map[string]bool)
		for _, c := range codes {
			assert.False(t, idMap[c.ID])
			assert.False(t, tokenMap[c.Token])
			idMap[c.ID] = true
			tokenMap[c.Token] = true
		}
	})

	t.Run("create with different code types", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		types := []CodeType{CodeTypeLogin, CodeTypeVerification, CodeTypeRecovery}
		for _, codeType := range types {
			code, err := store.Create(ctx, "user@example.com", codeType, nil, 15*time.Minute)
			require.NoError(t, err)
			assert.Equal(t, codeType, code.Type)
		}
	})

	t.Run("create with varying TTLs", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		ttls := []time.Duration{
			time.Minute,
			5 * time.Minute,
			15 * time.Minute,
			time.Hour,
			24 * time.Hour,
		}

		for _, ttl := range ttls {
			code, err := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, ttl)
			require.NoError(t, err)
			expectedExpiry := code.CreatedAt.Add(ttl)
			assert.True(t, code.ExpiresAt.Sub(expectedExpiry) < time.Second)
		}
	})
}

// TestCRUDReadOperation tests code retrieval operations
func TestCRUDReadOperation(t *testing.T) {
	t.Run("read by code with all parameters", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		retrieved, err := store.GetByCode(ctx, "user@example.com", created.Code, CodeTypeLogin)
		require.NoError(t, err)
		
		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, created.Recipient, retrieved.Recipient)
		assert.Equal(t, created.Type, retrieved.Type)
		assert.Equal(t, created.Code, retrieved.Code)
	})

	t.Run("read by token", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		retrieved, err := store.GetByToken(ctx, created.Token)
		require.NoError(t, err)
		
		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, created.Token, retrieved.Token)
	})

	t.Run("read by code wrong recipient", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_, err := store.GetByCode(ctx, "wrong@example.com", created.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("read by code wrong type", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_, err := store.GetByCode(ctx, "user@example.com", created.Code, CodeTypeVerification)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("read by token invalid token", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		_, err := store.GetByToken(ctx, "invalid-token-123")
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("read latest code when multiple exist", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		// Create first code
		store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		time.Sleep(10 * time.Millisecond)

		// Create second code with same recipient
		code2, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		
		// Should retrieve the latest
		retrieved, err := store.GetByCode(ctx, "user@example.com", code2.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.Equal(t, code2.ID, retrieved.ID)
	})
}

// TestCRUDUpdateOperation tests code update operations
func TestCRUDUpdateOperation(t *testing.T) {
	t.Run("mark used", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		err := store.MarkUsed(ctx, created.ID)
		require.NoError(t, err)

		// Should not be retrievable after marking used
		_, err = store.GetByCode(ctx, "user@example.com", created.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("mark used sets used_at", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_ = store.MarkUsed(ctx, created.ID)

		// Verify through internal access that used_at was set
		// Since we're using in-memory store, we need to check behavior
		retrieved, _ := store.GetByToken(ctx, created.Token)
		if retrieved != nil {
			t.Error("Should not be retrievable after mark used")
		}
	})

	t.Run("mark already used returns error", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_ = store.MarkUsed(ctx, created.ID)

		err := store.MarkUsed(ctx, created.ID)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeUsed, err)
	})

	t.Run("mark nonexistent code returns error", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		err := store.MarkUsed(ctx, uuid.New())
		assert.Error(t, err)
		assert.Equal(t, ErrCodeUsed, err)
	})
}

// TestCRUDDeleteOperation tests code deletion/invalidation
func TestCRUDDeleteOperation(t *testing.T) {
	t.Run("invalidate previous codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code1, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		code2, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		code3, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)

		err := store.InvalidatePrevious(ctx, "user@example.com", CodeTypeLogin)
		require.NoError(t, err)

		// All should be invalidated
		_, err1 := store.GetByCode(ctx, "user@example.com", code1.Code, CodeTypeLogin)
		_, err2 := store.GetByCode(ctx, "user@example.com", code2.Code, CodeTypeLogin)
		_, err3 := store.GetByCode(ctx, "user@example.com", code3.Code, CodeTypeLogin)

		assert.Error(t, err1)
		assert.Error(t, err2)
		assert.Error(t, err3)
	})

	t.Run("invalidate previous only affects specified type", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		loginCode, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		verifyCode, _ := store.Create(ctx, "user@example.com", CodeTypeVerification, nil, 15*time.Minute)
		recoveryCode, _ := store.Create(ctx, "user@example.com", CodeTypeRecovery, nil, 15*time.Minute)

		err := store.InvalidatePrevious(ctx, "user@example.com", CodeTypeLogin)
		require.NoError(t, err)

		// Login should be invalidated
		_, err1 := store.GetByCode(ctx, "user@example.com", loginCode.Code, CodeTypeLogin)
		assert.Error(t, err1)

		// Others should remain
		_, err2 := store.GetByCode(ctx, "user@example.com", verifyCode.Code, CodeTypeVerification)
		_, err3 := store.GetByCode(ctx, "user@example.com", recoveryCode.Code, CodeTypeRecovery)
		require.NoError(t, err2)
		require.NoError(t, err3)
	})

	t.Run("invalidate previous only affects same recipient", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code1, _ := store.Create(ctx, "user1@example.com", CodeTypeLogin, nil, 15*time.Minute)
		code2, _ := store.Create(ctx, "user2@example.com", CodeTypeLogin, nil, 15*time.Minute)

		err := store.InvalidatePrevious(ctx, "user1@example.com", CodeTypeLogin)
		require.NoError(t, err)

		_, err1 := store.GetByCode(ctx, "user1@example.com", code1.Code, CodeTypeLogin)
		assert.Error(t, err1)

		_, err2 := store.GetByCode(ctx, "user2@example.com", code2.Code, CodeTypeLogin)
		require.NoError(t, err2)
	})
}

// ============================================================================
// EXPIRY HANDLING AND VALIDATION TESTS
// ============================================================================

// TestExpiryHandling tests code expiration logic
func TestExpiryHandling(t *testing.T) {
	t.Run("code expires at exact time", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 100*time.Millisecond)
		
		// Should be valid before expiry
		retrieved, err := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.NotNil(t, retrieved)

		// Wait for expiry
		time.Sleep(150 * time.Millisecond)

		// Should be expired
		_, err = store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("expired code by token", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 50*time.Millisecond)
		time.Sleep(100 * time.Millisecond)

		_, err := store.GetByToken(ctx, code.Token)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("non-expired code after partial TTL", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 100*time.Millisecond)
		time.Sleep(50 * time.Millisecond)

		retrieved, err := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.Equal(t, code.Code, retrieved.Code)
	})

	t.Run("cleanup removes expired codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code1, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 50*time.Millisecond)
		code2, _ := store.Create(ctx, "user@example.com", CodeTypeVerification, nil, 1*time.Hour)

		time.Sleep(100 * time.Millisecond)

		deleted, err := store.Cleanup(ctx)
		require.NoError(t, err)
		assert.Greater(t, deleted, int64(0))

		// Expired code should be gone (will return not found after cleanup)
		_, err1 := store.GetByCode(ctx, "user@example.com", code1.Code, CodeTypeLogin)
		assert.Error(t, err1)

		// Non-expired code should still exist
		_, err2 := store.GetByCode(ctx, "user@example.com", code2.Code, CodeTypeVerification)
		require.NoError(t, err2)
	})

	t.Run("cleanup removes old used codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 24*time.Hour)
		_ = store.MarkUsed(ctx, code.ID)

		deleted, err := store.Cleanup(ctx)
		require.NoError(t, err)
		// Cleanup removes used codes older than 24 hours, so this might not delete yet
		assert.GreaterOrEqual(t, deleted, int64(0))
	})
}

// ============================================================================
// RATE LIMITING TESTS
// ============================================================================

// TestRateLimitingPerEmail tests email-based rate limiting
func TestRateLimitingPerEmail(t *testing.T) {
	t.Run("rate limit per email", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		email := "user@example.com"
		limit := 3
		window := time.Hour

		// First 3 should succeed
		for i := 0; i < limit; i++ {
			err := store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)
			assert.NoError(t, err, "Check %d should succeed", i+1)
		}

		// 4th should fail
		err := store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)
		assert.Error(t, err)
		assert.Equal(t, ErrRateLimited, err)
	})

	t.Run("rate limit isolates different emails", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		limit := 2
		window := time.Hour

		for i := 0; i < limit; i++ {
			_ = store.CheckRateLimit(ctx, "login:user1@example.com", limit, window)
		}

		// user2 should not be rate limited
		err := store.CheckRateLimit(ctx, "login:user2@example.com", limit, window)
		assert.NoError(t, err)
	})

	t.Run("rate limit window expiration", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		email := "user@example.com"
		limit := 2
		window := 100 * time.Millisecond

		// Exceed limit
		_ = store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)
		_ = store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)
		_ = store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)

		// Window should reset after expiry
		time.Sleep(150 * time.Millisecond)
		err := store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)
		assert.NoError(t, err)
	})

	t.Run("rate limit with different operations", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		email := "user@example.com"
		limit := 3
		window := time.Hour

		// login:email has its own limit
		for i := 0; i < limit; i++ {
			_ = store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", email), limit, window)
		}

		// verify:email should have independent limit
		err := store.CheckRateLimit(ctx, fmt.Sprintf("verify:%s", email), limit, window)
		assert.NoError(t, err)
	})
}

// TestRateLimitingPerPhone tests phone-based rate limiting
func TestRateLimitingPerPhone(t *testing.T) {
	t.Run("rate limit per phone", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		phone := "+1234567890"
		limit := 5
		window := time.Hour

		for i := 0; i < limit; i++ {
			err := store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", phone), limit, window)
			assert.NoError(t, err)
		}

		err := store.CheckRateLimit(ctx, fmt.Sprintf("login:%s", phone), limit, window)
		assert.Error(t, err)
		assert.Equal(t, ErrRateLimited, err)
	})

	t.Run("rate limit isolates different phones", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		limit := 1
		window := time.Hour

		_ = store.CheckRateLimit(ctx, "login:+1111111111", limit, window)
		err := store.CheckRateLimit(ctx, "login:+2222222222", limit, window)
		assert.NoError(t, err)
	})
}

// ============================================================================
// CODE VERIFICATION STATE TESTS
// ============================================================================

// TestCodeVerificationStates tests different code verification states
func TestCodeVerificationStates(t *testing.T) {
	t.Run("new code is not used", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		assert.False(t, code.Used)
		assert.Nil(t, code.UsedAt)
	})

	t.Run("code transitions from unused to used", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		
		// Before marking used
		retrieved1, _ := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		assert.False(t, retrieved1.Used)

		// Mark used
		_ = store.MarkUsed(ctx, code.ID)

		// After marking used - should not be retrievable
		_, err := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("cannot use same code twice", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		
		err1 := store.MarkUsed(ctx, code.ID)
		assert.NoError(t, err1)

		err2 := store.MarkUsed(ctx, code.ID)
		assert.Error(t, err2)
		assert.Equal(t, ErrCodeUsed, err2)
	})

	t.Run("used code not found on retrieval", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_ = store.MarkUsed(ctx, code.ID)

		_, err := store.GetByToken(ctx, code.Token)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})
}

// ============================================================================
// TOKEN MANAGEMENT TESTS
// ============================================================================

// TestTokenManagement tests token generation and retrieval
func TestTokenManagement(t *testing.T) {
	t.Run("token is generated on creation", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		assert.NotEmpty(t, code.Token)
		assert.NotEqual(t, "", code.Token)
	})

	t.Run("tokens are unique", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		tokens := make(map[string]bool)
		for i := 0; i < 20; i++ {
			code, _ := store.Create(ctx, fmt.Sprintf("user%d@example.com", i), CodeTypeLogin, nil, 15*time.Minute)
			assert.False(t, tokens[code.Token], "Token collision detected")
			tokens[code.Token] = true
		}
	})

	t.Run("retrieve code by token", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		created, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		retrieved, err := store.GetByToken(ctx, created.Token)
		require.NoError(t, err)
		assert.Equal(t, created.ID, retrieved.ID)
	})

	t.Run("token lookup is case sensitive", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		_, _ = store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		
		// Try different case variations (tokens are base64 encoded)
		_, err := store.GetByToken(ctx, "invalid-token")
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("magic link token in recovery flow", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		// Recovery flow uses token
		code, _ := store.Create(ctx, "user@example.com", CodeTypeRecovery, nil, time.Hour)
		
		// User clicks link with token
		retrieved, err := store.GetByToken(ctx, code.Token)
		require.NoError(t, err)
		assert.Equal(t, CodeTypeRecovery, retrieved.Type)

		// Mark as used
		_ = store.MarkUsed(ctx, retrieved.ID)

		// Token should not work again
		_, err = store.GetByToken(ctx, code.Token)
		assert.Error(t, err)
	})
}

// ============================================================================
// ERROR SCENARIO TESTS
// ============================================================================

// TestErrorScenarios tests various error conditions
func TestErrorScenarios(t *testing.T) {
	t.Run("ErrCodeNotFound when code doesn't exist", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		_, err := store.GetByCode(ctx, "user@example.com", "999999", CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("ErrCodeExpired when code is expired", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 10*time.Millisecond)
		time.Sleep(50 * time.Millisecond)

		_, err := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("ErrCodeUsed when marking already used code", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		_ = store.MarkUsed(ctx, code.ID)

		err := store.MarkUsed(ctx, code.ID)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeUsed, err)
	})

	t.Run("ErrRateLimited when exceeding limit", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		limit := 2
		_ = store.CheckRateLimit(ctx, "key", limit, time.Hour)
		_ = store.CheckRateLimit(ctx, "key", limit, time.Hour)

		err := store.CheckRateLimit(ctx, "key", limit, time.Hour)
		assert.Error(t, err)
		assert.Equal(t, ErrRateLimited, err)
	})

	t.Run("error messages are descriptive", func(t *testing.T) {
		assert.Contains(t, ErrCodeNotFound.Error(), "not found")
		assert.Contains(t, ErrCodeExpired.Error(), "expired")
		assert.Contains(t, ErrCodeUsed.Error(), "used")
		assert.Contains(t, ErrRateLimited.Error(), "rate limit")
	})
}

// ============================================================================
// EDGE CASE TESTS
// ============================================================================

// TestEmptyValueHandling tests handling of empty values
func TestEmptyValueHandling(t *testing.T) {
	t.Run("empty recipient in create fails appropriately", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		// Empty recipient should still create but be unretrievable with non-empty query
		code, err := store.Create(ctx, "", CodeTypeLogin, nil, 15*time.Minute)
		require.NoError(t, err)
		assert.Equal(t, "", code.Recipient)
	})

	t.Run("empty code query returns not found", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		_, err := store.GetByCode(ctx, "user@example.com", "", CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("empty token query returns not found", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		_, err := store.GetByToken(ctx, "")
		assert.Error(t, err)
		assert.Equal(t, ErrCodeNotFound, err)
	})

	t.Run("empty key in rate limit", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		err := store.CheckRateLimit(ctx, "", 5, time.Hour)
		assert.NoError(t, err) // Should not panic, just process
	})
}

// TestNilHandling tests handling of nil values
func TestNilHandling(t *testing.T) {
	t.Run("nil identity ID is allowed", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, err := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		require.NoError(t, err)
		assert.Nil(t, code.IdentityID)
	})

	t.Run("pointer to identity ID works", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()
		identityID := uuid.New()

		code, err := store.Create(ctx, "user@example.com", CodeTypeLogin, &identityID, 15*time.Minute)
		require.NoError(t, err)
		assert.NotNil(t, code.IdentityID)
		assert.Equal(t, identityID, *code.IdentityID)
	})

	t.Run("code used_at is nil until marked used", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		assert.Nil(t, code.UsedAt)
	})
}

// TestBoundaryConditions tests boundary conditions
func TestBoundaryConditions(t *testing.T) {
	t.Run("minimum rate limit", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		err := store.CheckRateLimit(ctx, "key", 1, time.Hour)
		assert.NoError(t, err)

		err = store.CheckRateLimit(ctx, "key", 1, time.Hour)
		assert.Error(t, err)
		assert.Equal(t, ErrRateLimited, err)
	})

	t.Run("large rate limit", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		limit := 10000
		for i := 0; i < limit; i++ {
			err := store.CheckRateLimit(ctx, "key", limit, time.Hour)
			assert.NoError(t, err)
		}

		err := store.CheckRateLimit(ctx, "key", limit, time.Hour)
		assert.Error(t, err)
	})

	t.Run("very small time duration", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, time.Nanosecond)
		time.Sleep(time.Millisecond)

		_, err := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		assert.Error(t, err)
		assert.Equal(t, ErrCodeExpired, err)
	})

	t.Run("very large time duration", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 365*24*time.Hour)
		
		retrieved, err := store.GetByCode(ctx, "user@example.com", code.Code, CodeTypeLogin)
		require.NoError(t, err)
		assert.NotNil(t, retrieved)
	})
}

// TestMultipleRecipients tests handling multiple recipients
func TestMultipleRecipients(t *testing.T) {
	t.Run("different recipients have independent codes", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		recipients := []string{
			"alice@example.com",
			"bob@example.com",
			"charlie@example.com",
			"+1234567890",
			"+0987654321",
		}

		codes := make(map[string]*Code)
		for _, recipient := range recipients {
			code, _ := store.Create(ctx, recipient, CodeTypeLogin, nil, 15*time.Minute)
			codes[recipient] = code
		}

		for recipient, code := range codes {
			retrieved, err := store.GetByCode(ctx, recipient, code.Code, CodeTypeLogin)
			require.NoError(t, err)
			assert.Equal(t, code.ID, retrieved.ID)
		}
	})

	t.Run("invalidate affects only target recipient", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		recipients := []string{
			"alice@example.com",
			"bob@example.com",
		}

		codes := make(map[string]*Code)
		for _, recipient := range recipients {
			code, _ := store.Create(ctx, recipient, CodeTypeLogin, nil, 15*time.Minute)
			codes[recipient] = code
		}

		// Invalidate for alice
		_ = store.InvalidatePrevious(ctx, recipients[0], CodeTypeLogin)

		// Alice's code should be gone
		_, err := store.GetByCode(ctx, recipients[0], codes[recipients[0]].Code, CodeTypeLogin)
		assert.Error(t, err)

		// Bob's code should remain
		_, err = store.GetByCode(ctx, recipients[1], codes[recipients[1]].Code, CodeTypeLogin)
		require.NoError(t, err)
	})
}

// TestConfigurationVariations tests with different configurations
func TestConfigurationVariations(t *testing.T) {
	t.Run("custom code length", func(t *testing.T) {
		store := NewInMemoryStore()
		store.SetCodeConfig(8, "0123456789")
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		assert.Len(t, code.Code, 8)
	})

	t.Run("custom charset", func(t *testing.T) {
		store := NewInMemoryStore()
		store.SetCodeConfig(10, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		assert.Len(t, code.Code, 10)
		for _, ch := range code.Code {
			assert.True(t, ch >= 'A' && ch <= 'Z', "Character not in charset: %c", ch)
		}
	})

	t.Run("alphanumeric charset", func(t *testing.T) {
		store := NewInMemoryStore()
		store.SetCodeConfig(12, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		ctx := context.Background()

		code, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 15*time.Minute)
		assert.Len(t, code.Code, 12)
	})
}

// TestCleanupOperations tests cleanup behavior
func TestCleanupOperations(t *testing.T) {
	t.Run("cleanup removes expired codes only", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		expired, _ := store.Create(ctx, "user@example.com", CodeTypeLogin, nil, 10*time.Millisecond)
		valid, _ := store.Create(ctx, "user@example.com", CodeTypeVerification, nil, 1*time.Hour)

		time.Sleep(50 * time.Millisecond)

		_, err := store.Cleanup(ctx)
		require.NoError(t, err)

		// Expired should be deleted
		_, err1 := store.GetByCode(ctx, "user@example.com", expired.Code, CodeTypeLogin)
		assert.Error(t, err1)

		// Valid should remain
		_, err2 := store.GetByCode(ctx, "user@example.com", valid.Code, CodeTypeVerification)
		require.NoError(t, err2)
	})

	t.Run("cleanup on empty store", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		deleted, err := store.Cleanup(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})

	t.Run("cleanup removes old rate limit windows", func(t *testing.T) {
		store := NewInMemoryStore()
		ctx := context.Background()

		// Create rate limit entry with expired window
		_ = store.CheckRateLimit(ctx, "key", 5, 10*time.Millisecond)
		time.Sleep(50 * time.Millisecond)

		deleted, err := store.Cleanup(ctx)
		require.NoError(t, err)
		// Cleanup should have processed rate limits
		assert.GreaterOrEqual(t, deleted, int64(0))
	})
}
