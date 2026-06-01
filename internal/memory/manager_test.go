package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chaseputnam/theseus/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestAddAndSearch(t *testing.T) {
	m := New(openTestDB(t), "", 0)
	ctx := context.Background()

	_, err := m.Add(ctx, "I prefer dark mode", "preference", "user", "alice", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = m.Add(ctx, "My favorite language is Go", "fact", "user", "alice", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := m.Search(ctx, "dark mode", "alice", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for 'dark mode'")
	}
	found := false
	for _, r := range results {
		if r.Text == "I prefer dark mode" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'I prefer dark mode' in results")
	}
}

func TestDeleteMemory(t *testing.T) {
	m := New(openTestDB(t), "", 0)
	ctx := context.Background()

	entry, _ := m.Add(ctx, "test memory", "fact", "user", "alice", "")
	if err := m.Delete(ctx, entry.ID, "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	results, _ := m.List(ctx, "alice")
	if len(results) != 0 {
		t.Errorf("expected 0 memories after delete, got %d", len(results))
	}
}

func TestImportDedup(t *testing.T) {
	m := New(openTestDB(t), "", 0)
	ctx := context.Background()

	m.Add(ctx, "I prefer dark mode", "preference", "user", "alice", "")

	entries := []*db.Memory{
		{Text: "I prefer dark mode", Category: "preference", Source: "import"}, // duplicate
		{Text: "I live in Seattle", Category: "fact", Source: "import"},         // new
	}
	added, err := m.Import(ctx, entries, "alice")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if added != 1 {
		t.Errorf("expected 1 added (dedup), got %d", added)
	}
}

func TestJaccard(t *testing.T) {
	a := tokenize("hello world foo")
	b := tokenize("hello world bar")
	j := jaccard(a, b)
	if j < 0.4 || j > 0.8 {
		t.Errorf("unexpected jaccard: %f", j)
	}
	// Identical
	c := tokenize("hello world")
	if jaccard(c, c) != 1.0 {
		t.Error("identical sets should have jaccard=1.0")
	}
}
