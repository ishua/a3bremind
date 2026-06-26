package domain

import "time"

// NotificationType indicates whether this is the first notification or a repeat.
type NotificationType int

const (
	NotificationFirst  NotificationType = iota // ⏰ first notification
	NotificationRepeat                         // 🔔 repeat notification
)

// Notification holds structured data about a notification event.
// Domain produces these; the bot layer consumes them to format messages.
type Notification struct {
	InstanceID     string
	ReminderID     string
	Label          string
	ScheduledAt    time.Time
	Attempt        int // which attempt this is (1-based)
	MaxAttempts    int
	Type           NotificationType
	UserID         string
	RecipientID    int64 // Telegram user ID
	Timezone       string
	ReminderRepeat string // "daily" or "once" — needed by RecordSent
}
