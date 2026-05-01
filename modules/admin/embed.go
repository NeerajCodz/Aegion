package admin

import (
	"embed"
	"io/fs"
)

// Embed the SPA dist files at build time
//
//go:embed spa/dist/*
var spaFiles embed.FS

//go:embed migrations/*.sql
var migrationFiles embed.FS

var subSPAFiles = fs.Sub

// GetSPAFiles returns the embedded filesystem containing the SPA files
func GetSPAFiles() fs.FS {
	// Strip the spa/dist prefix to serve files from root
	distFS, err := subSPAFiles(spaFiles, "spa/dist")
	if err != nil {
		// Fallback to empty filesystem if dist doesn't exist
		return embed.FS{}
	}
	return distFS
}

// GetMigrationFiles returns the embedded filesystem containing admin SQL migrations.
func GetMigrationFiles() fs.FS {
	return migrationFiles
}
