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

func TestSCIMHelpers(t *testing.T) {
	if got := scimIdentityState(true); got != "active" {
		t.Fatalf("scimIdentityState(true) = %q, want active", got)
	}
	if got := scimIdentityState(false); got != "inactive" {
		t.Fatalf("scimIdentityState(false) = %q, want inactive", got)
	}

	if where, args := buildSCIMUserFilter(nil); where != "" || len(args) != 0 {
		t.Fatalf("buildSCIMUserFilter(nil) = %q, %#v", where, args)
	}
	if where, args := buildSCIMUserFilter(&adminscim.Filter{Attribute: "userName", Operator: "eq", Value: "user@example.com"}); !strings.Contains(where, "LOWER(COALESCE") || len(args) != 1 {
		t.Fatalf("buildSCIMUserFilter(userName eq) = %q %#v", where, args)
	}
	if where, args := buildSCIMUserFilter(&adminscim.Filter{Attribute: "externalId", Operator: "eq", Value: "ext-1"}); !strings.Contains(where, "scim_external_id") || len(args) != 1 {
		t.Fatalf("buildSCIMUserFilter(externalId eq) = %q %#v", where, args)
	}
	if where, _ := buildSCIMUserFilter(&adminscim.Filter{Attribute: "active", Operator: "eq", Value: "true"}); !strings.Contains(where, "ci.state <> 'inactive'") {
		t.Fatalf("buildSCIMUserFilter(active true) = %q", where)
	}
	if where, _ := buildSCIMUserFilter(&adminscim.Filter{Attribute: "active", Operator: "eq", Value: false}); !strings.Contains(where, "ci.state = 'inactive'") {
		t.Fatalf("buildSCIMUserFilter(active false) = %q", where)
	}
	if where, args := buildSCIMUserFilter(&adminscim.Filter{Attribute: "active", Operator: "co", Value: "true"}); where != "" || len(args) != 0 {
		t.Fatalf("buildSCIMUserFilter(unsupported) = %q %#v", where, args)
	}

	if got := buildSCIMUserSort("username", adminscim.SortAscending); !strings.Contains(got, "ASC") {
		t.Fatalf("buildSCIMUserSort(username asc) = %q", got)
	}
	if got := buildSCIMUserSort("displayName", adminscim.SortDescending); !strings.Contains(got, "DESC") {
		t.Fatalf("buildSCIMUserSort(displayName desc) = %q", got)
	}
	if got := buildSCIMUserSort("unknown", adminscim.SortAscending); got != "ci.created_at DESC" {
		t.Fatalf("buildSCIMUserSort(default) = %q, want ci.created_at DESC", got)
	}

	if where, args := buildSCIMGroupFilter(nil); where != "" || len(args) != 0 {
		t.Fatalf("buildSCIMGroupFilter(nil) = %q %#v", where, args)
	}
	if where, args := buildSCIMGroupFilter(&adminscim.Filter{Attribute: "displayName", Operator: "eq", Value: "Team A"}); !strings.Contains(where, "LOWER(display_name)") || len(args) != 1 {
		t.Fatalf("buildSCIMGroupFilter(displayName eq) = %q %#v", where, args)
	}
	if where, args := buildSCIMGroupFilter(&adminscim.Filter{Attribute: "id", Operator: "eq", Value: "g1"}); where != "" || len(args) != 0 {
		t.Fatalf("buildSCIMGroupFilter(unsupported) = %q %#v", where, args)
	}

	if got := buildSCIMGroupSort("displayName", adminscim.SortDescending); got != "LOWER(display_name) DESC" {
		t.Fatalf("buildSCIMGroupSort(displayName desc) = %q", got)
	}
	if got := buildSCIMGroupSort("unknown", adminscim.SortAscending); got != "created_at DESC" {
		t.Fatalf("buildSCIMGroupSort(default) = %q", got)
	}

	traits := map[string]interface{}{"email": " user@example.com ", "flag": true}
	if got := stringFromTraits(traits, "email"); got != "user@example.com" {
		t.Fatalf("stringFromTraits(email) = %q", got)
	}
	if got := stringFromTraits(traits, "missing"); got != "" {
		t.Fatalf("stringFromTraits(missing) = %q, want empty", got)
	}
	if got := stringFromTraits(traits, "flag"); got != "" {
		t.Fatalf("stringFromTraits(non-string) = %q, want empty", got)
	}

	if got := nullableString("  "); got != nil {
		t.Fatalf("nullableString(blank) = %#v, want nil", got)
	}
	if got := nullableString("  abc "); got != "abc" {
		t.Fatalf("nullableString(value) = %#v, want abc", got)
	}
	if got := derefString(nil); got != "" {
		t.Fatalf("derefString(nil) = %q, want empty", got)
	}
	v := " value "
	if got := derefString(&v); got != "value" {
		t.Fatalf("derefString(ptr) = %q, want value", got)
	}
	if got := scimVersionFromTime(time.Time{}); got != "" {
		t.Fatalf("scimVersionFromTime(zero) = %q, want empty", got)
	}
	if got := scimVersionFromTime(time.Unix(1, 2)); got == "" {
		t.Fatalf("scimVersionFromTime(non-zero) expected non-empty")
	}
	if got := firstNonEmptySCIM(" ", "", " x ", "y"); got != "x" {
		t.Fatalf("firstNonEmptySCIM(...) = %q, want x", got)
	}

	userTraits := scimUserTraits(&adminscim.SCIMUser{
		UserName:    "user@example.com",
		DisplayName: "",
		ExternalID:  " ext-1 ",
		Name: &adminscim.Name{
			GivenName:  "First",
			FamilyName: "Last",
			Formatted:  "First Last",
		},
	})
	if userTraits["display_name"] != "First Last" || userTraits["scim_external_id"] != "ext-1" {
		t.Fatalf("scimUserTraits() unexpected traits: %#v", userTraits)
	}
}

func TestSCIMScanHelpers(t *testing.T) {
	now := time.Now().UTC().Round(0)
	id := uuid.New()
	email := "user@example.com"

	t.Run("scanSCIMUser not found", func(t *testing.T) {
		_, err := scanSCIMUser(fakeRow{err: pgx.ErrNoRows})
		if !errors.Is(err, ErrIdentityNotFound) {
			t.Fatalf("scanSCIMUser(not found) error = %v, want %v", err, ErrIdentityNotFound)
		}
	})

	t.Run("scanSCIMUser success", func(t *testing.T) {
		rawTraits := []byte(`{"email":"trait@example.com","display_name":"Trait User","given_name":"Trait","family_name":"User","scim_external_id":"ext-1"}`)
		user, err := scanSCIMUser(fakeRow{vals: []any{id, rawTraits, "active", now, now, email}})
		if err != nil {
			t.Fatalf("scanSCIMUser(success) error = %v", err)
		}
		if user.ID != id.String() || user.UserName != email || user.ExternalID != "ext-1" {
			t.Fatalf("scanSCIMUser(success) unexpected user: %#v", user)
		}
		if user.Name == nil || user.Name.GivenName != "Trait" || user.Meta.Location != "/Users/"+id.String() {
			t.Fatalf("scanSCIMUser(success) unexpected name/meta: %#v", user)
		}
	})

	t.Run("scanSCIMGroup not found", func(t *testing.T) {
		_, err := scanSCIMGroupRow(fakeRow{err: pgx.ErrNoRows})
		if err == nil || err.Error() != "not found" {
			t.Fatalf("scanSCIMGroupRow(not found) = %v, want not found", err)
		}
	})

	t.Run("scanSCIMGroup success", func(t *testing.T) {
		group, err := scanSCIMGroupRow(fakeRow{vals: []any{
			"group-1",
			"ext-g1",
			"Group One",
			[]byte(`[{"value":"id-1","display":"User One"}]`),
			now,
			now,
		}})
		if err != nil {
			t.Fatalf("scanSCIMGroupRow(success) error = %v", err)
		}
		if group.ID != "group-1" || group.ExternalID != "ext-g1" || group.Meta.Location != "/Groups/group-1" {
			t.Fatalf("scanSCIMGroupRow(success) unexpected group: %#v", group)
		}
		if len(group.Members) != 1 || group.Members[0].Display != "User One" {
			t.Fatalf("scanSCIMGroupRow(success) unexpected members: %#v", group.Members)
		}
	})

	t.Run("scanSCIMMapping success defaults nil maps", func(t *testing.T) {
		m, err := scanSCIMMapping(fakeRow{vals: []any{
			uuid.New(),
			"default",
			"description",
			"email",
			"",
			"primary",
			[]byte(`null`),
			[]byte(`null`),
			[]byte(`null`),
			now,
			now,
		}})
		if err != nil {
			t.Fatalf("scanSCIMMapping(success) error = %v", err)
		}
		if m.NameMapping == nil || m.AttributeMapping == nil || m.GroupMapping == nil {
			t.Fatalf("scanSCIMMapping(success) expected non-nil maps: %#v", m)
		}
	})

	t.Run("scanSCIMToken success defaults permissions", func(t *testing.T) {
		token, err := scanSCIMToken(fakeRow{vals: []any{
			uuid.New(),
			"token",
			"description",
			"hash",
			"pre_123",
			[]byte(`null`),
			uuid.New(),
			now,
			(*time.Time)(nil),
			(*time.Time)(nil),
			true,
		}})
		if err != nil {
			t.Fatalf("scanSCIMToken(success) error = %v", err)
		}
		if token.Permissions == nil || len(token.Permissions) != 0 {
			t.Fatalf("scanSCIMToken(success) expected empty non-nil permissions, got %#v", token.Permissions)
		}
	})
}

func TestSCIMStoreCoveragePaths(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	schemaID := uuid.New()
	now := time.Now().UTC().Round(0)

	userTraits := []byte(`{"email":"user@example.com","display_name":"User Name","scim_external_id":"ext-1","given_name":"Given","family_name":"Family"}`)
	groupMembers := []byte(`[{"value":"` + userID.String() + `","display":"User Name"}]`)
	nameMapping := []byte(`{"givenName":"given_name"}`)
	attrMapping := []byte(`{"department":"traits.department"}`)
	groupMapping := []byte(`{"engineering":"role-engineering"}`)
	perms := []byte(`["users:read","users:write"]`)
	expires := now.Add(time.Hour)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return fakeRow{vals: []any{schemaID}}
			case strings.Contains(sql, "SELECT COUNT(*)") && strings.Contains(sql, "FROM core_identities ci"):
				return fakeRow{vals: []any{1}}
			case strings.Contains(sql, "SELECT COUNT(*)") && strings.Contains(sql, "FROM adm_scim_groups"):
				return fakeRow{vals: []any{1}}
			case strings.Contains(sql, "FROM core_identities ci"):
				email := "user@example.com"
				return fakeRow{vals: []any{userID, userTraits, "active", now, now, email}}
			case strings.Contains(sql, "FROM adm_scim_groups"):
				external := "ext-group"
				return fakeRow{vals: []any{"group-1", &external, "Group One", groupMembers, now, now}}
			case strings.Contains(sql, "FROM adm_scim_mappings"):
				return fakeRow{vals: []any{
					uuid.New(),
					"default",
					"description",
					"email",
					"",
					"primary",
					nameMapping,
					attrMapping,
					groupMapping,
					now,
					now,
				}}
			case strings.Contains(sql, "FROM adm_scim_tokens"):
				return fakeRow{vals: []any{
					uuid.New(),
					"default token",
					"description",
					"hash",
					"tok_123",
					perms,
					uuid.New(),
					now,
					&expires,
					(*time.Time)(nil),
					true,
				}}
			default:
				return fakeRow{err: pgx.ErrNoRows}
			}
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			switch {
			case strings.Contains(sql, "FROM core_identities ci"):
				email := "user@example.com"
				return &fakeRows{data: [][]any{{userID, userTraits, "active", now, now, email}}}, nil
			case strings.Contains(sql, "FROM adm_scim_groups"):
				return &fakeRows{data: [][]any{{"group-1", "ext-group", "Group One", groupMembers, now, now}}}, nil
			case strings.Contains(sql, "FROM adm_scim_mappings"):
				return &fakeRows{data: [][]any{{uuid.New(), "default", "desc", "email", "", "primary", nameMapping, attrMapping, groupMapping, now, now}}}, nil
			case strings.Contains(sql, "FROM adm_scim_tokens"):
				return &fakeRows{data: [][]any{{uuid.New(), "token", "desc", "hash", "tok_123", perms, uuid.New(), now, &expires, (*time.Time)(nil), true}}}, nil
			default:
				return &fakeRows{}, nil
			}
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	s := &Store{db: db}

	user := &adminscim.SCIMUser{
		ID:          userID.String(),
		UserName:    "user@example.com",
		DisplayName: "User Name",
		ExternalID:  "ext-1",
		Active:      true,
		Emails:      []adminscim.Email{{Value: "user@example.com", Primary: true}},
		Name:        &adminscim.Name{GivenName: "Given", FamilyName: "Family", Formatted: "User Name"},
	}

	if _, err := s.GetUserByID(ctx, userID.String()); err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if _, err := s.GetUserByUserName(ctx, "user@example.com"); err != nil {
		t.Fatalf("GetUserByUserName() error = %v", err)
	}
	if _, err := s.GetUserByExternalID(ctx, "ext-1"); err != nil {
		t.Fatalf("GetUserByExternalID() error = %v", err)
	}
	if users, total, err := s.ListUsers(ctx, &adminscim.Filter{Attribute: "active", Operator: "eq", Value: true}, "userName", adminscim.SortAscending, 1, 10); err != nil || total != 1 || len(users) != 1 {
		t.Fatalf("ListUsers() = len:%d total:%d err:%v", len(users), total, err)
	}
	if err := s.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if err := s.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	if patched, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{{Op: "replace", Path: "displayName", Value: "Updated User"}}); err != nil || patched == nil {
		t.Fatalf("PatchUser() error = %v", err)
	}
	if err := s.DeleteUser(ctx, userID.String()); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}

	if _, err := s.GetGroupByID(ctx, "group-1"); err != nil {
		t.Fatalf("GetGroupByID() error = %v", err)
	}
	if _, err := s.GetGroupByDisplayName(ctx, "Group One"); err != nil {
		t.Fatalf("GetGroupByDisplayName() error = %v", err)
	}
	if groups, total, err := s.ListGroups(ctx, &adminscim.Filter{Attribute: "displayName", Operator: "eq", Value: "Group One"}, "displayName", adminscim.SortDescending, 1, 10); err != nil || total != 1 || len(groups) != 1 {
		t.Fatalf("ListGroups() = len:%d total:%d err:%v", len(groups), total, err)
	}
	group := &adminscim.SCIMGroup{
		ID:          "group-1",
		ExternalID:  "ext-group",
		DisplayName: "Group One",
		Members:     []adminscim.Member{{Value: userID.String(), Display: "User Name"}},
	}
	if err := s.CreateGroup(ctx, group); err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if err := s.UpdateGroup(ctx, group); err != nil {
		t.Fatalf("UpdateGroup() error = %v", err)
	}
	if patched, err := s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "remove", Path: "members"}}); err != nil || patched == nil {
		t.Fatalf("PatchGroup() error = %v", err)
	}
	if err := s.DeleteGroup(ctx, "group-1"); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}

	mapping := &adminscim.SCIMMapping{
		ID:               uuid.New(),
		Name:             "default",
		Description:      "desc",
		UserNameSource:   "email",
		UserNameCustom:   "",
		EmailSource:      "primary",
		NameMapping:      map[string]string{"givenName": "given_name"},
		AttributeMapping: map[string]string{"department": "traits.department"},
		GroupMapping:     map[string]string{"engineering": "role-engineering"},
	}
	if err := s.CreateSCIMMapping(ctx, mapping); err != nil {
		t.Fatalf("CreateSCIMMapping() error = %v", err)
	}
	if err := s.UpdateSCIMMapping(ctx, mapping); err != nil {
		t.Fatalf("UpdateSCIMMapping() error = %v", err)
	}
	if _, err := s.GetSCIMMapping(ctx, mapping.ID); err != nil {
		t.Fatalf("GetSCIMMapping() error = %v", err)
	}
	if items, err := s.ListSCIMMappings(ctx); err != nil || len(items) != 1 {
		t.Fatalf("ListSCIMMappings() len:%d err:%v", len(items), err)
	}
	if err := s.DeleteSCIMMapping(ctx, mapping.ID); err != nil {
		t.Fatalf("DeleteSCIMMapping() error = %v", err)
	}

	token := &adminscim.SCIMToken{
		ID:          uuid.New(),
		Name:        "token",
		Description: "desc",
		TokenHash:   "hash",
		Prefix:      "tok_123",
		Permissions: []string{"users:read"},
		CreatedBy:   uuid.New(),
		CreatedAt:   now,
		ExpiresAt:   &expires,
		Active:      true,
	}
	if err := s.CreateSCIMToken(ctx, token); err != nil {
		t.Fatalf("CreateSCIMToken() error = %v", err)
	}
	if _, err := s.GetSCIMTokenByPrefix(ctx, token.Prefix); err != nil {
		t.Fatalf("GetSCIMTokenByPrefix() error = %v", err)
	}
	if tokens, err := s.ListSCIMTokens(ctx); err != nil || len(tokens) != 1 {
		t.Fatalf("ListSCIMTokens() len:%d err:%v", len(tokens), err)
	}
	if err := s.UpdateSCIMTokenLastUsed(ctx, token.ID); err != nil {
		t.Fatalf("UpdateSCIMTokenLastUsed() error = %v", err)
	}
	if err := s.DeleteSCIMToken(ctx, token.ID); err != nil {
		t.Fatalf("DeleteSCIMToken() error = %v", err)
	}
}

func TestSCIMStoreErrorPaths(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	now := time.Now().UTC()
	db := &fakeDB{}
	s := &Store{db: db}

	if _, err := s.GetUserByID(ctx, "not-a-uuid"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("GetUserByID(invalid) error = %v, want %v", err, ErrIdentityNotFound)
	}
	if err := s.UpdateUser(ctx, &adminscim.SCIMUser{ID: "bad"}); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("UpdateUser(invalid id) error = %v, want %v", err, ErrIdentityNotFound)
	}
	if err := s.DeleteUser(ctx, "bad"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("DeleteUser(invalid id) error = %v, want %v", err, ErrIdentityNotFound)
	}

	db.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		if strings.Contains(sql, "FROM core_identities ci") {
			email := "user@example.com"
			return fakeRow{vals: []any{userID, []byte(`{"email":"user@example.com"}`), "active", now, now, email}}
		}
		if strings.Contains(sql, "FROM adm_scim_groups") {
			return fakeRow{vals: []any{"group-1", "ext-g1", "Group One", []byte(`[]`), now, now}}
		}
		return fakeRow{err: pgx.ErrNoRows}
	}

	_, err := s.PatchUser(ctx, userID.String(), []adminscim.PatchOperation{{Op: "unknown", Path: "displayName", Value: "x"}})
	if err == nil || err.Error() != "invalid operation" {
		t.Fatalf("PatchUser(invalid op) error = %v, want invalid operation", err)
	}

	_, err = s.PatchGroup(ctx, "group-1", []adminscim.PatchOperation{{Op: "remove", Path: "displayName"}})
	if err == nil || err.Error() != "invalid operation" {
		t.Fatalf("PatchGroup(invalid remove path) error = %v, want invalid operation", err)
	}

	db.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		if strings.Contains(sql, "SELECT COUNT(*)") {
			return fakeRow{err: errors.New("count failed")}
		}
		return fakeRow{vals: []any{1}}
	}
	if _, _, err := s.ListUsers(ctx, nil, "", adminscim.SortAscending, 1, 10); err == nil || err.Error() != "count failed" {
		t.Fatalf("ListUsers(count error) = %v, want count failed", err)
	}
	if _, _, err := s.ListGroups(ctx, nil, "", adminscim.SortAscending, 1, 10); err == nil || err.Error() != "count failed" {
		t.Fatalf("ListGroups(count error) = %v, want count failed", err)
	}

	db.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		if strings.Contains(sql, "FROM core_identity_schemas") {
			return fakeRow{err: pgx.ErrNoRows}
		}
		email := "user@example.com"
		return fakeRow{vals: []any{userID, []byte(`{}`), "active", now, now, email}}
	}
	if err := s.CreateUser(ctx, &adminscim.SCIMUser{ID: userID.String(), UserName: "user@example.com"}); err == nil || !strings.Contains(err.Error(), "identity schema not found") {
		t.Fatalf("CreateUser(no schema) error = %v", err)
	}

	db.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return fakeRow{vals: []any{uuid.New()}}
	}
	db.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		switch {
		case strings.Contains(sql, "UPDATE core_identities"):
			return pgconn.NewCommandTag("UPDATE 0"), nil
		case strings.Contains(sql, "DELETE FROM core_identity_addresses"):
			return pgconn.NewCommandTag("DELETE 1"), errors.New("delete emails failed")
		default:
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
	}
	if err := s.UpdateUser(ctx, &adminscim.SCIMUser{ID: userID.String(), UserName: "user@example.com"}); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("UpdateUser(not found) error = %v, want %v", err, ErrIdentityNotFound)
	}

	db.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "UPDATE adm_scim_groups") {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		if strings.Contains(sql, "DELETE FROM adm_scim_groups") {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := s.UpdateGroup(ctx, &adminscim.SCIMGroup{ID: "group-1", DisplayName: "Group"}); err == nil || err.Error() != "not found" {
		t.Fatalf("UpdateGroup(not found) error = %v, want not found", err)
	}
	if err := s.DeleteGroup(ctx, "group-1"); err == nil || err.Error() != "not found" {
		t.Fatalf("DeleteGroup(not found) error = %v, want not found", err)
	}

	db.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "DELETE FROM core_identity_addresses") {
			return pgconn.NewCommandTag("DELETE 1"), errors.New("delete emails failed")
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := s.replaceSCIMUserEmails(ctx, userID, []adminscim.Email{{Value: "user@example.com"}}); err == nil || err.Error() != "delete emails failed" {
		t.Fatalf("replaceSCIMUserEmails(delete error) = %v, want delete emails failed", err)
	}

	db.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "INSERT INTO core_identity_addresses") {
			return pgconn.NewCommandTag("INSERT 0"), errors.New("insert email failed")
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := s.replaceSCIMUserEmails(ctx, userID, []adminscim.Email{{Value: "user@example.com"}}); err == nil || err.Error() != "insert email failed" {
		t.Fatalf("replaceSCIMUserEmails(insert error) = %v, want insert email failed", err)
	}

	db.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := s.replaceSCIMUserEmails(ctx, userID, []adminscim.Email{{Value: " "}}); err != nil {
		t.Fatalf("replaceSCIMUserEmails(blank email) error = %v", err)
	}
}
