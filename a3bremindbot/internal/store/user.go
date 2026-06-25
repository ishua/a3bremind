package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// User represents a Telegram user of the bot.
type User struct {
	ID          string
	TelegramID  int64
	Timezone    string
	Paused      bool
	LastResetAt *time.Time
	CreatedAt   time.Time
}

// GetOrCreate upserts a user by telegram_id.
// If the user exists, it returns the existing user without error.
func GetOrCreate(db *sql.DB, telegramID int64) (User, error) {
	const insert = `INSERT OR IGNORE INTO users (id, telegram_id, timezone, paused, last_reset_at, created_at)
		VALUES (?, ?, '', 0, NULL, ?)`

	id := uuid.New().String()
	now := time.Now().Unix()

	if _, err := db.Exec(insert, id, telegramID, now); err != nil {
		return User{}, fmt.Errorf("insert or ignore user: %w", err)
	}

	return GetByTelegramID(db, telegramID)
}

// GetByTelegramID retrieves a user by telegram_id.
func GetByTelegramID(db *sql.DB, telegramID int64) (User, error) {
	const query = `SELECT id, telegram_id, timezone, paused, last_reset_at, created_at FROM users WHERE telegram_id = ?`

	row := db.QueryRow(query, telegramID)
	return scanUser(row)
}

// SetTimezone updates the timezone for a user.
func SetTimezone(db *sql.DB, userID string, tz string) error {
	const query = `UPDATE users SET timezone = ? WHERE id = ?`
	res, err := db.Exec(query, tz, userID)
	if err != nil {
		return fmt.Errorf("set timezone: %w", err)
	}
	return checkRowsAffected(res, "user", userID)
}

// SetPaused updates the paused flag for a user.
func SetPaused(db *sql.DB, userID string, paused bool) error {
	const query = `UPDATE users SET paused = ? WHERE id = ?`
	pausedInt := 0
	if paused {
		pausedInt = 1
	}
	res, err := db.Exec(query, pausedInt, userID)
	if err != nil {
		return fmt.Errorf("set paused: %w", err)
	}
	return checkRowsAffected(res, "user", userID)
}

// SetLastResetAt updates the last_reset_at timestamp for a user.
func SetLastResetAt(db *sql.DB, userID string, t time.Time) error {
	const query = `UPDATE users SET last_reset_at = ? WHERE id = ?`
	res, err := db.Exec(query, t.Unix(), userID)
	if err != nil {
		return fmt.Errorf("set last_reset_at: %w", err)
	}
	return checkRowsAffected(res, "user", userID)
}

// scanUser scans a single User from a row scanner.
func scanUser(row scannable) (User, error) {
	var u User
	var lastResetAt sql.NullInt64
	var createdAt int64

	if err := row.Scan(&u.ID, &u.TelegramID, &u.Timezone, &u.Paused, &lastResetAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return User{}, fmt.Errorf("user not found")
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}

	u.CreatedAt = time.Unix(createdAt, 0)

	if lastResetAt.Valid {
		t := time.Unix(lastResetAt.Int64, 0)
		u.LastResetAt = &t
	}

	return u, nil
}
