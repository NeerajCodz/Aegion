package analytics

import (
	"errors"
	"time"
)

// StorageType defines the type of storage backend.
type StorageType string

const (
	StorageTypeLocal   StorageType = "local"
	StorageTypeS3      StorageType = "s3"
	StorageTypeIceberg StorageType = "iceberg"
	StorageTypeK8s     StorageType = "k8s"
)

// Config holds analytics module configuration.
type Config struct {
	// Enabled determines if the analytics module is active
	Enabled bool `yaml:"enabled"`

	// DuckDB configuration
	DuckDB DuckDBConfig `yaml:"duckdb"`

	// Storage backend configuration
	Storage StorageConfig `yaml:"storage"`

	// Sync settings for syncing data from PostgreSQL
	Sync SyncConfig `yaml:"sync"`
}

// DuckDBConfig holds DuckDB-specific settings.
type DuckDBConfig struct {
	// Path to the DuckDB database file (for local mode)
	Path string `yaml:"path"`

	// MaxMemory sets the maximum memory in MB DuckDB can use
	MaxMemory int `yaml:"max_memory"`

	// Threads sets the number of threads DuckDB will use
	Threads int `yaml:"threads"`

	// ConnectionPoolSize is the max number of concurrent connections
	ConnectionPoolSize int `yaml:"connection_pool_size"`

	// HealthCheckInterval is how often to check DuckDB health
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`

	// InitializeOnStartup determines if schema should be created on startup
	InitializeOnStartup bool `yaml:"initialize_on_startup"`
}

// StorageConfig holds storage backend configuration.
type StorageConfig struct {
	// Type of storage backend (local, s3, iceberg, k8s)
	Type StorageType `yaml:"type"`

	// Local storage path (used when Type is "local")
	LocalPath string `yaml:"local_path"`

	// S3 configuration
	S3 S3Config `yaml:"s3,omitempty"`

	// Iceberg configuration
	Iceberg IcebergConfig `yaml:"iceberg,omitempty"`

	// Kubernetes configuration
	K8s K8sConfig `yaml:"k8s,omitempty"`
}

// S3Config holds S3 storage configuration.
type S3Config struct {
	// Bucket is the S3 bucket name
	Bucket string `yaml:"bucket"`

	// Region is the AWS region
	Region string `yaml:"region"`

	// Prefix is the object key prefix
	Prefix string `yaml:"prefix"`

	// EndpointURL can be used for S3-compatible services
	EndpointURL string `yaml:"endpoint_url,omitempty"`

	// UsePathStyle determines if path-style S3 URLs are used
	UsePathStyle bool `yaml:"use_path_style"`

	// AccessKeyID (prefer environment variables in production)
	AccessKeyID string `yaml:"access_key_id,omitempty"`

	// SecretAccessKey (prefer environment variables in production)
	SecretAccessKey string `yaml:"secret_access_key,omitempty"`
}

// IcebergConfig holds Apache Iceberg configuration.
type IcebergConfig struct {
	// CatalogType can be "nessie", "dynamodb", "rest", or "hive"
	CatalogType string `yaml:"catalog_type"`

	// WarehousePath is the location where Iceberg stores tables
	WarehousePath string `yaml:"warehouse_path"`

	// NessieURI is the Nessie server URI (if using Nessie)
	NessieURI string `yaml:"nessie_uri,omitempty"`

	// CatalogName is the default catalog name
	CatalogName string `yaml:"catalog_name"`
}

// K8sConfig holds Kubernetes persistent volume configuration.
type K8sConfig struct {
	// PersistentVolumeName is the name of the PVC to use
	PersistentVolumeName string `yaml:"pvc_name"`

	// MountPath is the path where the PVC is mounted
	MountPath string `yaml:"mount_path"`

	// StorageClassName is the storage class for the PVC
	StorageClassName string `yaml:"storage_class,omitempty"`

	// Size is the size of the persistent volume
	Size string `yaml:"size"`
}

// SyncConfig holds configuration for syncing data from PostgreSQL.
type SyncConfig struct {
	// Enabled determines if sync is active
	Enabled bool `yaml:"enabled"`

	// Strategies lists active sync strategies (e.g., "real_time", "batch", "async")
	Strategies []string `yaml:"strategies"`

	// Real-time sync configuration
	RealTime RealTimeSyncConfig `yaml:"real_time"`

	// Batch sync configuration
	Batch BatchSyncConfig `yaml:"batch"`

	// Async sync configuration
	Async AsyncSyncConfig `yaml:"async"`
}

// RealTimeSyncConfig holds real-time CDC/trigger sync settings.
type RealTimeSyncConfig struct {
	// Enabled determines if real-time sync is active
	Enabled bool `yaml:"enabled"`

	// BatchSize is how many events to batch before flushing
	BatchSize int `yaml:"batch_size"`

	// FlushIntervalMs is the max time to wait before flushing batched events
	FlushIntervalMs int `yaml:"flush_interval_ms"`

	// MaxRetries for failed events
	MaxRetries int `yaml:"max_retries"`

	// RetryBackoffMs is the initial backoff for retries (exponential)
	RetryBackoffMs int `yaml:"retry_backoff_ms"`
}

// BatchSyncConfig holds batch scheduler sync settings.
type BatchSyncConfig struct {
	// Enabled determines if batch sync is active
	Enabled bool `yaml:"enabled"`

	// Interval is the cron-like interval (e.g., "1h", "1d", "@hourly")
	Interval string `yaml:"interval"`

	// StartTime is when to start the first batch (e.g., "02:00" for 2 AM)
	StartTime string `yaml:"start_time"`

	// Tables defines which tables to sync in batch mode
	Tables []string `yaml:"tables"`

	// BatchSize is how many records to sync per batch
	BatchSize int `yaml:"batch_size"`

	// ChunkSize is the internal chunk size for bulk inserts
	ChunkSize int `yaml:"chunk_size"`
}

// AsyncSyncConfig holds message queue based sync settings.
type AsyncSyncConfig struct {
	// Enabled determines if async sync is active
	Enabled bool `yaml:"enabled"`

	// Broker type: "kafka", "rabbitmq", "redis", "memory"
	Broker string `yaml:"broker"`

	// Topic or queue name
	Topic string `yaml:"topic"`

	// Partitions for Kafka-style brokers
	Partitions int `yaml:"partitions"`

	// ConsumerGroup for coordinated consumption
	ConsumerGroup string `yaml:"consumer_group"`

	// WorkerCount is the number of concurrent workers
	WorkerCount int `yaml:"worker_count"`

	// RetryBackoffMs is the initial backoff for retries
	RetryBackoffMs int `yaml:"retry_backoff_ms"`

	// MaxRetries for failed events
	MaxRetries int `yaml:"max_retries"`

	// BrokerConfig holds broker-specific settings (JSON for flexibility)
	BrokerConfig map[string]interface{} `yaml:"broker_config,omitempty"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.DuckDB.Path == "" && c.DuckDB.MaxMemory == 0 {
		return errors.New("duckdb configuration missing: must specify path or max_memory")
	}

	if c.DuckDB.ConnectionPoolSize == 0 {
		return errors.New("duckdb connection_pool_size must be greater than 0")
	}

	if c.DuckDB.MaxMemory < 0 {
		return errors.New("duckdb max_memory must be non-negative")
	}

	if c.DuckDB.Threads == 0 {
		return errors.New("duckdb threads must be greater than 0")
	}

	switch c.Storage.Type {
	case StorageTypeLocal:
		if c.Storage.LocalPath == "" {
			return errors.New("local storage requires local_path to be set")
		}
	case StorageTypeS3:
		if c.Storage.S3.Bucket == "" {
			return errors.New("S3 storage requires bucket to be set")
		}
		if c.Storage.S3.Region == "" {
			return errors.New("S3 storage requires region to be set")
		}
	case StorageTypeIceberg:
		if c.Storage.Iceberg.WarehousePath == "" {
			return errors.New("Iceberg storage requires warehouse_path to be set")
		}
		if c.Storage.Iceberg.CatalogType == "" {
			return errors.New("Iceberg storage requires catalog_type to be set")
		}
	case StorageTypeK8s:
		if c.Storage.K8s.PersistentVolumeName == "" {
			return errors.New("K8s storage requires pvc_name to be set")
		}
		if c.Storage.K8s.MountPath == "" {
			return errors.New("K8s storage requires mount_path to be set")
		}
	default:
		return errors.New("invalid storage type")
	}

	return nil
}

// DefaultConfig returns a configuration suitable for development.
func DefaultConfig() *Config {
	return &Config{
		Enabled: true,
		DuckDB: DuckDBConfig{
			Path:                   "analytics.duckdb",
			MaxMemory:              4096,
			Threads:                4,
			ConnectionPoolSize:     10,
			HealthCheckInterval:    30 * time.Second,
			InitializeOnStartup:    true,
		},
		Storage: StorageConfig{
			Type:      StorageTypeLocal,
			LocalPath: "./analytics_data",
		},
		Sync: SyncConfig{
			Enabled:    false,
			Strategies: []string{"batch"},
			RealTime: RealTimeSyncConfig{
				Enabled:        false,
				BatchSize:      100,
				FlushIntervalMs: 5000,
				MaxRetries:     3,
				RetryBackoffMs: 100,
			},
			Batch: BatchSyncConfig{
				Enabled:   false,
				Interval:  "1h",
				StartTime: "02:00",
				Tables:    []string{"audit_events", "sessions"},
				BatchSize: 1000,
				ChunkSize: 100,
			},
			Async: AsyncSyncConfig{
				Enabled:        false,
				Broker:         "memory",
				Topic:          "analytics-events",
				Partitions:     3,
				ConsumerGroup:  "aegion-analytics",
				WorkerCount:    4,
				RetryBackoffMs: 1000,
				MaxRetries:     5,
			},
		},
	}
}
