package store

// Store handles social module persistence concerns.
type Store struct{}

// New creates a new social store.
func New() *Store {
	return &Store{}
}
