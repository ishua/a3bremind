package bot

import (
	"fmt"
	"strings"

	"github.com/a3bremind/a3bremindbot/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleDelete обрабатывает /delete <id> — удаляет напоминание и все его Instance.
func (h *Handler) handleDelete(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Парсим ID
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		h.sendText(update.Message.Chat.ID, "Использование: `/delete <id>` (ID напоминания из /list)")
		return
	}
	reminderID := parts[1]

	// Проверяем, что Reminder существует и принадлежит пользователю
	reminder, err := store.GetByID(h.db, reminderID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Напоминание не найдено")
		return
	}

	if reminder.UserID != user.ID {
		h.sendText(update.Message.Chat.ID, "Напоминание не найдено")
		return
	}

	// Каскадное удаление: сначала Instance, потом Reminder
	if err := store.DeleteReminderInstances(h.db, reminderID); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if err := store.Delete(h.db, reminderID); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	h.sendText(update.Message.Chat.ID, fmt.Sprintf("🗑 Напоминание «%s» удалено", reminder.Label))
}
