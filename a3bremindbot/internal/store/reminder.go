package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Reminder represents a reminder template created by a user.
type Reminder struct {
	ID        string
	UserID    string
	Label     string
	Times     []string
	MinGap    *int
	Repeat    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Create inserts a new reminder.
func Create(db *sql.DB, r Reminder) (Reminder, error) {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	now := time.Now().Unix()
	r.CreatedAt = time.Unix(now, 0)
	r.UpdatedAt = time.Unix(now, 0)

	timesJSON, err := json.Marshal(r.Times)
	if err != nil {
		return Reminder{}, fmt.Errorf("marshal times: %w", err)
	}

	const query = `INSERT INTO reminders (id, user_id, label, times, min_gap, repeat, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = db.Exec(query, r.ID, r.UserID, r.Label, string(timesJSON), r.MinGap, r.Repeat, now, now)
	if err != nil {
		return Reminder{}, fmt.Errorf("create reminder: %w", err)
	}

	return r, nil
}

// GetAll retrieves all reminders for a user.
func GetAll(db *sql.DB, userID string) ([]Reminder, error) {
	const query = `SELECT id, user_id, label, times, min_gap, repeat, created_at, updated_at
		FROM reminders WHERE user_id = ?`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("get all reminders: %w", err)
	}
	defer rows.Close()

	var result []Reminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	// Return empty slice instead of nil for consistent JSON serialization.
	if result == nil {
		result = []Reminder{}
	}

	return result, nil
}

// GetByID retrieves a reminder by its ID.
func GetByID(db *sql.DB, id string) (Reminder, error) {
	const query = `SELECT id, user_id, label, times, min_gap, repeat, created_at, updated_at
		FROM reminders WHERE id = ?`

	row := db.QueryRow(query, id)
	return scanReminder(row)
}

// Update updates a reminder's mutable fields and sets updated_at to now.
func Update(db *sql.DB, r Reminder) error {
	now := time.Now().Unix()

	timesJSON, err := json.Marshal(r.Times)
	if err != nil {
		return fmt.Errorf("marshal times: %w", err)
	}

	const query = `UPDATE reminders SET label = ?, times = ?, min_gap = ?, repeat = ?, updated_at = ?
		WHERE id = ?`

	res, err := db.Exec(query, r.Label, string(timesJSON), r.MinGap, r.Repeat, now, r.ID)
	if err != nil {
		return fmt.Errorf("update reminder: %w", err)
	}
	return checkRowsAffected(res, "reminder", r.ID)
}

// Delete removes a reminder by ID.
func Delete(db *sql.DB, id string) error {
	const query = `DELETE FROM reminders WHERE id = ?`
	res, err := db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	return checkRowsAffected(res, "reminder", id)
}

// scanReminder scans a single Reminder from a row scanner.
func scanReminder(row scannable) (Reminder, error) {
	var r Reminder
	var timesJSON string
	var minGap sql.NullInt64
	var createdAt, updatedAt int64

	if err := row.Scan(&r.ID, &r.UserID, &r.Label, &timesJSON, &minGap, &r.Repeat, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return Reminder{}, fmt.Errorf("reminder not found")
		}
		return Reminder{}, fmt.Errorf("scan reminder: %w", err)
	}

	if minGap.Valid {
		v := int(minGap.Int64)
		r.MinGap = &v
	}

	if err := json.Unmarshal([]byte(timesJSON), &r.Times); err != nil {
		return Reminder{}, fmt.Errorf("unmarshal times: %w", err)
	}

	r.CreatedAt = time.Unix(createdAt, 0)
	r.UpdatedAt = time.Unix(updatedAt, 0)

	return r, nil
}
