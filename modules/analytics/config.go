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

	// Security configuration
	Security SecurityConfig `yaml:"security"`

	// DuckDB configuration
	DuckDB DuckDBConfig `yaml:"duckdb"`

	// Storage backend configuration
	Storage StorageConfig `yaml:"storage"`

	// Sync settings for syncing data from PostgreSQL
	Sync SyncConfig `yaml:"sync"`

	// REST API configuration
	REST RestAPIConfig `yaml:"rest"`

	// GraphQL API configuration
	GraphQL GraphQLAPIConfig `yaml:"graphql"`

	// gRPC API configuration
	GRPC gRPCAPIConfig `yaml:"grpc"`

	// Webhooks configuration
	Webhooks WebhooksConfig `yaml:"webhooks"`

	// Retention configuration
	Retention RetentionConfig `yaml:"retention"`
}

// RestAPIConfig holds REST API configuration.
type RestAPIConfig struct {
	// Enabled determines if REST API is active
	Enabled bool `yaml:"enabled"`

	// Endpoint is the base path for REST endpoints
	Endpoint string `yaml:"endpoint"`

	// QueryTimeoutSeconds is the timeout for queries
	QueryTimeoutSeconds int `yaml:"query_timeout_seconds"`

	// RateLimitPerMinute limits requests per minute
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`

	// MaxPageSize limits result page size
	MaxPageSize int `yaml:"max_page_size"`

	// DefaultPageSize is the default result page size
	DefaultPageSize int `yaml:"default_page_size"`

	// CORS configuration
	CORS CORSConfig `yaml:"cors"`
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	Enabled         bool     `yaml:"enabled"`
	AllowedOrigins  []string `yaml:"allowed_origins"`
	AllowedMethods  []string `yaml:"allowed_methods"`
	AllowedHeaders  []string `yaml:"allowed_headers"`
	AllowCredentials bool    `yaml:"allow_credentials"`
	MaxAge          int      `yaml:"max_age"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	// Enabled determines if security features are active
	Enabled bool `yaml:"enabled"`

	// RBAC configuration
	RBAC RBACConfig `yaml:"rbac"`

	// Encryption configuration
	Encryption EncryptionConfig `yaml:"encryption"`

	// Rate limiting configuration
	RateLimiting RateLimitingConfig `yaml:"rate_limiting"`

	// Audit logging configuration
	Audit AuditConfig `yaml:"audit"`

	// Query validation configuration
	QueryValidation QueryValidationConfig `yaml:"query_validation"`
}

// RBACConfig holds role-based access control configuration
type RBACConfig struct {
	Enabled     bool   `yaml:"enabled"`
	DefaultRole string `yaml:"default_role"`
}

// EncryptionConfig holds encryption configuration
type EncryptionConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Algorithm       string `yaml:"algorithm"`
	KeyRotationDays int    `yaml:"key_rotation_days"`
}

// RateLimitingConfig holds rate limiting configuration
type RateLimitingConfig struct {
	Enabled            bool          `yaml:"enabled"`
	RequestsPerMinute  int           `yaml:"requests_per_minute"`
	Endpoints          map[string]int `yaml:"endpoints"`
}

// AuditConfig holds audit logging configuration
type AuditConfig struct {
	Enabled       bool `yaml:"enabled"`
	RetentionDays int  `yaml:"retention_days"`
}

// QueryValidationConfig holds query validation configuration
type QueryValidationConfig struct {
	MaxComplexity     int `yaml:"max_complexity"`
	MaxRecursionDepth int `yaml:"max_recursion_depth"`
	MaxFields         int `yaml:"max_fields"`
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

	// Performance optimization settings
	Performance PerformanceConfig `yaml:"performance"`
}

// PerformanceConfig holds performance tuning settings
type PerformanceConfig struct {
	// Query execution settings
	QueryTimeoutSeconds    int `yaml:"query_timeout_seconds"`
	MaxConcurrentQueries   int `yaml:"max_concurrent_queries"`
	ExplainThresholdMs     int `yaml:"explain_threshold_ms"`

	// Caching settings
	CachingEnabled bool `yaml:"caching_enabled"`
	CacheTTLMinutes int `yaml:"cache_ttl_minutes"`
	CacheMaxSizeMB int `yaml:"cache_max_size_mb"`

	// Memory and threading
	GCIntervalMs int `yaml:"gc_interval_ms"`

	// Batch operation settings
	SyncBatchSize       int `yaml:"sync_batch_size"`
	SyncFlushIntervalMs int `yaml:"sync_flush_interval_ms"`
	ExportBatchSize     int `yaml:"export_batch_size"`
	WebhookBatchSize    int `yaml:"webhook_batch_size"`
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

// GraphQLAPIConfig holds GraphQL API configuration.
type GraphQLAPIConfig struct {
	// Enabled determines if GraphQL API is active
	Enabled bool `yaml:"enabled"`

	// Endpoint is the HTTP path for GraphQL queries
	Endpoint string `yaml:"endpoint"`

	// EnableIntrospection enables schema introspection
	Introspection bool `yaml:"introspection"`

	// EnablePlayground enables GraphQL Playground
	Playground bool `yaml:"playground"`

	// MaxQueryDepth limits the depth of queries
	MaxQueryDepth int `yaml:"max_query_depth"`

	// MaxQueryComplexity limits the complexity score of queries
	MaxQueryComplexity int `yaml:"max_query_complexity"`

	// QueryTimeoutSeconds is the timeout for query execution
	QueryTimeoutSeconds int `yaml:"query_timeout_seconds"`

	// RateLimitPerMinute limits requests per minute
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
}

// gRPCAPIConfig holds gRPC API configuration.
type gRPCAPIConfig struct {
	// Enabled determines if gRPC API is active
	Enabled bool `yaml:"enabled"`

	// Port is the TCP port for gRPC server
	Port int `yaml:"port"`

	// MaxConcurrentStreams limits concurrent streams
	MaxConcurrentStreams int `yaml:"max_concurrent_streams"`

	// KeepaliveTimeSeconds is the keepalive ping interval
	KeepaliveTimeSeconds int `yaml:"keepalive_time_seconds"`

	// KeepaliveTimeoutSeconds is the keepalive ping timeout
	KeepaliveTimeoutSeconds int `yaml:"keepalive_timeout_seconds"`

	// MaxConnectionIdleSeconds is the max idle time for connections
	MaxConnectionIdleSeconds int `yaml:"max_connection_idle_seconds"`

	// EnableAuth requires service authentication
	EnableAuth bool `yaml:"enable_auth"`

	// EnableLogging enables request/response logging
	EnableLogging bool `yaml:"enable_logging"`

	// EnableTracing enables OpenTelemetry tracing
	EnableTracing bool `yaml:"enable_tracing"`
}

// WebhooksConfig holds webhook system configuration.
type WebhooksConfig struct {
	// Enabled determines if webhook system is active
	Enabled bool `yaml:"enabled"`

	// MaxPerUser limits the number of webhooks per user
	MaxPerUser int `yaml:"max_per_user"`

	// MaxRetries is the maximum number of retry attempts
	MaxRetries int `yaml:"max_retries"`

	// RetryBackoffBaseMs is the initial backoff time in milliseconds
	RetryBackoffBaseMs int `yaml:"retry_backoff_base_ms"`

	// TimeoutSeconds is the HTTP request timeout
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// BatchSize for processing webhooks
	BatchSize int `yaml:"batch_size"`

	// WorkerThreads is the number of concurrent delivery workers
	WorkerThreads int `yaml:"worker_threads"`

	// StoreDeliveryHistoryDays is how long to keep delivery history
	StoreDeliveryHistoryDays int `yaml:"store_delivery_history_days"`
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
		Security: SecurityConfig{
			Enabled: true,
			RBAC: RBACConfig{
				Enabled:     true,
				DefaultRole: "user",
			},
			Encryption: EncryptionConfig{
				Enabled:          true,
				Algorithm:        "aes256",
				KeyRotationDays:  90,
			},
			RateLimiting: RateLimitingConfig{
				Enabled:            true,
				RequestsPerMinute:  1000,
				Endpoints: map[string]int{
					"export": 60,
				},
			},
			Audit: AuditConfig{
				Enabled:       true,
				RetentionDays: 365,
			},
			QueryValidation: QueryValidationConfig{
				MaxComplexity:     1000,
				MaxRecursionDepth: 10,
				MaxFields:         100,
			},
		},
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
		REST: RestAPIConfig{
			Enabled:             true,
			Endpoint:            "/api/v1/analytics",
			QueryTimeoutSeconds: 30,
			RateLimitPerMinute:  100,
			MaxPageSize:         1000,
			DefaultPageSize:     50,
			CORS: CORSConfig{
				Enabled:         true,
				AllowedOrigins:  []string{"http://localhost:3000"},
				AllowedMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
				AllowedHeaders:  []string{"Content-Type", "Authorization"},
				AllowCredentials: true,
				MaxAge:          3600,
			},
		},
		GraphQL: GraphQLAPIConfig{
			Enabled:             true,
			Endpoint:            "/graphql",
			Introspection:       true,
			Playground:          true,
			MaxQueryDepth:       10,
			MaxQueryComplexity:  1000,
			QueryTimeoutSeconds: 30,
			RateLimitPerMinute:  100,
		},
		GRPC: gRPCAPIConfig{
			Enabled:                  true,
			Port:                     50051,
			MaxConcurrentStreams:     100,
			KeepaliveTimeSeconds:     20,
			KeepaliveTimeoutSeconds:  10,
			MaxConnectionIdleSeconds: 300,
			EnableAuth:               true,
			EnableLogging:            true,
			EnableTracing:            true,
		},
		Webhooks: WebhooksConfig{
			Enabled:                  true,
			MaxPerUser:               50,
			MaxRetries:               5,
			RetryBackoffBaseMs:       1000,
			TimeoutSeconds:           30,
			BatchSize:                100,
			WorkerThreads:            10,
			StoreDeliveryHistoryDays: 30,
		},
		Retention: RetentionConfig{
			Enabled:           true,
			DefaultPolicy:     "tiered",
			HotTTLDays:        7,
			WarmTTLDays:       90,
			ColdTTLDays:       730,
			ArchivalInterval:  "24h",
			CleanupInterval:   "168h",
			TieringInterval:   "6h",
			Categories: map[string]CategoryConfig{
				"audit_events": {
					HotDays:  30,
					WarmDays: 180,
					ColdDays: 730,
				},
				"authentication": {
					HotDays:  14,
					WarmDays: 60,
					ColdDays: 365,
				},
			},
		},
	}
}

// RetentionConfig holds retention and tiering configuration.
type RetentionConfig struct {
	// Enabled determines if retention management is active
	Enabled bool `yaml:"enabled"`

	// DefaultPolicy for retention (e.g., "tiered")
	DefaultPolicy string `yaml:"default_policy"`

	// TTL for each tier
	HotTTLDays  int `yaml:"hot_ttl_days"`
	WarmTTLDays int `yaml:"warm_ttl_days"`
	ColdTTLDays int `yaml:"cold_ttl_days"`

	// Scheduling intervals
	ArchivalInterval string `yaml:"archival_interval"`
	CleanupInterval  string `yaml:"cleanup_interval"`
	TieringInterval  string `yaml:"tiering_interval"`

	// Category-specific overrides
	Categories map[string]CategoryConfig `yaml:"categories,omitempty"`
}

// CategoryConfig defines retention settings for a specific event category.
type CategoryConfig struct {
	HotDays  int `yaml:"hot_days"`
	WarmDays int `yaml:"warm_days"`
	ColdDays int `yaml:"cold_days"`
}
