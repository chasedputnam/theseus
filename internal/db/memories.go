package db

import (
	"fmt"
	"strings"
	"time"
)

// AddMemory inserts a memory entry.
func (db *DB) AddMemory(m *Memory) error {
	_, err := db.Exec(`INSERT INTO memories (id,text,category,source,owner,session_id,timestamp)
		VALUES (?,?,?,?,?,?,?)`,
		m.ID, m.Text, m.Category, m.Source, m.Owner, m.SessionID, m.Timestamp,
	)
	return err
}

// GetMemory returns a memory by ID.
func (db *DB) GetMemory(id string) (*Memory, error) {
	row := db.QueryRow(`SELECT id,text,category,source,owner,session_id,timestamp,pinned FROM memories WHERE id=?`, id)
	return scanMemory(row)
}

// ListMemories returns all memories for an owner.
func (db *DB) ListMemories(owner string) ([]*Memory, error) {
	q := `SELECT id,text,category,source,owner,session_id,timestamp,pinned FROM memories`
	args := []any{}
	if owner != "" {
		q += " WHERE owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY timestamp DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// SearchMemories performs keyword search over memory text.
func (db *DB) SearchMemories(query, owner string, limit int) ([]*Memory, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return db.ListMemories(owner)
	}
	// Build LIKE conditions for each word
	conditions := make([]string, len(words))
	args := make([]any, len(words))
	for i, w := range words {
		conditions[i] = "LOWER(text) LIKE ?"
		args[i] = "%" + w + "%"
	}
	q := `SELECT id,text,category,source,owner,session_id,timestamp,pinned FROM memories WHERE ` +
		strings.Join(conditions, " OR ")
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	q += fmt.Sprintf(" ORDER BY timestamp DESC LIMIT %d", limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// GetMemoriesByIDs returns memories matching the given IDs.
func (db *DB) GetMemoriesByIDs(ids []string, owner string) ([]*Memory, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := `SELECT id,text,category,source,owner,session_id,timestamp,pinned FROM memories WHERE id IN (` + placeholders + `)`
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// DeleteMemory removes a memory entry.
func (db *DB) DeleteMemory(id, owner string) error {
	if owner != "" {
		_, err := db.Exec(`DELETE FROM memories WHERE id=? AND owner=?`, id, owner)
		return err
	}
	_, err := db.Exec(`DELETE FROM memories WHERE id=?`, id)
	return err
}

// PinMemory toggles the pinned flag on a memory.
func (db *DB) PinMemory(id, owner string, pinned bool) error {
	val := 0
	if pinned {
		val = 1
	}
	_, err := db.Exec(`UPDATE memories SET pinned=? WHERE id=? AND owner=?`, val, id, owner)
	return err
}

func scanMemory(row scanner) (*Memory, error) {
	m := &Memory{}
	var pinned int
	err := row.Scan(&m.ID, &m.Text, &m.Category, &m.Source, &m.Owner, &m.SessionID, &m.Timestamp, &pinned)
	if err != nil {
		return nil, err
	}
	m.Pinned = pinned != 0
	return m, nil
}

func scanMemories(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*Memory, error) {
	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var pinned int
		if err := rows.Scan(&m.ID, &m.Text, &m.Category, &m.Source, &m.Owner, &m.SessionID, &m.Timestamp, &pinned); err != nil {
			return nil, err
		}
		m.Pinned = pinned != 0
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

var _ = time.Now // keep import
