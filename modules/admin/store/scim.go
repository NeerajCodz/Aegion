package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	adminscim "github.com/aegion/aegion/modules/admin/scim"
)

func (s *Store) GetUserByID(ctx context.Context, id string) (*adminscim.SCIMUser, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return nil, ErrIdentityNotFound
	}
	return s.scanSCIMUserRow(ctx, `
		SELECT ci.id, ci.traits, ci.state, ci.created_at, ci.updated_at, cia.value
		FROM core_identities ci
		LEFT JOIN LATERAL (
			SELECT value
			FROM core_identity_addresses
			WHERE identity_id = ci.id AND type = 'email'
			ORDER BY is_primary DESC, created_at ASC
			LIMIT 1
		) cia ON TRUE
		WHERE ci.id = $1 AND ci.deleted_at IS NULL
	`, parsed)
}

func (s *Store) GetUserByUserName(ctx context.Context, userName string) (*adminscim.SCIMUser, error) {
	return s.scanSCIMUserRow(ctx, `
		SELECT ci.id, ci.traits, ci.state, ci.created_at, ci.updated_at, cia.value
		FROM core_identities ci
		LEFT JOIN LATERAL (
			SELECT value
			FROM core_identity_addresses
			WHERE identity_id = ci.id AND type = 'email'
			ORDER BY is_primary DESC, created_at ASC
			LIMIT 1
		) cia ON TRUE
		WHERE ci.deleted_at IS NULL
		  AND LOWER(COALESCE(cia.value, ci.traits->>'email', '')) = LOWER($1)
	`, strings.TrimSpace(userName))
}

func (s *Store) GetUserByExternalID(ctx context.Context, externalID string) (*adminscim.SCIMUser, error) {
	return s.scanSCIMUserRow(ctx, `
		SELECT ci.id, ci.traits, ci.state, ci.created_at, ci.updated_at, cia.value
		FROM core_identities ci
		LEFT JOIN LATERAL (
			SELECT value
			FROM core_identity_addresses
			WHERE identity_id = ci.id AND type = 'email'
			ORDER BY is_primary DESC, created_at ASC
			LIMIT 1
		) cia ON TRUE
		WHERE ci.deleted_at IS NULL
		  AND COALESCE(ci.traits->>'scim_external_id', '') = $1
	`, strings.TrimSpace(externalID))
}

func (s *Store) ListUsers(ctx context.Context, filter *adminscim.Filter, sortBy string, sortOrder adminscim.SortOrder, startIndex, count int) ([]*adminscim.SCIMUser, int, error) {
	where, args := buildSCIMUserFilter(filter)
	orderBy := buildSCIMUserSort(sortBy, sortOrder)
	offset := 0
	if startIndex > 1 {
		offset = startIndex - 1
	}

	var total int
	countQuery := `
		SELECT COUNT(*)
		FROM core_identities ci
		LEFT JOIN LATERAL (
			SELECT value
			FROM core_identity_addresses
			WHERE identity_id = ci.id AND type = 'email'
			ORDER BY is_primary DESC, created_at ASC
			LIMIT 1
		) cia ON TRUE
		WHERE ci.deleted_at IS NULL ` + where
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT ci.id, ci.traits, ci.state, ci.created_at, ci.updated_at, cia.value
		FROM core_identities ci
		LEFT JOIN LATERAL (
			SELECT value
			FROM core_identity_addresses
			WHERE identity_id = ci.id AND type = 'email'
			ORDER BY is_primary DESC, created_at ASC
			LIMIT 1
		) cia ON TRUE
		WHERE ci.deleted_at IS NULL ` + where + `
		ORDER BY ` + orderBy + `
		LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, count, offset)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*adminscim.SCIMUser
	for rows.Next() {
		user, err := scanSCIMUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, user *adminscim.SCIMUser) error {
	schemaID, err := s.defaultIdentitySchemaID(ctx)
	if err != nil {
		return err
	}
	rawTraits, err := json.Marshal(scimUserTraits(user))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO core_identities (id, schema_id, traits, state, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, NOW(), NOW())
	`, uuid.MustParse(user.ID), schemaID, string(rawTraits), scimIdentityState(user.Active))
	if err != nil {
		return err
	}
	return s.replaceSCIMUserEmails(ctx, uuid.MustParse(user.ID), user.Emails)
}

func (s *Store) UpdateUser(ctx context.Context, user *adminscim.SCIMUser) error {
	identityID, err := uuid.Parse(strings.TrimSpace(user.ID))
	if err != nil {
		return ErrIdentityNotFound
	}
	rawTraits, err := json.Marshal(scimUserTraits(user))
	if err != nil {
		return err
	}
	result, err := s.db.Exec(ctx, `
		UPDATE core_identities
		SET traits = $2::jsonb, state = $3, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, identityID, string(rawTraits), scimIdentityState(user.Active))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrIdentityNotFound
	}
	return s.replaceSCIMUserEmails(ctx, identityID, user.Emails)
}

func (s *Store) PatchUser(ctx context.Context, id string, operations []adminscim.PatchOperation) (*adminscim.SCIMUser, error) {
	user, err := s.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, op := range operations {
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "replace", "add":
			switch strings.ToLower(strings.TrimSpace(op.Path)) {
			case "active":
				if active, ok := op.Value.(bool); ok {
					user.Active = active
				}
			case "displayname":
				if value, ok := op.Value.(string); ok {
					user.DisplayName = strings.TrimSpace(value)
				}
			case "username":
				if value, ok := op.Value.(string); ok {
					user.UserName = strings.TrimSpace(value)
				}
			case "emails":
				raw, err := json.Marshal(op.Value)
				if err != nil {
					return nil, errors.New("invalid operation")
				}
				var emails []adminscim.Email
				if err := json.Unmarshal(raw, &emails); err != nil {
					return nil, errors.New("invalid operation")
				}
				user.Emails = emails
			}
		case "remove":
			switch strings.ToLower(strings.TrimSpace(op.Path)) {
			case "displayname":
				user.DisplayName = ""
			case "emails":
				user.Emails = nil
			default:
				return nil, errors.New("invalid operation")
			}
		default:
			return nil, errors.New("invalid operation")
		}
	}
	if err := s.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	return s.GetUserByID(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return ErrIdentityNotFound
	}
	result, err := s.db.Exec(ctx, `
		UPDATE core_identities
		SET deleted_at = NOW(), state = 'inactive', updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, parsed)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

func (s *Store) GetGroupByID(ctx context.Context, id string) (*adminscim.SCIMGroup, error) {
	return s.scanSCIMGroup(ctx, `SELECT id, external_id, display_name, members, created_at, updated_at FROM adm_scim_groups WHERE id = $1`, strings.TrimSpace(id))
}

func (s *Store) GetGroupByDisplayName(ctx context.Context, displayName string) (*adminscim.SCIMGroup, error) {
	return s.scanSCIMGroup(ctx, `SELECT id, external_id, display_name, members, created_at, updated_at FROM adm_scim_groups WHERE display_name = $1`, strings.TrimSpace(displayName))
}

func (s *Store) ListGroups(ctx context.Context, filter *adminscim.Filter, sortBy string, sortOrder adminscim.SortOrder, startIndex, count int) ([]*adminscim.SCIMGroup, int, error) {
	where, args := buildSCIMGroupFilter(filter)
	orderBy := buildSCIMGroupSort(sortBy, sortOrder)
	offset := 0
	if startIndex > 1 {
		offset = startIndex - 1
	}

	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM adm_scim_groups WHERE 1=1 `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT id, external_id, display_name, members, created_at, updated_at FROM adm_scim_groups WHERE 1=1 ` + where + `
		ORDER BY ` + orderBy + ` LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, count, offset)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var groups []*adminscim.SCIMGroup
	for rows.Next() {
		group, err := scanSCIMGroupRow(rows)
		if err != nil {
			return nil, 0, err
		}
		groups = append(groups, group)
	}
	return groups, total, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, group *adminscim.SCIMGroup) error {
	members, err := json.Marshal(group.Members)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_scim_groups (id, external_id, display_name, members, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, NOW(), NOW())
	`, group.ID, nullableString(group.ExternalID), group.DisplayName, string(members))
	return err
}

func (s *Store) UpdateGroup(ctx context.Context, group *adminscim.SCIMGroup) error {
	members, err := json.Marshal(group.Members)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(ctx, `
		UPDATE adm_scim_groups
		SET external_id = $2, display_name = $3, members = $4::jsonb, updated_at = NOW()
		WHERE id = $1
	`, group.ID, nullableString(group.ExternalID), group.DisplayName, string(members))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("not found")
	}
	return nil
}

func (s *Store) PatchGroup(ctx context.Context, id string, operations []adminscim.PatchOperation) (*adminscim.SCIMGroup, error) {
	group, err := s.GetGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, op := range operations {
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "replace", "add":
			switch strings.ToLower(strings.TrimSpace(op.Path)) {
			case "displayname":
				if value, ok := op.Value.(string); ok {
					group.DisplayName = strings.TrimSpace(value)
				}
			case "members":
				raw, err := json.Marshal(op.Value)
				if err != nil {
					return nil, errors.New("invalid operation")
				}
				var members []adminscim.Member
				if err := json.Unmarshal(raw, &members); err != nil {
					return nil, errors.New("invalid operation")
				}
				group.Members = members
			}
		case "remove":
			if strings.EqualFold(strings.TrimSpace(op.Path), "members") {
				group.Members = nil
			} else {
				return nil, errors.New("invalid operation")
			}
		default:
			return nil, errors.New("invalid operation")
		}
	}
	if err := s.UpdateGroup(ctx, group); err != nil {
		return nil, err
	}
	return s.GetGroupByID(ctx, id)
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, `DELETE FROM adm_scim_groups WHERE id = $1`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("not found")
	}
	return nil
}

func (s *Store) GetSCIMMapping(ctx context.Context, id uuid.UUID) (*adminscim.SCIMMapping, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, name, description, username_source, username_custom, email_source, name_mapping, attribute_mapping, group_mapping, created_at, updated_at
		FROM adm_scim_mappings
		WHERE id = $1
	`, id)
	return scanSCIMMapping(row)
}

func (s *Store) ListSCIMMappings(ctx context.Context) ([]*adminscim.SCIMMapping, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, description, username_source, username_custom, email_source, name_mapping, attribute_mapping, group_mapping, created_at, updated_at
		FROM adm_scim_mappings
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []*adminscim.SCIMMapping
	for rows.Next() {
		item, err := scanSCIMMapping(rows)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, item)
	}
	return mappings, rows.Err()
}

func (s *Store) CreateSCIMMapping(ctx context.Context, mapping *adminscim.SCIMMapping) error {
	return s.upsertSCIMMapping(ctx, mapping, false)
}

func (s *Store) UpdateSCIMMapping(ctx context.Context, mapping *adminscim.SCIMMapping) error {
	return s.upsertSCIMMapping(ctx, mapping, true)
}

func (s *Store) DeleteSCIMMapping(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM adm_scim_mappings WHERE id = $1`, id)
	return err
}

func (s *Store) GetSCIMTokenByPrefix(ctx context.Context, prefix string) (*adminscim.SCIMToken, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, name, description, token_hash, prefix, permissions, created_by, created_at, expires_at, last_used_at, active
		FROM adm_scim_tokens
		WHERE prefix = $1
	`, strings.TrimSpace(prefix))
	return scanSCIMToken(row)
}

func (s *Store) ListSCIMTokens(ctx context.Context) ([]*adminscim.SCIMToken, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, description, token_hash, prefix, permissions, created_by, created_at, expires_at, last_used_at, active
		FROM adm_scim_tokens
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*adminscim.SCIMToken
	for rows.Next() {
		item, err := scanSCIMToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, item)
	}
	return tokens, rows.Err()
}

func (s *Store) CreateSCIMToken(ctx context.Context, token *adminscim.SCIMToken) error {
	permsRaw, err := json.Marshal(token.Permissions)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_scim_tokens (id, name, description, token_hash, prefix, permissions, created_by, created_at, expires_at, last_used_at, active)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11)
	`, token.ID, token.Name, token.Description, token.TokenHash, token.Prefix, string(permsRaw), token.CreatedBy, token.CreatedAt, token.ExpiresAt, token.LastUsedAt, token.Active)
	return err
}

func (s *Store) UpdateSCIMTokenLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE adm_scim_tokens SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteSCIMToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM adm_scim_tokens WHERE id = $1`, id)
	return err
}

func (s *Store) defaultIdentitySchemaID(ctx context.Context) (uuid.UUID, error) {
	var schemaID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id
		FROM core_identity_schemas
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1
	`).Scan(&schemaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("identity schema not found")
		}
		return uuid.Nil, err
	}
	return schemaID, nil
}

func (s *Store) replaceSCIMUserEmails(ctx context.Context, identityID uuid.UUID, emails []adminscim.Email) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM core_identity_addresses WHERE identity_id = $1 AND type = 'email'`, identityID); err != nil {
		return err
	}
	for idx, email := range emails {
		value := strings.TrimSpace(email.Value)
		if value == "" {
			continue
		}
		_, err := s.db.Exec(ctx, `
			INSERT INTO core_identity_addresses (id, identity_id, type, value, is_primary, verified, created_at, updated_at)
			VALUES ($1, $2, 'email', $3, $4, FALSE, NOW(), NOW())
		`, uuid.New(), identityID, value, idx == 0 || email.Primary)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) scanSCIMUserRow(ctx context.Context, query string, args ...any) (*adminscim.SCIMUser, error) {
	return scanSCIMUser(s.db.QueryRow(ctx, query, args...))
}

func scanSCIMUser(row interface{ Scan(dest ...any) error }) (*adminscim.SCIMUser, error) {
	var (
		id        uuid.UUID
		traitsRaw []byte
		state     string
		createdAt time.Time
		updatedAt time.Time
		email     *string
	)
	if err := row.Scan(&id, &traitsRaw, &state, &createdAt, &updatedAt, &email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrIdentityNotFound
		}
		return nil, err
	}

	traits := map[string]interface{}{}
	_ = json.Unmarshal(traitsRaw, &traits)
	user := &adminscim.SCIMUser{
		Schemas:     []string{adminscim.SchemaUser},
		ID:          id.String(),
		ExternalID:  stringFromTraits(traits, "scim_external_id"),
		UserName:    firstNonEmptySCIM(derefString(email), stringFromTraits(traits, "email")),
		DisplayName: firstNonEmptySCIM(stringFromTraits(traits, "display_name"), stringFromTraits(traits, "name")),
		Active:      !strings.EqualFold(strings.TrimSpace(state), "inactive"),
		Meta: adminscim.Meta{
			ResourceType: "User",
			Created:      &createdAt,
			LastModified: &updatedAt,
			Location:     "/Users/" + id.String(),
			Version:      "1",
		},
	}
	if user.UserName != "" {
		user.Emails = []adminscim.Email{{Value: user.UserName, Primary: true, Type: "work"}}
	}
	if given := stringFromTraits(traits, "given_name"); given != "" || stringFromTraits(traits, "family_name") != "" {
		user.Name = &adminscim.Name{
			GivenName:  given,
			FamilyName: stringFromTraits(traits, "family_name"),
			Formatted:  user.DisplayName,
		}
	}
	return user, nil
}

func (s *Store) scanSCIMGroup(ctx context.Context, query string, args ...any) (*adminscim.SCIMGroup, error) {
	return scanSCIMGroupRow(s.db.QueryRow(ctx, query, args...))
}

func scanSCIMGroupRow(row interface{ Scan(dest ...any) error }) (*adminscim.SCIMGroup, error) {
	var (
		id          string
		externalID  *string
		displayName string
		membersRaw  []byte
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&id, &externalID, &displayName, &membersRaw, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("not found")
		}
		return nil, err
	}
	group := &adminscim.SCIMGroup{
		Schemas:     []string{adminscim.SchemaGroup},
		ID:          id,
		ExternalID:  derefString(externalID),
		DisplayName: displayName,
		Meta: adminscim.Meta{
			ResourceType: "Group",
			Created:      &createdAt,
			LastModified: &updatedAt,
			Location:     "/Groups/" + id,
			Version:      "1",
		},
	}
	_ = json.Unmarshal(membersRaw, &group.Members)
	return group, nil
}

func scanSCIMMapping(row interface{ Scan(dest ...any) error }) (*adminscim.SCIMMapping, error) {
	var (
		item        adminscim.SCIMMapping
		nameMapRaw  []byte
		attrMapRaw  []byte
		groupMapRaw []byte
	)
	if err := row.Scan(&item.ID, &item.Name, &item.Description, &item.UserNameSource, &item.UserNameCustom, &item.EmailSource, &nameMapRaw, &attrMapRaw, &groupMapRaw, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(nameMapRaw, &item.NameMapping)
	_ = json.Unmarshal(attrMapRaw, &item.AttributeMapping)
	_ = json.Unmarshal(groupMapRaw, &item.GroupMapping)
	if item.NameMapping == nil {
		item.NameMapping = map[string]string{}
	}
	if item.AttributeMapping == nil {
		item.AttributeMapping = map[string]string{}
	}
	if item.GroupMapping == nil {
		item.GroupMapping = map[string]string{}
	}
	return &item, nil
}

func (s *Store) upsertSCIMMapping(ctx context.Context, mapping *adminscim.SCIMMapping, update bool) error {
	nameRaw, err := json.Marshal(mapping.NameMapping)
	if err != nil {
		return err
	}
	attrRaw, err := json.Marshal(mapping.AttributeMapping)
	if err != nil {
		return err
	}
	groupRaw, err := json.Marshal(mapping.GroupMapping)
	if err != nil {
		return err
	}
	if update {
		_, err = s.db.Exec(ctx, `
			UPDATE adm_scim_mappings
			SET name = $2, description = $3, username_source = $4, username_custom = $5, email_source = $6,
			    name_mapping = $7::jsonb, attribute_mapping = $8::jsonb, group_mapping = $9::jsonb, updated_at = NOW()
			WHERE id = $1
		`, mapping.ID, mapping.Name, mapping.Description, mapping.UserNameSource, mapping.UserNameCustom, mapping.EmailSource, string(nameRaw), string(attrRaw), string(groupRaw))
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO adm_scim_mappings (id, name, description, username_source, username_custom, email_source, name_mapping, attribute_mapping, group_mapping, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, NOW(), NOW())
	`, mapping.ID, mapping.Name, mapping.Description, mapping.UserNameSource, mapping.UserNameCustom, mapping.EmailSource, string(nameRaw), string(attrRaw), string(groupRaw))
	return err
}

func scanSCIMToken(row interface{ Scan(dest ...any) error }) (*adminscim.SCIMToken, error) {
	var (
		item     adminscim.SCIMToken
		permsRaw []byte
	)
	if err := row.Scan(&item.ID, &item.Name, &item.Description, &item.TokenHash, &item.Prefix, &permsRaw, &item.CreatedBy, &item.CreatedAt, &item.ExpiresAt, &item.LastUsedAt, &item.Active); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(permsRaw, &item.Permissions)
	if item.Permissions == nil {
		item.Permissions = []string{}
	}
	return &item, nil
}

func scimUserTraits(user *adminscim.SCIMUser) map[string]interface{} {
	traits := map[string]interface{}{
		"email":            user.UserName,
		"display_name":     user.DisplayName,
		"scim_external_id": strings.TrimSpace(user.ExternalID),
	}
	if user.Name != nil {
		traits["given_name"] = user.Name.GivenName
		traits["family_name"] = user.Name.FamilyName
		if strings.TrimSpace(user.DisplayName) == "" {
			traits["display_name"] = strings.TrimSpace(user.Name.Formatted)
		}
	}
	return traits
}

func scimIdentityState(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}

func buildSCIMUserFilter(filter *adminscim.Filter) (string, []any) {
	if filter == nil {
		return "", nil
	}
	attr := strings.ToLower(strings.TrimSpace(filter.Attribute))
	op := strings.ToLower(strings.TrimSpace(filter.Operator))
	value := fmt.Sprintf("%v", filter.Value)
	switch attr {
	case "username":
		if op == "eq" {
			return " AND LOWER(COALESCE(cia.value, ci.traits->>'email', '')) = LOWER($1)", []any{value}
		}
	case "externalid":
		if op == "eq" {
			return " AND COALESCE(ci.traits->>'scim_external_id', '') = $1", []any{value}
		}
	case "active":
		if op == "eq" && strings.EqualFold(value, "true") {
			return " AND ci.state <> 'inactive'", nil
		}
		if op == "eq" {
			return " AND ci.state = 'inactive'", nil
		}
	}
	return "", nil
}

func buildSCIMUserSort(sortBy string, sortOrder adminscim.SortOrder) string {
	direction := "ASC"
	if sortOrder == adminscim.SortDescending {
		direction = "DESC"
	}
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "username":
		return "LOWER(COALESCE(cia.value, ci.traits->>'email', '')) " + direction
	case "displayname":
		return "LOWER(COALESCE(ci.traits->>'display_name', ci.traits->>'name', '')) " + direction
	default:
		return "ci.created_at DESC"
	}
}

func buildSCIMGroupFilter(filter *adminscim.Filter) (string, []any) {
	if filter == nil {
		return "", nil
	}
	if strings.EqualFold(strings.TrimSpace(filter.Attribute), "displayName") && strings.EqualFold(strings.TrimSpace(filter.Operator), "eq") {
		return " AND LOWER(display_name) = LOWER($1)", []any{fmt.Sprintf("%v", filter.Value)}
	}
	return "", nil
}

func buildSCIMGroupSort(sortBy string, sortOrder adminscim.SortOrder) string {
	direction := "ASC"
	if sortOrder == adminscim.SortDescending {
		direction = "DESC"
	}
	if strings.EqualFold(strings.TrimSpace(sortBy), "displayName") {
		return "LOWER(display_name) " + direction
	}
	return "created_at DESC"
}

func stringFromTraits(traits map[string]interface{}, key string) string {
	raw, ok := traits[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmptySCIM(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
