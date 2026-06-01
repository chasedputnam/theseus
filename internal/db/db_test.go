package db

import (
	"path/filepath"
	"testing"
)

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")

	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Run migrations a second time — must not error
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	// Verify all expected tables exist
	tables := []string{
		"sessions", "chat_messages", "documents", "document_versions",
		"gallery_albums", "gallery_images", "email_accounts",
		"model_endpoints", "mcp_servers", "comparisons", "signatures",
		"api_tokens", "webhooks", "user_tools", "user_tool_data",
		"crew_members", "scheduled_tasks", "task_runs", "editor_drafts",
		"memories", "notes", "calendar_cals", "calendar_events",
	}
	for _, tbl := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}
