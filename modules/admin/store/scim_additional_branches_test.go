package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	adminscim "github.com/aegion/aegion/modules/admin/scim"
)

func TestSCIMStoreAdditionalListAndDeleteBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	userID := uuid.New()

	t.Run("list users startIndex offset, query error and scan error", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{1}}
			},
			queryFn: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
				if len(args) < 2 || args[len(args)-1] != 1 {
					t.Fatalf("expected offset 1 for startIndex=2, args=%#v", args)
				}
				return nil, errors.New("query failed")
			},
		}}
		if _, _, err := s.ListUsers(ctx, nil, "", adminscim.SortAscending, 2, 10); err == nil || err.Error() != "query failed" {
			t.Fatalf("ListUsers(query error) = %v", err)
		}

		s = &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{1}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"only-id"}}}, nil
			},
		}}
		if _, _, err := s.ListUsers(ctx, nil, "", adminscim.SortAscending, 1, 10); err == nil {
			t.Fatal("ListUsers(scan error) expected error")
		}
	})

	t.Run("delete user exec and not-found branches", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}}
		if err := s.DeleteUser(ctx, userID.String()); err == nil || err.Error() != "delete failed" {
			t.Fatalf("DeleteUser(exec error) = %v", err)
		}

		s = &Store{db: &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}}
		if err := s.DeleteUser(ctx, userID.String()); !errors.Is(err, ErrIdentityNotFound) {
			t.Fatalf("DeleteUser(not found) = %v", err)
		}
	})

	t.Run("list groups startIndex offset, query error and scan error", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{1}}
			},
			queryFn: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
				if len(args) < 2 || args[len(args)-1] != 1 {
					t.Fatalf("expected offset 1 for startIndex=2, args=%#v", args)
				}
				return nil, errors.New("group query failed")
			},
		}}
		if _, _, err := s.ListGroups(ctx, nil, "", adminscim.SortAscending, 2, 10); err == nil || err.Error() != "group query failed" {
			t.Fatalf("ListGroups(query error) = %v", err)
		}

		s = &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{1}}
			},
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{"group-1", "ext", "Group", []byte(`[]`), now}}}, nil
			},
		}}
		if _, _, err := s.ListGroups(ctx, nil, "", adminscim.SortAscending, 1, 10); err == nil {
			t.Fatal("ListGroups(scan error) expected error")
		}
	})

	t.Run("delete group exec error", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete group failed")
			},
		}}
		if err := s.DeleteGroup(ctx, "group-1"); err == nil || err.Error() != "delete group failed" {
			t.Fatalf("DeleteGroup(exec error) = %v", err)
		}
	})
}

func TestSCIMStoreAdditionalPatchBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	userID := uuid.New()
	userTraits := []byte(`{"email":"user@example.com","display_name":"Original User","scim_external_id":"ext-1"}`)
	groupMembers := []byte(`[{"value":"id-1","display":"User One"}]`)

	t.Run("patch user get-by-id error", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}}
		if _, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{{Op: "replace", Path: "displayName", Value: "x"}}); !errors.Is(err, ErrIdentityNotFound) {
			t.Fatalf("PatchUser(get error) = %v", err)
		}
	})

	t.Run("patch user add/replace branches", func(t *testing.T) {
		readCalls := 0
		s := &Store{db: &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM core_identities ci"):
					readCalls++
					email := "user@example.com"
					if readCalls > 1 {
						email = "new@example.com"
					}
					return fakeRow{vals: []any{userID, userTraits, "active", now, now, email}}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}}
		out, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{
			{Op: "add", Path: "active", Value: false},
			{Op: "replace", Path: "displayName", Value: " Updated User "},
			{Op: "replace", Path: "userName", Value: " new@example.com "},
			{Op: "replace", Path: "emails", Value: []map[string]any{{"value": "new@example.com", "primary": true}}},
		})
		if err != nil {
			t.Fatalf("PatchUser(add/replace) error = %v", err)
		}
		if out == nil || out.UserName != "new@example.com" {
			t.Fatalf("PatchUser(add/replace) unexpected result: %#v", out)
		}
	})

	t.Run("patch user emails marshal/unmarshal and update error branches", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities ci") {
					email := "user@example.com"
					return fakeRow{vals: []any{userID, userTraits, "active", now, now, email}}
				}
				return fakeRow{err: pgx.ErrNoRows}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}}
		if _, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{{Op: "replace", Path: "emails", Value: make(chan int)}}); err == nil || err.Error() != "invalid operation" {
			t.Fatalf("PatchUser(emails marshal error) = %v", err)
		}
		if _, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{{Op: "replace", Path: "emails", Value: "not-an-array"}}); err == nil || err.Error() != "invalid operation" {
			t.Fatalf("PatchUser(emails unmarshal error) = %v", err)
		}
		if _, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{{Op: "remove", Path: "displayName"}}); !errors.Is(err, ErrIdentityNotFound) {
			t.Fatalf("PatchUser(update error) = %v", err)
		}
	})

	t.Run("patch group get-by-id and operation branches", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("load group failed")}
			},
		}}
		if _, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "replace", Path: "displayName", Value: "x"}}); err == nil || err.Error() != "load group failed" {
			t.Fatalf("PatchGroup(get error) = %v", err)
		}

		readCalls := 0
		s = &Store{db: &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM adm_scim_groups") {
					readCalls++
					external := "ext-group"
					return fakeRow{vals: []any{"group-1", &external, "Group One", groupMembers, now, now}}
				}
				return fakeRow{err: pgx.ErrNoRows}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}}
		out, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{
			{Op: "add", Path: "displayName", Value: " Group Updated "},
			{Op: "replace", Path: "members", Value: []map[string]any{{"value": "id-2", "display": "User Two"}}},
		})
		if err != nil {
			t.Fatalf("PatchGroup(add/replace) error = %v", err)
		}
		if out == nil || out.DisplayName != "Group One" || readCalls < 2 {
			t.Fatalf("PatchGroup(add/replace) unexpected result: %#v calls=%d", out, readCalls)
		}

		if _, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "replace", Path: "members", Value: make(chan int)}}); err == nil || err.Error() != "invalid operation" {
			t.Fatalf("PatchGroup(members marshal error) = %v", err)
		}
		if _, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "replace", Path: "members", Value: "not-an-array"}}); err == nil || err.Error() != "invalid operation" {
			t.Fatalf("PatchGroup(members unmarshal error) = %v", err)
		}
		if _, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "unknown", Path: "displayName", Value: "x"}}); err == nil || err.Error() != "invalid operation" {
			t.Fatalf("PatchGroup(default invalid op) = %v", err)
		}
	})

	t.Run("patch group update error", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM adm_scim_groups") {
					external := "ext-group"
					return fakeRow{vals: []any{"group-1", &external, "Group One", groupMembers, now, now}}
				}
				return fakeRow{err: pgx.ErrNoRows}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE adm_scim_groups") {
					return pgconn.CommandTag{}, errors.New("update failed")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}}
		if _, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "replace", Path: "displayName", Value: "x"}}); err == nil || err.Error() != "update failed" {
			t.Fatalf("PatchGroup(update error) = %v", err)
		}
	})
}

func TestSCIMStoreAdditionalMappingTokenAndScanBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("list mapping/token query and scan errors", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}}
		if _, err := s.ListSCIMMappings(ctx); err == nil || err.Error() != "query failed" {
			t.Fatalf("ListSCIMMappings(query error) = %v", err)
		}
		if _, err := s.ListSCIMTokens(ctx); err == nil || err.Error() != "query failed" {
			t.Fatalf("ListSCIMTokens(query error) = %v", err)
		}

		s = &Store{db: &fakeDB{
			queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
				if strings.Contains(sql, "FROM adm_scim_mappings") {
					return &fakeRows{data: [][]any{{"bad"}}}, nil
				}
				return &fakeRows{data: [][]any{{"bad"}}}, nil
			},
		}}
		if _, err := s.ListSCIMMappings(ctx); err == nil {
			t.Fatal("ListSCIMMappings(scan error) expected error")
		}
		if _, err := s.ListSCIMTokens(ctx); err == nil {
			t.Fatal("ListSCIMTokens(scan error) expected error")
		}
	})

	t.Run("create token and default schema generic error", func(t *testing.T) {
		s := &Store{db: &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert token failed")
			},
		}}
		err := s.CreateSCIMToken(ctx, &adminscim.SCIMToken{
			ID:          uuid.New(),
			Name:        "token",
			Description: "desc",
			TokenHash:   "hash",
			Prefix:      "pre_1",
			Permissions: []string{"users:read"},
			CreatedBy:   uuid.New(),
			CreatedAt:   time.Now().UTC(),
			Active:      true,
		})
		if err == nil || err.Error() != "insert token failed" {
			t.Fatalf("CreateSCIMToken(exec error) = %v", err)
		}

		s = &Store{db: &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("schema query failed")}
			},
		}}
		if _, err := s.defaultIdentitySchemaID(ctx); err == nil || err.Error() != "schema query failed" {
			t.Fatalf("defaultIdentitySchemaID(generic error) = %v", err)
		}
	})

	t.Run("scan helper generic errors", func(t *testing.T) {
		if _, err := scanSCIMUser(fakeRow{err: errors.New("scan user failed")}); err == nil || err.Error() != "scan user failed" {
			t.Fatalf("scanSCIMUser(generic error) = %v", err)
		}
		if _, err := scanSCIMGroupRow(fakeRow{err: errors.New("scan group failed")}); err == nil || err.Error() != "scan group failed" {
			t.Fatalf("scanSCIMGroupRow(generic error) = %v", err)
		}
		if _, err := scanSCIMMapping(fakeRow{err: errors.New("scan mapping failed")}); err == nil || err.Error() != "scan mapping failed" {
			t.Fatalf("scanSCIMMapping(generic error) = %v", err)
		}
		if _, err := scanSCIMToken(fakeRow{err: errors.New("scan token failed")}); err == nil || err.Error() != "scan token failed" {
			t.Fatalf("scanSCIMToken(generic error) = %v", err)
		}
	})
}

