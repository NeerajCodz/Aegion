package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
)

type moduleMigrationDeps struct {
	moduleOrder    func(moduleVersions map[string]string) ([]string, error)
	moduleFS       func(configPath string) (fs.FS, error)
	moduleMigrator func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator
}

func defaultModuleMigrationDeps() moduleMigrationDeps {
	return moduleMigrationDeps{
		moduleOrder: orchestrator.EnabledModuleOrder,
		moduleFS:    resolveModuleFS,
		moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
			return database.NewModuleMigrator(db, migrationFS, basePath, moduleID)
		},
	}
}

func runEnabledModuleMigrations(ctx context.Context, cfg *config.Config, db *database.DB, configPath string) error {
	return runEnabledModuleMigrationsWithDeps(ctx, cfg, db, configPath, defaultModuleMigrationDeps())
}

func runEnabledModuleMigrationsWithDeps(
	ctx context.Context,
	cfg *config.Config,
	db *database.DB,
	configPath string,
	deps moduleMigrationDeps,
) error {
	if cfg == nil {
		return nil
	}

	modules, err := deps.moduleOrder(cfg.ModuleVersions)
	if err != nil {
		return fmt.Errorf("resolving enabled module order: %w", err)
	}
	if len(modules) == 0 {
		return nil
	}

	migrationFS, err := deps.moduleFS(configPath)
	if err != nil {
		return err
	}

	for _, moduleID := range modules {
		basePath := path.Join("modules", moduleID, "migrations")
		if _, err := fs.Stat(migrationFS, basePath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("module %q migrations not found at %q", moduleID, basePath)
			}
			return fmt.Errorf("checking module %q migrations at %q: %w", moduleID, basePath, err)
		}

		migrator := deps.moduleMigrator(db, moduleID, migrationFS, basePath)
		if err := migrator.Migrate(ctx); err != nil {
			return fmt.Errorf("running module %q migrations: %w", moduleID, err)
		}
	}

	return nil
}

func resolveModuleFS(configPath string) (fs.FS, error) {
	candidates, err := moduleRootCandidates(configPath)
	if err != nil {
		return nil, err
	}

	for _, root := range candidates {
		fsys := os.DirFS(root)
		if _, err := fs.Stat(fsys, "modules"); err == nil {
			return fsys, nil
		}
	}

	return nil, fmt.Errorf("modules directory not found from config path %q", configPath)
}

func moduleRootCandidates(configPath string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	seen := map[string]struct{}{}
	candidates := make([]string, 0, 3)
	add := func(candidate string) {
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	add(cwd)

	if configPath != "" {
		absConfigPath := configPath
		if !filepath.IsAbs(absConfigPath) {
			absConfigPath = filepath.Join(cwd, configPath)
		}
		absConfigPath = filepath.Clean(absConfigPath)
		configDir := filepath.Dir(absConfigPath)
		add(configDir)
		add(filepath.Dir(configDir))
	}

	return candidates, nil
}
