package domain

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// NextInstance creates the next instance in the chain after a done/skipped instance.
// It does nothing if the current instance is at the last time_index.
// It should only be called for instances with status "done" or "skipped", not "missed".
func NextInstance(db *sql.DB, inst store.ReminderInstance) error {
	reminder, err := store.GetByID(db, inst.ReminderID)
	if err != nil {
		return fmt.Errorf("get reminder for next instance: %w", err)
	}

	// If we're at the last index, the chain is complete.
	if inst.TimeIndex >= len(reminder.Times)-1 {
		return nil
	}

	nextIndex := inst.TimeIndex + 1

	// Compute scheduled_at: today's date + next time in user's timezone.
	user, err := store.GetUserByID(db, reminder.UserID)
	if err != nil {
		return fmt.Errorf("get user for next instance: %w", err)
	}

	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return fmt.Errorf("load location %q: %w", user.Timezone, err)
	}

	now := time.Now().In(loc)
	nextTime := reminder.Times[nextIndex]

	scheduledAt, err := time.ParseInLocation("15:04", nextTime, loc)
	if err != nil {
		return fmt.Errorf("parse time %q: %w", nextTime, err)
	}

	instanceTime := time.Date(
		now.Year(), now.Month(), now.Day(),
		scheduledAt.Hour(), scheduledAt.Minute(), 0, 0,
		loc,
	)

	newInst := store.ReminderInstance{
		ReminderID:  inst.ReminderID,
		TimeIndex:   nextIndex,
		ScheduledAt: instanceTime,
		Status:      "pending",
	}

	if _, err := store.CreateInstance(db, newInst); err != nil {
		return fmt.Errorf("create next instance: %w", err)
	}

	log.Printf("next instance created: reminder=%s time_index=%d->%d scheduled_at=%s",
		inst.ReminderID, inst.TimeIndex, nextIndex, instanceTime.Format(time.RFC3339))

	return nil
}
