package store

// Store handles SSO module persistence concerns.
type Store struct{}

// New creates a new SSO store.
func New() *Store {
	return &Store{}
}
