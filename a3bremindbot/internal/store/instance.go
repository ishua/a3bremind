package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MessageIDEntry holds a Telegram message ID and the unix timestamp when it was sent.
type MessageIDEntry struct {
	MessageID int   `json:"message_id"`
	SentAt    int64 `json:"sent_at"` // unix timestamp
}

// ReminderInstance represents a single occurrence of a reminder.
type ReminderInstance struct {
	ID          string
	ReminderID  string
	TimeIndex   int
	ScheduledAt time.Time
	DoneAt      *time.Time
	Status      string
	MessageIDs  []MessageIDEntry
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Create inserts a new reminder instance.
func CreateInstance(db Querier, i ReminderInstance) (ReminderInstance, error) {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	if i.Status == "" {
		i.Status = "pending"
	}
	if i.MessageIDs == nil {
		i.MessageIDs = []MessageIDEntry{}
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
func GetInstanceByID(db  Querier , id string) (ReminderInstance, error) {
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances WHERE id = ?`

	row := db.QueryRow(query, id)
	return scanReminderInstance(row)
}

// GetPending retrieves all pending instances whose scheduled_at <= now.
func GetPending(db  Querier , now time.Time) ([]ReminderInstance, error) {
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
func GetActiveByUser(db  Querier , userID string) ([]ReminderInstance, error) {
	const query = `SELECT ri.id, ri.reminder_id, ri.time_index, ri.scheduled_at, ri.done_at, ri.status, ri.message_ids, ri.created_at, ri.updated_at
		FROM reminder_instances ri
		JOIN reminders r ON r.id = ri.reminder_id
		WHERE r.user_id = ? AND ri.status = 'pending'
		ORDER BY ri.scheduled_at DESC`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("get active by user: %w", err)
	}
	defer rows.Close()

	return scanReminderInstances(rows)
}

// GetInstanceByMessageID retrieves a reminder instance by a message_id (for reply binding).
// Uses json_each to avoid a full table scan.
func GetInstanceByMessageID(db  Querier , messageID int) (ReminderInstance, error) {
	const query = `SELECT ri.id, ri.reminder_id, ri.time_index, ri.scheduled_at, ri.done_at, ri.status, ri.message_ids, ri.created_at, ri.updated_at
		FROM reminder_instances ri, json_each(ri.message_ids)
		WHERE json_extract(json_each.value, '$.message_id') = ?
		LIMIT 1`

	row := db.QueryRow(query, messageID)
	return scanReminderInstance(row)
}

// GetInstancesByUserAndDay retrieves all instances for a user on a specific day in their timezone.
// It computes the start/end of the day in the user's timezone and converts to UTC for the SQL query.
func GetInstancesByUserAndDay(db  Querier , userID string, date time.Time, loc *time.Location) ([]ReminderInstance, error) {
	year, month, day := date.In(loc).Date()
	startOfDay := time.Date(year, month, day, 0, 0, 0, 0, loc)
	endOfDay := startOfDay.Add(24 * time.Hour)

	const query = `SELECT ri.id, ri.reminder_id, ri.time_index, ri.scheduled_at, ri.done_at, ri.status, ri.message_ids, ri.created_at, ri.updated_at
		FROM reminder_instances ri
		JOIN reminders r ON r.id = ri.reminder_id
		WHERE r.user_id = ? AND ri.scheduled_at >= ? AND ri.scheduled_at < ?
		ORDER BY ri.scheduled_at ASC`

	rows, err := db.Query(query, userID, startOfDay.Unix(), endOfDay.Unix())
	if err != nil {
		return nil, fmt.Errorf("get instances by user and day: %w", err)
	}
	defer rows.Close()

	return scanReminderInstances(rows)
}

// SetInstanceScheduledAt updates the scheduled_at and updated_at fields of a reminder instance.
func SetInstanceScheduledAt(db  Querier , id string, t time.Time) error {
	const query = `UPDATE reminder_instances SET scheduled_at = ?, updated_at = ? WHERE id = ?`
	now := time.Now().Unix()
	res, err := db.Exec(query, t.Unix(), now, id)
	if err != nil {
		return fmt.Errorf("set instance scheduled_at: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// GetLastByReminder retrieves the last instance for a given reminder and time_index (for rescheduler).
func GetLastByReminder(db  Querier , reminderID string, timeIndex int) (ReminderInstance, error) {
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances
		WHERE reminder_id = ? AND time_index = ?
		ORDER BY scheduled_at DESC
		LIMIT 1`

	row := db.QueryRow(query, reminderID, timeIndex)
	return scanReminderInstance(row)
}

// SetStatus updates the status of a reminder instance.
// If status is "done", done_at is also set to the current time.
// If status is "missed", the update is conditional on the current status being "pending"
// to prevent races with the done handler. If the instance is already in a non-pending
// state, the update is silently skipped (no-op).
func SetStatus(db  Querier , id string, status string) error {
	now := time.Now().Unix()

	var query string
	var args []any
	if status == "done" {
		query = `UPDATE reminder_instances SET status = ?, done_at = ?, updated_at = ? WHERE id = ?`
		args = []any{status, now, now, id}
	} else if status == "missed" {
		// Only mark as missed if still pending — prevents race with done handler.
		query = `UPDATE reminder_instances SET status = ?, updated_at = ? WHERE id = ? AND status = 'pending'`
		args = []any{status, now, id}
		_, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("set status: %w", err)
		}
		// 0 rows affected is not an error — instance was already handled (e.g. done).
		return nil
	} else {
		query = `UPDATE reminder_instances SET status = ?, updated_at = ? WHERE id = ?`
		args = []any{status, now, id}
	}

	res, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// GetReminderInstancesByReminder retrieves all instances for a given reminder.
func GetReminderInstancesByReminder(db  Querier , reminderID string) ([]ReminderInstance, error) {
	const query = `SELECT id, reminder_id, time_index, scheduled_at, done_at, status, message_ids, created_at, updated_at
		FROM reminder_instances WHERE reminder_id = ?`

	rows, err := db.Query(query, reminderID)
	if err != nil {
		return nil, fmt.Errorf("get instances by reminder: %w", err)
	}
	defer rows.Close()

	return scanReminderInstances(rows)
}

// DeleteReminderInstances deletes all instances for a given reminder.
func DeleteReminderInstances(db  Querier , reminderID string) error {
	const query = `DELETE FROM reminder_instances WHERE reminder_id = ?`
	_, err := db.Exec(query, reminderID)
	if err != nil {
		return fmt.Errorf("delete reminder instances: %w", err)
	}
	return nil
}

// SetStatusWithDoneAt updates the status and done_at of a reminder instance.
// Intended for "done" status with a specific doneAt time.
func SetStatusWithDoneAt(db  Querier , id string, status string, doneAt time.Time) error {
	const query = `UPDATE reminder_instances SET status = ?, done_at = ?, updated_at = ? WHERE id = ?`
	now := time.Now().Unix()
	res, err := db.Exec(query, status, doneAt.Unix(), now, id)
	if err != nil {
		return fmt.Errorf("set status with done_at: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// MarkMissedAndDeleteOnce atomically marks an instance as "missed" and deletes all instances
// and the reminder itself in a single transaction. Intended for once-reminders to prevent
// race conditions between the scheduler and the done handler.
func MarkMissedAndDeleteOnce(db *sql.DB, instanceID, reminderID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// SetStatus("missed") — only if still pending (conditional update prevents race).
	now := time.Now().Unix()
	res, err := tx.Exec(`UPDATE reminder_instances SET status = 'missed', updated_at = ? WHERE id = ? AND status = 'pending'`, now, instanceID)
	if err != nil {
		return fmt.Errorf("set status missed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		// Instance was already handled by done handler — rollback silently.
		return nil
	}

	// Delete all instances for this reminder.
	if _, err := tx.Exec(`DELETE FROM reminder_instances WHERE reminder_id = ?`, reminderID); err != nil {
		return fmt.Errorf("delete reminder instances: %w", err)
	}

	// Delete the reminder itself.
	res, err = tx.Exec(`DELETE FROM reminders WHERE id = ?`, reminderID)
	if err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("reminder %q not found", reminderID)
	}

	return tx.Commit()
}

// AddMessageID appends a MessageIDEntry to the instance's message_ids JSON array atomically via json_set.
func AddMessageID(db Querier, id string, messageID int, sentAt time.Time) error {
	entry := MessageIDEntry{MessageID: messageID, SentAt: sentAt.Unix()}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal message_id entry: %w", err)
	}
	const query = `UPDATE reminder_instances SET message_ids = json_set(message_ids, '$[#]', json(?)), updated_at = ? WHERE id = ?`
	now := time.Now().Unix()
	res, err := db.Exec(query, string(entryJSON), now, id)
	if err != nil {
		return fmt.Errorf("add message_id: %w", err)
	}
	return checkRowsAffected(res, "reminder_instance", id)
}

// AddMessageIDAndMarkMissedDeleteOnce atomically adds a message ID, marks the instance as missed,
// and deletes the reminder (all instances + reminder row) in a single transaction.
// Intended for once-reminders to prevent race conditions between the scheduler and the done handler.
func AddMessageIDAndMarkMissedDeleteOnce(db *sql.DB, instanceID, reminderID string, messageID int, sentAt time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Add message ID and set missed atomically, only if still pending.
	entry := MessageIDEntry{MessageID: messageID, SentAt: sentAt.Unix()}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal message_id entry: %w", err)
	}

	now := time.Now().Unix()
	res, err := tx.Exec(`UPDATE reminder_instances SET message_ids = json_set(message_ids, '$[#]', json(?)), status = 'missed', updated_at = ? WHERE id = ? AND status = 'pending'`, string(entryJSON), now, instanceID)
	if err != nil {
		return fmt.Errorf("add message and set missed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		// Already handled by done handler — skip.
		return nil
	}

	// Delete all instances for this reminder.
	if _, err := tx.Exec(`DELETE FROM reminder_instances WHERE reminder_id = ?`, reminderID); err != nil {
		return fmt.Errorf("delete reminder instances: %w", err)
	}

	// Delete the reminder itself.
	res, err = tx.Exec(`DELETE FROM reminders WHERE id = ?`, reminderID)
	if err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("reminder %q not found", reminderID)
	}

	return tx.Commit()
}

// AddMessageIDAndSetMissed atomically adds a message ID and marks the instance as missed.
// This prevents races between the scheduler and the done handler.
func AddMessageIDAndSetMissed(db *sql.DB, instanceID string, messageID int, sentAt time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	entry := MessageIDEntry{MessageID: messageID, SentAt: sentAt.Unix()}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal message_id entry: %w", err)
	}

	now := time.Now().Unix()
	// Add message ID and set missed atomically, only if still pending.
	res, err := tx.Exec(`UPDATE reminder_instances SET message_ids = json_set(message_ids, '$[#]', json(?)), status = 'missed', updated_at = ? WHERE id = ? AND status = 'pending'`, string(entryJSON), now, instanceID)
	if err != nil {
		return fmt.Errorf("add message and set missed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		// Already handled by done handler — skip.
		return nil
	}

	return tx.Commit()
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
	if i.MessageIDs == nil {
		i.MessageIDs = []MessageIDEntry{}
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
