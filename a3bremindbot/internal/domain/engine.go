package domain

import (
	"database/sql"
	"time"
)

// Engine implements the scheduler's Engine interface implicitly.
// It holds the DB connection and provides domain operations
// that the scheduler calls on each tick.
type Engine struct {
	db *sql.DB
}

// NewEngine creates a new Engine.
func NewEngine(db *sql.DB) *Engine {
	return &Engine{db: db}
}

// Tick processes pending instances and daily resets,
// returning notifications that should be sent.
func (e *Engine) Tick(now time.Time) ([]Notification, error) {
	notifications := e.processPending(now)
	e.checkDailyReset(now)
	return notifications, nil
}

// RecordSent is called by the scheduler after a notification was successfully sent.
func (e *Engine) RecordSent(notification Notification, messageID int, sentAt time.Time) error {
	return e.recordSent(notification, messageID, sentAt)
}
