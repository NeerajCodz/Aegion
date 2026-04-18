package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/admin/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func newPolicyHandler(db *fakeDB) (*Handler, *store.Operator) {
	h := New(&fakeService{store: &fakeStore{}})
	h.db = db
	operator := &store.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	return h, operator
}

func withPolicyOperator(req *http.Request, operator *store.Operator) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
}

func TestPolicyABACHandlersCoverage(t *testing.T) {
	now := time.Now().UTC()
	boom := errors.New("boom")

	t.Run("list abac branches", func(t *testing.T) {
		h, operator := newPolicyHandler(&fakeDB{})

		rec := httptest.NewRecorder()
		h.ListPolicyABACRules(rec, httptest.NewRequest(http.MethodGet, "/admin/policy/abac-rules", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, boom }}
		rec = httptest.NewRecorder()
		req := withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/abac-rules", nil), operator)
		h.ListPolicyABACRules(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{uuid.NewString(), "rule", "desc", "subject == true", "bad-priority", "allow", true, now, now}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/abac-rules", nil), operator)
		h.ListPolicyABACRules(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on scan failure, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{err: boom}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/abac-rules", nil), operator)
		h.ListPolicyABACRules(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on rows err, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{uuid.NewString(), "rule", "desc", "subject == true", 10, "allow", true, now, now}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/abac-rules", nil), operator)
		h.ListPolicyABACRules(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("upsert abac branches", func(t *testing.T) {
		h, operator := newPolicyHandler(&fakeDB{})

		rec := httptest.NewRecorder()
		h.UpsertPolicyABACRule(rec, httptest.NewRequest(http.MethodPost, "/admin/policy/abac-rules", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/abac-rules", bytes.NewBufferString(`{"name":"x"}{"extra":1}`)), operator)
		h.UpsertPolicyABACRule(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid json, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/abac-rules", bytes.NewBufferString(`{"name":" ","expression":" "}`)), operator)
		h.UpsertPolicyABACRule(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 missing fields, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/abac-rules", bytes.NewBufferString(`{"name":"rule","expression":"subject == true","effect":"maybe"}`)), operator)
		h.UpsertPolicyABACRule(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid effect, got %d", rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return fakeRow{err: boom} }}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/abac-rules", bytes.NewBufferString(`{"name":"rule","expression":"subject == true","effect":"allow"}`)), operator)
		h.UpsertPolicyABACRule(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 from query error, got %d", rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return fakeRow{vals: []any{now, now}}
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/abac-rules", bytes.NewBufferString(`{"name":"rule","description":"desc","expression":"subject == true","priority":5,"effect":"allow","enabled":true}`)), operator)
		h.UpsertPolicyABACRule(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 upsert success, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete abac branches", func(t *testing.T) {
		h, operator := newPolicyHandler(&fakeDB{})

		rec := httptest.NewRecorder()
		h.DeletePolicyABACRule(rec, httptest.NewRequest(http.MethodDelete, "/admin/policy/abac-rules/x", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/abac-rules/invalid", nil), operator), "id", "invalid")
		h.DeletePolicyABACRule(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid id, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, boom
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/abac-rules/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyABACRule(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 exec error, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/abac-rules/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyABACRule(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 missing rule, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/abac-rules/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyABACRule(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 delete success, got %d", rec.Code)
		}
	})
}

func TestPolicyReBACHandlersCoverage(t *testing.T) {
	now := time.Now().UTC()
	boom := errors.New("boom")

	t.Run("list tuples and namespaces", func(t *testing.T) {
		h, operator := newPolicyHandler(&fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, boom }})

		rec := httptest.NewRecorder()
		h.ListPolicyReBACTuples(rec, httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-tuples", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-tuples", nil), operator)
		h.ListPolicyReBACTuples(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 tuple query error, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{"bad"}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-tuples", nil), operator)
		h.ListPolicyReBACTuples(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 tuple scan error, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{err: boom}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-tuples", nil), operator)
		h.ListPolicyReBACTuples(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 tuple rows error, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{uuid.NewString(), "doc", "1", "viewer", "u1", now}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-tuples", nil), operator)
		h.ListPolicyReBACTuples(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 tuple list success, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, boom }}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-namespaces", nil), operator)
		h.ListPolicyReBACNamespaces(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 namespace query error, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		h.ListPolicyReBACNamespaces(rec, httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-namespaces", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 namespace unauthorized, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{"bad"}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-namespaces", nil), operator)
		h.ListPolicyReBACNamespaces(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 namespace scan error, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{err: boom}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-namespaces", nil), operator)
		h.ListPolicyReBACNamespaces(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 namespace rows error, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeRows{data: [][]any{{uuid.NewString(), "doc", []byte(`{"enabled":true}`), 2, true, now, now}}}, nil
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodGet, "/admin/policy/rebac-namespaces", nil), operator)
		h.ListPolicyReBACNamespaces(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 namespace list success, got %d", rec.Code)
		}
	})

	t.Run("namespace upsert and delete", func(t *testing.T) {
		h, operator := newPolicyHandler(&fakeDB{})

		rec := httptest.NewRecorder()
		h.UpsertPolicyReBACNamespace(rec, httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-namespaces", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-namespaces", bytes.NewBufferString(`{"name":"x"}{"extra":1}`)), operator)
		h.UpsertPolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-namespaces", bytes.NewBufferString(`{"name":" "}`)), operator)
		h.UpsertPolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 missing name, got %d", rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return fakeRow{err: boom} }}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-namespaces", bytes.NewBufferString(`{"name":"docs","config":{"scope":"all"},"active":true}`)), operator)
		h.UpsertPolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 save failure, got %d", rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return fakeRow{vals: []any{uuid.NewString(), 3, now, now}}
		}}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-namespaces", bytes.NewBufferString(`{"name":"docs","config":{"scope":"all"},"active":true}`)), operator)
		h.UpsertPolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 save success, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		h.DeletePolicyReBACNamespace(rec, httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-namespaces/id", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-namespaces/invalid", nil), operator), "id", "invalid")
		h.DeletePolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid id, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, boom }}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-namespaces/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 delete failure, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-namespaces/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 missing namespace, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-namespaces/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyReBACNamespace(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 delete success, got %d", rec.Code)
		}
	})
}

func TestPolicyTupleAndSimulateHandlersCoverage(t *testing.T) {
	now := time.Now().UTC()
	boom := errors.New("boom")
	h, operator := newPolicyHandler(&fakeDB{})

	t.Run("upsert tuple branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.UpsertPolicyReBACTuple(rec, httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-tuples", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-tuples", bytes.NewBufferString(`{"namespace":"x"}{"extra":1}`)), operator)
		h.UpsertPolicyReBACTuple(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-tuples", bytes.NewBufferString(`{"namespace":" ","object_id":" ","relation":" ","subject_id":" "}`)), operator)
		h.UpsertPolicyReBACTuple(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 missing fields, got %d", rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return fakeRow{err: boom} }}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-tuples", bytes.NewBufferString(`{"namespace":"doc","object_id":"1","relation":"viewer","subject_id":"u1"}`)), operator)
		h.UpsertPolicyReBACTuple(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 save failure, got %d", rec.Code)
		}

		h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return fakeRow{vals: []any{now}} }}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/rebac-tuples", bytes.NewBufferString(`{"namespace":"doc","object_id":"1","relation":"viewer","subject_id":"u1"}`)), operator)
		h.UpsertPolicyReBACTuple(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 save success, got %d", rec.Code)
		}
	})

	t.Run("delete tuple branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.DeletePolicyReBACTuple(rec, httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-tuples/id", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-tuples/id", nil), operator), "id", "invalid")
		h.DeletePolicyReBACTuple(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid id, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, boom }}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-tuples/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyReBACTuple(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 delete failure, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-tuples/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyReBACTuple(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 missing tuple, got %d", rec.Code)
		}

		h.db = &fakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		}}
		rec = httptest.NewRecorder()
		req = withRouteParam(withPolicyOperator(httptest.NewRequest(http.MethodDelete, "/admin/policy/rebac-tuples/id", nil), operator), "id", uuid.NewString())
		h.DeletePolicyReBACTuple(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 delete success, got %d", rec.Code)
		}
	})

	t.Run("simulate branches and helper", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.SimulatePolicyDecision(rec, httptest.NewRequest(http.MethodPost, "/admin/policy/simulate", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/simulate", bytes.NewBufferString(`{"subject":"x"}{"extra":1}`)), operator)
		h.SimulatePolicyDecision(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/simulate", bytes.NewBufferString(`{"subject":" ","resource_type":" ","action":" "}`)), operator)
		h.SimulatePolicyDecision(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 missing required fields, got %d", rec.Code)
		}

		h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, boom }}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/simulate", bytes.NewBufferString(`{"subject":"user:1","resource":"doc:1","resource_type":"doc","action":"read","model":"abac"}`)), operator)
		h.SimulatePolicyDecision(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 from checker failure, got %d body=%s", rec.Code, rec.Body.String())
		}

		h.db = &fakeDB{}
		rec = httptest.NewRecorder()
		req = withPolicyOperator(httptest.NewRequest(http.MethodPost, "/admin/policy/simulate", bytes.NewBufferString(`{"subject":"user:1","resource":"doc:1","resource_type":"doc","action":"read","model":"abac"}`)), operator)
		h.SimulatePolicyDecision(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 simulate success, got %d body=%s", rec.Code, rec.Body.String())
		}

		if got := emptyToNil("   "); got != nil {
			t.Fatalf("expected nil for emptyToNil whitespace, got %#v", got)
		}
		if got := emptyToNil("  value  "); got == nil || *got != "value" {
			t.Fatalf("unexpected emptyToNil value: %#v", got)
		}
		if !strings.Contains(http.StatusText(http.StatusBadRequest), "Bad") {
			t.Fatal("unexpected status text sanity check")
		}
	})
}
