// Package store provides the SQLite data access layer for a3bremindbot.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// InitDB opens a SQLite database and runs automigration.
// driverName is typically "sqlite", dataSourceName is the file path or ":memory:".
func InitDB(driverName, dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	// Enable WAL mode for better concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			telegram_id INTEGER UNIQUE NOT NULL,
			timezone TEXT NOT NULL DEFAULT '',
			paused INTEGER NOT NULL DEFAULT 0,
			last_reset_at INTEGER,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS reminders (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			label TEXT NOT NULL,
			times TEXT NOT NULL,
			min_gap INTEGER,
			repeat TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS reminder_instances (
			id TEXT PRIMARY KEY,
			reminder_id TEXT NOT NULL REFERENCES reminders(id),
			for_date INTEGER NOT NULL,
			time_index INTEGER NOT NULL,
			scheduled_at INTEGER NOT NULL,
			done_at INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			message_ids TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS instance_replies (
			reply_message_id INTEGER PRIMARY KEY,
			instance_id TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}

	// Create indexes for performance.
	indexStatements := []string{
		`CREATE INDEX IF NOT EXISTS idx_instances_reminder_id ON reminder_instances(reminder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_instances_scheduled_at_status ON reminder_instances(scheduled_at, status)`,
		`CREATE INDEX IF NOT EXISTS idx_instances_reminder_for_date ON reminder_instances(reminder_id, for_date)`,
		`CREATE INDEX IF NOT EXISTS idx_instance_replies_message_id ON instance_replies(reply_message_id)`,
	}
	for _, stmt := range indexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}

	return nil
}
