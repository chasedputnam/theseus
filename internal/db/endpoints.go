package db

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateModelEndpoint inserts a new endpoint.
func (db *DB) CreateModelEndpoint(e *ModelEndpoint) error {
	_, err := db.Exec(`INSERT INTO model_endpoints
		(id,name,base_url,api_key,is_enabled,hidden_models,cached_models,model_type,supports_tools,owner,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Name, e.BaseURL, e.APIKey, boolInt(e.IsEnabled),
		e.HiddenModels, e.CachedModels, e.ModelType, e.SupportsTools, e.Owner,
		now(), now(),
	)
	return err
}

// GetModelEndpoint returns an endpoint by ID.
func (db *DB) GetModelEndpoint(id string) (*ModelEndpoint, error) {
	row := db.QueryRow(`SELECT id,name,base_url,api_key,is_enabled,hidden_models,cached_models,
		model_type,supports_tools,owner,created_at,updated_at FROM model_endpoints WHERE id=?`, id)
	return scanEndpoint(row)
}

// ListModelEndpoints returns all endpoints visible to a user.
// Admins see all; regular users see owner=NULL or owner=themselves.
func (db *DB) ListModelEndpoints(owner string, isAdmin bool) ([]*ModelEndpoint, error) {
	var rows *sql.Rows
	var err error
	if isAdmin || owner == "" {
		rows, err = db.Query(`SELECT id,name,base_url,api_key,is_enabled,hidden_models,cached_models,
			model_type,supports_tools,owner,created_at,updated_at FROM model_endpoints ORDER BY name`)
	} else {
		rows, err = db.Query(`SELECT id,name,base_url,api_key,is_enabled,hidden_models,cached_models,
			model_type,supports_tools,owner,created_at,updated_at FROM model_endpoints
			WHERE owner IS NULL OR owner=? ORDER BY name`, owner)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var endpoints []*ModelEndpoint
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, e)
	}
	return endpoints, rows.Err()
}

// UpdateModelEndpoint updates mutable endpoint fields.
func (db *DB) UpdateModelEndpoint(e *ModelEndpoint) error {
	_, err := db.Exec(`UPDATE model_endpoints SET
		name=?,base_url=?,api_key=?,is_enabled=?,hidden_models=?,cached_models=?,
		model_type=?,supports_tools=?,owner=?,updated_at=? WHERE id=?`,
		e.Name, e.BaseURL, e.APIKey, boolInt(e.IsEnabled),
		e.HiddenModels, e.CachedModels, e.ModelType, e.SupportsTools, e.Owner,
		now(), e.ID,
	)
	return err
}

// DeleteModelEndpoint removes an endpoint.
func (db *DB) DeleteModelEndpoint(id string) error {
	_, err := db.Exec(`DELETE FROM model_endpoints WHERE id=?`, id)
	return err
}

func scanEndpoint(row scanner) (*ModelEndpoint, error) {
	e := &ModelEndpoint{}
	var isEnabled int
	var createdAt, updatedAt string
	err := row.Scan(
		&e.ID, &e.Name, &e.BaseURL, &e.APIKey, &isEnabled,
		&e.HiddenModels, &e.CachedModels, &e.ModelType, &e.SupportsTools, &e.Owner,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("endpoint not found")
		}
		return nil, err
	}
	e.IsEnabled = isEnabled != 0
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return e, nil
}
