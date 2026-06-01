package db

import (
	"database/sql"
	"fmt"
	"time"
)

func (db *DB) CreateWebhook(w *Webhook) error {
	_, err := db.Exec(`INSERT INTO webhooks (id,name,url,secret,events,is_active,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		w.ID, w.Name, w.URL, w.Secret, w.Events, boolInt(w.IsActive), now(), now(),
	)
	return err
}

func (db *DB) GetWebhook(id string) (*Webhook, error) {
	row := db.QueryRow(`SELECT id,name,url,secret,events,is_active,last_triggered_at,
		last_status_code,last_error,created_at,updated_at FROM webhooks WHERE id=?`, id)
	return scanWebhook(row)
}

func (db *DB) ListWebhooks() ([]*Webhook, error) {
	rows, err := db.Query(`SELECT id,name,url,secret,events,is_active,last_triggered_at,
		last_status_code,last_error,created_at,updated_at FROM webhooks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hooks []*Webhook
	for rows.Next() {
		h, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (db *DB) UpdateWebhook(w *Webhook) error {
	_, err := db.Exec(`UPDATE webhooks SET name=?,url=?,secret=?,events=?,is_active=?,
		last_triggered_at=?,last_status_code=?,last_error=?,updated_at=? WHERE id=?`,
		w.Name, w.URL, w.Secret, w.Events, boolInt(w.IsActive),
		w.LastTriggeredAt, w.LastStatusCode, w.LastError, now(), w.ID,
	)
	return err
}

func (db *DB) DeleteWebhook(id string) error {
	_, err := db.Exec(`DELETE FROM webhooks WHERE id=?`, id)
	return err
}

func (db *DB) ListActiveWebhooksForEvent(event string) ([]*Webhook, error) {
	rows, err := db.Query(`SELECT id,name,url,secret,events,is_active,last_triggered_at,
		last_status_code,last_error,created_at,updated_at FROM webhooks
		WHERE is_active=1 AND (events LIKE ? OR events LIKE ? OR events LIKE ? OR events='*')`,
		event, event+",%", "%,"+event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hooks []*Webhook
	for rows.Next() {
		h, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func scanWebhook(row scanner) (*Webhook, error) {
	h := &Webhook{}
	var isActive int
	var createdAt, updatedAt string
	err := row.Scan(&h.ID, &h.Name, &h.URL, &h.Secret, &h.Events, &isActive,
		&h.LastTriggeredAt, &h.LastStatusCode, &h.LastError, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("webhook not found")
		}
		return nil, err
	}
	h.IsActive = isActive != 0
	h.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	h.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return h, nil
}
