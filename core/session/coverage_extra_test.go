package session

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewManager_WithDBInitializesDelegates(t *testing.T) {
	m := NewManager(ManagerConfig{
		DB:           &pgxpool.Pool{},
		CookieSecret: []byte("session-secret"),
		CookieConfig: CookieConfig{Name: "aegion_session"},
		Lifespan:     2 * time.Hour,
		IdleTimeout:  30 * time.Minute,
	})

	if m.execStmt == nil || m.queryRowFn == nil || m.queryRows == nil || m.beginTx == nil {
		t.Fatalf("expected DB-backed delegates to be initialized")
	}

	if m.now().Location() != time.UTC {
		t.Fatalf("expected manager clock to use UTC")
	}
}
