package store

// Store handles CLI module persistence concerns.
type Store struct{}

// New creates a new CLI store.
func New() *Store {
	return &Store{}
}
