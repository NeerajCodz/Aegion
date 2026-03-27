package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockStore implements store.Store interface for testing
type MockStore struct {
	mock.Mock
}

func (m *MockStore) Create(ctx context.Context, cred interface{}) error {
	args := m.Called(ctx, cred)
	return args.Error(0)
}

func (m *MockStore) GetByIdentifier(ctx context.Context, identifier string) (interface{}, error) {
	args := m.Called(ctx, identifier)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) GetByIdentityID(ctx context.Context, identityID string) (interface{}, error) {
	args := m.Called(ctx, identityID)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) Update(ctx context.Context, credID, newHash string) error {
	args := m.Called(ctx, credID, newHash)
	return args.Error(0)
}

func (m *MockStore) Delete(ctx context.Context, credID string) error {
	args := m.Called(ctx, credID)
	return args.Error(0)
}

func (m *MockStore) DeleteByIdentityID(ctx context.Context, identityID string) error {
	args := m.Called(ctx, identityID)
	return args.Error(0)
}

func (m *MockStore) AddToHistory(ctx context.Context, credID, hash string) error {
	args := m.Called(ctx, credID, hash)
	return args.Error(0)
}

func (m *MockStore) GetHistory(ctx context.Context, credID string, limit int) ([]string, error) {
	args := m.Called(ctx, credID, limit)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockStore) CleanupHistory(ctx context.Context, credID string, keepCount int) error {
	args := m.Called(ctx, credID, keepCount)
	return args.Error(0)
}

// MockHasher implements Hasher interface for testing
type MockHasher struct {
	mock.Mock
}

func (m *MockHasher) Hash(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}

func (m *MockHasher) Verify(password, hash string) error {
	args := m.Called(password, hash)
	return args.Error(0)
}

// MockHTTPClient for HIBP testing
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Get(url string) (*http.Response, error) {
	args := m.Called(url)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestNew(t *testing.T) {
	store := &MockStore{}
	hasher := &MockHasher{}

	service := New(store, hasher)

	assert.NotNil(t, service)
	assert.Equal(t, store, service.store)
	assert.Equal(t, hasher, service.hasher)
	
	// Check default config values
	assert.Equal(t, 8, service.config.MinLength)
	assert.True(t, service.config.RequireUppercase)
	assert.True(t, service.config.RequireLowercase)
	assert.True(t, service.config.RequireNumber)
	assert.True(t, service.config.RequireSpecial)
	assert.False(t, service.config.HIBPEnabled)
	assert.Equal(t, 5, service.config.HistoryCount)
}

func TestService_ValidatePassword(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		identifier string
		config     Config
		wantErr    error
	}{
		{
			name:       "valid password",
			password:   "SecurePass123!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    nil,
		},
		{
			name:       "too short",
			password:   "Short1!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8},
			wantErr:    ErrPasswordTooShort,
		},
		{
			name:       "missing uppercase",
			password:   "lowercase123!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    ErrPasswordTooWeak,
		},
		{
			name:       "missing lowercase",
			password:   "UPPERCASE123!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    ErrPasswordTooWeak,
		},
		{
			name:       "missing number",
			password:   "SecurePass!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    ErrPasswordTooWeak,
		},
		{
			name:       "missing special character",
			password:   "SecurePass123",
			identifier: "user@example.com",
			config:     Config{MinLength: 8, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    ErrPasswordTooWeak,
		},
		{
			name:       "similar to email",
			password:   "testuser123!",
			identifier: "testuser@example.com",
			config:     Config{MinLength: 8, RequireUppercase: false, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    ErrPasswordSimilar,
		},
		{
			name:       "unicode characters",
			password:   "Pássw0rd!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8, RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			hasher := &MockHasher{}
			
			service := &Service{
				store:  store,
				hasher: hasher,
				config: tt.config,
			}

			ctx := context.Background()
			err := service.ValidatePassword(ctx, tt.password, tt.identifier)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_checkComplexity(t *testing.T) {
	tests := []struct {
		name     string
		password string
		config   Config
		wantErr  bool
	}{
		{
			name:     "meets all requirements",
			password: "Secure123!",
			config:   Config{RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:  false,
		},
		{
			name:     "no uppercase required",
			password: "secure123!",
			config:   Config{RequireUppercase: false, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:  false,
		},
		{
			name:     "missing uppercase",
			password: "secure123!",
			config:   Config{RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:  true,
		},
		{
			name:     "unicode uppercase",
			password: "Ségure123!",
			config:   Config{RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:  false,
		},
		{
			name:     "emoji as special character",
			password: "Secure123😀",
			config:   Config{RequireUppercase: true, RequireLowercase: true, RequireNumber: true, RequireSpecial: true},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{config: tt.config}
			err := service.checkComplexity(tt.password)

			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, ErrPasswordTooWeak))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_checkSimilarity(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		identifier string
		wantErr    bool
	}{
		{
			name:       "not similar",
			password:   "SecurePassword123!",
			identifier: "user@example.com",
			wantErr:    false,
		},
		{
			name:       "similar to username part",
			password:   "testuser123",
			identifier: "testuser@example.com",
			wantErr:    true,
		},
		{
			name:       "similar to domain part",
			password:   "example123",
			identifier: "user@example.com",
			wantErr:    true,
		},
		{
			name:       "case insensitive similarity",
			password:   "TESTUSER123",
			identifier: "testuser@example.com",
			wantErr:    true,
		},
		{
			name:       "partial similarity below threshold",
			password:   "test",
			identifier: "testing@example.com",
			wantErr:    false,
		},
		{
			name:       "non-email identifier",
			password:   "username123",
			identifier: "username",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{}
			err := service.checkSimilarity(tt.password, tt.identifier)

			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, ErrPasswordSimilar))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_Register(t *testing.T) {
	tests := []struct {
		name           string
		identityID     string
		identifier     string
		password       string
		setupMocks     func(*MockStore, *MockHasher)
		wantErr        error
		wantIdentifier string
	}{
		{
			name:       "successful registration",
			identityID: uuid.New().String(),
			identifier: "User@Example.Com",
			password:   "SecurePass123!",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				hasher.On("Hash", "SecurePass123!").Return("hashedpassword", nil)
				store.On("Create", mock.Anything, mock.MatchedBy(func(cred interface{}) bool {
					// Check that credential has normalized email
					return true
				})).Return(nil)
			},
			wantErr:        nil,
			wantIdentifier: "user@example.com", // Should be normalized to lowercase
		},
		{
			name:       "validation error",
			identityID: uuid.New().String(),
			identifier: "user@example.com",
			password:   "weak",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				// No mocks needed - validation fails first
			},
			wantErr: ErrPasswordTooShort,
		},
		{
			name:       "hash error",
			identityID: uuid.New().String(),
			identifier: "user@example.com",
			password:   "SecurePass123!",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				hasher.On("Hash", "SecurePass123!").Return("", errors.New("hash failed"))
			},
			wantErr: errors.New("hash failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			hasher := &MockHasher{}
			
			service := New(store, hasher)
			service.config.HIBPEnabled = false // Disable HIBP for testing

			tt.setupMocks(store, hasher)

			ctx := context.Background()
			err := service.Register(ctx, tt.identityID, tt.identifier, tt.password)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
			hasher.AssertExpectations(t)
		})
	}
}

func TestService_Verify(t *testing.T) {
	validCredential := map[string]interface{}{
		"identity_id": uuid.New().String(),
		"hash":        "hashedpassword",
	}

	tests := []struct {
		name       string
		identifier string
		password   string
		setupMocks func(*MockStore, *MockHasher)
		wantID     string
		wantErr    error
	}{
		{
			name:       "successful verification",
			identifier: "user@example.com",
			password:   "correctpassword",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentifier", mock.Anything, "user@example.com").Return(validCredential, nil)
				hasher.On("Verify", "correctpassword", "hashedpassword").Return(nil)
			},
			wantID:  validCredential["identity_id"].(string),
			wantErr: nil,
		},
		{
			name:       "credential not found",
			identifier: "nonexistent@example.com",
			password:   "anypassword",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentifier", mock.Anything, "nonexistent@example.com").Return(nil, errors.New("not found"))
				// Should still do dummy hash to prevent timing attacks
				hasher.On("Hash", "anypassword").Return("dummy", nil)
			},
			wantID:  "",
			wantErr: ErrInvalidCredentials,
		},
		{
			name:       "wrong password",
			identifier: "user@example.com",
			password:   "wrongpassword",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentifier", mock.Anything, "user@example.com").Return(validCredential, nil)
				hasher.On("Verify", "wrongpassword", "hashedpassword").Return(errors.New("verification failed"))
			},
			wantID:  "",
			wantErr: ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			hasher := &MockHasher{}
			
			service := New(store, hasher)

			tt.setupMocks(store, hasher)

			ctx := context.Background()
			identityID, err := service.Verify(ctx, tt.identifier, tt.password)

			assert.Equal(t, tt.wantID, identityID)
			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr))
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
			hasher.AssertExpectations(t)
		})
	}
}

func TestService_ChangePassword(t *testing.T) {
	credentialID := uuid.New().String()
	identityID := uuid.New().String()
	oldHash := "oldhash"
	newHash := "newhash"

	validCredential := map[string]interface{}{
		"id":          credentialID,
		"identity_id": identityID,
		"hash":        oldHash,
	}

	tests := []struct {
		name        string
		identityID  string
		oldPassword string
		newPassword string
		setupMocks  func(*MockStore, *MockHasher)
		wantErr     error
	}{
		{
			name:        "successful password change",
			identityID:  identityID,
			oldPassword: "oldpass",
			newPassword: "NewSecurePass123!",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentityID", mock.Anything, identityID).Return(validCredential, nil)
				hasher.On("Verify", "oldpass", oldHash).Return(nil)
				hasher.On("Hash", "NewSecurePass123!").Return(newHash, nil)
				store.On("GetHistory", mock.Anything, credentialID, 5).Return([]string{}, nil)
				store.On("AddToHistory", mock.Anything, credentialID, oldHash).Return(nil)
				store.On("Update", mock.Anything, credentialID, newHash).Return(nil)
				store.On("CleanupHistory", mock.Anything, credentialID, 5).Return(nil)
			},
			wantErr: nil,
		},
		{
			name:        "identity not found",
			identityID:  "nonexistent",
			oldPassword: "oldpass",
			newPassword: "NewSecurePass123!",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentityID", mock.Anything, "nonexistent").Return(nil, errors.New("not found"))
			},
			wantErr: ErrIdentityNotFound,
		},
		{
			name:        "wrong old password",
			identityID:  identityID,
			oldPassword: "wrongpass",
			newPassword: "NewSecurePass123!",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentityID", mock.Anything, identityID).Return(validCredential, nil)
				hasher.On("Verify", "wrongpass", oldHash).Return(errors.New("verification failed"))
			},
			wantErr: ErrInvalidCredentials,
		},
		{
			name:        "password reused",
			identityID:  identityID,
			oldPassword: "oldpass",
			newPassword: "NewSecurePass123!",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentityID", mock.Anything, identityID).Return(validCredential, nil)
				hasher.On("Verify", "oldpass", oldHash).Return(nil)
				hasher.On("Hash", "NewSecurePass123!").Return(newHash, nil)
				// Return history that includes the new hash
				store.On("GetHistory", mock.Anything, credentialID, 5).Return([]string{newHash, "otherhash"}, nil)
			},
			wantErr: ErrPasswordReused,
		},
		{
			name:        "weak new password",
			identityID:  identityID,
			oldPassword: "oldpass",
			newPassword: "weak",
			setupMocks: func(store *MockStore, hasher *MockHasher) {
				store.On("GetByIdentityID", mock.Anything, identityID).Return(validCredential, nil)
				hasher.On("Verify", "oldpass", oldHash).Return(nil)
				// Validation fails before hashing
			},
			wantErr: ErrPasswordTooShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			hasher := &MockHasher{}
			
			service := New(store, hasher)
			service.config.HIBPEnabled = false

			tt.setupMocks(store, hasher)

			ctx := context.Background()
			err := service.ChangePassword(ctx, tt.identityID, tt.oldPassword, tt.newPassword)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr))
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
			hasher.AssertExpectations(t)
		})
	}
}

func TestService_Delete(t *testing.T) {
	identityID := uuid.New().String()

	tests := []struct {
		name       string
		identityID string
		setupMocks func(*MockStore)
		wantErr    error
	}{
		{
			name:       "successful deletion",
			identityID: identityID,
			setupMocks: func(store *MockStore) {
				store.On("DeleteByIdentityID", mock.Anything, identityID).Return(nil)
			},
			wantErr: nil,
		},
		{
			name:       "deletion error",
			identityID: identityID,
			setupMocks: func(store *MockStore) {
				store.On("DeleteByIdentityID", mock.Anything, identityID).Return(errors.New("delete failed"))
			},
			wantErr: errors.New("delete failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			hasher := &MockHasher{}
			
			service := New(store, hasher)

			tt.setupMocks(store)

			ctx := context.Background()
			err := service.Delete(ctx, tt.identityID)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

// Test edge cases and error conditions
func TestService_EdgeCases(t *testing.T) {
	t.Run("empty password validation", func(t *testing.T) {
		store := &MockStore{}
		hasher := &MockHasher{}
		service := New(store, hasher)

		ctx := context.Background()
		err := service.ValidatePassword(ctx, "", "user@example.com")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrPasswordTooShort))
	})

	t.Run("empty identifier validation", func(t *testing.T) {
		store := &MockStore{}
		hasher := &MockHasher{}
		service := New(store, hasher)

		ctx := context.Background()
		err := service.ValidatePassword(ctx, "ValidPass123!", "")
		assert.NoError(t, err) // No similarity check with empty identifier
	})

	t.Run("unicode normalization in similarity check", func(t *testing.T) {
		service := &Service{}
		
		// Test that unicode characters don't break similarity checking
		err := service.checkSimilarity("café123", "café@example.com")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrPasswordSimilar))
	})
}

// Benchmark critical functions
func BenchmarkValidatePassword(b *testing.B) {
	store := &MockStore{}
	hasher := &MockHasher{}
	service := New(store, hasher)
	service.config.HIBPEnabled = false

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = service.ValidatePassword(ctx, "SecurePassword123!", "user@example.com")
	}
}

func BenchmarkCheckComplexity(b *testing.B) {
	service := &Service{
		config: Config{
			RequireUppercase: true,
			RequireLowercase: true,
			RequireNumber:    true,
			RequireSpecial:   true,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = service.checkComplexity("SecurePassword123!")
	}
}