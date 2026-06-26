package domain

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// DailyReset creates instances for all daily reminders of a user for today.
func DailyReset(db *sql.DB, userID string, now time.Time) error {
	reminders, err := store.GetAll(db, userID)
	if err != nil {
		return fmt.Errorf("get reminders for daily reset: %w", err)
	}

	user, err := store.GetUserByID(db, userID)
	if err != nil {
		return fmt.Errorf("get user for daily reset: %w", err)
	}

	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return fmt.Errorf("load location %q: %w", user.Timezone, err)
	}

	localNow := now.In(loc)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)

	for _, r := range reminders {
		if r.Repeat != "daily" {
			continue
		}
		if len(r.Times) == 0 {
			continue
		}

		// Parse the first time of the day.
		firstTime := r.Times[0]
		scheduledAt, err := time.ParseInLocation("15:04", firstTime, loc)
		if err != nil {
			slog.Error("daily reset: invalid time", "reminder_id", r.ID, "time", firstTime, "error", err)
			continue
		}

		// Combine today's date with the first time.
		instanceTime := time.Date(
			todayStart.Year(), todayStart.Month(), todayStart.Day(),
			scheduledAt.Hour(), scheduledAt.Minute(), 0, 0,
			loc,
		)

		inst := store.ReminderInstance{
			ReminderID:  r.ID,
			TimeIndex:   0,
			ScheduledAt: instanceTime,
			Status:      "pending",
		}

		if _, err := store.CreateInstance(db, inst); err != nil {
			slog.Error("daily reset: create instance", "reminder_id", r.ID, "error", err)
			continue
		}
	}

	// Update last_reset_at.
	if err := store.SetLastResetAt(db, userID, now); err != nil {
		return fmt.Errorf("set last_reset_at: %w", err)
	}

	return nil
}

// checkDailyReset checks if any user needs a daily reset and triggers it.
func (s *Scheduler) checkDailyReset(now time.Time) {
	users, err := store.GetAllUsers(s.db)
	if err != nil {
		slog.Error("get all users for daily reset", "error", err)
		return
	}

	for _, user := range users {
		if user.Timezone == "" {
			continue
		}

		loc, err := time.LoadLocation(user.Timezone)
		if err != nil {
			slog.Error("load location for user", "user_id", user.ID, "error", err)
			continue
		}

		localNow := now.In(loc)

		// Only trigger at the reset hour, minute 0.
		if localNow.Hour() != ResetHour || localNow.Minute() != 0 {
			continue
		}

		// Check if already reset today (in user's timezone).
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)

		if user.LastResetAt != nil {
			lastResetLocal := user.LastResetAt.In(loc)
			lastResetDay := time.Date(lastResetLocal.Year(), lastResetLocal.Month(), lastResetLocal.Day(), 0, 0, 0, 0, loc)
			if !lastResetDay.Before(today) {
				// Already reset today.
				continue
			}
		}

		if err := DailyReset(s.db, user.ID, now); err != nil {
			slog.Error("daily reset for user", "user_id", user.ID, "error", err)
		}
	}
}
