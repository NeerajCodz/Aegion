package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
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
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")

	ip := getClientIP(req)
	assert.Equal(t, "1.1.1.1", ip)
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

func TestRequireAdminWithIdentity(t *testing.T) {
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
		op := OperatorFromContext(r.Context())
		assert.NotNil(t, op)
		assert.Equal(t, operator.ID, op.ID)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
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

	prefix := "123456789012"
	apiKey := &store.APIKey{
		ID:         uuid.New(),
		OperatorID: operatorID,
		KeyPrefix:  prefix,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	storeStub := &fakeStore{
		operators:    map[uuid.UUID]*store.Operator{operatorID: operator},
		apiKeysByPrefix: map[string]*store.APIKey{prefix: apiKey},
	}

	h := New(&fakeService{store: storeStub})
	req := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	req.Header.Set("Authorization", "Bearer aegion_12345678901234567890")
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
	store                   service.Store
	getOperatorByIdentityIDFn func(ctx context.Context, identityID uuid.UUID) (*store.Operator, error)
	evaluateCapabilityFn    func(ctx context.Context, operatorID uuid.UUID, permission string) error
	listRolesFn            func(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error)
	listAuditLogsFn        func(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error)
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

func (f *fakeService) ListOperators(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error) {
	return []*store.Operator{}, 0, nil
}

func (f *fakeService) GetOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error) {
	return nil, store.ErrOperatorNotFound
}

func (f *fakeService) CreateOperator(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
	return nil, nil
}

func (f *fakeService) UpdateOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error) {
	return nil, nil
}

func (f *fakeService) DeleteOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error {
	return nil
}

func (f *fakeService) ListRoles(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error) {
	if f.listRolesFn != nil {
		return f.listRolesFn(ctx, actorID, limit, offset)
	}
	return []*store.Role{}, 0, nil
}

func (f *fakeService) GetRole(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error) {
	return nil, store.ErrRoleNotFound
}

func (f *fakeService) ListAuditLogs(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error) {
	if f.listAuditLogsFn != nil {
		return f.listAuditLogsFn(ctx, actorID, filter, limit, offset)
	}
	return []*store.AuditLogEntry{}, 0, nil
}

type fakeStore struct {
	operators       map[uuid.UUID]*store.Operator
	apiKeysByPrefix map[string]*store.APIKey
	updatedLastUsed bool
	auditLogs       []*store.AuditLogEntry
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

func (f *fakeStore) ListAuditLogs(ctx context.Context, filter store.AuditFilter, opts store.ListOptions) ([]*store.AuditLogEntry, int64, error) {
	return []*store.AuditLogEntry{}, 0, nil
}

func (f *fakeStore) LogAction(ctx context.Context, entry *store.AuditLogEntry) error {
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
