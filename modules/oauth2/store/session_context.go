package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionAuthContext captures authentication context from the core session store.
type SessionAuthContext struct {
	AAL             string    `json:"aal"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	Methods         []string  `json:"methods"`
}

// GetSessionAuthContext returns session authentication details used for OIDC claim derivation.
func (s *Store) GetSessionAuthContext(ctx context.Context, sessionID string) (*SessionAuthContext, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrNotFound
	}

	authCtx := &SessionAuthContext{}
	if err := s.db.QueryRow(ctx, `
		SELECT aal, authenticated_at
		FROM core_sessions
		WHERE id = $1
	`, sessionID).Scan(&authCtx.AAL, &authCtx.AuthenticatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rows, err := s.db.Query(ctx, `
		SELECT method
		FROM core_session_auth_methods
		WHERE session_id = $1
		ORDER BY completed_at
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	methods := make([]string, 0, 2)
	for rows.Next() {
		var method string
		if err := rows.Scan(&method); err != nil {
			return nil, err
		}
		methods = append(methods, method)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	authCtx.Methods = methods
	return authCtx, nil
}
