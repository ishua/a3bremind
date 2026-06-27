package bot

import (
	"fmt"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleSkip обрабатывает /skip — пропускает напоминание.
// Работает только при reply на уведомление бота.
// Для skip без reply используй inline-кнопку ⏭ Skip на уведомлении.
func (h *Handler) handleSkip(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Требуем reply на уведомление бота
	if update.Message.ReplyToMessage == nil {
		h.sendText(update.Message.Chat.ID, "Используй reply на уведомление бота или inline-кнопку ⏭ Skip.")
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

	// SetStatus("skipped") + GetInstanceByID + NextInstance в одной транзакции
	tx, err := h.db.Begin()
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is idiomatic

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
