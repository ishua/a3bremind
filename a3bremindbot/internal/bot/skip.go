package bot

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleSkip обрабатывает /skip — пропускает текущее напоминание.
func (h *Handler) handleSkip(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
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

	// SetStatus("skipped") + GetInstanceByID + NextInstance в одной транзакции
	tx, err := h.db.Begin()
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}
	defer tx.Rollback()

	// SetStatus("skipped") — не проставляет done_at
	if err := store.SetStatus(tx, instance.ID, "skipped"); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Перечитываем instance (DoneAt будет nil для skipped)
	updated, err := store.GetInstanceByID(tx, instance.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// NextInstance: создаёт следующий в цепочке, если есть
	// DoneAt == nil → рескедул не применяется, это правильно для skip
	_, err = domain.NextInstance(tx, updated, time.Now())
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if err := tx.Commit(); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	h.sendText(update.Message.Chat.ID, fmt.Sprintf("⏭️ %s — пропущено", reminder.Label))
}
