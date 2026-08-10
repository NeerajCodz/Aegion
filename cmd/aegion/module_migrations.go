package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
)

type moduleMigrationDeps struct {
	moduleMigrator func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator
}

func defaultModuleMigrationDeps() moduleMigrationDeps {
	return moduleMigrationDeps{
		moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
			return database.NewModuleMigrator(db, migrationFS, basePath, moduleID)
		},
	}
}

func runEnabledModuleMigrations(ctx context.Context, plan config.ModulePlan, db *database.DB) error {
	return runEnabledModuleMigrationsWithDeps(ctx, plan, db, defaultModuleMigrationDeps())
}

func runEnabledModuleMigrationsWithDeps(
	ctx context.Context,
	plan config.ModulePlan,
	db *database.DB,
	deps moduleMigrationDeps,
) error {
	for _, module := range plan.Modules {
		migrationFS := module.Migration.Filesystem
		basePath := module.Migration.BasePath
		if migrationFS == nil || basePath == "" {
			return fmt.Errorf("module %q has no migration source", module.ID)
		}
		if _, err := fs.Stat(migrationFS, basePath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("module %q migrations not found at %q", module.ID, basePath)
			}
			return fmt.Errorf("checking module %q migrations at %q: %w", module.ID, basePath, err)
		}

		migrator := deps.moduleMigrator(db, module.ID, migrationFS, basePath)
		if err := migrator.Migrate(ctx); err != nil {
			return fmt.Errorf("running module %q migrations: %w", module.ID, err)
		}
	}

	return nil
}
