package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
	socialservice "github.com/aegion/aegion/modules/social/service"
	socialstore "github.com/aegion/aegion/modules/social/store"
)

func TestParsePaginationDefaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)

	page, perPage, offset := parsePagination(req)
	assert.Equal(t, 1, page)
	assert.Equal(t, 20, perPage)
	assert.Equal(t, 0, offset)
}

func TestParsePaginationBounds(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/operators?page=2&per_page=500", nil)

	page, perPage, offset := parsePagination(req)
	assert.Equal(t, 2, page)
	assert.Equal(t, 20, perPage)
	assert.Equal(t, 20, offset)
}

func TestBuildPaginationMeta(t *testing.T) {
	meta := buildPaginationMeta(2, 20, 45)
	assert.Equal(t, 2, meta.Page)
	assert.Equal(t, 20, meta.PerPage)
	assert.Equal(t, int64(45), meta.Total)
	assert.Equal(t, 3, meta.Pages)
}

func TestGetClientIP(t *testing.T) {
	t.Run("does not trust forwarded headers by default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
		req.RemoteAddr = "192.0.2.10:12345"

		ip := getClientIP(req)
		assert.Equal(t, "192.0.2.10", ip)
	})

	t.Run("trusts forwarded headers when enabled", func(t *testing.T) {
		t.Setenv("AEGION_ADMIN_TRUST_FORWARDED_HEADERS", "true")
		t.Setenv("AEGION_ADMIN_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
		req.RemoteAddr = "192.0.2.10:12345"

		ip := getClientIP(req)
		assert.Equal(t, "1.1.1.1", ip)
	})
}

func TestRequireAdminUnauthorized(t *testing.T) {
	h := New(&fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminInvalidIdentity(t *testing.T) {
	h := New(&fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("X-Aegion-Session-Identity-ID", "not-a-uuid")
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminRejectsSessionIdentityHeader(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	svc := &fakeService{
		getOperatorByIdentityIDFn: func(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
			return operator, nil
		},
	}

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("X-Aegion-Session-Identity-ID", operator.IdentityID.String())
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminRejectsSessionIdentityHeaderByDefault(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	called := false
	svc := &fakeService{
		getOperatorByIdentityIDFn: func(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
			called = true
			return operator, nil
		},
	}

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("X-Aegion-Session-Identity-ID", operator.IdentityID.String())
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, called)
}

func TestRequireAdminAPIKeyInvalid(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer aegion_short")
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminAPIKeySuccess(t *testing.T) {
	operatorID := uuid.New()
	operator := &store.Operator{
		ID:         operatorID,
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	prefix := "examplekey01"
	apiKey := &store.APIKey{
		ID:         uuid.New(),
		OperatorID: operatorID,
		KeyPrefix:  prefix,
		KeyHash:    store.HashAPIKeyToken("aegion_examplekey01_not_a_real_token"),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	storeStub := &fakeStore{
		operators:       map[uuid.UUID]*store.Operator{operatorID: operator},
		apiKeysByPrefix: map[string]*store.APIKey{prefix: apiKey},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer aegion_examplekey01_not_a_real_token")
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op := OperatorFromContext(r.Context())
		assert.NotNil(t, op)
		assert.Equal(t, operatorID, op.ID)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, storeStub.updatedLastUsed)
}

func TestRequirePermissionDenied(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "viewer",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	svc := &fakeService{
		evaluateCapabilityFn: func(ctx context.Context, operatorID uuid.UUID, permission string) error {
			return service.ErrPermissionDenied
		},
	}

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
	rec := httptest.NewRecorder()

	handler := RequirePermission(h, service.PermOperatorsRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	var resp ErrorResponse
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "insufficient_permissions", resp.Error.Status)
}

type fakeService struct {
	store                     service.Store
	getOperatorByIdentityIDFn func(ctx context.Context, identityID uuid.UUID) (*store.Operator, error)
	getEffectivePermissionsFn func(ctx context.Context, operatorID uuid.UUID) ([]string, error)
	evaluateCapabilityFn      func(ctx context.Context, operatorID uuid.UUID, permission string) error
	listOperatorsFn           func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error)
	getOperatorFn             func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error)
	createOperatorFn          func(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error)
	updateOperatorFn          func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error)
	deleteOperatorFn          func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error
	listRolesFn               func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error)
	getRoleFn                 func(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error)
	createRoleFn              func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error)
	updateRoleFn              func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error)
	deleteRoleFn              func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error
	availablePermissionsFn    func() []string
	listAuditLogsFn           func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error)
}

type fakeSocialProviderManager struct {
	listConfiguredProvidersFn func(ctx context.Context, includeDisabled bool) ([]socialstore.Provider, error)
	getProviderFn             func(ctx context.Context, slug string) (*socialstore.Provider, error)
	upsertProviderFn          func(ctx context.Context, req socialservice.ProviderUpsertRequest) (*socialstore.Provider, error)
	deleteProviderFn          func(ctx context.Context, slug string) error
}

func (f *fakeSocialProviderManager) ListConfiguredProviders(ctx context.Context, includeDisabled bool) ([]socialstore.Provider, error) {
	if f.listConfiguredProvidersFn != nil {
		return f.listConfiguredProvidersFn(ctx, includeDisabled)
	}
	return nil, nil
}

func (f *fakeSocialProviderManager) GetProvider(ctx context.Context, slug string) (*socialstore.Provider, error) {
	if f.getProviderFn != nil {
		return f.getProviderFn(ctx, slug)
	}
	return nil, socialstore.ErrProviderNotFound
}

func (f *fakeSocialProviderManager) UpsertProvider(ctx context.Context, req socialservice.ProviderUpsertRequest) (*socialstore.Provider, error) {
	if f.upsertProviderFn != nil {
		return f.upsertProviderFn(ctx, req)
	}
	return nil, errors.New("upsert not implemented")
}

func (f *fakeSocialProviderManager) DeleteProvider(ctx context.Context, slug string) error {
	if f.deleteProviderFn != nil {
		return f.deleteProviderFn(ctx, slug)
	}
	return socialstore.ErrProviderNotFound
}

func (f *fakeService) Store() service.Store {
	return f.store
}

func (f *fakeService) EvaluateCapability(ctx context.Context, operatorID uuid.UUID, permission string) error {
	if f.evaluateCapabilityFn != nil {
		return f.evaluateCapabilityFn(ctx, operatorID, permission)
	}
	return nil
}

func (f *fakeService) GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
	if f.getOperatorByIdentityIDFn != nil {
		return f.getOperatorByIdentityIDFn(ctx, identityID)
	}
	return nil, store.ErrOperatorNotFound
}

func (f *fakeService) GetEffectivePermissions(ctx context.Context, operatorID uuid.UUID) ([]string, error) {
	if f.getEffectivePermissionsFn != nil {
		return f.getEffectivePermissionsFn(ctx, operatorID)
	}
	return []string{}, nil
}

func (f *fakeService) ListOperators(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error) {
	if f.listOperatorsFn != nil {
		return f.listOperatorsFn(ctx, actorID, limit, offset)
	}
	return []*store.Operator{}, 0, nil
}

func (f *fakeService) GetOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error) {
	if f.getOperatorFn != nil {
		return f.getOperatorFn(ctx, actorID, operatorID)
	}
	return nil, store.ErrOperatorNotFound
}

func (f *fakeService) CreateOperator(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
	if f.createOperatorFn != nil {
		return f.createOperatorFn(ctx, actorID, identityID, role, permissions, ipAddress)
	}
	return nil, nil
}

func (f *fakeService) UpdateOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
	if f.updateOperatorFn != nil {
		return f.updateOperatorFn(ctx, actorID, operatorID, role, permissions, ipAddress)
	}
	return nil, nil
}

func (f *fakeService) DeleteOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error {
	if f.deleteOperatorFn != nil {
		return f.deleteOperatorFn(ctx, actorID, operatorID, ipAddress)
	}
	return nil
}

func (f *fakeService) ListRoles(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
	if f.listRolesFn != nil {
		return f.listRolesFn(ctx, actorID, limit, offset)
	}
	return []*store.Role{}, 0, nil
}

func (f *fakeService) GetRole(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error) {
	if f.getRoleFn != nil {
		return f.getRoleFn(ctx, actorID, name)
	}
	return nil, store.ErrRoleNotFound
}

func (f *fakeService) CreateRole(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
	if f.createRoleFn != nil {
		return f.createRoleFn(ctx, actorID, name, description, permissions, ipAddress)
	}
	return nil, nil
}

func (f *fakeService) UpdateRole(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
	if f.updateRoleFn != nil {
		return f.updateRoleFn(ctx, actorID, name, description, permissions, ipAddress)
	}
	return nil, nil
}

func (f *fakeService) DeleteRole(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
	if f.deleteRoleFn != nil {
		return f.deleteRoleFn(ctx, actorID, name, ipAddress)
	}
	return nil
}

func (f *fakeService) AvailablePermissions() []string {
	if f.availablePermissionsFn != nil {
		return f.availablePermissionsFn()
	}
	return []string{}
}

func (f *fakeService) ListAuditLogs(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
	if f.listAuditLogsFn != nil {
		return f.listAuditLogsFn(ctx, actorID, filter, limit, offset)
	}
	return []*store.AuditLogEntry{}, 0, nil
}

type fakeStore struct {
	operators        map[uuid.UUID]*store.Operator
	apiKeysByPrefix  map[string]*store.APIKey
	updatedLastUsed  bool
	auditLogs        []*store.AuditLogEntry
	auditLogsMu      sync.RWMutex
	createdAPIKey    *store.APIKey
	deletedAPIKeyIDs []uuid.UUID

	authenticateFn       func(ctx context.Context, email, password string) (*store.Operator, error)
	getIdentityProfileFn func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error)
	createAPIKeyFn       func(ctx context.Context, key *store.APIKey) error
	deleteAPIKeyFn       func(ctx context.Context, id uuid.UUID) error
}

func (f *fakeStore) CreateOperator(ctx context.Context, op *store.Operator) error {
	return nil
}

func (f *fakeStore) GetOperator(ctx context.Context, id uuid.UUID) (*store.Operator, error) {
	if f.operators != nil {
		if op, ok := f.operators[id]; ok {
			return op, nil
		}
	}
	return nil, store.ErrOperatorNotFound
}

func (f *fakeStore) GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
	return nil, store.ErrOperatorNotFound
}

func (f *fakeStore) UpdateOperator(ctx context.Context, op *store.Operator) error {
	return nil
}

func (f *fakeStore) DeleteOperator(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeStore) ListOperators(ctx context.Context, opts store.ListOptions) ([]*store.Operator, int64, error) {
	return []*store.Operator{}, 0, nil
}

func (f *fakeStore) ListRoles(ctx context.Context, opts store.ListOptions) ([]*store.Role, int64, error) {
	return []*store.Role{}, 0, nil
}

func (f *fakeStore) GetRoleByName(ctx context.Context, name string) (*store.Role, error) {
	return nil, store.ErrRoleNotFound
}

func (f *fakeStore) CreateRole(ctx context.Context, role *store.Role) error {
	return nil
}

func (f *fakeStore) UpdateRole(ctx context.Context, role *store.Role) error {
	return nil
}

func (f *fakeStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeStore) CountOperatorsByRole(ctx context.Context, role string) (int64, error) {
	return 0, nil
}

func (f *fakeStore) ListAuditLogs(ctx context.Context, filter store.AuditFilter, opts store.ListOptions) ([]*store.AuditLogEntry, int64, error) {
	return []*store.AuditLogEntry{}, 0, nil
}

func (f *fakeStore) LogAction(ctx context.Context, entry *store.AuditLogEntry) error {
	f.auditLogsMu.Lock()
	defer f.auditLogsMu.Unlock()
	f.auditLogs = append(f.auditLogs, entry)
	return nil
}

func (f *fakeStore) GetAPIKeyByPrefix(ctx context.Context, prefix string) (*store.APIKey, error) {
	if f.apiKeysByPrefix != nil {
		if key, ok := f.apiKeysByPrefix[prefix]; ok {
			return key, nil
		}
	}
	return nil, store.ErrAPIKeyNotFound
}

func (f *fakeStore) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	f.updatedLastUsed = true
	return nil
}

func (f *fakeStore) DB() *pgxpool.Pool {
	return nil
}

func (f *fakeStore) AuthenticateOperatorByEmail(ctx context.Context, email, password string) (*store.Operator, error) {
	if f.authenticateFn != nil {
		return f.authenticateFn(ctx, email, password)
	}
	return nil, store.ErrInvalidCredentials
}

func (f *fakeStore) GetIdentityProfile(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
	if f.getIdentityProfileFn != nil {
		return f.getIdentityProfileFn(ctx, identityID)
	}
	return nil, store.ErrIdentityNotFound
}

func (f *fakeStore) CreateAPIKey(ctx context.Context, key *store.APIKey) error {
	if f.createAPIKeyFn != nil {
		return f.createAPIKeyFn(ctx, key)
	}
	if f.apiKeysByPrefix == nil {
		f.apiKeysByPrefix = map[string]*store.APIKey{}
	}
	copyKey := *key
	f.apiKeysByPrefix[key.KeyPrefix] = &copyKey
	f.createdAPIKey = &copyKey
	return nil
}

func (f *fakeStore) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	if f.deleteAPIKeyFn != nil {
		return f.deleteAPIKeyFn(ctx, id)
	}
	f.deletedAPIKeyIDs = append(f.deletedAPIKeyIDs, id)
	return nil
}

type fakeDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	beginFn    func(ctx context.Context) (pgx.Tx, error)
}

func (f *fakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return fakeRow{err: pgx.ErrNoRows}
}

func (f *fakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &fakeRows{}, nil
}

func (f *fakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.beginFn != nil {
		return f.beginFn(ctx)
	}
	return nil, errors.New("begin not implemented")
}

type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.vals) != len(dest) {
		return fmt.Errorf("scan mismatch: have %d values, need %d", len(r.vals), len(dest))
	}
	for i := range dest {
		if err := assignScanDest(dest[i], r.vals[i]); err != nil {
			return err
		}
	}
	return nil
}

type fakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close() {}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.NewCommandTag("SELECT 0")
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan called without current row")
	}
	row := r.data[r.idx-1]
	if len(row) != len(dest) {
		return fmt.Errorf("scan mismatch: have %d values, need %d", len(row), len(dest))
	}
	for i := range dest {
		if err := assignScanDest(dest[i], row[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil, errors.New("values called without current row")
	}
	return r.data[r.idx-1], nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

type fakeTx struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	commitFn   func(ctx context.Context) error
	rollbackFn func(ctx context.Context) error
}

func (t *fakeTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errors.New("nested begin not implemented")
}

func (t *fakeTx) Commit(ctx context.Context) error {
	if t.commitFn != nil {
		return t.commitFn(ctx)
	}
	return nil
}

func (t *fakeTx) Rollback(ctx context.Context) error {
	if t.rollbackFn != nil {
		return t.rollbackFn(ctx)
	}
	return nil
}

func (t *fakeTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("copy from not implemented")
}

func (t *fakeTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

func (t *fakeTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (t *fakeTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("prepare not implemented")
}

func (t *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.execFn != nil {
		return t.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (t *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &fakeRows{}, nil
}

func (t *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.queryRowFn != nil {
		return t.queryRowFn(ctx, sql, args...)
	}
	return fakeRow{err: pgx.ErrNoRows}
}

func (t *fakeTx) Conn() *pgx.Conn {
	return nil
}

func assignScanDest(dest any, val any) error {
	switch d := dest.(type) {
	case *string:
		v, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string value, got %T", val)
		}
		*d = v
		return nil
	case *int:
		switch v := val.(type) {
		case int:
			*d = v
		case int64:
			*d = int(v)
		default:
			return fmt.Errorf("expected int value, got %T", val)
		}
		return nil
	case *int64:
		switch v := val.(type) {
		case int:
			*d = int64(v)
		case int64:
			*d = v
		default:
			return fmt.Errorf("expected int64 value, got %T", val)
		}
		return nil
	case *float64:
		v, ok := val.(float64)
		if !ok {
			return fmt.Errorf("expected float64 value, got %T", val)
		}
		*d = v
		return nil
	case *bool:
		v, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected bool value, got %T", val)
		}
		*d = v
		return nil
	case *uuid.UUID:
		v, ok := val.(uuid.UUID)
		if !ok {
			return fmt.Errorf("expected uuid value, got %T", val)
		}
		*d = v
		return nil
	case *[]byte:
		switch v := val.(type) {
		case []byte:
			*d = append((*d)[:0], v...)
			return nil
		case string:
			*d = []byte(v)
			return nil
		default:
			return fmt.Errorf("expected []byte/string value, got %T", val)
		}
	case *time.Time:
		v, ok := val.(time.Time)
		if !ok {
			return fmt.Errorf("expected time value, got %T", val)
		}
		*d = v
		return nil
	case **time.Time:
		if val == nil {
			*d = nil
			return nil
		}
		v, ok := val.(time.Time)
		if !ok {
			return fmt.Errorf("expected time value for *time.Time, got %T", val)
		}
		cp := v
		*d = &cp
		return nil
	default:
		return fmt.Errorf("unsupported scan destination %T", dest)
	}
}

func TestAdminHandler_ConfigAndLoginAdditionalBranches(t *testing.T) {
	t.Run("config override defaults and client ip branches", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}}, HandlerConfig{})
		assert.Equal(t, 20, h.config.DefaultPageSize)
		assert.Equal(t, 100, h.config.MaxPageSize)
		assert.Equal(t, 8*time.Hour, h.config.SessionTokenExpiry)
		assert.Equal(t, "aegion_", h.config.APIKeyPrefix)
		assert.Equal(t, 12, h.config.APIKeyPrefixLen)
		assert.Equal(t, 32, h.config.APIKeyEntropyBytes)
		assert.NotNil(t, h.log)

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("X-Forwarded-For", "5.5.5.5")
		req.RemoteAddr = "203.0.113.9:8080"
		assert.Equal(t, "203.0.113.9", getClientIP(req))

		t.Setenv("AEGION_ADMIN_TRUST_FORWARDED_HEADERS", "true")
		t.Setenv("AEGION_ADMIN_TRUSTED_PROXY_CIDRS", "203.0.113.0/24")
		req = httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("X-Forwarded-For", "5.5.5.5")
		req.RemoteAddr = "203.0.113.9:8080"
		assert.Equal(t, "5.5.5.5", getClientIP(req))

		req = httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("X-Real-IP", "9.9.9.9")
		req.RemoteAddr = "203.0.113.9:8080"
		assert.Equal(t, "9.9.9.9", getClientIP(req))
	})

	t.Run("login validation and generic auth failures", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":" ","password":" "}`))
		rec := httptest.NewRecorder()
		h.Login(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Email and password are required")

		storeStub := &fakeStore{
			authenticateFn: func(ctx context.Context, email, password string) (*store.Operator, error) {
				return nil, errors.New("auth backend down")
			},
		}
		h = New(&fakeService{store: storeStub})
		req = httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret"}`))
		rec = httptest.NewRecorder()
		h.Login(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Authentication failed")
	})

	t.Run("token helper branches", func(t *testing.T) {
		tokenHandler := New(&fakeService{store: &fakeStore{}}, HandlerConfig{
			APIKeyPrefix:       "aegion_",
			APIKeyPrefixLen:    12,
			APIKeyEntropyBytes: 1,
		})
		token, err := tokenHandler.generateAPIKeyToken()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(token, "aegion_"))
		assert.NotEmpty(t, tokenHandler.lookupPrefix(token))
		assert.Equal(t, "", tokenHandler.lookupPrefix("short"))
	})
}

func TestAdminHandler_DashboardAndSettingsAdditionalBranches(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	t.Run("dashboard first query failure", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("identity stats failed")}
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()
		h.DashboardStats(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to load identity stats")
	})

	t.Run("list sessions rows err branch", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{err: errors.New("rows failed")}, nil
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()
		h.ListSessions(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to list sessions")
	})

	t.Run("read settings invalid json fallback and marshal failure", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{[]byte("{invalid-json")}}
			},
		}
		settings, err := h.readSettings(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 24, settings.SessionLifetimeHours)
		assert.Equal(t, 8, settings.PasswordMinLength)

		err = h.upsertSetting(context.Background(), uuid.New(), "admin.settings", map[string]interface{}{
			"bad": make(chan int),
		})
		assert.Error(t, err)
	})
}

func TestAdminHandler_IdentityStoreAdditionalBranches(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	identityID := uuid.New()
	createdAt := time.Now().UTC()
	updatedAt := createdAt.Add(time.Minute)
	schemaID := uuid.New()

	t.Run("get identity success path", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM core_identities"):
					return fakeRow{vals: []any{identityID.String(), schemaID, []byte(`{"email":"x@example.com"}`), "active", createdAt, updatedAt}}
				case strings.Contains(sql, "FROM core_sessions") && strings.Contains(sql, "COUNT(*)"):
					return fakeRow{vals: []any{int64(0)}}
				default:
					return fakeRow{err: errors.New("unexpected query")}
				}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{}, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+identityID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		rec := httptest.NewRecorder()
		h.GetIdentity(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), identityID.String())
	})

	t.Run("invalid traits fallback, update no-op and marshal failure", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM core_identities"):
					return fakeRow{vals: []any{identityID.String(), schemaID, []byte("{invalid-json"), "active", createdAt, updatedAt}}
				case strings.Contains(sql, "FROM core_sessions") && strings.Contains(sql, "COUNT(*)"):
					return fakeRow{vals: []any{int64(0)}}
				default:
					return fakeRow{err: errors.New("unexpected query")}
				}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{}, nil
			},
		}

		identity, err := h.getIdentityWithSessions(context.Background(), identityID)
		require.NoError(t, err)
		assert.NotNil(t, identity.Traits)

		identity, err = h.updateIdentityInStore(context.Background(), identityID, UpdateIdentityRequest{})
		require.NoError(t, err)
		assert.Equal(t, identityID.String(), identity.ID)

		_, err = h.updateIdentityInStore(context.Background(), identityID, UpdateIdentityRequest{
			Traits: map[string]interface{}{"bad": make(chan int)},
		})
		assert.Error(t, err)
	})

	t.Run("list identities scan and direct scanner errors", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{identityID.String(), "not-a-uuid", []byte(`{}`), "active", createdAt, updatedAt}}}, nil
			},
		}

		_, _, err := h.listIdentitiesFromStore(context.Background(), 10, 0, "", "")
		assert.Error(t, err)

		_, err = scanIdentityRow(fakeRow{err: errors.New("scan failed")})
		assert.EqualError(t, err, "scan failed")
	})
}

func TestAdminHandler_SearchAndAuditAdditionalBranches(t *testing.T) {
	t.Run("search unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/search", bytes.NewBufferString(`{"email":"x@example.com"}`))
		rec := httptest.NewRecorder()
		h.SearchIdentities(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("search traits marshal, query and scan errors", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		_, _, err := h.searchIdentitiesInStore(context.Background(), SearchIdentitiesRequest{
			Traits: map[string]interface{}{"bad": make(chan int)},
		}, 10, 0)
		assert.Error(t, err)

		now := time.Now().UTC()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		_, _, err = h.searchIdentitiesInStore(context.Background(), SearchIdentitiesRequest{Email: "x@example.com"}, 10, 0)
		assert.EqualError(t, err, "query failed")

		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{uuid.New().String(), "not-a-uuid", []byte(`{}`), "active", now, now}}}, nil
			},
		}
		_, _, err = h.searchIdentitiesInStore(context.Background(), SearchIdentitiesRequest{Email: "x@example.com"}, 10, 0)
		assert.Error(t, err)
	})

	t.Run("log action adds request id", func(t *testing.T) {
		storeStub := &fakeStore{}
		h := New(&fakeService{store: storeStub})
		operatorID := uuid.New()
		ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "req-123")

		h.logAction(ctx, &operatorID, "read", "identity", "resource-1", map[string]interface{}{}, "127.0.0.1")
		require.Len(t, storeStub.auditLogs, 1)
		assert.Equal(t, "req-123", storeStub.auditLogs[0].Details["request_id"])
	})
}

func TestListRolesSuccess(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	role := &store.Role{
		ID:          uuid.New(),
		Name:        "admin",
		Description: "Administrator",
		Permissions: []string{"identities:*"},
		IsSystem:    true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	svc := &fakeService{
		listRolesFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
			assert.Equal(t, operator.ID, actorID)
			return []*store.Role{role}, 1, nil
		},
	}

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
	rec := httptest.NewRecorder()

	h.ListRoles(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp["items"])
}

func TestListAuditLogsFiltersAndCap(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	filterOperatorID := uuid.New()
	svc := &fakeService{
		listAuditLogsFn: func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
			assert.Equal(t, operator.ID, actorID)
			assert.NotNil(t, filter.OperatorID)
			assert.Equal(t, filterOperatorID, *filter.OperatorID)
			assert.Equal(t, "create", filter.Action)
			assert.Equal(t, 20, limit)
			assert.Equal(t, 0, offset)
			return []*store.AuditLogEntry{}, 0, nil
		},
	}

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/admin/audit?operator_id="+filterOperatorID.String()+"&action=create&per_page=999", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
	rec := httptest.NewRecorder()

	h.ListAuditLogs(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWriteErrorAndJSONHelpers(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "invalid_request", "bad request")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var errResp ErrorResponse
	assert.NoError(t, json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&errResp))
	assert.Equal(t, "invalid_request", errResp.Error.Status)

	rec2 := httptest.NewRecorder()
	writeJSON(rec2, http.StatusCreated, map[string]string{"ok": "true"})
	assert.Equal(t, http.StatusCreated, rec2.Code)
}

func TestLoginInvalidJSON(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()

	h.Login(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginRejectsUnknownFields(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret","extra":true}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_request")
}

func TestDecodeJSONBodyRejectsTrailingPayload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret"}{"x":1}`))
	rec := httptest.NewRecorder()
	var payload authLoginRequest

	err := decodeJSONBody(rec, req, &payload)
	require.Error(t, err)
}

func TestLoginInvalidCredentials(t *testing.T) {
	storeStub := &fakeStore{
		authenticateFn: func(ctx context.Context, email, password string) (*store.Operator, error) {
			return nil, store.ErrInvalidCredentials
		},
	}
	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginProfileLoadFailure(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	storeStub := &fakeStore{
		authenticateFn: func(ctx context.Context, email, password string) (*store.Operator, error) {
			return operator, nil
		},
		getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
			return nil, store.ErrIdentityNotFound
		},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLoginCreateAPIKeyFailure(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	storeStub := &fakeStore{
		authenticateFn: func(ctx context.Context, email, password string) (*store.Operator, error) {
			return operator, nil
		},
		getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
			return &store.IdentityProfile{
				Email: "admin@example.com",
				Name:  "Admin",
				State: "active",
			}, nil
		},
		createAPIKeyFn: func(ctx context.Context, key *store.APIKey) error {
			return errors.New("write failed")
		},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLoginSuccess(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	lastLogin := time.Now().Add(-time.Hour).UTC()

	storeStub := &fakeStore{
		authenticateFn: func(ctx context.Context, email, password string) (*store.Operator, error) {
			assert.Equal(t, "admin@example.com", strings.TrimSpace(email))
			assert.Equal(t, "secret", password)
			return operator, nil
		},
		getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
			assert.Equal(t, operator.IdentityID, identityID)
			return &store.IdentityProfile{
				Email:       "admin@example.com",
				Name:        "Admin",
				State:       "active",
				LastLoginAt: &lastLogin,
			}, nil
		},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", bytes.NewBufferString(`{"email":"admin@example.com","password":"secret"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Token    string       `json:"token"`
		Operator OperatorView `json:"operator"`
	}
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, strings.HasPrefix(resp.Token, "aegion_"))
	assert.Equal(t, operator.ID.String(), resp.Operator.ID)
	assert.Equal(t, "admin@example.com", resp.Operator.Email)

	if assert.NotNil(t, storeStub.createdAPIKey) {
		assert.Equal(t, operator.ID, storeStub.createdAPIKey.OperatorID)
		assert.True(t, store.ValidateAPIKeyToken(resp.Token, storeStub.createdAPIKey.KeyHash))
		assert.Equal(t, 12, len(storeStub.createdAPIKey.KeyPrefix))
	}
}

func TestMeUnauthorized(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})
	req := httptest.NewRequest(http.MethodGet, "/admin/auth/me", nil)
	rec := httptest.NewRecorder()

	h.Me(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMeProfileFailure(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	storeStub := &fakeStore{
		getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
			return nil, store.ErrIdentityNotFound
		},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodGet, "/admin/auth/me", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
	rec := httptest.NewRecorder()

	h.Me(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMeSuccess(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	lastLogin := time.Now().Add(-2 * time.Hour).UTC()

	storeStub := &fakeStore{
		getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
			return &store.IdentityProfile{
				Email:       "admin@example.com",
				Name:        "Admin User",
				State:       "banned",
				LastLoginAt: &lastLogin,
			}, nil
		},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodGet, "/admin/auth/me", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
	rec := httptest.NewRecorder()

	h.Me(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp OperatorView
	assert.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "inactive", resp.Status)
	assert.Equal(t, "admin@example.com", resp.Email)
	assert.NotNil(t, resp.LastLoginAt)
}

func TestLogout(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/logout", nil)
	rec := httptest.NewRecorder()

	h.Logout(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestLogoutRevokesAPIKeySession(t *testing.T) {
	keyID := uuid.New()
	storeStub := &fakeStore{}
	h := New(&fakeService{store: storeStub})

	req := httptest.NewRequest(http.MethodPost, "/admin/auth/logout", nil)
	ctx := context.WithValue(req.Context(), contextKeyAuthMethod, "api_key")
	ctx = context.WithValue(ctx, contextKeyAuthKeyID, keyID.String())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Logout(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, storeStub.deletedAPIKeyIDs, 1)
	assert.Equal(t, keyID, storeStub.deletedAPIKeyIDs[0])
}

func TestLogoutAPIKeyContextInvalid(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})

	req := httptest.NewRequest(http.MethodPost, "/admin/auth/logout", nil)
	ctx := context.WithValue(req.Context(), contextKeyAuthMethod, "api_key")
	ctx = context.WithValue(ctx, contextKeyAuthKeyID, "not-a-uuid")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Logout(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLogoutAPIKeyRevocationError(t *testing.T) {
	keyID := uuid.New()
	storeStub := &fakeStore{
		deleteAPIKeyFn: func(ctx context.Context, id uuid.UUID) error {
			return errors.New("delete failed")
		},
	}
	h := New(&fakeService{store: storeStub})

	req := httptest.NewRequest(http.MethodPost, "/admin/auth/logout", nil)
	ctx := context.WithValue(req.Context(), contextKeyAuthMethod, "api_key")
	ctx = context.WithValue(ctx, contextKeyAuthKeyID, keyID.String())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Logout(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAPIContractHelperFunctions(t *testing.T) {
	assert.Equal(t, "active", normalizeIdentityState("ACTIVE"))
	assert.Equal(t, "inactive", normalizeIdentityState("banned"))
	assert.Equal(t, "active", normalizeIdentityState("unknown-state"))

	i, ok := toInt(json.Number("42"))
	assert.True(t, ok)
	assert.Equal(t, 42, i)
	_, ok = toInt(json.Number("not-a-number"))
	assert.False(t, ok)
	_, ok = toInt("12")
	assert.False(t, ok)

	values, ok := toStringSlice([]interface{}{" example.com ", "", "aegion.dev"})
	assert.True(t, ok)
	assert.Equal(t, []string{"example.com", "aegion.dev"}, values)
	_, ok = toStringSlice([]interface{}{"ok", 123})
	assert.False(t, ok)

	assert.Equal(t, 1, paginationTotalPages(0, 20))
	assert.Equal(t, 3, paginationTotalPages(41, 20))
	assert.Equal(t, 1, paginationTotalPages(10, 0))
}

func TestHandlerDBConnFallbackAndOverride(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{}})
	require.Nil(t, h.db)
	pool, ok := h.dbConn().(*pgxpool.Pool)
	require.True(t, ok)
	require.Nil(t, pool)

	custom := &fakeDB{}
	h.db = custom
	require.Equal(t, custom, h.dbConn())
}

func TestRequirePermissionUnauthorized(t *testing.T) {
	h := New(&fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	rec := httptest.NewRecorder()

	handler := RequirePermission(h, service.PermOperatorsRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequirePermissionAllowed(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	h := New(&fakeService{
		evaluateCapabilityFn: func(ctx context.Context, operatorID uuid.UUID, permission string) error {
			assert.Equal(t, op.ID, operatorID)
			assert.Equal(t, service.PermOperatorsRead, permission)
			return nil
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
	rec := httptest.NewRecorder()

	handler := RequirePermission(h, service.PermOperatorsRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRequireAdminNotOperator(t *testing.T) {
	h := New(&fakeService{
		getOperatorByIdentityIDFn: func(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
			return nil, errors.New("not operator")
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("X-Aegion-Session-Identity-ID", uuid.NewString())
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminAPIKeyPrefixNotFound(t *testing.T) {
	h := New(&fakeService{store: &fakeStore{apiKeysByPrefix: map[string]*store.APIKey{}}})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer aegion_examplekey01_not_a_real_token")
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminAPIKeyHashMismatch(t *testing.T) {
	opID := uuid.New()
	operator := &store.Operator{
		ID:         opID,
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	key := &store.APIKey{
		ID:         uuid.New(),
		OperatorID: opID,
		KeyPrefix:  "examplekey01",
		KeyHash:    store.HashAPIKeyToken("aegion_different_token_value"),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	h := New(&fakeService{store: &fakeStore{
		operators:       map[uuid.UUID]*store.Operator{opID: operator},
		apiKeysByPrefix: map[string]*store.APIKey{"examplekey01": key},
	}})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer aegion_examplekey01_not_a_real_token")
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminAPIKeyExpired(t *testing.T) {
	opID := uuid.New()
	operator := &store.Operator{
		ID:         opID,
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	expired := time.Now().UTC().Add(-time.Hour)
	token := "aegion_examplekey01_not_a_real_token"
	key := &store.APIKey{
		ID:         uuid.New(),
		OperatorID: opID,
		KeyPrefix:  "examplekey01",
		KeyHash:    store.HashAPIKeyToken(token),
		ExpiresAt:  &expired,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	h := New(&fakeService{store: &fakeStore{
		operators:       map[uuid.UUID]*store.Operator{opID: operator},
		apiKeysByPrefix: map[string]*store.APIKey{"examplekey01": key},
	}})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAdminAPIKeyOperatorMissing(t *testing.T) {
	token := "aegion_examplekey01_not_a_real_token"
	key := &store.APIKey{
		ID:         uuid.New(),
		OperatorID: uuid.New(),
		KeyPrefix:  "examplekey01",
		KeyHash:    store.HashAPIKeyToken(token),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	h := New(&fakeService{store: &fakeStore{
		operators:       map[uuid.UUID]*store.Operator{},
		apiKeysByPrefix: map[string]*store.APIKey{"examplekey01": key},
	}})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler := h.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuditLogMiddlewareCapturesStatus(t *testing.T) {
	fs := &fakeStore{}
	h := New(&fakeService{store: fs})
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	req := httptest.NewRequest(http.MethodPost, "/admin/operators", nil)
	req.Header.Set("User-Agent", "test-agent")
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
	req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "req-123"))
	rec := httptest.NewRecorder()

	handler := h.AuditLog("create", "operator")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	handler.ServeHTTP(rec, req)
	require.Eventually(t, func() bool {
		fs.auditLogsMu.RLock()
		defer fs.auditLogsMu.RUnlock()
		return len(fs.auditLogs) > 0
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, http.StatusCreated, rec.Code)
	fs.auditLogsMu.RLock()
	last := fs.auditLogs[len(fs.auditLogs)-1]
	fs.auditLogsMu.RUnlock()
	assert.Equal(t, "create", last.Action)
	assert.Equal(t, "operator", last.ResourceType)
	assert.Equal(t, "127.0.0.1", last.IPAddress)
	assert.Equal(t, "req-123", last.Details["request_id"])
	assert.Equal(t, http.StatusCreated, last.Details["status_code"])
}

func TestResponseWriterWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}
	w.WriteHeader(http.StatusAccepted)
	assert.Equal(t, http.StatusAccepted, w.statusCode)
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestRegisterRoutesMountsExpectedPaths(t *testing.T) {
	h := New(&fakeService{})
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/auth/login", http.StatusBadRequest},
		{http.MethodGet, "/identities", http.StatusUnauthorized},
		{http.MethodGet, "/sessions", http.StatusUnauthorized},
		{http.MethodGet, "/operators", http.StatusUnauthorized},
		{http.MethodGet, "/roles", http.StatusUnauthorized},
		{http.MethodGet, "/dashboard/stats", http.StatusUnauthorized},
		{http.MethodGet, "/settings", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assert.Equal(t, tc.want, rec.Code, tc.path)
	}
}

func TestListOperatorsServicePermissionDenied(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	h := New(&fakeService{
		listOperatorsFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error) {
			return nil, 0, service.ErrPermissionDenied
		},
		store: &fakeStore{},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
	rec := httptest.NewRecorder()

	h.ListOperators(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestListOperatorsSuccess(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	target := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "viewer", Permissions: map[string]any{"a": true}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	h := New(&fakeService{
		listOperatorsFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error) {
			return []*store.Operator{target}, 1, nil
		},
		store: &fakeStore{
			getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
				return &store.IdentityProfile{Email: "viewer@example.com", Name: "Viewer", State: "active"}, nil
			},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators?page=1&per_page=20", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
	rec := httptest.NewRecorder()

	h.ListOperators(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "viewer@example.com")
}

func TestCreateAndUpdateOperatorInvalidPermissions(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("create invalid permissions", func(t *testing.T) {
		h := New(&fakeService{
			createOperatorFn: func(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrInvalidPermission
			},
			store: &fakeStore{},
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/operators", bytes.NewBufferString(`{"identity_id":"`+uuid.NewString()+`","role":"admin","permissions":{"roles:delete":"true"}}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_permissions")
	})

	t.Run("update invalid permissions", func(t *testing.T) {
		operatorID := uuid.NewString()
		h := New(&fakeService{
			updateOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrInvalidPermission
			},
			store: &fakeStore{},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+operatorID, bytes.NewBufferString(`{"permissions":{"roles:delete":"true"}}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", operatorID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_permissions")
	})
}

func TestGetOperatorValidationAndErrors(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("bad id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/not-a-uuid", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "not-a-uuid")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("permission denied", func(t *testing.T) {
		h := New(&fakeService{
			getOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error) {
				return nil, service.ErrPermissionDenied
			},
			store: &fakeStore{},
		})
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", uuid.NewString())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestGetRoleErrorsAndSuccess(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("missing name", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodGet, "/admin/roles/", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.GetRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("success", func(t *testing.T) {
		role := &store.Role{ID: uuid.New(), Name: "admin", Description: "Admin", Permissions: []string{"a:b"}, IsSystem: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		h := New(&fakeService{
			getRoleFn: func(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error) { return role, nil },
		})
		req := httptest.NewRequest(http.MethodGet, "/admin/roles/admin", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "admin")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.GetRole(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"name\":\"admin\"")
	})
}

func TestRoleMutationHandlers(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	withRoleParam := func(req *http.Request, name string) *http.Request {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", name)
		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	t.Run("list permissions", func(t *testing.T) {
		h := New(&fakeService{
			availablePermissionsFn: func() []string {
				return []string{"operators:create", "roles:update"}
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/admin/roles/permissions", nil)
		rec := httptest.NewRecorder()
		h.ListPermissions(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "operators:create")
	})

	t.Run("create role success", func(t *testing.T) {
		role := &store.Role{ID: uuid.New(), Name: "support_agent", Permissions: []string{"identities:read"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		h := New(&fakeService{
			createRoleFn: func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
				assert.Equal(t, op.ID, actorID)
				assert.Equal(t, "support_agent", name)
				assert.Equal(t, []string{"identities:read"}, permissions)
				return role, nil
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"support_agent","description":"Support","permissions":["identities:read"]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"name\":\"support_agent\"")
	})

	t.Run("create role invalid permission", func(t *testing.T) {
		h := New(&fakeService{
			createRoleFn: func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, service.ErrInvalidPermission
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"bad","permissions":["invalid"]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_permissions")
	})

	t.Run("create role invalid body", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString("{"))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create role unauthorized", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"support_agent","permissions":[]}`))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("create role duplicate", func(t *testing.T) {
		h := New(&fakeService{
			createRoleFn: func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, store.ErrDuplicateRole
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"support_agent","permissions":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("create role permission denied", func(t *testing.T) {
		h := New(&fakeService{
			createRoleFn: func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, service.ErrPermissionDenied
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"support_agent","permissions":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("create role invalid role name", func(t *testing.T) {
		h := New(&fakeService{
			createRoleFn: func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, service.ErrInvalidRoleName
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"bad-role","permissions":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create role internal error", func(t *testing.T) {
		h := New(&fakeService{
			createRoleFn: func(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, errors.New("boom")
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/roles", bytes.NewBufferString(`{"name":"support_agent","permissions":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.CreateRole(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("update role success", func(t *testing.T) {
		role := &store.Role{ID: uuid.New(), Name: "support_agent", Permissions: []string{"audit:read"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		h := New(&fakeService{
			updateRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
				assert.Equal(t, "support_agent", name)
				assert.NotNil(t, description)
				assert.Equal(t, []string{"audit:read"}, permissions)
				return role, nil
			},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString(`{"description":"Support","permissions":["audit:read"]}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"name\":\"support_agent\"")
	})

	t.Run("update role missing name", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update role invalid body", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString("{"))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update role not found", func(t *testing.T) {
		h := New(&fakeService{
			updateRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, store.ErrRoleNotFound
			},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update role invalid role name", func(t *testing.T) {
		h := New(&fakeService{
			updateRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, service.ErrInvalidRoleName
			},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update role permission denied", func(t *testing.T) {
		h := New(&fakeService{
			updateRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, service.ErrPermissionDenied
			},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("update role invalid permission", func(t *testing.T) {
		h := New(&fakeService{
			updateRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, service.ErrInvalidPermission
			},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update role internal error", func(t *testing.T) {
		h := New(&fakeService{
			updateRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error) {
				return nil, errors.New("boom")
			},
		})
		req := httptest.NewRequest(http.MethodPatch, "/admin/roles/support_agent", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.UpdateRole(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("delete role role_in_use", func(t *testing.T) {
		h := New(&fakeService{
			deleteRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
				return service.ErrRoleInUse
			},
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/support_agent", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "role_in_use")
	})

	t.Run("delete role missing name", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("delete role unauthorized", func(t *testing.T) {
		h := New(&fakeService{})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/support_agent", nil)
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("delete role not found", func(t *testing.T) {
		h := New(&fakeService{
			deleteRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
				return store.ErrRoleNotFound
			},
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/missing", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "missing")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("delete role permission denied", func(t *testing.T) {
		h := New(&fakeService{
			deleteRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
				return service.ErrPermissionDenied
			},
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/support_agent", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("delete role invalid role name", func(t *testing.T) {
		h := New(&fakeService{
			deleteRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
				return service.ErrInvalidRoleName
			},
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/Bad-Name", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "Bad-Name")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("delete role internal error", func(t *testing.T) {
		h := New(&fakeService{
			deleteRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
				return errors.New("boom")
			},
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/support_agent", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("delete role success", func(t *testing.T) {
		h := New(&fakeService{
			deleteRoleFn: func(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error {
				return nil
			},
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/roles/support_agent", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		req = withRoleParam(req, "support_agent")
		rec := httptest.NewRecorder()
		h.DeleteRole(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestDashboardStatsAndSettingsHandlers(t *testing.T) {
	op := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identities") && strings.Contains(sql, "deleted_at IS NULL") && strings.Contains(sql, "COUNT(*)"):
				return fakeRow{vals: []any{int64(10)}}
			case strings.Contains(sql, "FROM core_sessions") && strings.Contains(sql, "active = TRUE"):
				return fakeRow{vals: []any{int64(4)}}
			case strings.Contains(sql, "INTERVAL '24 hours'"):
				return fakeRow{vals: []any{int64(3)}}
			case strings.Contains(sql, "COUNT(DISTINCT ci.id) AS total"):
				return fakeRow{vals: []any{int64(10), int64(2)}}
			case strings.Contains(sql, "WHERE key = 'admin.settings'"):
				return fakeRow{err: pgx.ErrNoRows}
			default:
				return fakeRow{err: errors.New("unexpected query")}
			}
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	h := New(&fakeService{store: &fakeStore{}})
	h.db = db

	t.Run("dashboard unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil)
		rec := httptest.NewRecorder()
		h.DashboardStats(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("dashboard success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, op))
		rec := httptest.NewRecorder()
		h.DashboardStats(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"total_identities\":10")
		assert.Contains(t, rec.Body.String(), "\"mfa_adoption_rate\":20")
	})

	t.Run("settings get + patch success", func(t *testing.T) {
		getReq := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
		getReq = getReq.WithContext(context.WithValue(getReq.Context(), contextKeyOperator, op))
		getRec := httptest.NewRecorder()
		h.GetSettings(getRec, getReq)
		assert.Equal(t, http.StatusOK, getRec.Code)

		patchReq := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"session_lifetime_hours":12,"mfa_required":true,"password_min_length":10,"max_login_attempts":7,"lockout_duration_minutes":30,"allowed_domains":["example.com"]}`))
		patchReq = patchReq.WithContext(context.WithValue(patchReq.Context(), contextKeyOperator, op))
		patchRec := httptest.NewRecorder()
		h.UpdateSettings(patchRec, patchReq)
		assert.Equal(t, http.StatusOK, patchRec.Code)
		assert.Contains(t, patchRec.Body.String(), "\"mfa_required\":true")
	})
}

func withRouteParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestListSessionsHandlers(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions", nil)
		h.ListSessions(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("count query error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListSessions(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("success", func(t *testing.T) {
		sessionID := uuid.New()
		identityID := uuid.New()
		createdAt := time.Now().UTC().Add(-10 * time.Minute)
		expiresAt := time.Now().UTC().Add(50 * time.Minute)
		authAt := time.Now().UTC().Add(-5 * time.Minute)

		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{sessionID, identityID, "ua", "127.0.0.1", createdAt, expiresAt, authAt}}}, nil
			},
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions?page=1&per_page=5", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListSessions(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), sessionID.String())
		assert.Contains(t, rec.Body.String(), identityID.String())
	})
}

func TestSessionRevocationHandlers(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("list identity sessions invalid identity id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/bad/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "bad-uuid")
		h.ListIdentitySessions(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("list identity sessions success", func(t *testing.T) {
		identityID := uuid.New()
		sessionID := uuid.New()
		now := time.Now().UTC()
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{sessionID.String(), "aal1", true, now.Add(time.Hour), now, "127.0.0.1", "Mozilla"}}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+identityID.String()+"/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.ListIdentitySessions(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), sessionID.String())
	})

	t.Run("list identity sessions unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+uuid.NewString()+"/sessions", nil)
		h.ListIdentitySessions(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("list identity sessions db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("db error")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+uuid.NewString()+"/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.ListIdentitySessions(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("revoke all sessions success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 3"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+uuid.NewString()+"/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.RevokeAllIdentitySessions(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"sessions_revoked\":3")
	})

	t.Run("revoke all sessions invalid id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/invalid/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid-uuid")
		h.RevokeAllIdentitySessions(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("revoke all sessions unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+uuid.NewString()+"/sessions", nil)
		h.RevokeAllIdentitySessions(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("revoke all sessions db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("db error")
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+uuid.NewString()+"/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.RevokeAllIdentitySessions(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("revoke session not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{false}}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/sessions/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "session_id", uuid.NewString())
		h.RevokeSession(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("revoke session success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/sessions/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "session_id", uuid.NewString())
		h.RevokeSession(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("revoke session invalid id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/sessions/invalid", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "session_id", "invalid-uuid")
		h.RevokeSession(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("revoke session unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/sessions/"+uuid.NewString(), nil)
		h.RevokeSession(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("revoke session db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("db error")
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/sessions/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "session_id", uuid.NewString())
		h.RevokeSession(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestIdentityStateHelpers(t *testing.T) {
	assert.Equal(t, "ci.created_at ASC", identitySortExpr("created_at"))
	assert.Equal(t, "ci.created_at DESC", identitySortExpr("-created_at"))
	assert.Equal(t, "ci.state ASC, ci.created_at DESC", identitySortExpr("state"))
	assert.Equal(t, "ci.created_at DESC", identitySortExpr("unexpected"))

	assert.Equal(t, "banned", normalizeDBState(IdentityStateBlocked))
	assert.Equal(t, "inactive", normalizeDBState(IdentityStateDeleted))
	assert.Equal(t, "active", normalizeDBState(IdentityStateActive))

	assert.Equal(t, IdentityStateBlocked, mapDBStateToAPI("banned"))
	assert.Equal(t, IdentityStateInactive, mapDBStateToAPI("inactive"))
	assert.Equal(t, IdentityStateActive, mapDBStateToAPI("active"))
	assert.Equal(t, IdentityStateActive, mapDBStateToAPI("unknown"))
}

func TestScanIdentityRowHandlesInvalidTraits(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()
	row := fakeRow{
		vals: []any{id.String(), id, "{bad-json", "active", now, now},
	}
	resp, err := scanIdentityRow(row)
	require.NoError(t, err)
	assert.Equal(t, id.String(), resp.ID)
	assert.Equal(t, IdentityStateActive, resp.State)
	assert.NotNil(t, resp.Traits)
}

func TestIdentityHandlersBasicPaths(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("list identities unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities", nil)
		h.ListIdentities(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("search identities invalid body", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/search", bytes.NewBufferString("{"))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.SearchIdentities(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("get identity not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.GetIdentity(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestOperatorHandlersErrorMappings(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("create operator missing role", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", bytes.NewBufferString(`{"identity_id":"`+uuid.NewString()+`"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create operator invalid identity id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", bytes.NewBufferString(`{"identity_id":"bad","role":"admin"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("create operator service invalid role maps bad request", func(t *testing.T) {
		h := New(&fakeService{
			createOperatorFn: func(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrInvalidRole
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", bytes.NewBufferString(`{"identity_id":"`+uuid.NewString()+`","role":"bad_role"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update operator invalid id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/not-uuid", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "not-uuid")
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("delete operator self deletion mapping", func(t *testing.T) {
		h := New(&fakeService{
			deleteOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error {
				return service.ErrSelfDeletion
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id)
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("get operator success", func(t *testing.T) {
		operatorID := uuid.New()
		targetOp := &store.Operator{
			ID:         operatorID,
			IdentityID: uuid.New(),
			Role:       "admin",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			getOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID) (*store.Operator, error) {
				return targetOp, nil
			},
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return &store.IdentityProfile{Email: "op@example.com", Name: "Op User", State: "active"}, nil
				},
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+operatorID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", operatorID.String())
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), operatorID.String())
	})

	t.Run("get operator permission denied", func(t *testing.T) {
		h := New(&fakeService{
			getOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID) (*store.Operator, error) {
				return nil, service.ErrPermissionDenied
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("get operator not found", func(t *testing.T) {
		h := New(&fakeService{
			getOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID) (*store.Operator, error) {
				return nil, store.ErrOperatorNotFound
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update operator success with name", func(t *testing.T) {
		operatorID := uuid.New()
		updatedOp := &store.Operator{
			ID:         operatorID,
			IdentityID: uuid.New(),
			Role:       "operator",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			updateOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return updatedOp, nil
			},
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return &store.IdentityProfile{Email: "op@example.com", Name: "Updated User", State: "active"}, nil
				},
			},
		})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+operatorID.String(), bytes.NewBufferString(`{"role":"operator","name":"Updated User"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", operatorID.String())
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("update operator self demotion", func(t *testing.T) {
		h := New(&fakeService{
			updateOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrSelfDemotion
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+uuid.NewString(), bytes.NewBufferString(`{"role":"viewer"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("update operator not found", func(t *testing.T) {
		h := New(&fakeService{
			updateOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, store.ErrOperatorNotFound
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+uuid.NewString(), bytes.NewBufferString(`{"role":"admin"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("create operator success", func(t *testing.T) {
		newOp := &store.Operator{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			Role:       "admin",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return newOp, nil
			},
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return &store.IdentityProfile{Email: "new@example.com", Name: "New Op", State: "active"}, nil
				},
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", bytes.NewBufferString(`{"identity_id":"`+uuid.NewString()+`","role":"admin"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("delete operator success", func(t *testing.T) {
		h := New(&fakeService{
			deleteOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID, ipAddress string) error {
				return nil
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("delete operator not found", func(t *testing.T) {
		h := New(&fakeService{
			deleteOperatorFn: func(ctx context.Context, actorID, opID uuid.UUID, ipAddress string) error {
				return store.ErrOperatorNotFound
			},
			store: &fakeStore{},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/"+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestIdentityHandlersExtendedPaths(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	now := time.Now().UTC()
	identityID := uuid.New()

	t.Run("list identities success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "COUNT(*)") {
					return fakeRow{vals: []any{int64(1)}}
				}
				return fakeRow{err: errors.New("unexpected query row")}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{
					{identityID.String(), uuid.New(), []byte(`{"email":"user@example.com"}`), "active", now, now},
				}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities?page=1&per_page=10&sort=created_at&filter=user", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListIdentities(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), identityID.String())
	})

	t.Run("update identity invalid state", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString(`{"state":"wrong"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update identity success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities") {
					return fakeRow{vals: []any{
						identityID.String(),
						uuid.New(),
						[]byte(`{"email":"user@example.com","display_name":"User"}`),
						"active",
						now,
						now,
					}}
				}
				if strings.Contains(sql, "FROM core_sessions") {
					return fakeRow{vals: []any{int64(0)}}
				}
				return fakeRow{err: errors.New("unexpected query")}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString(`{"traits":{"display_name":"User"},"state":"active"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"display_name":"User"`)
	})

	t.Run("delete identity success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+identityID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.DeleteIdentity(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("search identities success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "COUNT(*)") {
					return fakeRow{vals: []any{int64(1)}}
				}
				return fakeRow{err: errors.New("unexpected count query")}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{
					{identityID.String(), uuid.New(), []byte(`{"email":"user@example.com"}`), "active", now, now},
				}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/search?page=1&per_page=10", bytes.NewBufferString(`{"email":"user@example.com","traits":{"tier":"gold"},"state":"active"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.SearchIdentities(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), identityID.String())
	})

	t.Run("suspend and activate identity", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities") {
					return fakeRow{vals: []any{
						identityID.String(),
						uuid.New(),
						[]byte(`{"email":"user@example.com"}`),
						"active",
						now,
						now,
					}}
				}
				if strings.Contains(sql, "COUNT(*)") {
					return fakeRow{vals: []any{int64(0)}}
				}
				return fakeRow{err: errors.New("unexpected query")}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{}, nil
			},
		}

		suspendRec := httptest.NewRecorder()
		suspendReq := httptest.NewRequest(http.MethodPost, "/admin/identities/"+identityID.String()+"/suspend", nil)
		suspendReq = suspendReq.WithContext(context.WithValue(suspendReq.Context(), contextKeyOperator, operator))
		suspendReq = withRouteParam(suspendReq, "id", identityID.String())
		h.SuspendIdentity(suspendRec, suspendReq)
		assert.Equal(t, http.StatusNoContent, suspendRec.Code)

		activateRec := httptest.NewRecorder()
		activateReq := httptest.NewRequest(http.MethodPost, "/admin/identities/"+identityID.String()+"/activate", nil)
		activateReq = activateReq.WithContext(context.WithValue(activateReq.Context(), contextKeyOperator, operator))
		activateReq = withRouteParam(activateReq, "id", identityID.String())
		h.ActivateIdentity(activateRec, activateReq)
		assert.Equal(t, http.StatusNoContent, activateRec.Code)
	})

	t.Run("reset mfa success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		execSQL := make([]string, 0, 5)
		h.db = &fakeDB{
			beginFn: func(ctx context.Context) (pgx.Tx, error) {
				return &fakeTx{
					execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
						execSQL = append(execSQL, strings.TrimSpace(sql))
						return pgconn.NewCommandTag("UPDATE 1"), nil
					},
				}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+identityID.String()+"/reset-mfa", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.ResetIdentityMFA(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
		require.Len(t, execSQL, 5)
		assert.Contains(t, execSQL[0], "UPDATE core_sessions")
		assert.Contains(t, execSQL[1], "DELETE FROM mfa_enrollments")
		assert.Contains(t, execSQL[2], "DELETE FROM mfa_backup_codes")
		assert.Contains(t, execSQL[3], "DELETE FROM mfa_totp_factors")
		assert.Contains(t, execSQL[4], "DELETE FROM mfa_trusted_devices")
	})
}

func TestOperatorIdentityHelpers(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("createOperatorIdentity success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		tx := &fakeTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identity_schemas") {
					return fakeRow{vals: []any{uuid.New()}}
				}
				return fakeRow{err: errors.New("unexpected tx query")}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}
		h.db = &fakeDB{
			beginFn: func(ctx context.Context) (pgx.Tx, error) {
				return tx, nil
			},
		}
		id, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
			Email:    "new@example.com",
			Name:     "New User",
			Password: "VeryStrongPassword123!",
		})
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, id)
	})

	t.Run("resolveDefaultSchemaID creates fallback schema", func(t *testing.T) {
		tx := &fakeTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}
		id, err := resolveDefaultSchemaID(context.Background(), tx)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, id)
	})

	t.Run("updateIdentityForOperator success and no-op", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		execCalls := 0
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		err := h.updateIdentityForOperator(context.Background(), operator.IdentityID, "Renamed", "inactive")
		require.NoError(t, err)
		assert.Equal(t, 1, execCalls)

		err = h.updateIdentityForOperator(context.Background(), operator.IdentityID, "", "")
		require.NoError(t, err)
		assert.Equal(t, 1, execCalls)
	})
}

func TestGetSettings(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
		// No operator in context
		h.GetSettings(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				// Return valid JSON settings
				return fakeRow{vals: []any{[]byte(`{"session_lifetime_hours":24,"mfa_required":false}`)}}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.GetSettings(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("db error")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.GetSettings(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestUpdateSettings(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewBufferString(`{}`))
		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewBufferString(`{invalid}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid session_lifetime_hours", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewBufferString(`{"session_lifetime_hours": 10000}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid mfa_required type", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewBufferString(`{"mfa_required": "yes"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("success", func(t *testing.T) {
		execCount := 0
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{}}, nil
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				execCount++
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewBufferString(`{"session_lifetime_hours": 48, "mfa_required": true}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Greater(t, execCount, 0)
	})
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected int
		ok       bool
	}{
		{42, 42, true},
		{int32(30), 30, true},
		{int64(100), 100, true},
		{float64(50), 50, true},
		{json.Number("25"), 25, true},
		{json.Number("invalid"), 0, false},
		{nil, 0, false},
		{"string", 0, false},
		{[]int{1, 2}, 0, false},
	}

	for _, tc := range tests {
		result, ok := toInt(tc.input)
		assert.Equal(t, tc.expected, result)
		assert.Equal(t, tc.ok, ok)
	}
}

func TestGetIdentityErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+uuid.NewString(), nil)
		h.GetIdentity(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid uuid", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/invalid-uuid", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid-uuid")
		h.GetIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id)
		h.GetIdentity(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestDeleteIdentityErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+uuid.NewString(), nil)
		h.DeleteIdentity(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid uuid", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/invalid-uuid", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid-uuid")
		h.DeleteIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id)
		h.DeleteIdentity(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag(""), errors.New("db error")
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodDelete, "/admin/identities/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id)
		h.DeleteIdentity(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestListAuditLogsExtended(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	now := time.Now().UTC()

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/audit-logs", nil)
		h.ListAuditLogs(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("with filters", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{
					data: [][]any{
						{uuid.New(), uuid.New(), "create_operator", `{"email":"test@example.com"}`, "1.2.3.4", now},
					},
				}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/audit-logs?action=create_operator&actor_id="+uuid.NewString(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListAuditLogs(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestGetRoleErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/roles/admin", nil)
		h.GetRole(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/roles/nonexistent", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "name", "nonexistent")
		h.GetRole(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestResetIdentityMFAErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+uuid.NewString()+"/reset-mfa", nil)
		h.ResetIdentityMFA(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid uuid", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/invalid/reset-mfa", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid")
		h.ResetIdentityMFA(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			beginFn: func(ctx context.Context) (pgx.Tx, error) {
				return &fakeTx{
					execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
						return pgconn.NewCommandTag(""), errors.New("db error")
					},
				}, nil
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+id+"/reset-mfa", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id)
		h.ResetIdentityMFA(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestSuspendIdentityErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New()}

	t.Run("no auth context", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+uuid.NewString()+"/suspend", nil)
		req = withRouteParam(req, "id", uuid.NewString())
		h.SuspendIdentity(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid uuid", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/invalid/suspend", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid")
		h.SuspendIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("identity not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+id+"/suspend", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.SuspendIdentity(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag(""), errors.New("db offline")
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+id+"/suspend", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.SuspendIdentity(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestActivateIdentityErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New()}

	t.Run("no auth context", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+uuid.NewString()+"/activate", nil)
		req = withRouteParam(req, "id", uuid.NewString())
		h.ActivateIdentity(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid uuid", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/invalid/activate", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid")
		h.ActivateIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("identity not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+id+"/activate", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.ActivateIdentity(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("db error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag(""), errors.New("connection failed")
			},
		}
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/"+id+"/activate", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.ActivateIdentity(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestCreateOperatorErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New()}

	t.Run("no auth context", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{invalid}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_request")
	})

	t.Run("unknown field", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"identity_id":"` + uuid.NewString() + `","role":"admin","unexpected":true}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_request")
	})

	t.Run("missing role", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"email":"test@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "missing_role")
	})

	t.Run("invalid identity id format", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"identity_id":"not-a-uuid","role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_identity_id")
	})

	t.Run("missing email and password without identity_id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "email and password are required")
	})

	t.Run("missing password without identity_id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"email":"test@example.com","role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "email and password are required")
	})

	t.Run("createOperatorIdentity duplicate error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		tx := &fakeTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identity_schemas") {
					return fakeRow{vals: []any{uuid.New()}}
				}
				return fakeRow{err: errors.New("unexpected tx query")}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT INTO core_identity_addresses") {
					return pgconn.CommandTag{}, store.ErrDuplicateOperator
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}
		h.db = &fakeDB{
			beginFn: func(ctx context.Context) (pgx.Tx, error) {
				return tx, nil
			},
		}
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"email":"test@example.com","password":"Test123!","role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "duplicate_identity")
	})

	t.Run("createOperatorIdentity internal error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("db error")}
			},
		}
		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"email":"test@example.com","password":"Test123!","role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to create identity")
	})

	t.Run("service permission denied", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrPermissionDenied
			},
		})
		rec := httptest.NewRecorder()
		identityID := uuid.New()
		body := bytes.NewBufferString(fmt.Sprintf(`{"identity_id":"%s","role":"admin"}`, identityID))
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "insufficient_permissions")
	})

	t.Run("service invalid role", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrInvalidRole
			},
		})
		rec := httptest.NewRecorder()
		identityID := uuid.New()
		body := bytes.NewBufferString(fmt.Sprintf(`{"identity_id":"%s","role":"invalid"}`, identityID))
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_role")
	})

	t.Run("service duplicate operator", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, store.ErrDuplicateOperator
			},
		})
		rec := httptest.NewRecorder()
		identityID := uuid.New()
		body := bytes.NewBufferString(fmt.Sprintf(`{"identity_id":"%s","role":"admin"}`, identityID))
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "duplicate_operator")
	})

	t.Run("service internal error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, errors.New("service error")
			},
		})
		rec := httptest.NewRecorder()
		identityID := uuid.New()
		body := bytes.NewBufferString(fmt.Sprintf(`{"identity_id":"%s","role":"admin"}`, identityID))
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to create operator")
	})

	t.Run("failed to load profile", func(t *testing.T) {
		newOp := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin"}
		h := New(&fakeService{
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return nil, errors.New("profile error")
				},
			},
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return newOp, nil
			},
		})
		rec := httptest.NewRecorder()
		identityID := uuid.New()
		body := bytes.NewBufferString(fmt.Sprintf(`{"identity_id":"%s","role":"admin"}`, identityID))
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to load operator profile")
	})
}

func TestUpdateOperatorAdditionalErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+uuid.NewString(), bytes.NewBufferString(`{}`))
		req = withRouteParam(req, "id", uuid.NewString())
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+uuid.NewString(), bytes.NewBufferString(`{`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", uuid.NewString())
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_request")
	})

	t.Run("permission denied", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			updateOperatorFn: func(ctx context.Context, actorID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrPermissionDenied
			},
		})
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+id, bytes.NewBufferString(`{"role":"admin"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "insufficient_permissions")
	})

	t.Run("invalid role", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			updateOperatorFn: func(ctx context.Context, actorID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, service.ErrInvalidRole
			},
		})
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+id, bytes.NewBufferString(`{"role":"bad_role"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_role")
	})

	t.Run("service internal error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			updateOperatorFn: func(ctx context.Context, actorID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return nil, errors.New("update failed")
			},
		})
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+id, bytes.NewBufferString(`{"role":"admin"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to update operator")
	})

	t.Run("identity profile update failure", func(t *testing.T) {
		updatedOp := &store.Operator{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			Role:       "operator",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return &store.IdentityProfile{Email: "op@example.com", Name: "Op User", State: "active"}, nil
				},
			},
			updateOperatorFn: func(ctx context.Context, actorID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return updatedOp, nil
			},
		})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("identity update failed")
			},
		}
		rec := httptest.NewRecorder()
		id := updatedOp.ID.String()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+id, bytes.NewBufferString(`{"name":"Renamed User"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to update operator identity profile")
	})

	t.Run("profile lookup failure", func(t *testing.T) {
		updatedOp := &store.Operator{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			Role:       "operator",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return nil, errors.New("profile lookup failed")
				},
			},
			updateOperatorFn: func(ctx context.Context, actorID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				return updatedOp, nil
			},
		})
		rec := httptest.NewRecorder()
		id := updatedOp.ID.String()
		req := httptest.NewRequest(http.MethodPatch, "/admin/operators/"+id, bytes.NewBufferString(`{"role":"operator"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.UpdateOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to load operator profile")
	})
}

func TestDeleteOperatorAdditionalErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/"+uuid.NewString(), nil)
		req = withRouteParam(req, "id", uuid.NewString())
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid operator id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/not-uuid", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "not-uuid")
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("permission denied", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			deleteOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error {
				return service.ErrPermissionDenied
			},
		})
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "insufficient_permissions")
	})

	t.Run("service internal error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			deleteOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error {
				return errors.New("delete failed")
			},
		})
		rec := httptest.NewRecorder()
		id := uuid.NewString()
		req := httptest.NewRequest(http.MethodDelete, "/admin/operators/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		req = withRouteParam(req, "id", id)
		h.DeleteOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to delete operator")
	})
}

func TestListRolesAdditionalErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
		h.ListRoles(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("permission denied", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listRolesFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
				return nil, 0, service.ErrPermissionDenied
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/roles?page=2&per_page=3", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListRoles(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "insufficient_permissions")
	})

	t.Run("internal error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listRolesFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
				return nil, 0, errors.New("list roles failed")
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListRoles(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to list roles")
	})
}

func TestListAuditLogsAdditionalErrors(t *testing.T) {
	operator := &store.Operator{ID: uuid.New(), IdentityID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	t.Run("permission denied", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listAuditLogsFn: func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
				return nil, 0, service.ErrPermissionDenied
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?action=update", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListAuditLogs(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "insufficient_permissions")
	})

	t.Run("internal error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listAuditLogsFn: func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
				return nil, 0, errors.New("list audit logs failed")
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListAuditLogs(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to list audit logs")
	})

	t.Run("invalid operator id filter ignored", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listAuditLogsFn: func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
				assert.Nil(t, filter.OperatorID)
				assert.Equal(t, "delete_operator", filter.Action)
				assert.Equal(t, "operator", filter.ResourceType)
				assert.Equal(t, "res-1", filter.ResourceID)
				return []*store.AuditLogEntry{}, 0, nil
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?operator_id=invalid&action=delete_operator&resource_type=operator&resource_id=res-1", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListAuditLogs(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestIdentitySortExprAdditionalCases(t *testing.T) {
	assert.Equal(t, "ci.updated_at ASC", identitySortExpr("updated_at"))
	assert.Equal(t, "ci.updated_at DESC", identitySortExpr("-updated_at"))
	assert.Equal(t, "ci.state DESC, ci.created_at DESC", identitySortExpr("-state"))
}

func TestDashboardStatsAndSettingsAdditionalErrors(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	t.Run("dashboard active sessions query error", func(t *testing.T) {
		calls := 0
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				calls++
				if calls == 1 {
					return fakeRow{vals: []any{int64(10)}}
				}
				return fakeRow{err: errors.New("session stats failed")}
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.DashboardStats(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("dashboard signup query error", func(t *testing.T) {
		calls := 0
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				calls++
				switch calls {
				case 1:
					return fakeRow{vals: []any{int64(10)}}
				case 2:
					return fakeRow{vals: []any{int64(3)}}
				default:
					return fakeRow{err: errors.New("signup stats failed")}
				}
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.DashboardStats(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("dashboard mfa query error tolerated", func(t *testing.T) {
		calls := 0
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				calls++
				switch calls {
				case 1:
					return fakeRow{vals: []any{int64(10)}}
				case 2:
					return fakeRow{vals: []any{int64(4)}}
				case 3:
					return fakeRow{vals: []any{int64(2)}}
				default:
					return fakeRow{err: errors.New("mfa query failed")}
				}
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/stats", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.DashboardStats(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "\"mfa_adoption_rate\":0")
	})

	t.Run("settings update read current settings error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("read failed")}
			},
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"mfa_required":true}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("settings update invalid password min length", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"password_min_length":5}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("settings update invalid max login attempts", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"max_login_attempts":25}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("settings update invalid lockout duration", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"lockout_duration_minutes":2000}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("settings update invalid allowed domains", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"allowed_domains":"example.com"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("settings update upsert failure", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("write failed")
			},
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/settings", bytes.NewBufferString(`{"mfa_required":true}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		rec := httptest.NewRecorder()

		h.UpdateSettings(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestListSessionsIdentityFilterAndScanError(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	filterIdentityID := uuid.New()

	t.Run("query error with identity filter", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				require.Len(t, args, 1)
				assert.Equal(t, filterIdentityID.String(), args[0])
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				assert.Contains(t, sql, "AND cs.identity_id = $1")
				require.Len(t, args, 3)
				assert.Equal(t, filterIdentityID.String(), args[0])
				return nil, errors.New("list failed")
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions?identity_id="+filterIdentityID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListSessions(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("scan error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{uuid.New(), "not-a-uuid", "ua", "ip", time.Now().UTC(), time.Now().UTC(), time.Now().UTC()}}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListSessions(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestIdentityHandlersAdditionalPaths(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	identityID := uuid.New()

	t.Run("list identities count error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities?filter=foo", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListIdentities(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("list identities query error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListIdentities(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("get identity internal error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("db down")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/identities/"+identityID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.GetIdentity(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("update identity unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString(`{}`))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("update identity invalid id", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/invalid", bytes.NewBufferString(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", "invalid")
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update identity invalid json", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString("{"))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("update identity unknown field", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString(`{"traits":{"name":"x"},"unexpected":true}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_request")
	})

	t.Run("update identity not found", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString(`{"traits":{"name":"x"}}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("update identity internal error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/identities/"+identityID.String(), bytes.NewBufferString(`{"traits":{"name":"x"}}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", identityID.String())
		h.UpdateIdentity(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("search identities internal error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/identities/search", bytes.NewBufferString(`{"email":"x@example.com"}`))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.SearchIdentities(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestOperatorHandlersAdditionalPaths(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	t.Run("list operators unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
		h.ListOperators(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("list operators internal error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listOperatorsFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error) {
				return nil, 0, errors.New("boom")
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListOperators(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("get operator unauthorized", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+uuid.NewString(), nil)
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("get operator internal service error", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			getOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error) {
				return nil, errors.New("boom")
			},
		})
		id := uuid.NewString()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+id, nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", id)
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("get operator profile lookup failure", func(t *testing.T) {
		target := &store.Operator{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			Role:       "admin",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			getOperatorFn: func(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error) {
				return target, nil
			},
			store: &fakeStore{
				getIdentityProfileFn: func(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
					return nil, errors.New("profile fail")
				},
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/operators/"+target.ID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "id", target.ID.String())
		h.GetOperator(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("list roles success with entry operator id", func(t *testing.T) {
		role := &store.Role{
			ID:          uuid.New(),
			Name:        "operator",
			Description: "Operator",
			Permissions: []string{"operators:read"},
			IsSystem:    true,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		actorID := uuid.New()
		entry := &store.AuditLogEntry{
			ID:           uuid.New(),
			OperatorID:   &actorID,
			Action:       "read",
			ResourceType: "operator",
			ResourceID:   "res-1",
			Details:      map[string]interface{}{"ok": true},
			IPAddress:    "127.0.0.1",
			CreatedAt:    time.Now().UTC(),
		}
		h := New(&fakeService{
			store: &fakeStore{},
			listRolesFn: func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
				return []*store.Role{role}, 1, nil
			},
			listAuditLogsFn: func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
				return []*store.AuditLogEntry{entry}, 1, nil
			},
		})

		roleRec := httptest.NewRecorder()
		roleReq := httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
		roleReq = roleReq.WithContext(context.WithValue(roleReq.Context(), contextKeyOperator, operator))
		h.ListRoles(roleRec, roleReq)
		assert.Equal(t, http.StatusOK, roleRec.Code)

		auditRec := httptest.NewRecorder()
		auditReq := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
		auditReq = auditReq.WithContext(context.WithValue(auditReq.Context(), contextKeyOperator, operator))
		h.ListAuditLogs(auditRec, auditReq)
		assert.Equal(t, http.StatusOK, auditRec.Code)
		assert.Contains(t, auditRec.Body.String(), actorID.String())
	})
}

func TestOperatorHandler_AdditionalCoveragePaths(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	t.Run("create operator without identity id success", func(t *testing.T) {
		newOp := &store.Operator{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			Role:       "admin",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		h := New(&fakeService{
			store: &fakeStore{
				getIdentityProfileFn: func(context.Context, uuid.UUID) (*store.IdentityProfile, error) {
					return &store.IdentityProfile{Email: "new@example.com", Name: "New User", State: "active"}, nil
				},
			},
			createOperatorFn: func(ctx context.Context, actorID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
				if identityID == uuid.Nil {
					t.Fatalf("expected createOperatorIdentity path to provide a non-nil identity ID")
				}
				return newOp, nil
			},
		})
		tx := &fakeTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identity_schemas") {
					return fakeRow{vals: []any{uuid.New()}}
				}
				return fakeRow{err: errors.New("unexpected schema query")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
		}
		h.db = &fakeDB{
			beginFn: func(context.Context) (pgx.Tx, error) { return tx, nil },
		}

		rec := httptest.NewRecorder()
		body := bytes.NewBufferString(`{"email":"new@example.com","password":"StrongPass123!","role":"admin"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/operators", body)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = req.WithContext(context.WithValue(req.Context(), contextKeyIPAddress, "127.0.0.1"))
		h.CreateOperator(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("createOperatorIdentity inactive and failure branches", func(t *testing.T) {
		t.Run("inactive status is persisted", func(t *testing.T) {
			var capturedState string
			h := New(&fakeService{store: &fakeStore{}})
			tx := &fakeTx{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					if strings.Contains(sql, "FROM core_identity_schemas") {
						return fakeRow{vals: []any{uuid.New()}}
					}
					return fakeRow{err: errors.New("unexpected query")}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "INSERT INTO core_identities") {
						capturedState, _ = args[3].(string)
					}
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			}
			h.db = &fakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}

			_, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
				Email:    "inactive@example.com",
				Password: "StrongPass123!",
				Status:   "inactive",
			})
			require.NoError(t, err)
			assert.Equal(t, "inactive", capturedState)
		})

		t.Run("bcrypt rejects too long password", func(t *testing.T) {
			h := New(&fakeService{store: &fakeStore{}})
			_, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
				Email:    "toolong@example.com",
				Password: strings.Repeat("x", 73),
			})
			require.Error(t, err)
		})

		t.Run("schema lookup error propagates", func(t *testing.T) {
			h := New(&fakeService{store: &fakeStore{}})
			tx := &fakeTx{
				queryRowFn: func(context.Context, string, ...any) pgx.Row {
					return fakeRow{err: errors.New("schema lookup failed")}
				},
			}
			h.db = &fakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}

			_, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
				Email:    "schema@example.com",
				Password: "StrongPass123!",
			})
			require.EqualError(t, err, "schema lookup failed")
		})

		t.Run("identity insert error propagates", func(t *testing.T) {
			h := New(&fakeService{store: &fakeStore{}})
			tx := &fakeTx{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					if strings.Contains(sql, "FROM core_identity_schemas") {
						return fakeRow{vals: []any{uuid.New()}}
					}
					return fakeRow{err: errors.New("unexpected query")}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "INSERT INTO core_identities") {
						return pgconn.CommandTag{}, errors.New("identity insert failed")
					}
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			}
			h.db = &fakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}

			_, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
				Email:    "insert@example.com",
				Password: "StrongPass123!",
			})
			require.EqualError(t, err, "identity insert failed")
		})

		t.Run("password credential insert error propagates", func(t *testing.T) {
			h := New(&fakeService{store: &fakeStore{}})
			tx := &fakeTx{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					if strings.Contains(sql, "FROM core_identity_schemas") {
						return fakeRow{vals: []any{uuid.New()}}
					}
					return fakeRow{err: errors.New("unexpected query")}
				},
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "INSERT INTO pwd_credentials") {
						return pgconn.CommandTag{}, errors.New("credentials insert failed")
					}
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
			}
			h.db = &fakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}

			_, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
				Email:    "creds@example.com",
				Password: "StrongPass123!",
			})
			require.EqualError(t, err, "credentials insert failed")
		})

		t.Run("commit error propagates", func(t *testing.T) {
			h := New(&fakeService{store: &fakeStore{}})
			tx := &fakeTx{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					if strings.Contains(sql, "FROM core_identity_schemas") {
						return fakeRow{vals: []any{uuid.New()}}
					}
					return fakeRow{err: errors.New("unexpected query")}
				},
				execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
				commitFn: func(context.Context) error { return errors.New("commit failed") },
			}
			h.db = &fakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}

			_, err := h.createOperatorIdentity(context.Background(), CreateOperatorRequest{
				Email:    "commit@example.com",
				Password: "StrongPass123!",
			})
			require.EqualError(t, err, "commit failed")
		})
	})

	t.Run("resolveDefaultSchemaID fallback insert error", func(t *testing.T) {
		tx := &fakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert default schema failed")
			},
		}
		_, err := resolveDefaultSchemaID(context.Background(), tx)
		require.EqualError(t, err, "insert default schema failed")
	})

	t.Run("get role permission denied and internal error", func(t *testing.T) {
		t.Run("permission denied", func(t *testing.T) {
			h := New(&fakeService{
				store: &fakeStore{},
				getRoleFn: func(context.Context, uuid.UUID, string) (*store.Role, error) {
					return nil, service.ErrPermissionDenied
				},
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/roles/admin", nil)
			req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
			req = withRouteParam(req, "name", "admin")
			h.GetRole(rec, req)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		})

		t.Run("internal error", func(t *testing.T) {
			h := New(&fakeService{
				store: &fakeStore{},
				getRoleFn: func(context.Context, uuid.UUID, string) (*store.Role, error) {
					return nil, errors.New("role service down")
				},
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/roles/admin", nil)
			req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
			req = withRouteParam(req, "name", "admin")
			h.GetRole(rec, req)
			assert.Equal(t, http.StatusInternalServerError, rec.Code)
		})
	})

	t.Run("list audit logs respects pagination parsing cap", func(t *testing.T) {
		var gotLimit int
		h := New(&fakeService{
			store: &fakeStore{},
			listAuditLogsFn: func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
				gotLimit = limit
				return []*store.AuditLogEntry{}, 0, nil
			},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?per_page=999&page=1", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListAuditLogs(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 20, gotLimit)
	})
}

func TestIntegrationHandlers(t *testing.T) {
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	t.Run("overview success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "soc_providers"):
					return fakeRow{vals: []any{int64(2)}}
				case strings.Contains(sql, "soc_identity_links"):
					return fakeRow{vals: []any{int64(3)}}
				case strings.Contains(sql, "sso_connections"):
					return fakeRow{vals: []any{int64(4)}}
				case strings.Contains(sql, "proxy_upstreams"):
					return fakeRow{vals: []any{int64(5)}}
				case strings.Contains(sql, "proxy_routes"):
					return fakeRow{vals: []any{int64(6)}}
				case strings.Contains(sql, "adm_scim_tokens"):
					return fakeRow{vals: []any{int64(7)}}
				default:
					return fakeRow{err: errors.New("unexpected query")}
				}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/integrations/overview", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.IntegrationOverview(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"social_providers":2`)
		assert.Contains(t, rec.Body.String(), `"proxy_routes":6`)
	})

	t.Run("overview internal error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{err: errors.New("db down")}
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/integrations/overview", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.IntegrationOverview(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("social providers list success", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{
					{"google", "Google", "google", "oidc", true, "https://example.com/callback", time.Now().UTC(), time.Now().UTC()},
				}}, nil
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/integrations/social/providers", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListSocialProviders(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"count":1`)
		assert.Contains(t, rec.Body.String(), `"slug":"google"`)
	})

	t.Run("social provider upsert success", func(t *testing.T) {
		manager := &fakeSocialProviderManager{
			upsertProviderFn: func(ctx context.Context, req socialservice.ProviderUpsertRequest) (*socialstore.Provider, error) {
				assert.Equal(t, "google", req.Slug)
				assert.Equal(t, "Google", req.DisplayName)
				assert.Equal(t, "client-id", req.ClientID)
				assert.Equal(t, "super-secret", req.ClientSecret)
				return &socialstore.Provider{
					ID:                 uuid.New(),
					Slug:               req.Slug,
					DisplayName:        req.DisplayName,
					Preset:             req.Preset,
					Protocol:           req.Protocol,
					ClientID:           req.ClientID,
					RedirectURI:        req.RedirectURI,
					Enabled:            req.Enabled,
					TrustEmailVerified: req.TrustEmailVerified,
				}, nil
			},
		}
		h := New(&fakeService{store: &fakeStore{}}, HandlerConfig{SocialProviders: manager})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/integrations/social/providers", strings.NewReader(`{
			"slug":"google",
			"display_name":"Google",
			"preset":"google",
			"protocol":"oidc",
			"redirect_uri":"https://example.com/callback",
			"client_id":"client-id",
			"client_secret":"super-secret",
			"enabled":true,
			"trust_email_verified":true
		}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpsertSocialProvider(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"slug":"google"`)
		assert.NotContains(t, rec.Body.String(), "super-secret")
	})

	t.Run("social provider get success", func(t *testing.T) {
		manager := &fakeSocialProviderManager{
			getProviderFn: func(ctx context.Context, slug string) (*socialstore.Provider, error) {
				return &socialstore.Provider{
					ID:          uuid.New(),
					Slug:        slug,
					DisplayName: "Google",
					Protocol:    socialstore.ProtocolOIDC,
					Enabled:     true,
				}, nil
			},
		}
		h := New(&fakeService{store: &fakeStore{}}, HandlerConfig{SocialProviders: manager})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/integrations/social/providers/google", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "slug", "google")
		h.GetSocialProvider(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"slug":"google"`)
	})

	t.Run("social provider delete not found", func(t *testing.T) {
		manager := &fakeSocialProviderManager{
			deleteProviderFn: func(ctx context.Context, slug string) error {
				return socialstore.ErrProviderNotFound
			},
		}
		h := New(&fakeService{store: &fakeStore{}}, HandlerConfig{SocialProviders: manager})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/admin/integrations/social/providers/missing", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		req = withRouteParam(req, "slug", "missing")
		h.DeleteSocialProvider(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("social provider management unavailable", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/integrations/social/providers", strings.NewReader(`{"slug":"google"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.UpsertSocialProvider(rec, req)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("proxy routes list query error", func(t *testing.T) {
		h := New(&fakeService{store: &fakeStore{}})
		h.db = &fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/integrations/proxy/routes", nil)
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		h.ListProxyRoutes(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
