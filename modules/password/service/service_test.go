package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/modules/password/store"
)

type mockHasher struct {
	hashFn   func(password string) (string, error)
	verifyFn func(password, hash string) (bool, error)
}

func (m *mockHasher) Hash(password string) (string, error) {
	if m.hashFn != nil {
		return m.hashFn(password)
	}
	return "hashed_" + password, nil
}

func (m *mockHasher) Verify(password, hash string) (bool, error) {
	if m.verifyFn != nil {
		return m.verifyFn(password, hash)
	}
	return hash == "hashed_"+password, nil
}

type memoryStore struct {
	credentialsByIdentity map[uuid.UUID]*store.Credential
	credentialsByID       map[uuid.UUID]*store.Credential
	credentialsByIdent    map[string]*store.Credential
	historyByCred         map[uuid.UUID][]string

	createErr     error
	getByIdentErr error
	getByIDErr    error
	updateErr     error
	deleteErr     error
	addHistoryErr error
	getHistoryErr error
	cleanupErr    error

	lastCreated   *store.Credential
	lastUpdatedID uuid.UUID
	lastUpdatedTo string
	deletedID     uuid.UUID
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		credentialsByIdentity: map[uuid.UUID]*store.Credential{},
		credentialsByID:       map[uuid.UUID]*store.Credential{},
		credentialsByIdent:    map[string]*store.Credential{},
		historyByCred:         map[uuid.UUID][]string{},
	}
}

func (m *memoryStore) seedCredential(identityID uuid.UUID, identifier, hash string) *store.Credential {
	c := &store.Credential{
		ID:         uuid.New(),
		IdentityID: identityID,
		Identifier: identifier,
		Hash:       hash,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	m.credentialsByIdentity[identityID] = c
	m.credentialsByID[c.ID] = c
	m.credentialsByIdent[identifier] = c
	return c
}

func (m *memoryStore) Create(ctx context.Context, cred *store.Credential) error {
	if m.createErr != nil {
		return m.createErr
	}
	cp := *cred
	m.lastCreated = &cp
	m.credentialsByIdentity[cred.IdentityID] = &cp
	m.credentialsByID[cred.ID] = &cp
	m.credentialsByIdent[cred.Identifier] = &cp
	return nil
}

func (m *memoryStore) GetByIdentifier(ctx context.Context, identifier string) (*store.Credential, error) {
	if m.getByIdentErr != nil {
		return nil, m.getByIdentErr
	}
	if c, ok := m.credentialsByIdent[identifier]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, store.ErrCredentialNotFound
}

func (m *memoryStore) GetByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Credential, error) {
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	if c, ok := m.credentialsByIdentity[identityID]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, store.ErrCredentialNotFound
}

func (m *memoryStore) Update(ctx context.Context, credID uuid.UUID, newHash string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	c, ok := m.credentialsByID[credID]
	if !ok {
		return store.ErrCredentialNotFound
	}
	c.Hash = newHash
	c.UpdatedAt = time.Now().UTC()
	m.lastUpdatedID = credID
	m.lastUpdatedTo = newHash
	return nil
}

func (m *memoryStore) DeleteByIdentityID(ctx context.Context, identityID uuid.UUID) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	c, ok := m.credentialsByIdentity[identityID]
	if !ok {
		return nil
	}
	delete(m.credentialsByIdentity, identityID)
	delete(m.credentialsByID, c.ID)
	delete(m.credentialsByIdent, c.Identifier)
	m.deletedID = identityID
	return nil
}

func (m *memoryStore) AddToHistory(ctx context.Context, credID uuid.UUID, hash string) error {
	if m.addHistoryErr != nil {
		return m.addHistoryErr
	}
	m.historyByCred[credID] = append([]string{hash}, m.historyByCred[credID]...)
	return nil
}

func (m *memoryStore) GetHistory(ctx context.Context, credID uuid.UUID, limit int) ([]string, error) {
	if m.getHistoryErr != nil {
		return nil, m.getHistoryErr
	}
	h := m.historyByCred[credID]
	if limit <= 0 || len(h) <= limit {
		return append([]string{}, h...), nil
	}
	return append([]string{}, h[:limit]...), nil
}

func (m *memoryStore) CleanupHistory(ctx context.Context, credID uuid.UUID, keepCount int) error {
	if m.cleanupErr != nil {
		return m.cleanupErr
	}
	h := m.historyByCred[credID]
	if keepCount < 0 {
		keepCount = 0
	}
	if len(h) > keepCount {
		m.historyByCred[credID] = append([]string{}, h[:keepCount]...)
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		MinLength:        8,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSpecial:   true,
		HIBPEnabled:      false,
		HistoryCount:     3,
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	s := New(newMemoryStore(), &mockHasher{}, Config{})
	assert.Equal(t, 8, s.config.MinLength)
	assert.Equal(t, 5, s.config.HistoryCount)
}

func TestRegister(t *testing.T) {
	identityID := uuid.New()
	mem := newMemoryStore()
	h := &mockHasher{
		hashFn: func(password string) (string, error) {
			assert.Equal(t, "GoodPass1!", password)
			return "hashed-ok", nil
		},
	}
	s := New(mem, h, defaultConfig())

	err := s.Register(context.Background(), identityID, "USER@Example.com", "GoodPass1!")
	require.NoError(t, err)
	require.NotNil(t, mem.lastCreated)
	assert.Equal(t, identityID, mem.lastCreated.IdentityID)
	assert.Equal(t, "user@example.com", mem.lastCreated.Identifier)
	assert.Equal(t, "hashed-ok", mem.lastCreated.Hash)
}

func TestRegisterErrors(t *testing.T) {
	t.Run("validation failure", func(t *testing.T) {
		s := New(newMemoryStore(), &mockHasher{}, defaultConfig())
		err := s.Register(context.Background(), uuid.New(), "u@example.com", "short")
		assert.ErrorIs(t, err, ErrPasswordTooShort)
	})

	t.Run("hash failure", func(t *testing.T) {
		mem := newMemoryStore()
		s := New(mem, &mockHasher{
			hashFn: func(password string) (string, error) {
				return "", errors.New("hash failed")
			},
		}, defaultConfig())
		err := s.Register(context.Background(), uuid.New(), "u@example.com", "GoodPass1!")
		assert.EqualError(t, err, "failed to hash password: hash failed")
	})

	t.Run("store create failure", func(t *testing.T) {
		mem := newMemoryStore()
		mem.createErr = errors.New("create failed")
		s := New(mem, &mockHasher{}, defaultConfig())
		err := s.Register(context.Background(), uuid.New(), "u@example.com", "GoodPass1!")
		assert.EqualError(t, err, "create failed")
	})
}

func TestVerify(t *testing.T) {
	identityID := uuid.New()
	mem := newMemoryStore()
	cred := mem.seedCredential(identityID, "user@example.com", "stored-hash")

	t.Run("success", func(t *testing.T) {
		h := &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				assert.Equal(t, "GoodPass1!", password)
				assert.Equal(t, cred.Hash, hash)
				return true, nil
			},
		}
		s := New(mem, h, defaultConfig())
		gotID, err := s.Verify(context.Background(), "USER@example.com", "GoodPass1!")
		require.NoError(t, err)
		assert.Equal(t, identityID, gotID)
	})

	t.Run("credential not found returns invalid credentials", func(t *testing.T) {
		h := &mockHasher{
			hashFn: func(password string) (string, error) {
				return "dummy", nil
			},
		}
		s := New(newMemoryStore(), h, defaultConfig())
		_, err := s.Verify(context.Background(), "missing@example.com", "whatever")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("store lookup error", func(t *testing.T) {
		memErr := newMemoryStore()
		memErr.getByIdentErr = errors.New("db read failed")
		s := New(memErr, &mockHasher{}, defaultConfig())
		_, err := s.Verify(context.Background(), "u@example.com", "pw")
		assert.EqualError(t, err, "db read failed")
	})

	t.Run("hasher verify error", func(t *testing.T) {
		h := &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				return false, errors.New("verify failed")
			},
		}
		s := New(mem, h, defaultConfig())
		_, err := s.Verify(context.Background(), "user@example.com", "pw")
		assert.EqualError(t, err, "failed to verify password: verify failed")
	})

	t.Run("wrong password", func(t *testing.T) {
		h := &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				return false, nil
			},
		}
		s := New(mem, h, defaultConfig())
		_, err := s.Verify(context.Background(), "user@example.com", "pw")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})
}

func TestChangePassword(t *testing.T) {
	identityID := uuid.New()
	mem := newMemoryStore()
	cred := mem.seedCredential(identityID, "user@example.com", "old-hash")
	mem.historyByCred[cred.ID] = []string{"older-hash"}

	t.Run("success", func(t *testing.T) {
		h := &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				if hash == "old-hash" {
					return password == "OldPass1!", nil
				}
				return false, nil
			},
			hashFn: func(password string) (string, error) {
				return "new-hash", nil
			},
		}
		s := New(mem, h, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		require.NoError(t, err)
		assert.Equal(t, cred.ID, mem.lastUpdatedID)
		assert.Equal(t, "new-hash", mem.lastUpdatedTo)
	})

	t.Run("identity missing", func(t *testing.T) {
		s := New(newMemoryStore(), &mockHasher{}, defaultConfig())
		err := s.ChangePassword(context.Background(), uuid.New(), "OldPass1!", "GoodPass2!")
		assert.ErrorIs(t, err, ErrIdentityNotFound)
	})

	t.Run("get by identity store error", func(t *testing.T) {
		memErr := newMemoryStore()
		memErr.getByIDErr = errors.New("load failed")
		s := New(memErr, &mockHasher{}, defaultConfig())
		err := s.ChangePassword(context.Background(), uuid.New(), "old", "new")
		assert.EqualError(t, err, "load failed")
	})

	t.Run("old password invalid", func(t *testing.T) {
		s := New(mem, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				return false, nil
			},
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "wrong", "GoodPass2!")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("old password verify error", func(t *testing.T) {
		s := New(mem, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				return false, errors.New("verify old failed")
			},
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "old", "GoodPass2!")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("history store error", func(t *testing.T) {
		memErr := newMemoryStore()
		memErr.seedCredential(identityID, "user@example.com", "old-hash")
		memErr.getHistoryErr = errors.New("history read failed")
		s := New(memErr, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) { return true, nil },
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		assert.EqualError(t, err, "history read failed")
	})

	t.Run("password reused", func(t *testing.T) {
		memReuse := newMemoryStore()
		c := memReuse.seedCredential(identityID, "user@example.com", "old-hash")
		memReuse.historyByCred[c.ID] = []string{"history-hash"}
		s := New(memReuse, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				switch hash {
				case "old-hash":
					return true, nil
				case "history-hash":
					return true, nil
				default:
					return false, nil
				}
			},
			hashFn: func(password string) (string, error) { return "new-hash", nil },
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		assert.ErrorIs(t, err, ErrPasswordReused)
	})

	t.Run("hash generation failure", func(t *testing.T) {
		memHashFail := newMemoryStore()
		memHashFail.seedCredential(identityID, "user@example.com", "old-hash")
		s := New(memHashFail, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) { return true, nil },
			hashFn:   func(password string) (string, error) { return "", errors.New("hash failed") },
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		assert.EqualError(t, err, "failed to hash password: hash failed")
	})

	t.Run("add history failure", func(t *testing.T) {
		memErr := newMemoryStore()
		memErr.seedCredential(identityID, "user@example.com", "old-hash")
		memErr.addHistoryErr = errors.New("history insert failed")
		s := New(memErr, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) { return true, nil },
			hashFn:   func(password string) (string, error) { return "new-hash", nil },
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		assert.EqualError(t, err, "history insert failed")
	})

	t.Run("update failure", func(t *testing.T) {
		memErr := newMemoryStore()
		memErr.seedCredential(identityID, "user@example.com", "old-hash")
		memErr.updateErr = errors.New("update failed")
		s := New(memErr, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) { return true, nil },
			hashFn:   func(password string) (string, error) { return "new-hash", nil },
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		assert.EqualError(t, err, "update failed")
	})

	t.Run("cleanup failure", func(t *testing.T) {
		memErr := newMemoryStore()
		memErr.seedCredential(identityID, "user@example.com", "old-hash")
		memErr.cleanupErr = errors.New("cleanup failed")
		s := New(memErr, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) { return true, nil },
			hashFn:   func(password string) (string, error) { return "new-hash", nil },
		}, defaultConfig())
		err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "GoodPass2!")
		assert.EqualError(t, err, "cleanup failed")
	})
}

func TestResetPassword(t *testing.T) {
	identityID := uuid.New()
	mem := newMemoryStore()
	cred := mem.seedCredential(identityID, "user@example.com", "old-hash")

	t.Run("success", func(t *testing.T) {
		s := New(mem, &mockHasher{
			hashFn: func(password string) (string, error) { return "reset-hash", nil },
			verifyFn: func(password, hash string) (bool, error) {
				return false, nil
			},
		}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "GoodPass3!")
		require.NoError(t, err)
		assert.Equal(t, cred.ID, mem.lastUpdatedID)
		assert.Equal(t, "reset-hash", mem.lastUpdatedTo)
	})

	t.Run("identity missing", func(t *testing.T) {
		s := New(newMemoryStore(), &mockHasher{}, defaultConfig())
		err := s.ResetPassword(context.Background(), uuid.New(), "GoodPass3!")
		assert.ErrorIs(t, err, ErrIdentityNotFound)
	})

	t.Run("history reused", func(t *testing.T) {
		memReuse := newMemoryStore()
		c := memReuse.seedCredential(identityID, "user@example.com", "old-hash")
		memReuse.historyByCred[c.ID] = []string{"older-hash"}
		s := New(memReuse, &mockHasher{
			verifyFn: func(password, hash string) (bool, error) {
				return hash == "older-hash", nil
			},
			hashFn: func(password string) (string, error) { return "new-hash", nil },
		}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "GoodPass3!")
		assert.ErrorIs(t, err, ErrPasswordReused)
	})

	t.Run("validate password error", func(t *testing.T) {
		mem2 := newMemoryStore()
		mem2.seedCredential(identityID, "user@example.com", "old-hash")
		s := New(mem2, &mockHasher{}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "short")
		assert.ErrorIs(t, err, ErrPasswordTooShort)
	})

	t.Run("hash error", func(t *testing.T) {
		mem3 := newMemoryStore()
		mem3.seedCredential(identityID, "user@example.com", "old-hash")
		s := New(mem3, &mockHasher{
			hashFn: func(password string) (string, error) {
				return "", errors.New("hash failed")
			},
			verifyFn: func(password, hash string) (bool, error) {
				return false, nil
			},
		}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "GoodPass3!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to hash password")
	})

	t.Run("add to history error", func(t *testing.T) {
		mem4 := newMemoryStore()
		mem4.seedCredential(identityID, "user@example.com", "old-hash")
		mem4.addHistoryErr = errors.New("db write failed")
		s := New(mem4, &mockHasher{
			hashFn:   func(password string) (string, error) { return "new-hash", nil },
			verifyFn: func(password, hash string) (bool, error) { return false, nil },
		}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "GoodPass3!")
		assert.EqualError(t, err, "db write failed")
	})

	t.Run("update error", func(t *testing.T) {
		mem5 := newMemoryStore()
		mem5.seedCredential(identityID, "user@example.com", "old-hash")
		mem5.updateErr = errors.New("update failed")
		s := New(mem5, &mockHasher{
			hashFn:   func(password string) (string, error) { return "new-hash", nil },
			verifyFn: func(password, hash string) (bool, error) { return false, nil },
		}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "GoodPass3!")
		assert.EqualError(t, err, "update failed")
	})

	t.Run("get by identity error", func(t *testing.T) {
		mem6 := newMemoryStore()
		mem6.getByIDErr = errors.New("db connection lost")
		s := New(mem6, &mockHasher{}, defaultConfig())
		err := s.ResetPassword(context.Background(), identityID, "GoodPass3!")
		assert.EqualError(t, err, "db connection lost")
	})
}

func TestValidatePasswordAndHelpers(t *testing.T) {
	s := New(newMemoryStore(), &mockHasher{}, defaultConfig())

	assert.ErrorIs(t, s.ValidatePassword(context.Background(), "short", "u@example.com"), ErrPasswordTooShort)
	assert.ErrorIs(t, s.ValidatePassword(context.Background(), "alllowercase1!", "u@example.com"), ErrPasswordTooWeak)
	assert.ErrorIs(t, s.ValidatePassword(context.Background(), "user123!A", "user@example.com"), ErrPasswordSimilar)
	assert.NoError(t, s.ValidatePassword(context.Background(), "GoodPass1!", "user@example.com"))
	assert.NoError(t, s.checkSimilarity("GoodPass1!", "ab"))
	assert.NoError(t, s.checkSimilarity("GoodPass1!", ""))
}

func TestCheckHIBPAndHistoryPaths(t *testing.T) {
	s := New(newMemoryStore(), &mockHasher{}, Config{
		MinLength:   8,
		HIBPEnabled: true,
	})

	// Network failures should not fail validation.
	assert.NoError(t, s.checkHIBP(context.Background(), "GoodPass1!"))

	mem := newMemoryStore()
	identityID := uuid.New()
	c := mem.seedCredential(identityID, "user@example.com", "old-hash")
	mem.historyByCred[c.ID] = []string{"older-hash"}

	s = New(mem, &mockHasher{
		verifyFn: func(password, hash string) (bool, error) {
			return false, errors.New("verify history failed")
		},
	}, defaultConfig())
	assert.NoError(t, s.checkHistory(context.Background(), c.ID, "CandidatePass1!"))
}

func TestDelete(t *testing.T) {
	identityID := uuid.New()
	mem := newMemoryStore()
	mem.seedCredential(identityID, "user@example.com", "hash")
	s := New(mem, &mockHasher{}, defaultConfig())

	err := s.Delete(context.Background(), identityID)
	require.NoError(t, err)
	assert.Equal(t, identityID, mem.deletedID)

	mem.deleteErr = errors.New("delete failed")
	err = s.Delete(context.Background(), identityID)
	assert.EqualError(t, err, "delete failed")
}

func TestHIBPBaseURLFromHost(t *testing.T) {
	assert.Equal(t, "https://api.pwnedpasswords.com/range/", HIBPBaseURLFromHost(""))
	assert.Equal(t, "https://hibp.example.com/range/", HIBPBaseURLFromHost("hibp.example.com/"))
	assert.Equal(t, "https://hibp.example.com/api/range/", HIBPBaseURLFromHost("https://hibp.example.com/api"))
	assert.Equal(t, "http://hibp.example.com/api/range/", HIBPBaseURLFromHost("http://hibp.example.com/api/"))
}

func TestCheckHIBPBranchCoverage(t *testing.T) {
	password := "GoodPass1!"
	hash := sha1.Sum([]byte(password))
	hashStr := strings.ToUpper(hex.EncodeToString(hash[:]))
	prefix := hashStr[:5]
	suffix := hashStr[5:]

	t.Run("returns breached when count meets threshold", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/range/"+prefix, r.URL.Path)
			_, _ = w.Write([]byte(suffix + ":10\nOTHER:1"))
		}))
		defer srv.Close()

		s := New(newMemoryStore(), &mockHasher{}, Config{
			HIBPEnabled:             true,
			HIBPIgnoreNetworkErrors: false,
			HIBPBaseURL:             srv.URL + "/range/",
			HIBPMinBreachCount:      1,
		})

		err := s.checkHIBP(context.Background(), password)
		assert.ErrorIs(t, err, ErrPasswordBreached)
	})

	t.Run("ignores malformed and below-threshold counts", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(suffix + ":not-a-number\n" + suffix + ":1\n"))
		}))
		defer srv.Close()

		s := New(newMemoryStore(), &mockHasher{}, Config{
			HIBPEnabled:             true,
			HIBPIgnoreNetworkErrors: false,
			HIBPBaseURL:             srv.URL + "/range/",
			HIBPMinBreachCount:      2,
		})

		err := s.checkHIBP(context.Background(), password)
		assert.NoError(t, err)
	})

	t.Run("non-200 responses are tolerated", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		s := New(newMemoryStore(), &mockHasher{}, Config{
			HIBPEnabled:             true,
			HIBPIgnoreNetworkErrors: false,
			HIBPBaseURL:             srv.URL + "/range/",
		})

		err := s.checkHIBP(context.Background(), password)
		assert.NoError(t, err)
	})

	t.Run("returns network errors when configured", func(t *testing.T) {
		s := New(newMemoryStore(), &mockHasher{}, Config{
			HIBPEnabled:             true,
			HIBPIgnoreNetworkErrors: false,
			HIBPBaseURL:             "http://127.0.0.1:1/range/",
			HIBPTimeout:             100 * time.Millisecond,
		})

		err := s.checkHIBP(context.Background(), password)
		assert.Error(t, err)
	})
}
