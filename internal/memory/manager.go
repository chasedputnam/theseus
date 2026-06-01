package memory

import (
	"log"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/db"
	"github.com/google/uuid"
)

// Manager handles memory storage and retrieval.
type Manager struct {
	db     *db.DB
	chroma *ChromaClient // nil if unavailable
}

// New creates a memory Manager.
func New(database *db.DB, chromaHost string, chromaPort int) *Manager {
	m := &Manager{db: database}
	if chromaHost != "" {
		m.chroma = NewChromaClient(fmt.Sprintf("http://%s:%d", chromaHost, chromaPort))
	}
	return m
}

// Add persists a memory entry to SQL (and optionally ChromaDB).
func (m *Manager) Add(ctx context.Context, text, category, source, owner, sessionID string) (*db.Memory, error) {
	entry := &db.Memory{
		ID:        uuid.New().String(),
		Text:      text,
		Category:  category,
		Source:    source,
		Owner:     sql.NullString{String: owner, Valid: owner != ""},
		SessionID: sql.NullString{String: sessionID, Valid: sessionID != ""},
		Timestamp: time.Now().Unix(),
	}
	if err := m.db.AddMemory(entry); err != nil {
		return nil, err
	}
	// Best-effort vector upsert — log but don't fail on ChromaDB errors
	if m.chroma != nil {
		if err := m.chroma.Upsert(ctx, entry.ID, text, map[string]string{
			"owner": owner, "category": category,
		}); err != nil {
			log.Printf("memory: chroma upsert %s: %v", entry.ID, err)
		}
	}
	return entry, nil
}

// Search retrieves relevant memories for a query.
// Uses ChromaDB vector search when available, falls back to SQL keyword search.
func (m *Manager) Search(ctx context.Context, query, owner string, limit int) ([]*db.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	if m.chroma != nil {
		ids, err := m.chroma.Query(ctx, query, owner, limit)
		if err == nil && len(ids) > 0 {
			return m.db.GetMemoriesByIDs(ids, owner)
		}
		// Fall through to keyword search on ChromaDB failure
	}
	return m.db.SearchMemories(query, owner, limit)
}

// Delete removes a memory entry.
func (m *Manager) Delete(ctx context.Context, id, owner string) error {
	if m.chroma != nil {
		_ = m.chroma.Delete(ctx, id)
	}
	return m.db.DeleteMemory(id, owner)
}

// List returns all memories for an owner.
func (m *Manager) List(ctx context.Context, owner string) ([]*db.Memory, error) {
	return m.db.ListMemories(owner)
}

// Import merges memories, skipping near-duplicates (Jaccard similarity > 0.8).
func (m *Manager) Import(ctx context.Context, entries []*db.Memory, owner string) (int, error) {
	existing, err := m.db.ListMemories(owner)
	if err != nil {
		return 0, fmt.Errorf("list existing memories: %w", err)
	}
	existingTexts := make([]string, len(existing))
	for i, e := range existing {
		existingTexts[i] = strings.ToLower(e.Text)
	}

	added := 0
	for _, entry := range entries {
		if isDuplicate(strings.ToLower(entry.Text), existingTexts) {
			continue
		}
		entry.ID = uuid.New().String()
		entry.Owner = sql.NullString{String: owner, Valid: owner != ""}
		if err := m.db.AddMemory(entry); err != nil {
			continue
		}
		existingTexts = append(existingTexts, strings.ToLower(entry.Text))
		added++
	}
	return added, nil
}

// isDuplicate returns true if text is similar to any existing text (Jaccard > 0.8).
func isDuplicate(text string, existing []string) bool {
	tokens := tokenize(text)
	for _, e := range existing {
		if jaccard(tokens, tokenize(e)) > 0.8 {
			return true
		}
	}
	return false
}

func tokenize(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,!?;\"'")
		if w != "" {
			set[w] = true
		}
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
