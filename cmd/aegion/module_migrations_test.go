package main

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
)

type moduleTestMigrator struct {
	err   error
	calls int
}

func (m *moduleTestMigrator) Migrate(ctx context.Context) error {
	m.calls++
	return m.err
}

func TestRunEnabledModuleMigrationsWithDeps(t *testing.T) {
	t.Run("runs enabled modules in resolved order", func(t *testing.T) {
		cfg := &config.Config{
			ModuleVersions: map[string]string{
				"oauth2":        "latest",
				"introspection": "latest",
				"policy":        "latest",
				"admin":         "latest",
			},
		}

		var ran []string
		deps := moduleMigrationDeps{
			moduleOrder: func(moduleVersions map[string]string) ([]string, error) {
				return []string{"oauth2", "introspection", "policy", "admin"}, nil
			},
			moduleFS: func(configPath string) (fs.FS, error) {
				return fstest.MapFS{
					"modules/oauth2/migrations/0001_oauth2.up.sql":               {Data: []byte("SELECT 1;")},
					"modules/introspection/migrations/0001_introspection.up.sql": {Data: []byte("SELECT 1;")},
					"modules/policy/migrations/0001_policy.up.sql":               {Data: []byte("SELECT 1;")},
					"modules/admin/migrations/0001_admin.up.sql":                 {Data: []byte("SELECT 1;")},
				}, nil
			},
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				ran = append(ran, moduleID)
				return &moduleTestMigrator{}
			},
		}

		if err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs/aegion.yaml", deps); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		want := []string{"oauth2", "introspection", "policy", "admin"}
		if strings.Join(ran, ",") != strings.Join(want, ",") {
			t.Fatalf("unexpected module migration order: got %v, want %v", ran, want)
		}
	})

	t.Run("returns error when module path is missing", func(t *testing.T) {
		cfg := &config.Config{
			ModuleVersions: map[string]string{"oauth2": "latest"},
		}
		deps := moduleMigrationDeps{
			moduleOrder: func(moduleVersions map[string]string) ([]string, error) {
				return []string{"oauth2"}, nil
			},
			moduleFS: func(configPath string) (fs.FS, error) {
				return fstest.MapFS{}, nil
			},
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				return &moduleTestMigrator{}
			},
		}

		err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs/aegion.yaml", deps)
		if err == nil || !strings.Contains(err.Error(), `module "oauth2" migrations not found`) {
			t.Fatalf("expected missing migration path error, got %v", err)
		}
	})

	t.Run("wraps module order errors", func(t *testing.T) {
		cfg := &config.Config{
			ModuleVersions: map[string]string{"oauth2": "latest"},
		}
		deps := moduleMigrationDeps{
			moduleOrder: func(moduleVersions map[string]string) ([]string, error) {
				return nil, errors.New("order failed")
			},
			moduleFS: func(configPath string) (fs.FS, error) {
				return fstest.MapFS{}, nil
			},
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				return &moduleTestMigrator{}
			},
		}

		err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs/aegion.yaml", deps)
		if err == nil || !strings.Contains(err.Error(), "resolving enabled module order") {
			t.Fatalf("expected wrapped order error, got %v", err)
		}
	})

	t.Run("wraps module migration failures", func(t *testing.T) {
		cfg := &config.Config{
			ModuleVersions: map[string]string{"oauth2": "latest"},
		}
		deps := moduleMigrationDeps{
			moduleOrder: func(moduleVersions map[string]string) ([]string, error) {
				return []string{"oauth2"}, nil
			},
			moduleFS: func(configPath string) (fs.FS, error) {
				return fstest.MapFS{
					"modules/oauth2/migrations/0001_oauth2.up.sql": {Data: []byte("SELECT 1;")},
				}, nil
			},
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				return &moduleTestMigrator{err: errors.New("boom")}
			},
		}

		err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs/aegion.yaml", deps)
		if err == nil || !strings.Contains(err.Error(), `running module "oauth2" migrations`) {
			t.Fatalf("expected wrapped module migration error, got %v", err)
		}
	})
}
