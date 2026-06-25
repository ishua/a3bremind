package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleSnooze обрабатывает /snooze N — откладывает напоминание на N минут.
func (h *Handler) handleSnooze(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Парсим N из текста команды
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		h.sendText(update.Message.Chat.ID, "Использование: `/snooze N` (N — минуты, от 1 до 1440)")
		return
	}

	n, err := strconv.Atoi(parts[1])
	if err != nil || n <= 0 || n > 1440 {
		h.sendText(update.Message.Chat.ID, "Использование: `/snooze N` (N — минуты, от 1 до 1440)")
		return
	}

	// Получаем последний активный Instance
	active, err := store.GetActiveByUser(h.db, user.ID)
	if err != nil || len(active) == 0 {
		h.sendText(update.Message.Chat.ID, "Нет активных напоминаний")
		return
	}
	instance := active[len(active)-1]

	// Загружаем reminder для label
	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Сдвигаем scheduled_at на N минут
	newTime := time.Now().Add(time.Duration(n) * time.Minute)
	if err := store.SetInstanceScheduledAt(h.db, instance.ID, newTime); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	h.sendText(update.Message.Chat.ID, fmt.Sprintf("🔇 %s — напомню через %d минут", reminder.Label, n))
}
