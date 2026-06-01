package db

import (
	"database/sql"
	"fmt"
	"time"
)

func (db *DB) CreateAPIToken(t *APIToken) error {
	_, err := db.Exec(`INSERT INTO api_tokens (id,owner,name,token_hash,token_prefix,scopes,is_active,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Owner, t.Name, t.TokenHash, t.TokenPrefix, t.Scopes, boolInt(t.IsActive), now(), now(),
	)
	return err
}

func (db *DB) GetAPIToken(id string) (*APIToken, error) {
	row := db.QueryRow(`SELECT id,owner,name,token_hash,token_prefix,scopes,is_active,last_used_at,created_at,updated_at
		FROM api_tokens WHERE id=?`, id)
	return scanAPIToken(row)
}

func (db *DB) ListAPITokens(owner string, isAdmin bool) ([]*APIToken, error) {
	var rows *sql.Rows
	var err error
	if isAdmin {
		rows, err = db.Query(`SELECT id,owner,name,token_hash,token_prefix,scopes,is_active,last_used_at,created_at,updated_at
			FROM api_tokens ORDER BY created_at DESC`)
	} else {
		rows, err = db.Query(`SELECT id,owner,name,token_hash,token_prefix,scopes,is_active,last_used_at,created_at,updated_at
			FROM api_tokens WHERE owner=? ORDER BY created_at DESC`, owner)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []*APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (db *DB) DeleteAPIToken(id string) error {
	_, err := db.Exec(`UPDATE api_tokens SET is_active=0,updated_at=? WHERE id=?`, now(), id)
	return err
}

func (db *DB) UpdateAPITokenLastUsed(id string) error {
	_, err := db.Exec(`UPDATE api_tokens SET last_used_at=? WHERE id=?`, now(), id)
	return err
}

func (db *DB) GetActiveTokensByPrefix(prefix string) ([]*APIToken, error) {
	rows, err := db.Query(`SELECT id,owner,name,token_hash,token_prefix,scopes,is_active,last_used_at,created_at,updated_at
		FROM api_tokens WHERE token_prefix=? AND is_active=1`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []*APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func scanAPIToken(row scanner) (*APIToken, error) {
	t := &APIToken{}
	var isActive int
	var createdAt, updatedAt string
	err := row.Scan(&t.ID, &t.Owner, &t.Name, &t.TokenHash, &t.TokenPrefix,
		&t.Scopes, &isActive, &t.LastUsedAt, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("token not found")
		}
		return nil, err
	}
	t.IsActive = isActive != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}
