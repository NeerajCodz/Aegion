package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openDuckDBForTest(t *testing.T) *DuckDB {
	t.Helper()

	db, err := NewDuckDB(DuckDBConfig{
		Path:                ":memory:",
		InitializeOnStartup: true,
		HealthCheckInterval: time.Millisecond,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duckdb_extension") {
			t.Skipf("duckdb runtime missing required extension in this environment: %v", err)
		}
		require.NoError(t, err)
	}

	return db
}

func TestDuckDBLifecycleAndQueries(t *testing.T) {
	ctx := context.Background()
	db := openDuckDBForTest(t)
	defer func() {
		require.NoError(t, db.Close(context.Background()))
	}()

	require.NoError(t, db.Initialize(ctx))

	var err error
	_, err = db.Exec(ctx, `CREATE TABLE test_events (id INTEGER, name VARCHAR)`)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `INSERT INTO test_events VALUES (1, 'alpha'), (2, 'beta')`)
	require.NoError(t, err)

	var count int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM test_events`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	rows, err := db.Query(ctx, `SELECT id, name FROM test_events ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var ids []int
	var names []string
	for rows.Next() {
		var id int
		var name string
		require.NoError(t, rows.Scan(&id, &name))
		ids = append(ids, id)
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{1, 2}, ids)
	assert.Equal(t, []string{"alpha", "beta"}, names)

	results, err := db.ExecuteSQL(ctx, `SELECT name FROM test_events ORDER BY id`)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "alpha", results[0]["name"])

	assert.NoError(t, db.Health(ctx))
	assert.Greater(t, db.Stats().MaxOpenConnections, 0)
}

func TestDuckDBDefaultsAndTimeouts(t *testing.T) {
	db := openDuckDBForTest(t)
	defer func() {
		require.NoError(t, db.Close(context.Background()))
	}()

	assert.Equal(t, 4096, db.config.MaxMemory)
	assert.Equal(t, 4, db.config.Threads)
	assert.Equal(t, 10, db.config.ConnectionPoolSize)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var err error
	_, err = db.Exec(ctx, `SELECT 1`)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
