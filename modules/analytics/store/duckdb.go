package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// DuckDB manages DuckDB connections and provides the main analytics interface.
type DuckDB struct {
	db              *sql.DB
	config          DuckDBConfig
	mu              sync.RWMutex
	lastHealthCheck time.Time
	isHealthy       bool
}

// DuckDBConfig holds DuckDB configuration.
type DuckDBConfig struct {
	// Path to the database file (e.g., "analytics.duckdb" or ":memory:")
	Path string

	// MaxMemory in MB for DuckDB to use
	MaxMemory int

	// Threads for DuckDB to use
	Threads int

	// ConnectionPoolSize is the maximum number of open connections
	ConnectionPoolSize int

	// HealthCheckInterval is how often to check database health
	HealthCheckInterval time.Duration

	// InitializeOnStartup determines if schema should be created on startup
	InitializeOnStartup bool
}

// NewDuckDB creates a new DuckDB instance and establishes the connection pool.
func NewDuckDB(cfg DuckDBConfig) (*DuckDB, error) {
	if cfg.Path == "" {
		cfg.Path = "analytics.duckdb"
	}

	if cfg.MaxMemory == 0 {
		cfg.MaxMemory = 4096 // 4GB default
	}

	if cfg.Threads == 0 {
		cfg.Threads = 4
	}

	if cfg.ConnectionPoolSize == 0 {
		cfg.ConnectionPoolSize = 10
	}

	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 30 * time.Second
	}

	// Construct connection string with DuckDB parameters
	connStr := fmt.Sprintf("file:%s?access_mode=automatic&memory_limit=%dMB&threads=%d",
		cfg.Path, cfg.MaxMemory, cfg.Threads)

	db, err := sql.Open("duckdb", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.ConnectionPoolSize)
	db.SetMaxIdleConns(cfg.ConnectionPoolSize / 2)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to DuckDB: %w", err)
	}

	duckdb := &DuckDB{
		db:              db,
		config:          cfg,
		lastHealthCheck: time.Now(),
		isHealthy:       true,
	}

	// Load DuckDB extensions
	if err := duckdb.loadExtensions(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load extensions: %w", err)
	}

	return duckdb, nil
}

// loadExtensions loads commonly used DuckDB extensions.
func (d *DuckDB) loadExtensions(ctx context.Context) error {
	extensions := []string{
		"json",
		"parquet",
	}

	for _, ext := range extensions {
		query := fmt.Sprintf("INSTALL %s; LOAD %s;", ext, ext)
		_, err := d.db.ExecContext(ctx, query)
		if err != nil {
			// Some extensions might not be available in all builds; log but don't fail
			fmt.Printf("Warning: failed to load extension %s: %v\n", ext, err)
		}
	}

	return nil
}

// Initialize creates the analytics schema if it doesn't exist.
func (d *DuckDB) Initialize(ctx context.Context) error {
	if !d.config.InitializeOnStartup {
		return nil
	}

	// Schema initialization happens through migrations
	// This is a placeholder for any runtime initialization needs
	return nil
}

// Query executes a SELECT query and returns results.
func (d *DuckDB) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a SELECT query returning a single row.
func (d *DuckDB) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.db.QueryRowContext(ctx, query, args...)
}

// Exec executes a statement (INSERT, UPDATE, DELETE, CREATE, etc.).
func (d *DuckDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.db.ExecContext(ctx, query, args...)
}

// BeginTx starts a new transaction.
func (d *DuckDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.db.BeginTx(ctx, opts)
}

// Health performs a health check on the DuckDB connection.
func (d *DuckDB) Health(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if enough time has passed since last health check
	if time.Since(d.lastHealthCheck) < d.config.HealthCheckInterval {
		if d.isHealthy {
			return nil
		}
	}

	// Perform health check
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := d.db.PingContext(ctx); err != nil {
		d.isHealthy = false
		return fmt.Errorf("DuckDB health check failed: %w", err)
	}

	d.lastHealthCheck = time.Now()
	d.isHealthy = true
	return nil
}

// Close closes the DuckDB connection.
func (d *DuckDB) Close(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.db.Close()
}

// Stats returns connection pool statistics.
func (d *DuckDB) Stats() sql.DBStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.db.Stats()
}

// Backup creates a backup of the DuckDB database.
func (d *DuckDB) Backup(ctx context.Context, destination string) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := fmt.Sprintf("BACKUP DATABASE TO '%s'", destination)
	_, err := d.db.ExecContext(ctx, query)
	return err
}

// ExecuteSQL executes raw SQL and returns results as generic interface.
// This is useful for dynamic queries and debugging.
func (d *DuckDB) ExecuteSQL(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		entry := make(map[string]interface{})
		for i, col := range columns {
			var v interface{}
			val := values[i]
			b, ok := val.([]byte)
			if ok {
				v = string(b)
			} else {
				v = val
			}
			entry[col] = v
		}

		results = append(results, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
