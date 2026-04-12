package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreAndModuleSQLSeparation(t *testing.T) {
	coreMigrationFiles, err := fs.Glob(migrations, "migrations/*.up.sql")
	if err != nil {
		t.Fatalf("glob core migrations: %v", err)
	}
	if len(coreMigrationFiles) == 0 {
		t.Fatal("expected embedded core migration files")
	}

	for _, file := range coreMigrationFiles {
		if strings.Contains(strings.ToLower(file), "policy") {
			t.Fatalf("core migration file should not include module policy migration: %s", file)
		}
		content, err := fs.ReadFile(migrations, file)
		if err != nil {
			t.Fatalf("read core migration %s: %v", file, err)
		}
		sql := strings.ToLower(string(content))
		if strings.Contains(sql, "create table pol_") || strings.Contains(sql, "drop table if exists pol_") {
			t.Fatalf("core migration %s includes policy module tables; keep module DDL under modules/policy/migrations", file)
		}
	}
}

func TestPolicyMigrationDirection(t *testing.T) {
	moduleFS := os.DirFS("../..")

	upSQL, err := fs.ReadFile(moduleFS, "modules/policy/migrations/0001_policy_tables.up.sql")
	if err != nil {
		t.Fatalf("read policy up migration: %v", err)
	}
	downSQL, err := fs.ReadFile(moduleFS, "modules/policy/migrations/0001_policy_tables.down.sql")
	if err != nil {
		t.Fatalf("read policy down migration: %v", err)
	}

	up := strings.ToLower(string(upSQL))
	down := strings.ToLower(string(downSQL))

	if !strings.Contains(up, "create table pol_roles") {
		t.Fatal("policy up migration must create policy tables")
	}
	if !strings.Contains(down, "drop table if exists pol_roles") {
		t.Fatal("policy down migration must drop policy tables")
	}
}

func TestMigrationTreesExcludeTestFixtureSQL(t *testing.T) {
	moduleFS := os.DirFS("../..")
	moduleMigrationFiles, err := fs.Glob(moduleFS, "modules/*/migrations/*.sql")
	if err != nil {
		t.Fatalf("glob module migrations: %v", err)
	}

	coreMigrationFiles, err := fs.Glob(moduleFS, "core/migrations/*.sql")
	if err != nil {
		t.Fatalf("glob core migrations: %v", err)
	}

	allFiles := append(coreMigrationFiles, moduleMigrationFiles...)
	if len(allFiles) == 0 {
		t.Fatal("expected migration files for fixture separation check")
	}

	for _, file := range allFiles {
		name := strings.ToLower(filepath.Base(file))
		if strings.Contains(name, "fixture") || strings.Contains(name, "test") || strings.Contains(name, "seed") {
			t.Fatalf("production migration tree contains non-production SQL file: %s", file)
		}
	}
}
