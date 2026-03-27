package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

// Helper function to create mock command tags
func mockCommandTag(rowsAffected int64) pgconn.CommandTag {
	if rowsAffected == 0 {
		return pgconn.NewCommandTag("")
	}
	return pgconn.NewCommandTag("INSERT 0 1")
}

// MockRows implements pgx.Rows interface for testing
type MockRows struct {
	data    [][]interface{}
	index   int
	closed  bool
	scanErr error
}

func (m *MockRows) Scan(dest ...interface{}) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	if m.index >= len(m.data) {
		return nil
	}
	for i, d := range dest {
		if i < len(m.data[m.index]) {
			switch v := m.data[m.index][i].(type) {
			case string:
				*d.(*string) = v
			case int:
				*d.(*int) = v
			case uuid.UUID:
				*d.(*uuid.UUID) = v
			case time.Time:
				*d.(*time.Time) = v
			}
		}
	}
	return nil
}

func (m *MockRows) Next() bool {
	if m.closed || m.index >= len(m.data) {
		return false
	}
	defer func() { m.index++ }()
	return true
}

func (m *MockRows) Close() {
	m.closed = true
}

func (m *MockRows) Err() error {
	return nil
}

func (m *MockRows) CommandTag() pgconn.CommandTag {
	return mockCommandTag(0)
}

func (m *MockRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (m *MockRows) Values() ([]interface{}, error) {
	return nil, nil
}

func (m *MockRows) RawValues() [][]byte {
	return nil
}

func (m *MockRows) Conn() *pgx.Conn {
	return nil
}

// MockRow implements pgx.Row interface
type MockRow struct {
	scanErr error
	data    []interface{}
}

func (m *MockRow) Scan(dest ...interface{}) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	for i, d := range dest {
		if i < len(m.data) {
			switch v := m.data[i].(type) {
			case uuid.UUID:
				*d.(*uuid.UUID) = v
			case string:
				*d.(*string) = v
			case time.Time:
				*d.(*time.Time) = v
			}
		}
	}
	return nil
}

// MockDB is a test double implementing DB interface
type MockDB struct {
	ExecFunc     func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	QueryRowFunc func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row
	QueryFunc    func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error)
}

func (m *MockDB) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, sql, arguments...)
	}
	return mockCommandTag(1), nil
}

func (m *MockDB) QueryRow(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
	if m.QueryRowFunc != nil {
		return m.QueryRowFunc(ctx, sql, optionsAndArgs...)
	}
	return &MockRow{}
}

func (m *MockDB) Query(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, sql, optionsAndArgs...)
	}
	return &MockRows{data: [][]interface{}{}}, nil
}

// ============ CREDENTIAL CRUD TESTS ============

// TestCreate_Success tests successful credential creation
func TestCreate_Success(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(1), nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "user@example.com",
		Hash:       "$argon2id$v=19$m=65540,t=3,p=4$...",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := store.Create(ctx, cred)
	assert.NoError(t, err)
}

// TestCreate_DuplicateKey tests duplicate key error handling
func TestCreate_DuplicateKey(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			// Simulate PostgreSQL duplicate key violation error (code 23505)
			return mockCommandTag(0), errors.New("ERROR: duplicate key value violates unique constraint \"idx_pwd_credentials_identifier\" (SQLSTATE 23505)")
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "duplicate@example.com",
		Hash:       "$argon2id$v=19$m=65540,t=3,p=4$...",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := store.Create(ctx, cred)
	assert.Error(t, err)
	assert.Equal(t, ErrCredentialExists, err)
}

// TestCreate_DuplicateKey_StringMatch tests duplicate key detection via string matching
func TestCreate_DuplicateKey_StringMatch(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), errors.New("duplicate key value violates unique constraint")
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "test@example.com",
		Hash:       "hash",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := store.Create(ctx, cred)
	assert.Error(t, err)
	assert.Equal(t, ErrCredentialExists, err)
}

// TestCreate_OtherError tests non-duplicate errors
func TestCreate_OtherError(t *testing.T) {
	customErr := errors.New("connection timeout")
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), customErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "test@example.com",
		Hash:       "hash",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := store.Create(ctx, cred)
	assert.Error(t, err)
	assert.Equal(t, customErr, err)
}

// ============ CREDENTIAL RETRIEVAL TESTS ============

// TestGetByIdentifier_Found tests successful retrieval by identifier
func TestGetByIdentifier_Found(t *testing.T) {
	credID := uuid.New()
	identityID := uuid.New()
	now := time.Now()

	mock := &MockDB{
		QueryRowFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &MockRow{
				data: []interface{}{credID, identityID, "user@example.com", "hash123", now, now},
			}
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred, err := store.GetByIdentifier(ctx, "user@example.com")

	assert.NoError(t, err)
	assert.NotNil(t, cred)
	assert.Equal(t, credID, cred.ID)
	assert.Equal(t, identityID, cred.IdentityID)
	assert.Equal(t, "user@example.com", cred.Identifier)
	assert.Equal(t, "hash123", cred.Hash)
}

// TestGetByIdentifier_NotFound tests not found error
func TestGetByIdentifier_NotFound(t *testing.T) {
	mock := &MockDB{
		QueryRowFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &MockRow{scanErr: pgx.ErrNoRows}
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred, err := store.GetByIdentifier(ctx, "nonexistent@example.com")

	assert.Error(t, err)
	assert.Nil(t, cred)
	assert.Equal(t, ErrCredentialNotFound, err)
}

// TestGetByIdentifier_ScanError tests database scan errors
func TestGetByIdentifier_ScanError(t *testing.T) {
	scanErr := errors.New("scan failed")
	mock := &MockDB{
		QueryRowFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &MockRow{scanErr: scanErr}
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred, err := store.GetByIdentifier(ctx, "user@example.com")

	assert.Error(t, err)
	assert.Nil(t, cred)
	assert.Equal(t, scanErr, err)
}

// TestGetByIdentityID_Found tests retrieval by identity ID
func TestGetByIdentityID_Found(t *testing.T) {
	credID := uuid.New()
	identityID := uuid.New()
	now := time.Now()

	mock := &MockDB{
		QueryRowFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &MockRow{
				data: []interface{}{credID, identityID, "user@example.com", "hash123", now, now},
			}
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred, err := store.GetByIdentityID(ctx, identityID)

	assert.NoError(t, err)
	assert.NotNil(t, cred)
	assert.Equal(t, credID, cred.ID)
	assert.Equal(t, identityID, cred.IdentityID)
}

// TestGetByIdentityID_NotFound tests not found when using identity ID
func TestGetByIdentityID_NotFound(t *testing.T) {
	mock := &MockDB{
		QueryRowFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &MockRow{scanErr: pgx.ErrNoRows}
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred, err := store.GetByIdentityID(ctx, uuid.New())

	assert.Error(t, err)
	assert.Nil(t, cred)
	assert.Equal(t, ErrCredentialNotFound, err)
}

// TestGetByIdentityID_ScanError tests scan errors for identity ID query
func TestGetByIdentityID_ScanError(t *testing.T) {
	scanErr := errors.New("database error")
	mock := &MockDB{
		QueryRowFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) pgx.Row {
			return &MockRow{scanErr: scanErr}
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred, err := store.GetByIdentityID(ctx, uuid.New())

	assert.Error(t, err)
	assert.Nil(t, cred)
	assert.Equal(t, scanErr, err)
}

// ============ CREDENTIAL UPDATE TESTS ============

// TestUpdate_Success tests successful credential update
func TestUpdate_Success(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(1), nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	credID := uuid.New()
	newHash := "$argon2id$v=19$m=65540,t=3,p=4$newsalt..."

	err := store.Update(ctx, credID, newHash)
	assert.NoError(t, err)
}

// TestUpdate_NotFound tests update when credential doesn't exist
func TestUpdate_NotFound(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), nil // No rows affected
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	credID := uuid.New()

	err := store.Update(ctx, credID, "newhash")
	assert.Error(t, err)
	assert.Equal(t, ErrCredentialNotFound, err)
}

// TestUpdate_Error tests database errors during update
func TestUpdate_Error(t *testing.T) {
	dbErr := errors.New("constraint violation")
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), dbErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.Update(ctx, uuid.New(), "newhash")
	assert.Error(t, err)
	assert.Equal(t, dbErr, err)
}

// ============ CREDENTIAL DELETION TESTS ============

// TestDelete_Success tests successful credential deletion
func TestDelete_Success(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(1), nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	credID := uuid.New()

	err := store.Delete(ctx, credID)
	assert.NoError(t, err)
}

// TestDelete_Error tests database errors during deletion
func TestDelete_Error(t *testing.T) {
	dbErr := errors.New("database connection lost")
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), dbErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.Delete(ctx, uuid.New())
	assert.Error(t, err)
	assert.Equal(t, dbErr, err)
}

// TestDeleteByIdentityID_Success tests deleting all credentials for an identity
func TestDeleteByIdentityID_Success(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(2), nil // Could delete multiple credentials
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	identityID := uuid.New()

	err := store.DeleteByIdentityID(ctx, identityID)
	assert.NoError(t, err)
}

// TestDeleteByIdentityID_Error tests errors when deleting by identity ID
func TestDeleteByIdentityID_Error(t *testing.T) {
	dbErr := errors.New("cascading delete failed")
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), dbErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.DeleteByIdentityID(ctx, uuid.New())
	assert.Error(t, err)
	assert.Equal(t, dbErr, err)
}

// ============ PASSWORD HISTORY TESTS ============

// TestAddToHistory_Success tests adding password hash to history
func TestAddToHistory_Success(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(1), nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	credID := uuid.New()
	hash := "oldhash123"

	err := store.AddToHistory(ctx, credID, hash)
	assert.NoError(t, err)
}

// TestAddToHistory_Error tests error handling when adding to history
func TestAddToHistory_Error(t *testing.T) {
	dbErr := errors.New("history table full or locked")
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), dbErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.AddToHistory(ctx, uuid.New(), "hash")
	assert.Error(t, err)
	assert.Equal(t, dbErr, err)
}

// TestGetHistory_Success tests retrieving password history
func TestGetHistory_Success(t *testing.T) {
	hashes := [][]interface{}{
		{"hash5"},
		{"hash4"},
		{"hash3"},
		{"hash2"},
		{"hash1"},
	}

	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			return &MockRows{data: hashes}, nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	credID := uuid.New()

	result, err := store.GetHistory(ctx, credID, 5)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 5, len(result))
}

// TestGetHistory_DefaultLimit tests that limit defaults to 5
func TestGetHistory_DefaultLimit(t *testing.T) {
	hashes := [][]interface{}{
		{"hash1"},
		{"hash2"},
		{"hash3"},
	}

	var receivedLimit int
	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			// Last argument should be the limit
			if len(optionsAndArgs) > 1 {
				receivedLimit = optionsAndArgs[len(optionsAndArgs)-1].(int)
			}
			return &MockRows{data: hashes}, nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	// Pass 0 to trigger default
	result, err := store.GetHistory(ctx, uuid.New(), 0)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 5, receivedLimit)
}

// TestGetHistory_CustomLimit tests custom history limit
func TestGetHistory_CustomLimit(t *testing.T) {
	hashes := [][]interface{}{
		{"hash1"},
		{"hash2"},
		{"hash3"},
	}

	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			return &MockRows{data: hashes}, nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	result, err := store.GetHistory(ctx, uuid.New(), 10)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 3, len(result))
}

// TestGetHistory_Empty tests retrieving empty history
func TestGetHistory_Empty(t *testing.T) {
	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			return &MockRows{data: [][]interface{}{}}, nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	result, err := store.GetHistory(ctx, uuid.New(), 5)

	assert.NoError(t, err)
	// Go returns nil for empty slices
	assert.Nil(t, result)
	assert.Equal(t, 0, len(result))
}

// TestGetHistory_QueryError tests error during history retrieval
func TestGetHistory_QueryError(t *testing.T) {
	queryErr := errors.New("query timeout")
	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			return nil, queryErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	result, err := store.GetHistory(ctx, uuid.New(), 5)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, queryErr, err)
}

// TestGetHistory_ScanError tests error during row scanning
func TestGetHistory_ScanError(t *testing.T) {
	scanErr := errors.New("corrupted data")
	rows := &MockRows{
		data:    [][]interface{}{{"hash1"}},
		scanErr: scanErr,
	}

	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			return rows, nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	result, err := store.GetHistory(ctx, uuid.New(), 5)

	assert.Error(t, err)
	assert.Nil(t, result)
}

// ============ HISTORY CLEANUP TESTS ============

// TestCleanupHistory_Success tests successful history cleanup
func TestCleanupHistory_Success(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(5), nil // Deleted 5 old entries
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()
	credID := uuid.New()

	err := store.CleanupHistory(ctx, credID, 10)

	assert.NoError(t, err)
}

// TestCleanupHistory_NoDelete tests cleanup with nothing to delete
func TestCleanupHistory_NoDelete(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), nil // Nothing to delete
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.CleanupHistory(ctx, uuid.New(), 10)

	assert.NoError(t, err)
}

// TestCleanupHistory_Error tests errors during history cleanup
func TestCleanupHistory_Error(t *testing.T) {
	cleanupErr := errors.New("cleanup query failed")
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(0), cleanupErr
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.CleanupHistory(ctx, uuid.New(), 10)

	assert.Error(t, err)
	assert.Equal(t, cleanupErr, err)
}

// ============ ERROR DETECTION TESTS ============

// TestIsDuplicateKeyError_Code23505 tests PostgreSQL error code detection
func TestIsDuplicateKeyError_Code23505(t *testing.T) {
	err := errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)")
	result := isDuplicateKeyError(err)
	assert.True(t, result)
}

// TestIsDuplicateKeyError_StringMatch tests string matching for duplicate errors
func TestIsDuplicateKeyError_StringMatch(t *testing.T) {
	err := errors.New("ERROR: duplicate key value violates constraint")
	result := isDuplicateKeyError(err)
	assert.True(t, result)
}

// TestIsDuplicateKeyError_NilError tests nil error handling
func TestIsDuplicateKeyError_NilError(t *testing.T) {
	result := isDuplicateKeyError(nil)
	assert.False(t, result)
}

// TestIsDuplicateKeyError_OtherError tests non-duplicate errors
func TestIsDuplicateKeyError_OtherError(t *testing.T) {
	err := errors.New("ERROR: connection refused")
	result := isDuplicateKeyError(err)
	assert.False(t, result)
}

// ============ CONTEXT HANDLING TESTS ============

// TestCreateWithCanceledContext tests behavior with canceled context
func TestCreateWithCanceledContext(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			// In real scenario, this would check ctx.Done()
			select {
			case <-ctx.Done():
				return mockCommandTag(0), context.Canceled
			default:
				return mockCommandTag(1), nil
			}
		},
	}

	store := NewWithDB(mock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "user@example.com",
		Hash:       "hash",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := store.Create(ctx, cred)
	assert.Equal(t, context.Canceled, err)
}

// TestUpdateWithTimeoutContext tests behavior with timeout context
func TestUpdateWithTimeoutContext(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			select {
			case <-ctx.Done():
				return mockCommandTag(0), context.DeadlineExceeded
			default:
				return mockCommandTag(1), nil
			}
		},
	}

	store := NewWithDB(mock)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := store.Update(ctx, uuid.New(), "newhash")
	assert.Equal(t, context.DeadlineExceeded, err)
}

// ============ EDGE CASE TESTS ============

// TestCreateWithEmptyIdentifier tests edge case with empty identifier
func TestCreateWithEmptyIdentifier(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			// Empty identifier might be rejected by DB constraints
			return mockCommandTag(0), errors.New("constraint violation: identifier cannot be empty")
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "", // Empty identifier
		Hash:       "hash",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := store.Create(ctx, cred)
	assert.Error(t, err)
}

// TestGetHistoryWithZeroKeepCount tests cleanup with zero entries to keep
func TestCleanupHistoryWithZeroKeepCount(t *testing.T) {
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			return mockCommandTag(10), nil // Deletes all
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	err := store.CleanupHistory(ctx, uuid.New(), 0)

	assert.NoError(t, err)
}

// TestGetHistoryWithNegativeLimit tests with negative limit (should be treated as positive)
func TestGetHistoryWithLargeCustomLimit(t *testing.T) {
	hashes := [][]interface{}{}
	for i := 0; i < 100; i++ {
		hashes = append(hashes, []interface{}{})
	}

	mock := &MockDB{
		QueryFunc: func(ctx context.Context, sql string, optionsAndArgs ...interface{}) (pgx.Rows, error) {
			return &MockRows{data: hashes}, nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	result, err := store.GetHistory(ctx, uuid.New(), 1000)

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// ============ MULTIPLE OPERATIONS TESTS ============

// TestSequentialOperations tests multiple sequential operations
func TestSequentialOperations(t *testing.T) {
	credID := uuid.New()
	identityID := uuid.New()
	hash1 := "hash1"
	hash2 := "hash2"

	operationCount := 0
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			operationCount++
			return mockCommandTag(1), nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred := &Credential{
		ID:         credID,
		IdentityID: identityID,
		Identifier: "user@example.com",
		Hash:       hash1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Create
	err := store.Create(ctx, cred)
	assert.NoError(t, err)

	// Add to history
	err = store.AddToHistory(ctx, credID, hash1)
	assert.NoError(t, err)

	// Update password
	err = store.Update(ctx, credID, hash2)
	assert.NoError(t, err)

	// Add new hash to history
	err = store.AddToHistory(ctx, credID, hash2)
	assert.NoError(t, err)

	assert.Equal(t, 4, operationCount)
}

// TestConflictingOperations tests handling of conflicting operations
func TestConflictingOperations(t *testing.T) {
	callCount := 0
	mock := &MockDB{
		ExecFunc: func(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
			callCount++
			// Second call tries to create duplicate
			if callCount == 2 {
				return mockCommandTag(0), errors.New("duplicate key 23505")
			}
			return mockCommandTag(1), nil
		},
	}

	store := NewWithDB(mock)
	ctx := context.Background()

	cred1 := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "duplicate",
		Hash:       "hash1",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err1 := store.Create(ctx, cred1)
	assert.NoError(t, err1)

	// Try to create with same identifier
	cred2 := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "duplicate",
		Hash:       "hash2",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err2 := store.Create(ctx, cred2)
	assert.Error(t, err2)
	assert.Equal(t, ErrCredentialExists, err2)
}

// ============ TYPE AND INTERFACE TESTS ============

// TestNewStore tests store initialization
func TestNewStore(t *testing.T) {
	mock := &MockDB{}
	store := NewWithDB(mock)

	assert.NotNil(t, store)
	assert.Equal(t, mock, store.db)
}

// TestCredentialTypeFields verifies all Credential fields exist
func TestCredentialTypeFields(t *testing.T) {
	cred := &Credential{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Identifier: "test@example.com",
		Hash:       "hash",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	assert.NotNil(t, cred.ID)
	assert.NotNil(t, cred.IdentityID)
	assert.NotEmpty(t, cred.Identifier)
	assert.NotEmpty(t, cred.Hash)
	assert.False(t, cred.CreatedAt.IsZero())
	assert.False(t, cred.UpdatedAt.IsZero())
}

// TestContainsEdgeCases tests contains function edge cases
func TestContainsEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		substr   string
		expected bool
	}{
		{"identical strings", "abc", "abc", true},
		{"substring at start", "abcdef", "abc", true},
		{"substring at end", "abcdef", "def", true},
		{"substring in middle", "abcdef", "cd", true},
		{"substring not found", "abcdef", "xyz", false},
		{"single char match", "abcdef", "c", true},
		{"single char no match", "abcdef", "z", false},
		{"empty substring", "abcdef", "", true},
		{"empty string", "", "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.str, tt.substr)
			assert.Equal(t, tt.expected, result, "contains(%q, %q) should be %v", tt.str, tt.substr, tt.expected)
		})
	}
}
