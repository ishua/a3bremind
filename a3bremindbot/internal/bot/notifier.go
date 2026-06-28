package bot

import (
	"fmt"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Notifier реализует scheduler.Notifier через Telegram Bot API.
// Формирует текст из структурированных domain.Notification.
type Notifier struct {
	bot BotAPI
}

func NewNotifier(bot BotAPI) *Notifier {
	return &Notifier{bot: bot}
}

// Notify отправляет структурированное уведомление пользователю через Telegram
// с inline-кнопками ✅ Done / ⏰ Done at... / 💤 Snooze / ⏭ Skip.
func (n *Notifier) Notify(recipientID int64, notification domain.Notification) (int, time.Time, error) {
	text := formatNotificationText(notification)
	msg := tgbotapi.NewMessage(recipientID, text)

	// Inline-кнопки: Done, Done at, Snooze, Skip
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Done", "done:"+notification.InstanceID),
			tgbotapi.NewInlineKeyboardButtonData("⏰ Done at...", "done_time:"+notification.InstanceID),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💤 Snooze", "snooze:"+notification.InstanceID),
			tgbotapi.NewInlineKeyboardButtonData("⏭ Skip", "skip:"+notification.InstanceID),
		),
	)
	msg.ReplyMarkup = keyboard

	sent, err := n.bot.Send(msg)
	if err != nil {
		return 0, time.Time{}, err
	}
	return sent.MessageID, time.Now(), nil
}

// formatNotificationText формирует читаемый текст из Notification.
func formatNotificationText(notification domain.Notification) string {
	switch notification.Type {
	case domain.NotificationFirst:
		return fmt.Sprintf("⏰ %s · %s",
			formatTimeInTimezone(notification.ScheduledAt, notification.Timezone),
			notification.Label)
	case domain.NotificationRepeat:
		return fmt.Sprintf("🔔 Напоминаю: %s (попытка %d/%d)",
			notification.Label, notification.Attempt, notification.MaxAttempts)
	default:
		return notification.Label
	}
}

func formatTimeInTimezone(t time.Time, timezone string) string {
	if timezone == "" {
		return t.Format("15:04")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return t.Format("15:04")
	}
	return t.In(loc).Format("15:04")
}
