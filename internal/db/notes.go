package db

import (
	"database/sql"
	"fmt"
	"time"
)

func (db *DB) CreateNote(n *Note) error {
	_, err := db.Exec(`INSERT INTO notes
		(id,owner,title,content,items,note_type,color,label,pinned,archived,due_date,
		 source,session_id,image_url,repeat,sort_order,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ID, n.Owner, n.Title, n.Content, n.Items, n.NoteType, n.Color, n.Label,
		boolInt(n.Pinned), boolInt(n.Archived), n.DueDate, n.Source, n.SessionID,
		n.ImageURL, n.Repeat, n.SortOrder, now(), now(),
	)
	return err
}

func (db *DB) GetNote(id string) (*Note, error) {
	row := db.QueryRow(`SELECT id,owner,title,content,items,note_type,color,label,pinned,archived,
		due_date,source,session_id,image_url,repeat,sort_order,created_at,updated_at
		FROM notes WHERE id=?`, id)
	return scanNote(row)
}

func (db *DB) ListNotes(owner string, includeArchived bool) ([]*Note, error) {
	q := `SELECT id,owner,title,content,items,note_type,color,label,pinned,archived,
		due_date,source,session_id,image_url,repeat,sort_order,created_at,updated_at
		FROM notes WHERE 1=1`
	args := []any{}
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	if !includeArchived {
		q += " AND archived=0"
	}
	q += " ORDER BY pinned DESC, sort_order ASC, updated_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []*Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

func (db *DB) UpdateNote(n *Note) error {
	_, err := db.Exec(`UPDATE notes SET
		title=?,content=?,items=?,note_type=?,color=?,label=?,pinned=?,archived=?,
		due_date=?,image_url=?,repeat=?,sort_order=?,updated_at=? WHERE id=?`,
		n.Title, n.Content, n.Items, n.NoteType, n.Color, n.Label,
		boolInt(n.Pinned), boolInt(n.Archived), n.DueDate, n.ImageURL,
		n.Repeat, n.SortOrder, now(), n.ID,
	)
	return err
}

func (db *DB) DeleteNote(id string) error {
	_, err := db.Exec(`DELETE FROM notes WHERE id=?`, id)
	return err
}

// UpdateNoteSortOrder sets the sort_order for a note.
func (db *DB) UpdateNoteSortOrder(id string, order int) error {
	_, err := db.Exec(`UPDATE notes SET sort_order=?,updated_at=? WHERE id=?`, order, now(), id)
	return err
}

func scanNote(row scanner) (*Note, error) {
	n := &Note{}
	var pinned, archived int
	var createdAt, updatedAt string
	err := row.Scan(
		&n.ID, &n.Owner, &n.Title, &n.Content, &n.Items, &n.NoteType,
		&n.Color, &n.Label, &pinned, &archived, &n.DueDate, &n.Source,
		&n.SessionID, &n.ImageURL, &n.Repeat, &n.SortOrder,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("note not found")
		}
		return nil, err
	}
	n.Pinned = pinned != 0
	n.Archived = archived != 0
	n.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	n.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return n, nil
}
