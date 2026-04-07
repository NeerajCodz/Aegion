package store

// Store handles introspection persistence concerns.
type Store struct{}

// New creates a new introspection store.
func New() *Store {
	return &Store{}
}
