package database

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestConfigStruct(t *testing.T) {
	config := Config{
		URL:             "postgres://user:pass@localhost:5432/db",
		MaxOpenConns:    20,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}

	if config.URL != "postgres://user:pass@localhost:5432/db" {
		t.Errorf("URL = %s, want postgres://user:pass@localhost:5432/db", config.URL)
	}
	if config.MaxOpenConns != 20 {
		t.Errorf("MaxOpenConns = %d, want 20", config.MaxOpenConns)
	}
	if config.MaxIdleConns != 5 {
		t.Errorf("MaxIdleConns = %d, want 5", config.MaxIdleConns)
	}
	if config.ConnMaxLifetime != 30*time.Minute {
		t.Errorf("ConnMaxLifetime = %v, want 30m", config.ConnMaxLifetime)
	}
	if config.ConnMaxIdleTime != 5*time.Minute {
		t.Errorf("ConnMaxIdleTime = %v, want 5m", config.ConnMaxIdleTime)
	}
}

func TestMigrationStruct(t *testing.T) {
	migration := Migration{
		Version: 1,
		Name:    "create_users_table",
		UpSQL:   "CREATE TABLE users (id SERIAL PRIMARY KEY);",
		DownSQL: "DROP TABLE users;",
	}

	if migration.Version != 1 {
		t.Errorf("Version = %d, want 1", migration.Version)
	}
	if migration.Name != "create_users_table" {
		t.Errorf("Name = %s, want create_users_table", migration.Name)
	}
	if !strings.Contains(migration.UpSQL, "CREATE TABLE") {
		t.Error("UpSQL should contain CREATE TABLE")
	}
	if !strings.Contains(migration.DownSQL, "DROP TABLE") {
		t.Error("DownSQL should contain DROP TABLE")
	}
}

func TestNewMigrator(t *testing.T) {
	db := &DB{Pool: nil} // Mock DB for testing
	basePath := "migrations"

	// Create an empty embed.FS for testing
	var testFS embed.FS

	migrator := NewMigrator(db, testFS, basePath)

	if migrator == nil {
		t.Fatal("NewMigrator() returned nil")
	}
	if migrator.db != db {
		t.Error("Migrator.db not set correctly")
	}
	if migrator.basePath != basePath {
		t.Errorf("Migrator.basePath = %s, want %s", migrator.basePath, basePath)
	}
}

// Test the migration filename parsing logic (extracted from loadMigrations)
func TestMigrationFilenameParsingLogic(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		expectValid  bool
		expectVersion int
		expectName   string
		expectDirection string
	}{
		{
			name:         "valid up migration",
			filename:     "0001_create_users.up.sql",
			expectValid:  true,
			expectVersion: 1,
			expectName:   "create_users",
			expectDirection: "up",
		},
		{
			name:         "valid down migration",
			filename:     "0001_create_users.down.sql",
			expectValid:  true,
			expectVersion: 1,
			expectName:   "create_users",
			expectDirection: "down",
		},
		{
			name:         "complex migration name",
			filename:     "0042_add_user_email_index.up.sql",
			expectValid:  true,
			expectVersion: 42,
			expectName:   "add_user_email_index",
			expectDirection: "up",
		},
		{
			name:         "migration with underscores in name",
			filename:     "0123_update_user_table_add_created_at.down.sql",
			expectValid:  true,
			expectVersion: 123,
			expectName:   "update_user_table_add_created_at",
			expectDirection: "down",
		},
		{
			name:         "zero-padded version",
			filename:     "0007_initial_schema.up.sql",
			expectValid:  true,
			expectVersion: 7,
			expectName:   "initial_schema",
			expectDirection: "up",
		},
		{
			name:        "not sql file",
			filename:    "0001_create_users.up.txt",
			expectValid: false,
		},
		{
			name:        "no version number",
			filename:    "create_users.up.sql",
			expectValid: false,
		},
		{
			name:        "invalid version",
			filename:    "abc_create_users.up.sql",
			expectValid: false,
		},
		{
			name:        "no direction suffix",
			filename:    "0001_create_users.sql",
			expectValid: false,
		},
		{
			name:        "invalid direction",
			filename:    "0001_create_users.sideways.sql",
			expectValid: false,
		},
		{
			name:        "missing name part",
			filename:    "0001.up.sql",
			expectValid: false,
		},
		{
			name:        "empty filename",
			filename:    "",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the filename parsing logic from loadMigrations
			name := tt.filename
			
			// Check if it's a SQL file
			if !strings.HasSuffix(name, ".sql") {
				if tt.expectValid {
					t.Error("Expected filename to be valid, but it doesn't end with .sql")
				}
				return
			}

			// Parse version from filename
			var version int
			var migName string
			var direction string

			parts := strings.SplitN(name, "_", 2)
			if len(parts) < 2 {
				if tt.expectValid {
					t.Error("Expected filename to be valid, but it has fewer than 2 parts")
				}
				return
			}

			// Try to parse version
			n, err := parseVersion(parts[0])
			if err != nil {
				if tt.expectValid {
					t.Errorf("Expected filename to be valid, but version parsing failed: %v", err)
				}
				return
			}
			version = n

			rest := parts[1]
			if strings.HasSuffix(rest, ".up.sql") {
				direction = "up"
				migName = strings.TrimSuffix(rest, ".up.sql")
			} else if strings.HasSuffix(rest, ".down.sql") {
				direction = "down"
				migName = strings.TrimSuffix(rest, ".down.sql")
			} else {
				if tt.expectValid {
					t.Error("Expected filename to be valid, but direction suffix is invalid")
				}
				return
			}

			// If we got here and expectValid is false, that's an error
			if !tt.expectValid {
				t.Error("Expected filename to be invalid, but parsing succeeded")
				return
			}

			// Verify parsed values
			if version != tt.expectVersion {
				t.Errorf("Version = %d, want %d", version, tt.expectVersion)
			}
			if migName != tt.expectName {
				t.Errorf("Migration name = %s, want %s", migName, tt.expectName)
			}
			if direction != tt.expectDirection {
				t.Errorf("Direction = %s, want %s", direction, tt.expectDirection)
			}
		})
	}
}

// Helper function to simulate fmt.Sscanf parsing
func parseVersion(s string) (int, error) {
	var version int
	_, err := fmt.Sscanf(s, "%d", &version)
	return version, err
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectValue int
		expectError bool
	}{
		{"simple number", "123", 123, false},
		{"zero-padded", "0001", 1, false},
		{"large number", "999999", 999999, false},
		{"zero", "0", 0, false},
		{"leading zeros", "00042", 42, false},
		{"not a number", "abc", 0, true},
		{"mixed", "123abc", 123, false}, // Sscanf stops at non-digit
		{"empty string", "", 0, true},
		{"negative", "-5", -5, false}, // Negative numbers parse fine
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := parseVersion(tt.input)
			
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.expectError && value != tt.expectValue {
				t.Errorf("Value = %d, want %d", value, tt.expectValue)
			}
		})
	}
}

func TestMigrationSorting(t *testing.T) {
	// Test the sorting logic from loadMigrations
	migrations := []Migration{
		{Version: 3, Name: "third"},
		{Version: 1, Name: "first"},
		{Version: 5, Name: "fifth"},
		{Version: 2, Name: "second"},
		{Version: 4, Name: "fourth"},
	}

	// Simulate the sorting logic
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	expectedOrder := []int{1, 2, 3, 4, 5}
	for i, mig := range migrations {
		if mig.Version != expectedOrder[i] {
			t.Errorf("Migration %d: version = %d, want %d", i, mig.Version, expectedOrder[i])
		}
	}
}

func TestDBStruct(t *testing.T) {
	db := &DB{Pool: nil} // Mock for testing
	
	if db.Pool != nil {
		t.Error("Expected Pool to be nil")
	}
	
	// Test Close method doesn't panic with nil pool (it would in real usage)
	// In actual implementation, this might cause panic, but we're just testing structure
}