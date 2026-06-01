package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps sql.DB with migration support.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at dsn and runs all migrations.
func Open(dsn string) (*DB, error) {
	dsn = strings.TrimPrefix(dsn, "sqlite:///")
	dsn = strings.TrimPrefix(dsn, "sqlite://")

	sqlDB, err := sql.Open("sqlite", dsn+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite serializes writes; limit to 1 writer + a small read pool
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	db := &DB{sqlDB}
	if err := db.Migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Migrate runs all schema statements idempotently.
func (db *DB) Migrate() error {
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column") ||
				strings.Contains(err.Error(), "already exists") {
				log.Printf("migration skip (already exists): %v", err)
				continue
			}
			preview := stmt
			if len(preview) > 120 {
				preview = preview[:120]
			}
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, preview)
		}
	}
	return nil
}
