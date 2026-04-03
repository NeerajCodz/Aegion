package flows

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubRow struct {
	values []interface{}
	err    error
}

func (s stubRow) Scan(dest ...interface{}) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			if s.values[i] == nil {
				var zero uuid.UUID
				*d = zero
			} else {
				*d = s.values[i].(uuid.UUID)
			}
		case **uuid.UUID:
			if s.values[i] == nil {
				*d = nil
			} else {
				switch v := s.values[i].(type) {
				case uuid.UUID:
					id := v
					*d = &id
				case *uuid.UUID:
					*d = v
				default:
					return errors.New("unsupported uuid pointer source")
				}
			}
		case *FlowType:
			*d = s.values[i].(FlowType)
		case *FlowState:
			*d = s.values[i].(FlowState)
		case *string:
			*d = s.values[i].(string)
		case *[]byte:
			*d = s.values[i].([]byte)
		case *time.Time:
			*d = s.values[i].(time.Time)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

type stubRows struct {
	rows    [][]interface{}
	scanErr map[int]error
	err     error
	idx     int
	closed  bool
}

func (s *stubRows) Close()                                       { s.closed = true }
func (s *stubRows) Err() error                                   { return s.err }
func (s *stubRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (s *stubRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (s *stubRows) Next() bool                                   { return s.idx < len(s.rows) }
func (s *stubRows) Values() ([]interface{}, error)               { return nil, errors.New("not implemented") }
func (s *stubRows) RawValues() [][]byte                          { return nil }
func (s *stubRows) Conn() *pgx.Conn                              { return nil }
func (s *stubRows) Scan(dest ...interface{}) error {
	rowIdx := s.idx
	s.idx++
	if err, ok := s.scanErr[rowIdx]; ok {
		return err
	}
	row := s.rows[rowIdx]
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = row[i].(uuid.UUID)
		case **uuid.UUID:
			if row[i] == nil {
				*d = nil
			} else {
				switch v := row[i].(type) {
				case uuid.UUID:
					id := v
					*d = &id
				case *uuid.UUID:
					*d = v
				default:
					return errors.New("unsupported uuid pointer source")
				}
			}
		case *FlowType:
			*d = row[i].(FlowType)
		case *FlowState:
			*d = row[i].(FlowState)
		case *string:
			*d = row[i].(string)
		case *[]byte:
			*d = row[i].([]byte)
		case *time.Time:
			*d = row[i].(time.Time)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func baseFlowJSONPayload() ([]byte, []byte) {
	uiBytes, _ := json.Marshal(&UIState{Action: "/self-service/login", Method: "POST"})
	ctxBytes, _ := json.Marshal(FlowCtx{"attempt": 1})
	return uiBytes, ctxBytes
}

func newSeamedFlowStore() *PostgresFlowStore {
	s := NewPostgresFlowStore(nil)
	s.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return s
}

func TestPostgresFlowStore_Create_Get_GetByCSRF_WithSeams(t *testing.T) {
	store := newSeamedFlowStore()
	flow := &Flow{
		ID:         uuid.New(),
		Type:       TypeLogin,
		State:      StateActive,
		RequestURL: "/self-service/login",
		ReturnTo:   "https://example.com/app",
		UI:         &UIState{Action: "/self-service/login", Method: "POST"},
		Context:    FlowCtx{"attempt": 1},
		IssuedAt:   store.now(),
		ExpiresAt:  store.now().Add(15 * time.Minute),
		CSRFToken:  "csrf-123",
		CreatedAt:  store.now(),
		UpdatedAt:  store.now(),
	}

	execCalls := 0
	store.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		execCalls++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if err := store.Create(context.Background(), flow); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if execCalls != 1 {
		t.Fatalf("expected one insert call, got %d", execCalls)
	}

	uiBytes, ctxBytes := baseFlowJSONPayload()
	store.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
		return stubRow{values: []interface{}{
			flow.ID, flow.Type, flow.State, (*uuid.UUID)(nil), (*uuid.UUID)(nil), flow.RequestURL, flow.ReturnTo,
			uiBytes, ctxBytes, flow.IssuedAt, flow.ExpiresAt, flow.CSRFToken, flow.CreatedAt, flow.UpdatedAt,
		}}
	}
	got, err := store.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != flow.ID || got.Type != TypeLogin {
		t.Fatalf("unexpected flow returned from Get")
	}

	gotByCSRF, err := store.GetByCSRF(context.Background(), flow.CSRFToken)
	if err != nil {
		t.Fatalf("GetByCSRF failed: %v", err)
	}
	if gotByCSRF.CSRFToken != flow.CSRFToken {
		t.Fatalf("unexpected CSRF token from GetByCSRF")
	}
}

func TestPostgresFlowStore_GetErrors_WithSeams(t *testing.T) {
	store := newSeamedFlowStore()
	flowID := uuid.New()

	store.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
		return stubRow{err: pgx.ErrNoRows}
	}
	if _, err := store.Get(context.Background(), flowID); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("expected ErrFlowNotFound, got %v", err)
	}
	if _, err := store.GetByCSRF(context.Background(), "missing"); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("expected ErrFlowNotFound, got %v", err)
	}

	store.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
		return stubRow{err: errors.New("query failed")}
	}
	if _, err := store.Get(context.Background(), flowID); err == nil || err.Error() != "query failed" {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestPostgresFlowStore_Update_Delete_DeleteExpired_WithSeams(t *testing.T) {
	store := newSeamedFlowStore()
	flow := &Flow{
		ID:         uuid.New(),
		Type:       TypeSettings,
		State:      StateActive,
		RequestURL: "/settings",
		UI:         &UIState{Action: "/settings", Method: "POST"},
		Context:    FlowCtx{"key": "value"},
		ExpiresAt:  store.now().Add(10 * time.Minute),
	}

	// Update success
	store.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := store.Update(context.Background(), flow); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !flow.UpdatedAt.Equal(store.now()) {
		t.Fatalf("expected UpdatedAt to use seam clock")
	}

	// Update not found
	store.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if err := store.Update(context.Background(), flow); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("expected ErrFlowNotFound on update 0 rows, got %v", err)
	}

	// Delete success and not found
	store.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	if err := store.Delete(context.Background(), flow.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	store.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	if err := store.Delete(context.Background(), flow.ID); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("expected ErrFlowNotFound when delete affects 0 rows, got %v", err)
	}

	// DeleteExpired
	store.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 4"), nil
	}
	deleted, err := store.DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("DeleteExpired failed: %v", err)
	}
	if deleted != 4 {
		t.Fatalf("expected 4 deleted rows, got %d", deleted)
	}
}

func TestPostgresFlowStore_ListByIdentity_WithSeams(t *testing.T) {
	store := newSeamedFlowStore()
	identityID := uuid.New()
	uiBytes, ctxBytes := baseFlowJSONPayload()

	flow1 := uuid.New()
	flow2 := uuid.New()
	now := store.now()
	rows := &stubRows{
		rows: [][]interface{}{
			{flow1, TypeLogin, StateActive, identityID, nil, "/login", "https://app/1", uiBytes, ctxBytes, now, now.Add(10 * time.Minute), "csrf-1", now, now},
			{flow2, TypeLogin, StateActive, identityID, nil, "/login", "https://app/2", uiBytes, ctxBytes, now, now.Add(12 * time.Minute), "csrf-2", now, now},
		},
	}
	store.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }

	flows, err := store.ListByIdentity(context.Background(), identityID, TypeLogin)
	if err != nil {
		t.Fatalf("ListByIdentity failed: %v", err)
	}
	if len(flows) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(flows))
	}
	if !rows.closed {
		t.Fatalf("expected rows to close")
	}

	// query error
	store.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
		return nil, errors.New("query failed")
	}
	if _, err := store.ListByIdentity(context.Background(), identityID, TypeLogin); err == nil {
		t.Fatalf("expected query error")
	}

	// scan error
	rows = &stubRows{
		rows:    [][]interface{}{{flow1, TypeLogin, StateActive, identityID, nil, "/login", "r", uiBytes, ctxBytes, now, now, "csrf", now, now}},
		scanErr: map[int]error{0: errors.New("scan failed")},
	}
	store.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }
	if _, err := store.ListByIdentity(context.Background(), identityID, TypeLogin); err == nil || err.Error() != "scan failed" {
		t.Fatalf("expected scan error, got %v", err)
	}

	// rows err
	rows = &stubRows{
		rows: [][]interface{}{},
		err:  errors.New("rows failed"),
	}
	store.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) { return rows, nil }
	if _, err := store.ListByIdentity(context.Background(), identityID, TypeLogin); err == nil || err.Error() != "rows failed" {
		t.Fatalf("expected rows failed error, got %v", err)
	}
}

func TestPostgresFlowStore_MarshalErrors_WithSeams(t *testing.T) {
	store := newSeamedFlowStore()

	flow := &Flow{
		ID:         uuid.New(),
		Type:       TypeLogin,
		State:      StateActive,
		RequestURL: "/login",
		UI: &UIState{
			Action: "/login",
			Method: "POST",
			Nodes: []Node{
				{
					Type:       NodeTypeInput,
					Group:      "default",
					Attributes: NodeAttributes{Name: "self", Type: InputTypeText},
				},
			},
		},
		Context:   FlowCtx{},
		ExpiresAt: store.now().Add(time.Minute),
	}
	flow.Context["self"] = flow.Context // recursive map triggers marshal error

	if err := store.Create(context.Background(), flow); err == nil {
		t.Fatalf("expected marshal error in Create")
	}
	if err := store.Update(context.Background(), flow); err == nil {
		t.Fatalf("expected marshal error in Update")
	}
}
