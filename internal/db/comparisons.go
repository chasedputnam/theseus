package db

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateComparison inserts a new comparison record.
func (db *DB) CreateComparison(c *Comparison) error {
	_, err := db.Exec(`INSERT INTO comparisons
		(id,session_id,owner,prompt,model_a,model_b,endpoint_a,endpoint_b,
		 is_blind,blind_mapping,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.SessionID, c.Owner, c.Prompt,
		c.ModelA, c.ModelB, c.EndpointA, c.EndpointB,
		boolInt(c.IsBlind), c.BlindMapping, now(), now(),
	)
	return err
}

// GetComparison returns a comparison by ID.
func (db *DB) GetComparison(id string) (*Comparison, error) {
	row := db.QueryRow(`SELECT id,session_id,owner,prompt,model_a,model_b,endpoint_a,endpoint_b,
		response_a,response_b,metrics_a,metrics_b,winner,is_blind,blind_mapping,voted_at,
		created_at,updated_at FROM comparisons WHERE id=?`, id)
	return scanComparison(row)
}

// UpdateComparison updates response/vote fields.
func (db *DB) UpdateComparison(c *Comparison) error {
	_, err := db.Exec(`UPDATE comparisons SET
		response_a=?,response_b=?,metrics_a=?,metrics_b=?,winner=?,voted_at=?,updated_at=?
		WHERE id=?`,
		c.ResponseA, c.ResponseB, c.MetricsA, c.MetricsB, c.Winner, c.VotedAt,
		now(), c.ID,
	)
	return err
}

// ListComparisons returns comparisons for an owner.
func (db *DB) ListComparisons(owner string, limit int) ([]*Comparison, error) {
	q := `SELECT id,session_id,owner,prompt,model_a,model_b,endpoint_a,endpoint_b,
		response_a,response_b,metrics_a,metrics_b,winner,is_blind,blind_mapping,voted_at,
		created_at,updated_at FROM comparisons`
	args := []any{}
	if owner != "" {
		q += " WHERE owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comps []*Comparison
	for rows.Next() {
		c, err := scanComparison(rows)
		if err != nil {
			return nil, err
		}
		comps = append(comps, c)
	}
	return comps, rows.Err()
}

func scanComparison(row scanner) (*Comparison, error) {
	c := &Comparison{}
	var isBlind int
	var createdAt, updatedAt string
	err := row.Scan(
		&c.ID, &c.SessionID, &c.Owner, &c.Prompt,
		&c.ModelA, &c.ModelB, &c.EndpointA, &c.EndpointB,
		&c.ResponseA, &c.ResponseB, &c.MetricsA, &c.MetricsB,
		&c.Winner, &isBlind, &c.BlindMapping, &c.VotedAt,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("comparison not found")
		}
		return nil, err
	}
	c.IsBlind = isBlind != 0
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return c, nil
}
