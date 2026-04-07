package store

// Store handles MFA persistence concerns.
type Store struct{}

// New creates a new MFA store.
func New() *Store {
	return &Store{}
}
