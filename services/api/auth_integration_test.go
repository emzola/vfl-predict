package api

import (
	"context"
	"os"
	"testing"
	"time"

	"bet9ja-vfl/services/ingest"
)

func TestPhase4AuthMigrationTables(t *testing.T) {
	databaseURL := os.Getenv("PHASE4_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PHASE4_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"auth_authorized_accounts", "auth_magic_link_tokens", "auth_sessions", "auth_login_rate_limits", "auth_audit_events", "user_preferences"} {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s is missing", table)
		}
	}
}
