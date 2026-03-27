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
