package analytics

import "testing"

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
