package tools

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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

func TestCreateDocument(t *testing.T) {
	store := openTestDB(t)
	result, err := DoCreateDocument(context.Background(),
		"My Title\nmarkdown\n# Hello\nThis is content.", "", "admin", store)
	if err != nil {
		t.Fatalf("DoCreateDocument: %v", err)
	}
	if !strings.Contains(result, "My Title") {
		t.Errorf("expected title in result, got %q", result)
	}
	docs, _ := store.ListDocuments("admin", false)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Title != "My Title" {
		t.Errorf("expected 'My Title', got %q", docs[0].Title)
	}
	if docs[0].Language != (sql.NullString{String: "markdown", Valid: true}) {
		t.Errorf("expected markdown language, got %v", docs[0].Language)
	}
}

func TestEditDocument(t *testing.T) {
	store := openTestDB(t)
	_, err := DoCreateDocument(context.Background(),
		"Edit Test\nmarkdown\nHello world, this is a test.", "", "admin", store)
	if err != nil {
		t.Fatalf("DoCreateDocument: %v", err)
	}
	docs, _ := store.ListDocuments("admin", false)
	docID := docs[0].ID

	editBody := docID + "\n<<<FIND>>>\nHello world\n<<<REPLACE>>>\nGoodbye world\n<<<END>>>"
	result, err := DoEditDocument(context.Background(), editBody, store)
	if err != nil {
		t.Fatalf("DoEditDocument: %v", err)
	}
	if !strings.Contains(result, "1 edit") {
		t.Errorf("expected 1 edit in result, got %q", result)
	}
	doc, _ := store.GetDocument(docID)
	if !strings.Contains(doc.CurrentContent, "Goodbye world") {
		t.Errorf("expected 'Goodbye world' in content, got %q", doc.CurrentContent)
	}
	if doc.VersionCount != 2 {
		t.Errorf("expected version 2, got %d", doc.VersionCount)
	}
}

func TestLanguageSniffing(t *testing.T) {
	cases := []struct {
		content  string
		expected string
	}{
		{"# Hello\nThis is markdown", "markdown"},
		{"def foo():\n    pass", "python"},
		{"const x = 1;\nfunction bar() {}", "javascript"},
		{"SELECT * FROM users", "sql"},
		{`{"key": "value"}`, "json"},
		{"<html><body></body></html>", "html"},
	}
	for _, tc := range cases {
		got := sniffLanguage(tc.content)
		if got != tc.expected {
			t.Errorf("sniffLanguage(%q) = %q, want %q", tc.content[:20], got, tc.expected)
		}
	}
}
