package domain

import "time"

// Notifier defines the interface for sending messages to users.
// Domain knows nothing about Telegram — it only uses User.TelegramID as a recipient identifier.
// The real implementation will be provided in Phase 3 (bot).
type Notifier interface {
	SendMessage(telegramID int64, text string) (messageID int, sentAt time.Time, err error)
}
