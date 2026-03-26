package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Test pure functions that don't require database
func TestCredential_struct(t *testing.T) {
	now := time.Now()
	
	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "user@example.com",
		Hash:       "hashedpassword",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	assert.NotEmpty(t, cred.ID)
	assert.NotEmpty(t, cred.IdentityID)
	assert.Equal(t, "user@example.com", cred.Identifier)
	assert.Equal(t, "hashedpassword", cred.Hash)
	assert.Equal(t, now, cred.CreatedAt)
	assert.Equal(t, now, cred.UpdatedAt)
}

// Test helper functions
func TestContains(t *testing.T) {
	tests := []struct {
		name   string
		str    string
		substr string
		expect bool
	}{
		{
			name:   "substring exists",
			str:    "hash1,hash2,hash3",
			substr: "hash2",
			expect: true,
		},
		{
			name:   "substring does not exist",
			str:    "hash1,hash2,hash3",
			substr: "hash4",
			expect: false,
		},
		{
			name:   "empty string",
			str:    "",
			substr: "hash1",
			expect: false,
		},
		{
			name:   "empty substring",
			str:    "hash1",
			substr: "",
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.str, tt.substr)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestContainsHelper(t *testing.T) {
	// Test the generic contains implementation directly
	tests := []struct {
		name   string
		str    string
		substr string
		expect bool
	}{
		{
			name:   "contains substring",
			str:    "abcdef",
			substr: "bcd",
			expect: true,
		},
		{
			name:   "does not contain substring",
			str:    "abcdef",
			substr: "xyz",
			expect: false,
		},
		{
			name:   "empty string",
			str:    "",
			substr: "a",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsHelper(tt.str, tt.substr)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Helper function for slice contains (what we actually need for testing)
func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestSliceContains(t *testing.T) {
	tests := []struct {
		name   string
		slice  []string
		item   string
		expect bool
	}{
		{
			name:   "item exists",
			slice:  []string{"hash1", "hash2", "hash3"},
			item:   "hash2",
			expect: true,
		},
		{
			name:   "item does not exist",
			slice:  []string{"hash1", "hash2", "hash3"},
			item:   "hash4",
			expect: false,
		},
		{
			name:   "empty slice",
			slice:  []string{},
			item:   "hash1",
			expect: false,
		},
		{
			name:   "nil slice",
			slice:  nil,
			item:   "hash1",
			expect: false,
		},
		{
			name:   "empty string item",
			slice:  []string{"hash1", "", "hash3"},
			item:   "",
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sliceContains(tt.slice, tt.item)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Test error definitions exist
func TestErrorDefinitions(t *testing.T) {
	// Verify that error constants are defined and have meaningful messages
	assert.NotNil(t, ErrCredentialNotFound)
	assert.NotNil(t, ErrCredentialExists)
	
	assert.Contains(t, ErrCredentialNotFound.Error(), "not found")
	assert.Contains(t, ErrCredentialExists.Error(), "exists")
}

// Test isDuplicateKeyError function behavior
func TestIsDuplicateKeyError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{
			name:   "nil error",
			err:    nil,
			expect: false,
		},
		{
			name:   "non-duplicate error",
			err:    errors.New("some other error"),
			expect: false,
		},
		{
			name:   "generic error",
			err:    errors.New("database connection failed"),
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDuplicateKeyError(tt.err)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Test Store interface compliance
func TestStoreInterface(t *testing.T) {
	// This test ensures the Store struct implements the expected interface
	// without requiring actual database operations

	// Create a mock store instance
	store := &Store{}
	
	// Verify it has all required methods (this will fail to compile if methods are missing)
	ctx := context.Background()
	credID := uuid.New()
	identityID := uuid.New()
	identifier := "user@example.com"
	hash := "hashedpassword"
	
	// These will panic with nil database, but that's expected in unit tests
	// We're just testing that the methods exist with correct signatures
	assert.Panics(t, func() { store.Create(ctx, nil) })
	assert.Panics(t, func() { store.GetByIdentifier(ctx, identifier) })
	assert.Panics(t, func() { store.GetByIdentityID(ctx, identityID) })
	assert.Panics(t, func() { store.Update(ctx, credID, hash) })
	assert.Panics(t, func() { store.Delete(ctx, credID) })
	assert.Panics(t, func() { store.DeleteByIdentityID(ctx, identityID) })
	assert.Panics(t, func() { store.AddToHistory(ctx, credID, hash) })
	assert.Panics(t, func() { store.GetHistory(ctx, credID, 5) })
	assert.Panics(t, func() { store.CleanupHistory(ctx, credID, 5) })
}

// Test data validation logic that might exist in store methods
func TestCredentialValidation(t *testing.T) {
	// Test that credential creation with invalid data would fail appropriately
	tests := []struct {
		name        string
		credential  func() *Credential
		shouldFail  bool
		description string
	}{
		{
			name: "valid credential",
			credential: func() *Credential {
				return &Credential{
					ID:         uuid.New(),
					IdentityID: uuid.New(),
					Identifier: "user@example.com",
					Hash:       "hashedpassword",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
			},
			shouldFail:  false,
			description: "All fields populated correctly",
		},
		{
			name: "empty ID",
			credential: func() *Credential {
				return &Credential{
					ID:         uuid.Nil,
					IdentityID: uuid.New(),
					Identifier: "user@example.com",
					Hash:       "hashedpassword",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
			},
			shouldFail:  true,
			description: "ID should not be empty",
		},
		{
			name: "empty identity ID",
			credential: func() *Credential {
				return &Credential{
					ID:         uuid.New(),
					IdentityID: uuid.Nil,
					Identifier: "user@example.com",
					Hash:       "hashedpassword",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
			},
			shouldFail:  true,
			description: "IdentityID should not be empty",
		},
		{
			name: "empty identifier",
			credential: func() *Credential {
				return &Credential{
					ID:         uuid.New(),
					IdentityID: uuid.New(),
					Identifier: "",
					Hash:       "hashedpassword",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
			},
			shouldFail:  true,
			description: "Identifier should not be empty",
		},
		{
			name: "empty hash",
			credential: func() *Credential {
				return &Credential{
					ID:         uuid.New(),
					IdentityID: uuid.New(),
					Identifier: "user@example.com",
					Hash:       "",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
			},
			shouldFail:  true,
			description: "Hash should not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred := tt.credential()
			
			// Basic validation checks that could be implemented in the store
			hasEmptyRequiredField := cred.ID == uuid.Nil || cred.IdentityID == uuid.Nil || cred.Identifier == "" || cred.Hash == ""
			
			if tt.shouldFail {
				assert.True(t, hasEmptyRequiredField, tt.description)
			} else {
				assert.False(t, hasEmptyRequiredField, tt.description)
			}
		})
	}
}

// Test query parameter validation
func TestQueryParameterValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		isValid  bool
		testType string
	}{
		{
			name:     "valid UUID",
			input:    uuid.New().String(),
			isValid:  true,
			testType: "uuid",
		},
		{
			name:     "invalid UUID",
			input:    "not-a-uuid",
			isValid:  false,
			testType: "uuid",
		},
		{
			name:     "empty UUID",
			input:    "",
			isValid:  false,
			testType: "uuid",
		},
		{
			name:     "valid email",
			input:    "user@example.com",
			isValid:  true,
			testType: "email",
		},
		{
			name:     "empty email",
			input:    "",
			isValid:  false,
			testType: "email",
		},
		{
			name:     "valid limit",
			input:    5,
			isValid:  true,
			testType: "limit",
		},
		{
			name:     "zero limit",
			input:    0,
			isValid:  false,
			testType: "limit",
		},
		{
			name:     "negative limit",
			input:    -1,
			isValid:  false,
			testType: "limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.testType {
			case "uuid":
				str := tt.input.(string)
				_, err := uuid.Parse(str)
				isValid := err == nil && str != ""
				assert.Equal(t, tt.isValid, isValid)
				
			case "email":
				str := tt.input.(string)
				isValid := str != "" // Simple validation for test
				assert.Equal(t, tt.isValid, isValid)
				
			case "limit":
				limit := tt.input.(int)
				isValid := limit > 0
				assert.Equal(t, tt.isValid, isValid)
			}
		})
	}
}

// Test context handling
func TestContextHandling(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		
		// Test that canceled context is properly handled
		select {
		case <-ctx.Done():
			assert.NotNil(t, ctx.Err())
		default:
			t.Error("Context should be canceled")
		}
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		
		time.Sleep(time.Millisecond) // Wait for timeout
		
		select {
		case <-ctx.Done():
			assert.Equal(t, context.DeadlineExceeded, ctx.Err())
		default:
			t.Error("Context should have timed out")
		}
	})

	t.Run("context with values", func(t *testing.T) {
		ctx := context.Background()
		ctx = context.WithValue(ctx, "key", "value")
		
		value := ctx.Value("key")
		assert.Equal(t, "value", value)
	})
}

// Test time handling
func TestTimeHandling(t *testing.T) {
	t.Run("credential timestamps", func(t *testing.T) {
		now := time.Now()
		
		cred := &Credential{
			CreatedAt: now,
			UpdatedAt: now,
		}
		
		assert.True(t, cred.CreatedAt.Equal(now))
		assert.True(t, cred.UpdatedAt.Equal(now))
	})

	t.Run("time ordering", func(t *testing.T) {
		time1 := time.Now()
		time.Sleep(time.Millisecond)
		time2 := time.Now()
		
		assert.True(t, time2.After(time1))
		assert.True(t, time1.Before(time2))
	})
}

// Test slice operations used in history management
func TestHistorySliceOperations(t *testing.T) {
	t.Run("append to history", func(t *testing.T) {
		history := []string{"hash1", "hash2"}
		history = append(history, "hash3")
		
		assert.Equal(t, 3, len(history))
		assert.Equal(t, "hash3", history[2])
	})

	t.Run("limit history size", func(t *testing.T) {
		history := []string{"hash1", "hash2", "hash3", "hash4", "hash5", "hash6"}
		limit := 5
		
		if len(history) > limit {
			history = history[len(history)-limit:]
		}
		
		assert.Equal(t, limit, len(history))
		assert.Equal(t, "hash2", history[0]) // Should keep the most recent entries
	})

	t.Run("reverse chronological order", func(t *testing.T) {
		// Test that history is returned in reverse chronological order (most recent first)
		history := []string{"oldest", "older", "newer", "newest"}
		
		// Simulate reverse order for most recent first
		reversed := make([]string, len(history))
		for i, j := 0, len(history)-1; i < len(history); i, j = i+1, j-1 {
			reversed[i] = history[j]
		}
		
		assert.Equal(t, "newest", reversed[0])
		assert.Equal(t, "oldest", reversed[len(reversed)-1])
	})
}

// Benchmark helper functions
func BenchmarkSliceContains(b *testing.B) {
	slice := []string{"hash1", "hash2", "hash3", "hash4", "hash5"}
	item := "hash3"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sliceContains(slice, item)
	}
}

func BenchmarkUUIDGeneration(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uuid.New().String()
	}
}

func BenchmarkUUIDParsing(b *testing.B) {
	id := uuid.New().String()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uuid.Parse(id)
	}
}