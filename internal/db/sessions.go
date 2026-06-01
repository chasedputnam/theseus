package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// CreateSession inserts a new session row.
func (db *DB) CreateSession(s *Session) error {
	headers, _ := json.Marshal(s.Headers)
	_, err := db.Exec(`
		INSERT INTO sessions (id, name, endpoint_url, model, owner, rag, archived, folder, headers,
			last_accessed, message_count, is_important, mode, crew_member_id,
			total_input_tokens, total_output_tokens, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.EndpointURL, s.Model,
		s.Owner, boolInt(s.RAG), boolInt(s.Archived), s.Folder,
		string(headers), now(), 0, boolInt(s.IsImportant),
		s.Mode, s.CrewMemberID, 0, 0, now(), now(),
	)
	return err
}

// GetSession returns a session by ID.
func (db *DB) GetSession(id string) (*Session, error) {
	row := db.QueryRow(`SELECT id,name,endpoint_url,model,owner,rag,archived,folder,headers,
		last_accessed,last_message_at,message_count,is_important,mode,crew_member_id,
		total_input_tokens,total_output_tokens,created_at,updated_at FROM sessions WHERE id=?`, id)
	return scanSession(row)
}

// ListSessions returns sessions for an owner (or all if owner is empty).
func (db *DB) ListSessions(owner string, includeArchived bool) ([]*Session, error) {
	q := `SELECT id,name,endpoint_url,model,owner,rag,archived,folder,headers,
		last_accessed,last_message_at,message_count,is_important,mode,crew_member_id,
		total_input_tokens,total_output_tokens,created_at,updated_at FROM sessions WHERE 1=1`
	args := []any{}
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	if !includeArchived {
		q += " AND archived=0"
	}
	q += " ORDER BY last_accessed DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// UpdateSession updates mutable session fields.
func (db *DB) UpdateSession(s *Session) error {
	headers, _ := json.Marshal(s.Headers)
	_, err := db.Exec(`UPDATE sessions SET name=?,endpoint_url=?,model=?,rag=?,archived=?,
		folder=?,headers=?,is_important=?,mode=?,crew_member_id=?,updated_at=? WHERE id=?`,
		s.Name, s.EndpointURL, s.Model, boolInt(s.RAG), boolInt(s.Archived),
		s.Folder, string(headers), boolInt(s.IsImportant), s.Mode, s.CrewMemberID,
		now(), s.ID,
	)
	return err
}

// DeleteSession removes a session and cascades to messages.
func (db *DB) DeleteSession(id string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

// TouchSession updates last_accessed.
func (db *DB) TouchSession(id string) error {
	_, err := db.Exec(`UPDATE sessions SET last_accessed=? WHERE id=?`, now(), id)
	return err
}

// AddMessage inserts a chat message and updates session counters.
func (db *DB) AddMessage(m *ChatMessage) error {
	meta := ""
	if m.Metadata.Valid {
		meta = m.Metadata.String
	}
	_, err := db.Exec(`INSERT INTO chat_messages (id,session_id,role,content,metadata,timestamp)
		VALUES (?,?,?,?,?,?)`,
		m.ID, m.SessionID, m.Role, m.Content, meta, m.Timestamp,
	)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE sessions SET
		message_count = message_count + 1,
		last_message_at = ?,
		last_accessed = ?,
		updated_at = ?
		WHERE id=?`, m.Timestamp, now(), now(), m.SessionID)
	return err
}

// UpdateSessionTokens adds to the token counters.
func (db *DB) UpdateSessionTokens(id string, inputTokens, outputTokens int) error {
	_, err := db.Exec(`UPDATE sessions SET
		total_input_tokens = total_input_tokens + ?,
		total_output_tokens = total_output_tokens + ?
		WHERE id=?`, inputTokens, outputTokens, id)
	return err
}

// ListMessages returns all messages for a session ordered by timestamp.
func (db *DB) ListMessages(sessionID string) ([]*ChatMessage, error) {
	rows, err := db.Query(`SELECT id,session_id,role,content,metadata,timestamp
		FROM chat_messages WHERE session_id=? ORDER BY timestamp ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []*ChatMessage
	for rows.Next() {
		m := &ChatMessage{}
		var ts string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Metadata, &ts); err != nil {
			return nil, err
		}
		m.Timestamp, _ = time.Parse(time.RFC3339, ts)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// DeleteMessages removes all messages for a session.
func (db *DB) DeleteMessages(sessionID string) error {
	_, err := db.Exec(`DELETE FROM chat_messages WHERE session_id=?`, sessionID)
	return err
}

// scanSession scans a session row from either *sql.Row or *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (*Session, error) {
	s := &Session{}
	var (
		rag, archived, isImportant int
		headersStr                 string
		lastAccessed               string
		lastMessageAt              sql.NullString
		createdAt, updatedAt       string
	)
	err := row.Scan(
		&s.ID, &s.Name, &s.EndpointURL, &s.Model,
		&s.Owner, &rag, &archived, &s.Folder, &headersStr,
		&lastAccessed, &lastMessageAt,
		&s.MessageCount, &isImportant, &s.Mode, &s.CrewMemberID,
		&s.TotalInputTokens, &s.TotalOutputTokens,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, err
	}
	s.RAG = rag != 0
	s.Archived = archived != 0
	s.IsImportant = isImportant != 0
	s.Headers = headersStr
	s.LastAccessedAt, _ = time.Parse(time.RFC3339, lastAccessed)
	if lastMessageAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastMessageAt.String)
		s.LastMessageAt = sql.NullTime{Time: t, Valid: true}
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return s, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
