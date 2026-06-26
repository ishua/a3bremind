package domain

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// processPending finds all pending instances and sends notifications as needed.
func (s *Scheduler) processPending(now time.Time) {
	instances, err := store.GetPending(s.db, now)
	if err != nil {
		slog.Error("get pending instances", "error", err)
		return
	}

	for _, inst := range instances {
		s.processInstance(inst, now)
	}
}

// processInstance handles a single pending instance.
func (s *Scheduler) processInstance(inst store.ReminderInstance, now time.Time) {
	reminder, err := store.GetByID(s.db, inst.ReminderID)
	if err != nil {
		slog.Error("get reminder", "reminder_id", inst.ReminderID, "error", err)
		return
	}

	user, err := store.GetUserByID(s.db, reminder.UserID)
	if err != nil {
		slog.Error("get user", "user_id", reminder.UserID, "error", err)
		return
	}

	// Check paused
	if user.Paused {
		return
	}

	msgCount := len(inst.MessageIDs)

	var text string

	switch {
	case msgCount == 0:
		// First notification.
		scheduledStr := inst.ScheduledAt.Format("15:04")
		text = fmt.Sprintf("⏰ %s · %s", scheduledStr, reminder.Label)

	case msgCount < RepeatCount:
		// Repeat notification — only if enough time has passed.
		lastEntry := inst.MessageIDs[msgCount-1]
		lastSentAt := time.Unix(lastEntry.SentAt, 0)
		if now.Sub(lastSentAt) < RepeatInterval {
			return // too early, skip this tick
		}
		attempt := msgCount + 1
		text = fmt.Sprintf("🔔 Напоминаю: %s (попытка %d/%d)", reminder.Label, attempt, RepeatCount)

	default:
		// Already >= RepeatCount notifications sent — nothing to do.
		return
	}

	// Send the notification.
	messageID, sentAt, err := s.notifier.SendMessage(user.TelegramID, text)
	if err != nil {
		slog.Error("send message", "telegram_id", user.TelegramID, "error", err)
		return
	}

	// Record the sent message.
	if msgCount+1 >= RepeatCount {
		// Last notification — atomically add message ID and mark as missed.
		if reminder.Repeat == "once" {
			if err := store.AddMessageID(s.db, inst.ID, messageID, sentAt); err != nil {
				slog.Error("add message id", "instance_id", inst.ID, "error", err)
				return
			}
			if err := store.MarkMissedAndDeleteOnce(s.db, inst.ID, reminder.ID); err != nil {
				slog.Error("mark missed and delete once reminder", "reminder_id", reminder.ID, "error", err)
			}
		} else {
			if err := store.AddMessageIDAndSetMissed(s.db, inst.ID, messageID, sentAt); err != nil {
				slog.Error("add message id and set missed", "instance_id", inst.ID, "error", err)
			}
		}
	} else {
		// Not the last repeat — just add the message ID.
		if err := store.AddMessageID(s.db, inst.ID, messageID, sentAt); err != nil {
			slog.Error("add message id", "instance_id", inst.ID, "error", err)
			return
		}
	}
}
