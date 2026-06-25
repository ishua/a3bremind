package bot

import (
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Notifier реализует domain.Notifier через Telegram Bot API.
type Notifier struct {
	bot BotAPI
}

func NewNotifier(bot BotAPI) *Notifier {
	return &Notifier{bot: bot}
}

// SendMessage отправляет текстовое сообщение пользователю через Telegram.
// Возвращает messageID, время отправки и ошибку.
func (n *Notifier) SendMessage(telegramID int64, text string) (int, time.Time, error) {
	msg := tgbotapi.NewMessage(telegramID, text)
	sent, err := n.bot.Send(msg)
	if err != nil {
		return 0, time.Time{}, err
	}
	return sent.MessageID, time.Now(), nil
}
