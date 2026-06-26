package scheduler

import (
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
)

// Notifier sends structured notifications to users.
// The bot package provides the implementation.
type Notifier interface {
	Notify(recipientID int64, notification domain.Notification) (messageID int, sentAt time.Time, err error)
}
