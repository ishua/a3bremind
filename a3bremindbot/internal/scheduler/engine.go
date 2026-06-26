package scheduler

import (
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
)

// Engine defines the domain operations needed by the scheduler.
// The domain package provides the implementation.
type Engine interface {
	// Tick processes pending instances and daily resets, returning notifications to send.
	Tick(now time.Time) ([]domain.Notification, error)
	// RecordSent is called after a notification was successfully sent.
	RecordSent(notification domain.Notification, messageID int, sentAt time.Time) error
}
