package store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	oauth2store "github.com/aegion/aegion/modules/oauth2/store"
)

// Store exposes OAuth2-backed persistence for the standalone introspection module.
type Store struct {
	oauth2 *oauth2store.Store
}

// New creates a new introspection store backed by a Postgres connection pool.
func New(pool *pgxpool.Pool) *Store {
	if pool == nil {
		return &Store{}
	}
	return &Store{oauth2: oauth2store.New(pool)}
}

// OAuth2 returns the underlying OAuth2 token store.
func (s *Store) OAuth2() *oauth2store.Store {
	if s == nil {
		return nil
	}
	return s.oauth2
}
