package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/modules/magic_link/store"
)

type mockCourier struct {
	sendFn func(ctx context.Context, to string, link string, code string) error
}

func (m *mockCourier) SendMagicLinkEmail(ctx context.Context, to string, link string, code string) error {
	if m.sendFn != nil {
		return m.sendFn(ctx, to, link, code)
	}
	return nil
}

type memoryStore struct {
	configLength  int
	configCharset string

	codesByID    map[uuid.UUID]*store.Code
	codesByToken map[string]*store.Code
	codesByKey   map[string]*store.Code
	rateCounts   map[string]int
	invalidated  []string

	checkRateLimitErr error
	invalidateErr     error
	createErr         error
	getByCodeErr      error
	getByTokenErr     error
	markUsedErr       error
	cleanupErr        error

	forceExpired bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		codesByID:    map[uuid.UUID]*store.Code{},
		codesByToken: map[string]*store.Code{},
		codesByKey:   map[string]*store.Code{},
		rateCounts:   map[string]int{},
		invalidated:  []string{},
	}
}

func codeKey(recipient string, codeType store.CodeType, code string) string {
	return string(codeType) + "|" + recipient + "|" + code
}

func (m *memoryStore) SetCodeConfig(length int, charset string) {
	m.configLength = length
	m.configCharset = charset
}

func (m *memoryStore) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) error {
	if m.checkRateLimitErr != nil {
		return m.checkRateLimitErr
	}
	m.rateCounts[key]++
	if m.rateCounts[key] > limit {
		return store.ErrRateLimited
	}
	return nil
}

func (m *memoryStore) InvalidatePrevious(ctx context.Context, recipient string, codeType store.CodeType) error {
	if m.invalidateErr != nil {
		return m.invalidateErr
	}
	m.invalidated = append(m.invalidated, string(codeType)+"|"+recipient)
	return nil
}

func (m *memoryStore) Create(ctx context.Context, recipient string, codeType store.CodeType, identityID *uuid.UUID, ttl time.Duration) (*store.Code, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	if m.forceExpired {
		expires = now.Add(-time.Minute)
	}
	c := &store.Code{
		ID:         uuid.New(),
		IdentityID: identityID,
		Recipient:  recipient,
		Type:       codeType,
		Code:       "123456",
		Token:      "tok_" + uuid.NewString(),
		Used:       false,
		ExpiresAt:  expires,
		CreatedAt:  now,
	}
	m.codesByID[c.ID] = c
	m.codesByToken[c.Token] = c
	m.codesByKey[codeKey(recipient, codeType, c.Code)] = c
	return c, nil
}

func (m *memoryStore) GetByCode(ctx context.Context, recipient string, otpCode string, codeType store.CodeType) (*store.Code, error) {
	if m.getByCodeErr != nil {
		return nil, m.getByCodeErr
	}
	c, ok := m.codesByKey[codeKey(recipient, codeType, otpCode)]
	if !ok {
		return nil, store.ErrCodeNotFound
	}
	if c.Used {
		return nil, store.ErrCodeUsed
	}
	if time.Now().UTC().After(c.ExpiresAt) {
		return nil, store.ErrCodeExpired
	}
	cp := *c
	return &cp, nil
}

func (m *memoryStore) GetByToken(ctx context.Context, token string) (*store.Code, error) {
	if m.getByTokenErr != nil {
		return nil, m.getByTokenErr
	}
	c, ok := m.codesByToken[token]
	if !ok {
		return nil, store.ErrCodeNotFound
	}
	if c.Used {
		return nil, store.ErrCodeUsed
	}
	if time.Now().UTC().After(c.ExpiresAt) {
		return nil, store.ErrCodeExpired
	}
	cp := *c
	return &cp, nil
}

func (m *memoryStore) MarkUsed(ctx context.Context, codeID uuid.UUID) error {
	if m.markUsedErr != nil {
		return m.markUsedErr
	}
	c, ok := m.codesByID[codeID]
	if !ok {
		return store.ErrCodeNotFound
	}
	if c.Used {
		return store.ErrCodeUsed
	}
	c.Used = true
	now := time.Now().UTC()
	c.UsedAt = &now
	return nil
}

func (m *memoryStore) Cleanup(ctx context.Context) (int64, error) {
	if m.cleanupErr != nil {
		return 0, m.cleanupErr
	}
	now := time.Now().UTC()
	var deleted int64
	for id, c := range m.codesByID {
		if c.ExpiresAt.Before(now) || c.Used {
			delete(m.codesByID, id)
			delete(m.codesByToken, c.Token)
			delete(m.codesByKey, codeKey(c.Recipient, c.Type, c.Code))
			deleted++
		}
	}
	return deleted, nil
}

func makeService(st codeStore, courier Courier) *Service {
	return New(st, courier, Config{
		BaseURL:      "https://example.com",
		CodeLength:   6,
		CodeCharset:  "0123456789",
		LinkLifespan: 15 * time.Minute,
		CodeLifespan: 15 * time.Minute,
		RateLimit:    2,
		RateWindow:   time.Hour,
	})
}

func TestNewAppliesDefaultsAndConfiguresStore(t *testing.T) {
	st := newMemoryStore()
	svc := New(st, nil, Config{})
	assert.NotNil(t, svc)
	assert.Equal(t, 6, st.configLength)
	assert.Equal(t, "0123456789", st.configCharset)
	assert.Equal(t, time.Hour, svc.config.RateWindow)
}

func TestSendLoginCode(t *testing.T) {
	t.Run("success with courier", func(t *testing.T) {
		st := newMemoryStore()
		sent := false
		svc := makeService(st, &mockCourier{
			sendFn: func(ctx context.Context, to string, link string, code string) error {
				sent = true
				assert.Equal(t, "user@example.com", to)
				assert.Contains(t, link, "/self-service/login/methods/link/verify")
				assert.Equal(t, "123456", code)
				return nil
			},
		})
		err := svc.SendLoginCode(context.Background(), "user@example.com")
		require.NoError(t, err)
		assert.True(t, sent)
		assert.Contains(t, st.invalidated, "login|user@example.com")
	})

	t.Run("success without courier", func(t *testing.T) {
		st := newMemoryStore()
		svc := makeService(st, nil)
		err := svc.SendLoginCode(context.Background(), "user@example.com")
		require.NoError(t, err)
	})

	t.Run("empty recipient", func(t *testing.T) {
		svc := makeService(newMemoryStore(), nil)
		err := svc.SendLoginCode(context.Background(), "")
		assert.ErrorIs(t, err, ErrRecipientEmpty)
	})

	t.Run("rate limited and other rate error", func(t *testing.T) {
		st := newMemoryStore()
		st.checkRateLimitErr = store.ErrRateLimited
		svc := makeService(st, nil)
		err := svc.SendLoginCode(context.Background(), "user@example.com")
		assert.ErrorIs(t, err, ErrRateLimited)

		st2 := newMemoryStore()
		st2.checkRateLimitErr = errors.New("redis down")
		svc2 := makeService(st2, nil)
		err = svc2.SendLoginCode(context.Background(), "user@example.com")
		assert.EqualError(t, err, "redis down")
	})

	t.Run("invalidate, create, and courier errors", func(t *testing.T) {
		st := newMemoryStore()
		st.invalidateErr = errors.New("invalidate failed")
		svc := makeService(st, nil)
		err := svc.SendLoginCode(context.Background(), "user@example.com")
		assert.EqualError(t, err, "invalidate failed")

		st2 := newMemoryStore()
		st2.createErr = errors.New("create failed")
		svc2 := makeService(st2, nil)
		err = svc2.SendLoginCode(context.Background(), "user@example.com")
		assert.EqualError(t, err, "create failed")

		st3 := newMemoryStore()
		svc3 := makeService(st3, &mockCourier{
			sendFn: func(ctx context.Context, to string, link string, code string) error {
				return errors.New("smtp down")
			},
		})
		err = svc3.SendLoginCode(context.Background(), "user@example.com")
		assert.EqualError(t, err, "smtp down")
	})
}

func TestVerifyCode(t *testing.T) {
	st := newMemoryStore()
	identityID := uuid.New()
	created, err := st.Create(context.Background(), "user@example.com", store.CodeTypeLogin, &identityID, 15*time.Minute)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		svc := makeService(st, nil)
		recipient, id, err := svc.VerifyCode(context.Background(), "user@example.com", created.Code)
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", recipient)
		require.NotNil(t, id)
		assert.Equal(t, identityID, *id)
	})

	t.Run("invalid by lookup error mapping", func(t *testing.T) {
		stErr := newMemoryStore()
		stErr.getByCodeErr = store.ErrCodeNotFound
		svc := makeService(stErr, nil)
		_, _, err := svc.VerifyCode(context.Background(), "u@example.com", "123456")
		assert.ErrorIs(t, err, ErrInvalidCode)

		stErr2 := newMemoryStore()
		stErr2.getByCodeErr = store.ErrCodeExpired
		svc2 := makeService(stErr2, nil)
		_, _, err = svc2.VerifyCode(context.Background(), "u@example.com", "123456")
		assert.ErrorIs(t, err, ErrInvalidCode)
	})

	t.Run("rate limited", func(t *testing.T) {
		stRate := newMemoryStore()
		stRate.checkRateLimitErr = store.ErrRateLimited
		svc := makeService(stRate, nil)

		_, _, err := svc.VerifyCode(context.Background(), "u@example.com", "123456")
		assert.ErrorIs(t, err, ErrRateLimited)
	})

	t.Run("verify rate limit key is incremented", func(t *testing.T) {
		stRate := newMemoryStore()
		svc := makeService(stRate, nil)

		_, _, err := svc.VerifyCode(context.Background(), "u@example.com", "bad")
		assert.ErrorIs(t, err, ErrInvalidCode)
		assert.Equal(t, 1, stRate.rateCounts["login_verify:u@example.com"])
	})

	t.Run("lookup and mark used errors", func(t *testing.T) {
		stErr := newMemoryStore()
		stErr.getByCodeErr = errors.New("db read failed")
		svc := makeService(stErr, nil)
		_, _, err := svc.VerifyCode(context.Background(), "u@example.com", "123456")
		assert.EqualError(t, err, "db read failed")

		stMark := newMemoryStore()
		c, _ := stMark.Create(context.Background(), "u@example.com", store.CodeTypeLogin, nil, 15*time.Minute)
		stMark.markUsedErr = store.ErrCodeUsed
		svcMark := makeService(stMark, nil)
		_, _, err = svcMark.VerifyCode(context.Background(), "u@example.com", c.Code)
		assert.ErrorIs(t, err, ErrInvalidCode)

		stMark2 := newMemoryStore()
		c2, _ := stMark2.Create(context.Background(), "u@example.com", store.CodeTypeLogin, nil, 15*time.Minute)
		stMark2.markUsedErr = errors.New("update failed")
		svcMark2 := makeService(stMark2, nil)
		_, _, err = svcMark2.VerifyCode(context.Background(), "u@example.com", c2.Code)
		assert.EqualError(t, err, "update failed")
	})
}

func TestVerifyMagicLink(t *testing.T) {
	st := newMemoryStore()
	id := uuid.New()
	c, err := st.Create(context.Background(), "u@example.com", store.CodeTypeLogin, &id, 15*time.Minute)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		svc := makeService(st, nil)
		recipient, gotID, err := svc.VerifyMagicLink(context.Background(), c.Token)
		require.NoError(t, err)
		assert.Equal(t, "u@example.com", recipient)
		require.NotNil(t, gotID)
		assert.Equal(t, id, *gotID)
	})

	t.Run("invalid and backend errors", func(t *testing.T) {
		stErr := newMemoryStore()
		stErr.getByTokenErr = store.ErrCodeNotFound
		svc := makeService(stErr, nil)
		_, _, err := svc.VerifyMagicLink(context.Background(), "missing")
		assert.ErrorIs(t, err, ErrInvalidCode)

		stErr2 := newMemoryStore()
		stErr2.getByTokenErr = errors.New("token read failed")
		svc2 := makeService(stErr2, nil)
		_, _, err = svc2.VerifyMagicLink(context.Background(), "x")
		assert.EqualError(t, err, "token read failed")
	})

	t.Run("mark used errors", func(t *testing.T) {
		// Test ErrCodeUsed returns ErrInvalidCode
		stUsed := newMemoryStore()
		id := uuid.New()
		cUsed, _ := stUsed.Create(context.Background(), "used@example.com", store.CodeTypeLogin, &id, 15*time.Minute)
		stUsed.markUsedErr = store.ErrCodeUsed
		svcUsed := makeService(stUsed, nil)
		_, _, err := svcUsed.VerifyMagicLink(context.Background(), cUsed.Token)
		assert.ErrorIs(t, err, ErrInvalidCode)

		// Test other MarkUsed errors
		stOther := newMemoryStore()
		cOther, _ := stOther.Create(context.Background(), "other@example.com", store.CodeTypeLogin, &id, 15*time.Minute)
		stOther.markUsedErr = errors.New("db connection lost")
		svcOther := makeService(stOther, nil)
		_, _, err = svcOther.VerifyMagicLink(context.Background(), cOther.Token)
		assert.EqualError(t, err, "db connection lost")
	})

	t.Run("expired code returns ErrInvalidCode", func(t *testing.T) {
		stExp := newMemoryStore()
		stExp.getByTokenErr = store.ErrCodeExpired
		svc := makeService(stExp, nil)
		_, _, err := svc.VerifyMagicLink(context.Background(), "expired")
		assert.ErrorIs(t, err, ErrInvalidCode)
	})
}

func TestSendVerificationCodeAndVerify(t *testing.T) {
	st := newMemoryStore()
	svc := makeService(st, nil)
	identityID := uuid.New()

	err := svc.SendVerificationCode(context.Background(), "user@example.com", identityID)
	require.NoError(t, err)

	var created *store.Code
	for _, c := range st.codesByID {
		if c.Type == store.CodeTypeVerification {
			created = c
			break
		}
	}
	require.NotNil(t, created)

	gotID, err := svc.VerifyVerificationCode(context.Background(), "user@example.com", created.Code)
	require.NoError(t, err)
	require.NotNil(t, gotID)
	assert.Equal(t, identityID, *gotID)
}

func TestVerificationAndRecoveryErrorPaths(t *testing.T) {
	identityID := uuid.New()

	t.Run("send verification errors", func(t *testing.T) {
		svc := makeService(newMemoryStore(), nil)
		assert.ErrorIs(t, svc.SendVerificationCode(context.Background(), "", identityID), ErrRecipientEmpty)

		stRate := newMemoryStore()
		stRate.checkRateLimitErr = store.ErrRateLimited
		svc = makeService(stRate, nil)
		assert.ErrorIs(t, svc.SendVerificationCode(context.Background(), "u@example.com", identityID), ErrRateLimited)

		stErr := newMemoryStore()
		stErr.invalidateErr = errors.New("invalidate failed")
		svc = makeService(stErr, nil)
		assert.EqualError(t, svc.SendVerificationCode(context.Background(), "u@example.com", identityID), "invalidate failed")
	})

	t.Run("verify verification errors", func(t *testing.T) {
		st := newMemoryStore()
		svc := makeService(st, nil)

		_, err := svc.VerifyVerificationCode(context.Background(), "u@example.com", "bad")
		assert.ErrorIs(t, err, ErrInvalidCode)

		c, _ := st.Create(context.Background(), "u@example.com", store.CodeTypeVerification, nil, 15*time.Minute)
		_, err = svc.VerifyVerificationCode(context.Background(), "u@example.com", c.Code)
		assert.ErrorIs(t, err, ErrInvalidCode)

		id := uuid.New()
		c2, _ := st.Create(context.Background(), "u2@example.com", store.CodeTypeVerification, &id, 15*time.Minute)
		st.markUsedErr = errors.New("mark used failed")
		_, err = svc.VerifyVerificationCode(context.Background(), "u2@example.com", c2.Code)
		assert.EqualError(t, err, "mark used failed")
	})

	t.Run("recovery flows", func(t *testing.T) {
		st := newMemoryStore()
		svc := makeService(st, nil)
		assert.ErrorIs(t, svc.SendRecoveryCode(context.Background(), "", identityID), ErrRecipientEmpty)

		err := svc.SendRecoveryCode(context.Background(), "recover@example.com", identityID)
		require.NoError(t, err)

		var rec *store.Code
		for _, c := range st.codesByID {
			if c.Type == store.CodeTypeRecovery {
				rec = c
				break
			}
		}
		require.NotNil(t, rec)

		gotID, err := svc.VerifyRecoveryCode(context.Background(), "recover@example.com", rec.Code)
		require.NoError(t, err)
		require.NotNil(t, gotID)
		assert.Equal(t, identityID, *gotID)

		st2 := newMemoryStore()
		svc2 := makeService(st2, nil)
		_, err = svc2.VerifyRecoveryCode(context.Background(), "x@example.com", "bad")
		assert.ErrorIs(t, err, ErrInvalidCode)
	})

	t.Run("recovery rate limit errors", func(t *testing.T) {
		// Test other rate limit errors (not ErrRateLimited)
		stRate := newMemoryStore()
		stRate.checkRateLimitErr = errors.New("redis connection failed")
		svc := makeService(stRate, nil)
		err := svc.SendRecoveryCode(context.Background(), "recover@example.com", identityID)
		assert.EqualError(t, err, "redis connection failed")
	})

	t.Run("recovery invalidate and create errors", func(t *testing.T) {
		stInv := newMemoryStore()
		stInv.invalidateErr = errors.New("invalidate db error")
		svc := makeService(stInv, nil)
		err := svc.SendRecoveryCode(context.Background(), "recover@example.com", identityID)
		assert.EqualError(t, err, "invalidate db error")

		stCreate := newMemoryStore()
		stCreate.createErr = errors.New("insert failed")
		svc2 := makeService(stCreate, nil)
		err = svc2.SendRecoveryCode(context.Background(), "recover@example.com", identityID)
		assert.EqualError(t, err, "insert failed")
	})

	t.Run("verify recovery mark used errors", func(t *testing.T) {
		// Test ErrCodeUsed returns ErrInvalidCode
		stUsed := newMemoryStore()
		c, _ := stUsed.Create(context.Background(), "recover@example.com", store.CodeTypeRecovery, &identityID, 15*time.Minute)
		stUsed.markUsedErr = store.ErrCodeUsed
		svc := makeService(stUsed, nil)
		_, err := svc.VerifyRecoveryCode(context.Background(), "recover@example.com", c.Code)
		assert.ErrorIs(t, err, ErrInvalidCode)

		// Test other MarkUsed errors
		stOther := newMemoryStore()
		c2, _ := stOther.Create(context.Background(), "recover2@example.com", store.CodeTypeRecovery, &identityID, 15*time.Minute)
		stOther.markUsedErr = errors.New("db write failed")
		svc2 := makeService(stOther, nil)
		_, err = svc2.VerifyRecoveryCode(context.Background(), "recover2@example.com", c2.Code)
		assert.EqualError(t, err, "db write failed")
	})

	t.Run("verify recovery get by code errors", func(t *testing.T) {
		// Test expired code returns ErrInvalidCode
		stExp := newMemoryStore()
		stExp.getByCodeErr = store.ErrCodeExpired
		svc := makeService(stExp, nil)
		_, err := svc.VerifyRecoveryCode(context.Background(), "x@example.com", "expired")
		assert.ErrorIs(t, err, ErrInvalidCode)

		// Test other GetByCode errors
		stOther := newMemoryStore()
		stOther.getByCodeErr = errors.New("db read failed")
		svc2 := makeService(stOther, nil)
		_, err = svc2.VerifyRecoveryCode(context.Background(), "x@example.com", "code")
		assert.EqualError(t, err, "db read failed")
	})

	t.Run("verify recovery nil identity", func(t *testing.T) {
		// Test code without identity returns ErrInvalidCode
		st := newMemoryStore()
		c, _ := st.Create(context.Background(), "recover@example.com", store.CodeTypeRecovery, nil, 15*time.Minute)
		svc := makeService(st, nil)
		_, err := svc.VerifyRecoveryCode(context.Background(), "recover@example.com", c.Code)
		assert.ErrorIs(t, err, ErrInvalidCode)
	})
}

func TestSendRecoveryCodeIfIdentityExists(t *testing.T) {
	t.Run("unknown identity only applies rate limits", func(t *testing.T) {
		st := newMemoryStore()
		svc := makeService(st, nil)

		err := svc.SendRecoveryCodeIfIdentityExists(context.Background(), "missing@example.com", nil)
		require.NoError(t, err)
		assert.Equal(t, 1, st.rateCounts["recover:missing@example.com"])
		assert.Empty(t, st.codesByID)
		assert.Empty(t, st.invalidated)
	})

	t.Run("known identity sends recovery code", func(t *testing.T) {
		st := newMemoryStore()
		svc := makeService(st, nil)
		identityID := uuid.New()

		err := svc.SendRecoveryCodeIfIdentityExists(context.Background(), "user@example.com", &identityID)
		require.NoError(t, err)
		assert.Contains(t, st.invalidated, "recovery|user@example.com")

		var found bool
		for _, code := range st.codesByID {
			if code.Type == store.CodeTypeRecovery && code.IdentityID != nil && *code.IdentityID == identityID {
				found = true
				break
			}
		}
		assert.True(t, found)
	})
}

func TestVerifyMagicLinkForType(t *testing.T) {
	st := newMemoryStore()
	svc := makeService(st, nil)

	identityID := uuid.New()
	loginCode, err := st.Create(context.Background(), "user@example.com", store.CodeTypeLogin, &identityID, 15*time.Minute)
	require.NoError(t, err)
	recoveryCode, err := st.Create(context.Background(), "recover@example.com", store.CodeTypeRecovery, &identityID, 15*time.Minute)
	require.NoError(t, err)

	t.Run("rejects mismatched flow type", func(t *testing.T) {
		_, _, err := svc.VerifyMagicLinkForType(context.Background(), recoveryCode.Token, store.CodeTypeLogin)
		assert.ErrorIs(t, err, ErrInvalidCode)
	})

	t.Run("accepts matching flow type", func(t *testing.T) {
		recipient, gotIdentityID, err := svc.VerifyMagicLinkForType(context.Background(), loginCode.Token, store.CodeTypeLogin)
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", recipient)
		require.NotNil(t, gotIdentityID)
		assert.Equal(t, identityID, *gotIdentityID)
	})
}

func TestCleanupAndBuildMagicLink(t *testing.T) {
	st := newMemoryStore()
	svc := makeService(st, nil)
	id := uuid.New()
	c, err := st.Create(context.Background(), "cleanup@example.com", store.CodeTypeLogin, &id, time.Minute)
	require.NoError(t, err)
	assert.NotEmpty(t, c.Token)

	count, err := svc.Cleanup(context.Background())
	assert.NoError(t, err)
	assert.EqualValues(t, 0, count)

	st.forceExpired = true
	_, _ = st.Create(context.Background(), "expired@example.com", store.CodeTypeLogin, nil, time.Minute)
	count, err = svc.Cleanup(context.Background())
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))

	st.cleanupErr = errors.New("cleanup failed")
	_, err = svc.Cleanup(context.Background())
	assert.EqualError(t, err, "cleanup failed")

	assert.Contains(t, svc.buildMagicLink("abc123", "login"), "/self-service/login/methods/link/verify")
	svc.config.BaseURL = ""
	assert.Contains(t, svc.buildMagicLink("abc123", "login"), "http://localhost:8080")
	svc.config.BaseURL = "ht!tp://invalid"
	assert.Equal(t, "", svc.buildMagicLink("abc123", "login"))
}
