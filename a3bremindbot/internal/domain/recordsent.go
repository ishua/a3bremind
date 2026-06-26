package domain

import (
	"log/slog"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// recordSent performs store operations after a notification was sent.
func (e *Engine) recordSent(notification Notification, messageID int, sentAt time.Time) error {
	// Record the reply mapping (reply_message_id -> instance_id).
	if err := store.InsertInstanceReply(e.db, messageID, notification.InstanceID); err != nil {
		return err
	}

	msgCount := notification.Attempt - 1 // before this attempt, that many were sent

	if msgCount+1 >= notification.MaxAttempts {
		// Last notification — atomically add message ID and mark as missed.
		if notification.ReminderRepeat == "once" {
			if err := store.AddMessageIDAndMarkMissedDeleteOnce(e.db, notification.InstanceID, notification.ReminderID, messageID, sentAt); err != nil {
				return err
			}
		} else {
			if err := store.AddMessageIDAndSetMissed(e.db, notification.InstanceID, messageID, sentAt); err != nil {
				return err
			}
		}
	} else {
		// Not the last repeat — just add the message ID.
		if err := store.AddMessageID(e.db, notification.InstanceID, messageID, sentAt); err != nil {
			return err
		}
	}

	slog.Info("notification recorded",
		"instance_id", notification.InstanceID,
		"message_id", messageID,
		"attempt", notification.Attempt,
		"max", notification.MaxAttempts,
	)
	return nil
}
