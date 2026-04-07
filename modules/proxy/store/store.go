package store

// Store handles proxy module persistence concerns.
type Store struct{}

// New creates a new proxy store.
func New() *Store {
	return &Store{}
}
