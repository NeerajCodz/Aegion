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
	t.Run("runs modules in the resolved plan order", func(t *testing.T) {
		plan, err := config.ResolveModulePlan(&config.Config{ModuleVersions: map[string]string{
			"oauth2":        "latest",
			"introspection": "latest",
			"policy":        "latest",
			"admin":         "latest",
		}})
		if err != nil {
			t.Fatalf("resolve module plan: %v", err)
		}

		var ran []string
		deps := moduleMigrationDeps{
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				ran = append(ran, moduleID)
				return &moduleTestMigrator{}
			},
		}

		if err := runEnabledModuleMigrationsWithDeps(context.Background(), plan, &database.DB{}, deps); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		want := []string{"oauth2", "introspection", "policy", "admin"}
		if strings.Join(ran, ",") != strings.Join(want, ",") {
			t.Fatalf("unexpected module migration order: got %v, want %v", ran, want)
		}
	})

	t.Run("returns error when migration path is missing", func(t *testing.T) {
		plan := config.ModulePlan{Modules: []config.ResolvedModule{{
			ID:        "oauth2",
			Migration: config.ModuleMigration{Filesystem: fstest.MapFS{}, BasePath: "oauth2/migrations"},
		}}}
		deps := moduleMigrationDeps{
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				return &moduleTestMigrator{}
			},
		}

		err := runEnabledModuleMigrationsWithDeps(context.Background(), plan, &database.DB{}, deps)
		if err == nil || !strings.Contains(err.Error(), `module "oauth2" migrations not found`) {
			t.Fatalf("expected missing migration path error, got %v", err)
		}
	})

	t.Run("returns error when module has no migration source", func(t *testing.T) {
		plan := config.ModulePlan{Modules: []config.ResolvedModule{{ID: "oauth2"}}}
		err := runEnabledModuleMigrationsWithDeps(context.Background(), plan, &database.DB{}, defaultModuleMigrationDeps())
		if err == nil || !strings.Contains(err.Error(), `module "oauth2" has no migration source`) {
			t.Fatalf("expected migration source error, got %v", err)
		}
	})

	t.Run("wraps module migration failures", func(t *testing.T) {
		plan := config.ModulePlan{Modules: []config.ResolvedModule{{
			ID: "oauth2",
			Migration: config.ModuleMigration{
				Filesystem: fstest.MapFS{"oauth2/migrations/0001_oauth2.up.sql": {Data: []byte("SELECT 1;")}},
				BasePath:   "oauth2/migrations",
			},
		}}}
		deps := moduleMigrationDeps{
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				return &moduleTestMigrator{err: errors.New("boom")}
			},
		}

		err := runEnabledModuleMigrationsWithDeps(context.Background(), plan, &database.DB{}, deps)
		if err == nil || !strings.Contains(err.Error(), `running module "oauth2" migrations`) {
			t.Fatalf("expected wrapped module migration error, got %v", err)
		}
	})
}
