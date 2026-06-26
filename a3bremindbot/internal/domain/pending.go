package domain

import (
	"log/slog"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// processPending finds all pending instances and returns notifications to send.
func (e *Engine) processPending(now time.Time) []Notification {
	instances, err := store.GetPending(e.db, now)
	if err != nil {
		slog.Error("get pending instances", "error", err)
		return nil
	}

	var notifications []Notification
	for _, inst := range instances {
		n := e.processInstance(inst, now)
		if n != nil {
			notifications = append(notifications, *n)
		}
	}
	return notifications
}

// processInstance checks a single pending instance.
// Returns a Notification if a message should be sent, nil otherwise.
func (e *Engine) processInstance(inst store.ReminderInstance, now time.Time) *Notification {
	reminder, err := store.GetByID(e.db, inst.ReminderID)
	if err != nil {
		slog.Error("get reminder", "reminder_id", inst.ReminderID, "error", err)
		return nil
	}

	user, err := store.GetUserByID(e.db, reminder.UserID)
	if err != nil {
		slog.Error("get user", "user_id", reminder.UserID, "error", err)
		return nil
	}

	// Check paused
	if user.Paused {
		return nil
	}

	msgCount := len(inst.MessageIDs)

	var n Notification

	switch {
	case msgCount == 0:
		// First notification.
		n = Notification{
			InstanceID:     inst.ID,
			ReminderID:     inst.ReminderID,
			Label:          reminder.Label,
			ScheduledAt:    inst.ScheduledAt,
			Attempt:        1,
			MaxAttempts:    RepeatCount,
			Type:           NotificationFirst,
			UserID:         user.ID,
			RecipientID:    user.TelegramID,
			Timezone:       user.Timezone,
			ReminderRepeat: reminder.Repeat,
		}

	case msgCount < RepeatCount:
		// Repeat notification — only if enough time has passed.
		lastEntry := inst.MessageIDs[msgCount-1]
		lastSentAt := time.Unix(lastEntry.SentAt, 0)
		if now.Sub(lastSentAt) < RepeatInterval {
			return nil // too early, skip this tick
		}
		attempt := msgCount + 1
		n = Notification{
			InstanceID:     inst.ID,
			ReminderID:     inst.ReminderID,
			Label:          reminder.Label,
			ScheduledAt:    inst.ScheduledAt,
			Attempt:        attempt,
			MaxAttempts:    RepeatCount,
			Type:           NotificationRepeat,
			UserID:         user.ID,
			RecipientID:    user.TelegramID,
			Timezone:       user.Timezone,
			ReminderRepeat: reminder.Repeat,
		}

	default:
		// Already >= RepeatCount notifications sent — nothing to do.
		return nil
	}

	return &n
}
