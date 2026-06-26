package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleDoneByUUID обрабатывает /done <uuid> [HH:MM].
// Отмечает Instance (любого статуса, включая missed) как выполненный,
// удаляет последующие Instance и пересоздаёт цепочку.
func (h *Handler) handleDoneByUUID(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Парсинг: /done <uuid> или /done <uuid> HH:MM
	text := strings.TrimSpace(update.Message.Text)
	parts := strings.Fields(text)
	if len(parts) < 2 {
		h.sendText(update.Message.Chat.ID, "Использование: `/done <uuid> [HH:MM]`")
		return
	}

	instanceID := parts[1]

	// Проверка UUID — должен быть 36 символов (стандартный UUID)
	if len(instanceID) != 36 {
		h.sendText(update.Message.Chat.ID, "Неверный UUID. Используй полный UUID из команды.")
		return
	}

	if user.Timezone == "" {
		h.sendText(update.Message.Chat.ID, "Сначала укажи часовой пояс: `/settings timezone Europe/Berlin`")
		return
	}

	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Ошибка часового пояса.")
		return
	}

	// Определяем doneAt
	now := time.Now().In(loc)
	var doneAt time.Time

	if len(parts) >= 3 {
		// /done <uuid> HH:MM
		timeStr := parts[2]
		parsed, err := time.ParseInLocation("15:04", timeStr, loc)
		if err != nil {
			h.sendText(update.Message.Chat.ID, "Неверный формат времени. Используй HH:MM.")
			return
		}
		doneAt = time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
	} else {
		// /done <uuid> — без времени, done_at = now
		doneAt = now
	}

	// Загружаем Instance по UUID
	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Напоминание не найдено")
		return
	}

	// Загружаем Reminder (проверка принадлежности пользователю)
	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil || reminder.UserID != user.ID {
		h.sendText(update.Message.Chat.ID, "Напоминание не найдено")
		return
	}

	// Если Instance уже done — сообщаем
	if instance.Status == "done" {
		h.sendText(update.Message.Chat.ID, "Это напоминание уже выполнено")
		return
	}

	// Алгоритм отметки в транзакции
	tx, err := h.db.Begin()
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}
	defer tx.Rollback()

	// 1. Удаляем все Instance с time_index > current
	if err := store.DeleteInstancesAfterIndex(tx, instance.ReminderID, instance.TimeIndex); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// 2. Устанавливаем статус done с указанным временем
	if err := store.SetStatusWithDoneAt(tx, instanceID, "done", doneAt); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// 3. Перезагружаем Instance (чтобы получить done_at)
	updated, err := store.GetInstanceByID(tx, instanceID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// 4. NextInstance — создаёт следующий в цепочке, если есть
	warning, err := domain.NextInstance(tx, updated, time.Now())
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if err := tx.Commit(); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Форматируем ответ
	doneTimeStr := doneAt.Format("15:04")
	h.sendText(update.Message.Chat.ID, fmt.Sprintf("✅ %s — записано в %s", reminder.Label, doneTimeStr))

	if warning != "" {
		h.sendText(update.Message.Chat.ID, fmt.Sprintf("⚠️ %s — пропустить?", warning))
	}

	// Уведомление о рескедуле
	if reminder.MinGap != nil && updated.DoneAt != nil {
		h.sendRescheduleNotification(update.Message.Chat.ID, user, reminder, updated)
	}
}
