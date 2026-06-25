package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReminderInstance represents a single occurrence of a reminder.
type ReminderInstance struct {
	ID          string
	ReminderID  string
	TimeIndex   int
	ScheduledAt time.Time
	DoneAt      *time.Time
	Status      string
	MessageIDs  []int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Create inserts a new reminder instance.
func CreateInstance(db *sql.DB, i ReminderInstance) (ReminderInstance, error) {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	if i.Status == "" {
		i.Status = "pending"
	}
	if i.MessageIDs == nil {
		i.MessageIDs = []int{}
	}
	now := time.Now().Unix()
	i.CreatedAt = time.Unix(now, 0)
	i.UpdatedAt = time.Unix(now, 0)

	messageIDsJSON, err := json.Marshal(i.MessageIDs)
	if err != nil {
		return ReminderInstance{}, fmt.Errorf("marshal message_ids: %w", err)
	}

	const query = `INSERT INTO reminder_instances (id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = db.Exec(query, i.ID, i.ReminderID, i.TimeIndex, i.ScheduledAt.Unix(), nullInt64FromTimePtr(i.DoneAt), i.Status, string(messageIDsJSON), now, now)
	if err != nil {
		return ReminderInstance{}, fmt.Errorf("create instance: %w", err)
	}

	return i, nil
}

// GetInstanceByID retrieves a reminder instance by ID.
func GetInstanceByID(db *sql.DB, id string) (ReminderInstance, error) {
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances WHERE id = ?`

	row := db.QueryRow(query, id)
	return scanReminderInstance(row)
}

// GetPending retrieves all pending instances whose scheduled_at <= now.
func GetPending(db *sql.DB, now time.Time) ([]ReminderInstance, error) {
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances WHERE scheduled_at <= ? AND status = 'pending'`

	rows, err := db.Query(query, now.Unix())
	if err != nil {
		return nil, fmt.Errorf("get pending instances: %w", err)
	}
	defer rows.Close()

	return scanReminderInstances(rows)
}

// GetActiveByUser retrieves all pending instances for a user (for done without reply fallback).
func GetActiveByUser(db *sql.DB, userID string) ([]ReminderInstance, error) {
	const query = `SELECT ri.id, ri.reminder_id, ri.time_index, ri.scheduled_at, ri.done_at, ri.status, ri.message_ids, ri.created_at, ri.updated_at
		FROM reminder_instances ri
		JOIN reminders r ON r.id = ri.reminder_id
		WHERE r.user_id = ? AND ri.status = 'pending'`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("get active by user: %w", err)
	}
	defer rows.Close()

	return scanReminderInstances(rows)
}

// GetInstanceByMessageID retrieves a reminder instance by a message_id (for reply binding).
func GetInstanceByMessageID(db *sql.DB, messageID int) (ReminderInstance, error) {
	// We need to scan all instances and find the one containing this message_id,
	// since message_ids is stored as a JSON array.
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances`

	rows, err := db.Query(query)
	if err != nil {
		return ReminderInstance{}, fmt.Errorf("get by message_id: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		inst, err := scanReminderInstance(rows)
		if err != nil {
			return ReminderInstance{}, err
		}
		for _, mid := range inst.MessageIDs {
			if mid == messageID {
				return inst, nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return ReminderInstance{}, fmt.Errorf("rows iteration: %w", err)
	}

	return ReminderInstance{}, fmt.Errorf("instance with message_id %d not found", messageID)
}

// GetLastByReminder retrieves the last instance for a given reminder and time_index (for rescheduler).
func GetLastByReminder(db *sql.DB, reminderID string, timeIndex int) (ReminderInstance, error) {
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances
		WHERE reminder_id = ? AND time_index = ?
		ORDER BY scheduled_at DESC
		LIMIT 1`

	row := db.QueryRow(query, reminderID, timeIndex)
	return scanReminderInstance(row)
}

// SetStatus updates the status of a reminder instance.
func SetStatus(db *sql.DB, id string, status string) error {
	const query = `UPDATE reminder_instances SET status = ?, updated_at = ? WHERE id = ?`
	now := time.Now().Unix()
	res, err := db.Exec(query, status, now, id)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// SetDoneAt sets the done_at timestamp of a reminder instance.
func SetDoneAt(db *sql.DB, id string, t time.Time) error {
	const query = `UPDATE reminder_instances SET done_at = ?, updated_at = ? WHERE id = ?`
	now := time.Now().Unix()
	res, err := db.Exec(query, t.Unix(), now, id)
	if err != nil {
		return fmt.Errorf("set done_at: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// AddMessageID appends a message_id to the instance's message_ids JSON array atomically via json_set.
func AddMessageID(db *sql.DB, id string, messageID int) error {
	const query = `UPDATE reminder_instances SET message_ids = json_set(message_ids, '$[#]', ?), updated_at = ? WHERE id = ?`
	now := time.Now().Unix()
	res, err := db.Exec(query, messageID, now, id)
	if err != nil {
		return fmt.Errorf("add message_id: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// scanReminderInstance scans a single ReminderInstance from a row scanner.
func scanReminderInstance(row scannable) (ReminderInstance, error) {
	var i ReminderInstance
	var doneAt sql.NullInt64
	var scheduledAt, createdAt, updatedAt int64
	var messageIDsJSON string

	if err := row.Scan(&i.ID, &i.ReminderID, &i.TimeIndex, &scheduledAt, &doneAt, &i.Status, &messageIDsJSON, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return ReminderInstance{}, fmt.Errorf("reminder_instance not found")
		}
		return ReminderInstance{}, fmt.Errorf("scan reminder_instance: %w", err)
	}

	i.ScheduledAt = time.Unix(scheduledAt, 0)
	i.CreatedAt = time.Unix(createdAt, 0)
	i.UpdatedAt = time.Unix(updatedAt, 0)

	if doneAt.Valid {
		t := time.Unix(doneAt.Int64, 0)
		i.DoneAt = &t
	}

	if err := json.Unmarshal([]byte(messageIDsJSON), &i.MessageIDs); err != nil {
		return ReminderInstance{}, fmt.Errorf("unmarshal message_ids: %w", err)
	}

	return i, nil
}

// scanReminderInstances scans multiple ReminderInstances from rows.
func scanReminderInstances(rows *sql.Rows) ([]ReminderInstance, error) {
	var result []ReminderInstance
	for rows.Next() {
		inst, err := scanReminderInstance(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	if result == nil {
		result = []ReminderInstance{}
	}
	return result, nil
}

// nullInt64FromTimePtr converts a *time.Time to an interface{} suitable for db.Exec.
// Returns nil for SQL NULL, or the unix timestamp as int64.
func nullInt64FromTimePtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Unix()
}
