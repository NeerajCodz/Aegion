package analytics

import (
	"testing"
	"time"
)

func TestDefaultConfig_Validates(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig should validate, got: %v", err)
	}
}

func TestConfigValidate_DisabledIsAlwaysValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled config should validate, got: %v", err)
	}
}

func TestConfigValidate_DuckDBRequirements(t *testing.T) {
	t.Run("requires_path_or_max_memory", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.Path = ""
		cfg.DuckDB.MaxMemory = 0
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for duckdb without path or max_memory")
		}
	})

	t.Run("connection_pool_size_must_be_positive", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.ConnectionPoolSize = 0
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for zero connection_pool_size")
		}
	})

	t.Run("max_memory_must_be_non_negative", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.Path = ""
		cfg.DuckDB.MaxMemory = -1
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for negative max_memory")
		}
	})

	t.Run("threads_must_be_positive", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.Threads = 0
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for zero threads")
		}
	})

	t.Run("valid_with_path", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.Path = "/path/to/db.duckdb"
		cfg.DuckDB.MaxMemory = 0
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid config with path, got: %v", err)
		}
	})

	t.Run("valid_with_max_memory", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.Path = ""
		cfg.DuckDB.MaxMemory = 4096
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid config with max_memory, got: %v", err)
		}
	})
}

func TestConfigValidate_StorageTypeRequirements(t *testing.T) {
	t.Run("invalid_storage_type", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Storage.Type = StorageType("nope")
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for invalid storage type")
		}
	})

	t.Run("local_requires_path", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Storage.Type = StorageTypeLocal
		cfg.Storage.LocalPath = ""
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for local storage missing local_path")
		}
	})

	t.Run("s3_requires_bucket_and_region", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Storage.Type = StorageTypeS3
		cfg.Storage.S3.Bucket = ""
		cfg.Storage.S3.Region = ""
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for s3 storage missing bucket/region")
		}
	})

	t.Run("iceberg_requires_catalog_and_warehouse", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Storage.Type = StorageTypeIceberg
		cfg.Storage.Iceberg.CatalogType = ""
		cfg.Storage.Iceberg.WarehousePath = ""
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for iceberg storage missing catalog_type/warehouse_path")
		}
	})

	t.Run("k8s_requires_pvc_and_mount", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Storage.Type = StorageTypeK8s
		cfg.Storage.K8s.PersistentVolumeName = ""
		cfg.Storage.K8s.MountPath = ""
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for k8s storage missing pvc_name/mount_path")
		}
	})
}

func TestConfigValidate_SyncStrategies(t *testing.T) {
	t.Run("multiple_strategies_enabled", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Sync.Enabled = true
		cfg.Sync.Strategies = []string{"real_time", "batch", "async"}
		cfg.Sync.RealTime.Enabled = true
		cfg.Sync.Batch.Enabled = true
		cfg.Sync.Async.Enabled = true
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid config with multiple strategies, got: %v", err)
		}
	})

	t.Run("real_time_strategy", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Sync.Enabled = true
		cfg.Sync.Strategies = []string{"real_time"}
		cfg.Sync.RealTime.Enabled = true
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid real-time sync config, got: %v", err)
		}
	})

	t.Run("batch_strategy", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Sync.Enabled = true
		cfg.Sync.Strategies = []string{"batch"}
		cfg.Sync.Batch.Enabled = true
		cfg.Sync.Batch.Interval = "1h"
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid batch sync config, got: %v", err)
		}
	})

	t.Run("async_strategy", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Sync.Enabled = true
		cfg.Sync.Strategies = []string{"async"}
		cfg.Sync.Async.Enabled = true
		cfg.Sync.Async.Broker = "memory"
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid async sync config, got: %v", err)
		}
	})
}

func TestConfigValidate_BatchTablesIdentifiers(t *testing.T) {
	t.Run("valid_identifiers", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Sync.Batch.Enabled = true
		cfg.Sync.Batch.Tables = []string{"events", "user_events_2026"}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid batch table identifiers, got: %v", err)
		}
	})

	t.Run("invalid_identifier_rejected", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Sync.Batch.Enabled = true
		cfg.Sync.Batch.Tables = []string{"events; DROP TABLE users; --"}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected invalid batch table identifier to be rejected")
		}
	})
}

func TestConfigValidate_RetentionTiers(t *testing.T) {
	t.Run("default_tiers_valid", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Retention.Enabled = true
		// hot < warm < cold by default
		if cfg.Retention.HotTTLDays > cfg.Retention.WarmTTLDays ||
			cfg.Retention.WarmTTLDays > cfg.Retention.ColdTTLDays {
			t.Fatalf("default retention tiers should be in order: hot < warm < cold")
		}
	})

	t.Run("category_overrides", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Retention.Enabled = true
		cfg.Retention.Categories = map[string]CategoryConfig{
			"custom_category": {
				HotDays:  3,
				WarmDays: 30,
				ColdDays: 365,
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid retention config with categories, got: %v", err)
		}
	})
}

func TestConfigValidate_APIs(t *testing.T) {
	t.Run("rest_api", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.REST.Enabled = true
		cfg.REST.Endpoint = "/api/v1/analytics"
		cfg.REST.QueryTimeoutSeconds = 30
		cfg.REST.RateLimitPerMinute = 100
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid REST API config, got: %v", err)
		}
	})

	t.Run("graphql_api", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.GraphQL.Enabled = true
		cfg.GraphQL.Endpoint = "/api/v1/graphql"
		cfg.GraphQL.MaxQueryDepth = 10
		cfg.GraphQL.MaxQueryComplexity = 1000
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid GraphQL API config, got: %v", err)
		}
	})

	t.Run("grpc_api", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.GRPC.Enabled = true
		cfg.GRPC.Port = 50051
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid gRPC API config, got: %v", err)
		}
	})
}

func TestConfigValidate_WebhookSettings(t *testing.T) {
	t.Run("webhook_config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Webhooks.Enabled = true
		cfg.Webhooks.MaxPerUser = 50
		cfg.Webhooks.MaxRetries = 5
		cfg.Webhooks.TimeoutSeconds = 30
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid webhook config, got: %v", err)
		}
	})
}

func TestConfigValidate_DuckDBPerformance(t *testing.T) {
	t.Run("performance_settings", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DuckDB.Performance.QueryTimeoutSeconds = 60
		cfg.DuckDB.Performance.MaxConcurrentQueries = 100
		cfg.DuckDB.Performance.CachingEnabled = true
		cfg.DuckDB.Performance.CacheTTLMinutes = 30
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid performance config, got: %v", err)
		}
	})
}

func TestConfigValidate_SecuritySettings(t *testing.T) {
	t.Run("security_config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Security.Enabled = true
		cfg.Security.RBAC.Enabled = true
		cfg.Security.Encryption.Enabled = true
		cfg.Security.RateLimiting.Enabled = true
		cfg.Security.Audit.Enabled = true
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid security config, got: %v", err)
		}
	})

	t.Run("query_validation", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Security.QueryValidation.MaxComplexity = 1000
		cfg.Security.QueryValidation.MaxRecursionDepth = 10
		cfg.Security.QueryValidation.MaxFields = 100
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid query validation config, got: %v", err)
		}
	})
}

func TestConfig_HealthCheckInterval(t *testing.T) {
	t.Run("parses_duration", func(t *testing.T) {
		cfg := DefaultConfig()
		if cfg.DuckDB.HealthCheckInterval != 30*time.Second {
			t.Fatalf("expected health check interval of 30s, got %v", cfg.DuckDB.HealthCheckInterval)
		}
	})
}

func TestConfig_ValidateMinimalConfig(t *testing.T) {
	t.Run("minimal_local_config", func(t *testing.T) {
		cfg := &Config{
			Enabled: true,
			DuckDB: DuckDBConfig{
				Path:               ":memory:",
				MaxMemory:          0,
				Threads:            1,
				ConnectionPoolSize: 1,
			},
			Storage: StorageConfig{
				Type:      StorageTypeLocal,
				LocalPath: "./data",
			},
			REST:      RestAPIConfig{Enabled: true},
			GraphQL:   GraphQLAPIConfig{Enabled: true},
			GRPC:      gRPCAPIConfig{Enabled: true},
			Webhooks:  WebhooksConfig{Enabled: false},
			Retention: RetentionConfig{Enabled: false},
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected minimal config to validate, got: %v", err)
		}
	})
}
