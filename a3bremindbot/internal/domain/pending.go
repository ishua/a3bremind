package domain

import (
	"fmt"
	"log"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
)

// processPending finds all pending instances and sends notifications as needed.
func (s *Scheduler) processPending(now time.Time) {
	instances, err := store.GetPending(s.db, now)
	if err != nil {
		log.Printf("get pending instances: %v", err)
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
		log.Printf("get reminder %s: %v", inst.ReminderID, err)
		return
	}

	user, err := store.GetUserByID(s.db, reminder.UserID)
	if err != nil {
		log.Printf("get user %s: %v", reminder.UserID, err)
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
		log.Printf("send message to %d: %v", user.TelegramID, err)
		return
	}

	// Record the sent message.
	if err := store.AddMessageID(s.db, inst.ID, messageID, sentAt); err != nil {
		log.Printf("add message id for instance %s: %v", inst.ID, err)
		return
	}

	// If this was the last repeat, mark as missed.
	if msgCount+1 >= RepeatCount {
		if err := store.SetStatus(s.db, inst.ID, "missed"); err != nil {
			log.Printf("set status missed for instance %s: %v", inst.ID, err)
		}

		// If the reminder is "once", delete it and all its instances
		if reminder.Repeat == "once" {
			if err := store.DeleteReminderInstances(s.db, reminder.ID); err != nil {
				log.Printf("delete instances for once reminder %s: %v", reminder.ID, err)
			}
			if err := store.Delete(s.db, reminder.ID); err != nil {
				log.Printf("delete once reminder %s: %v", reminder.ID, err)
			}
			log.Printf("once reminder %s (%s) deleted after missed", reminder.ID, reminder.Label)
		}
	}
}
