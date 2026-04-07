package store

// Store handles passkeys persistence concerns.
type Store struct{}

// New creates a new passkeys store.
func New() *Store {
	return &Store{}
}
