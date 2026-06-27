package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleSnooze обрабатывает /snooze N — откладывает напоминание на N минут.
// Работает только при reply на уведомление бота.
// Для snooze без reply используй inline-кнопку ⏰ на уведомлении.
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

	// Требуем reply на уведомление бота
	if update.Message.ReplyToMessage == nil {
		h.sendText(update.Message.Chat.ID, "Используй reply на уведомление бота или inline-кнопку ⏰ Snooze.")
		return
	}

	// Находим Instance по reply
	replyMsgID := update.Message.ReplyToMessage.MessageID
	instanceID, err := store.GetInstanceIDByReply(h.db, replyMsgID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Не удалось найти напоминание")
		return
	}

	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Не удалось найти напоминание")
		return
	}

	// Проверяем принадлежность пользователю
	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil || reminder.UserID != user.ID {
		h.sendText(update.Message.Chat.ID, "Это не твоё напоминание")
		return
	}

	if instance.Status != "pending" {
		h.sendText(update.Message.Chat.ID, "Это напоминание уже неактуально")
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
