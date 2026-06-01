package db

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateDocument inserts a new document row.
func (db *DB) CreateDocument(d *Document) error {
	_, err := db.Exec(`INSERT INTO documents
		(id,session_id,title,language,current_content,version_count,is_active,archived,owner,
		 tidy_verdict,source_email_uid,source_email_folder,source_email_account_id,source_email_message_id,
		 created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.SessionID, d.Title, d.Language, d.CurrentContent,
		d.VersionCount, boolInt(d.IsActive), boolInt(d.Archived), d.Owner,
		d.TidyVerdict, d.SourceEmailUID, d.SourceEmailFolder,
		d.SourceEmailAccountID, d.SourceEmailMessageID,
		now(), now(),
	)
	return err
}

// GetDocument returns a document by ID.
func (db *DB) GetDocument(id string) (*Document, error) {
	row := db.QueryRow(`SELECT id,session_id,title,language,current_content,version_count,
		is_active,archived,owner,tidy_verdict,source_email_uid,source_email_folder,
		source_email_account_id,source_email_message_id,created_at,updated_at
		FROM documents WHERE id=?`, id)
	return scanDocument(row)
}

// UpdateDocument updates mutable document fields.
func (db *DB) UpdateDocument(d *Document) error {
	_, err := db.Exec(`UPDATE documents SET
		title=?,language=?,current_content=?,version_count=?,is_active=?,archived=?,
		tidy_verdict=?,updated_at=? WHERE id=?`,
		d.Title, d.Language, d.CurrentContent, d.VersionCount,
		boolInt(d.IsActive), boolInt(d.Archived), d.TidyVerdict,
		now(), d.ID,
	)
	return err
}

// DeleteDocument removes a document and its versions.
func (db *DB) DeleteDocument(id string) error {
	_, err := db.Exec(`DELETE FROM documents WHERE id=?`, id)
	return err
}

// ListDocuments returns documents for an owner.
func (db *DB) ListDocuments(owner string, includeArchived bool) ([]*Document, error) {
	q := `SELECT id,session_id,title,language,current_content,version_count,
		is_active,archived,owner,tidy_verdict,source_email_uid,source_email_folder,
		source_email_account_id,source_email_message_id,created_at,updated_at
		FROM documents WHERE 1=1`
	args := []any{}
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	if !includeArchived {
		q += " AND archived=0"
	}
	q += " ORDER BY updated_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []*Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

// AddDocumentVersion inserts a version snapshot.
func (db *DB) AddDocumentVersion(v *DocumentVersion) error {
	_, err := db.Exec(`INSERT INTO document_versions
		(id,document_id,version_number,content,summary,source,created_at)
		VALUES (?,?,?,?,?,?,?)`,
		v.ID, v.DocumentID, v.VersionNumber, v.Content,
		v.Summary, v.Source, now(),
	)
	return err
}

// ListDocumentVersions returns all versions for a document.
func (db *DB) ListDocumentVersions(docID string) ([]*DocumentVersion, error) {
	rows, err := db.Query(`SELECT id,document_id,version_number,content,summary,source,created_at
		FROM document_versions WHERE document_id=? ORDER BY version_number ASC`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []*DocumentVersion
	for rows.Next() {
		v := &DocumentVersion{}
		var createdAt string
		if err := rows.Scan(&v.ID, &v.DocumentID, &v.VersionNumber, &v.Content,
			&v.Summary, &v.Source, &createdAt); err != nil {
			return nil, err
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func scanDocument(row scanner) (*Document, error) {
	d := &Document{}
	var (
		isActive, archived int
		createdAt, updatedAt string
	)
	err := row.Scan(
		&d.ID, &d.SessionID, &d.Title, &d.Language, &d.CurrentContent,
		&d.VersionCount, &isActive, &archived, &d.Owner, &d.TidyVerdict,
		&d.SourceEmailUID, &d.SourceEmailFolder, &d.SourceEmailAccountID,
		&d.SourceEmailMessageID, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("document not found")
		}
		return nil, err
	}
	d.IsActive = isActive != 0
	d.Archived = archived != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return d, nil
}
