// Package modules embeds the module migration assets consumed by the core migrator.
package modules

import "embed"

// MigrationFiles contains every module migration. Core is the single migration
// owner, so deployed module workloads never need a source checkout to verify
// their schema.
//
//go:embed */migrations/*.sql
var MigrationFiles embed.FS
