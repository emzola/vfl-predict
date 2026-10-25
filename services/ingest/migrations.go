package ingest

import (
	"context"
	"database/sql"
	"os"
	"strings"
)

// MigrationsEnabled preserves the historical development default while
// allowing production services to use the dedicated migration runner.
// Invalid values fail safe by keeping migrations enabled.
func MigrationsEnabled() bool {
	value := strings.TrimSpace(os.Getenv("RUN_MIGRATIONS"))
	if value == "" {
		return true
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// ApplyMigrationsIfEnabled is used by development-oriented commands. The
// production Compose file sets RUN_MIGRATIONS=false and runs cmd/migrate once.
func ApplyMigrationsIfEnabled(ctx context.Context, db *sql.DB, migrationsDir string) error {
	if !MigrationsEnabled() {
		return nil
	}
	return ApplyMigrations(ctx, db, migrationsDir)
}
