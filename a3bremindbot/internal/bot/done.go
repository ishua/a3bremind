package bot

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleDone обрабатывает done/ok/+ с reply или с fallback на последний активный Instance.
func (h *Handler) handleDone(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	var instance store.ReminderInstance

	if update.Message.ReplyToMessage != nil {
		// С reply: ищем Instance по message_id
		replyMsgID := update.Message.ReplyToMessage.MessageID
		inst, err := store.GetInstanceByMessageID(h.db, replyMsgID)
		if err != nil {
			h.sendText(update.Message.Chat.ID, "Не удалось найти напоминание")
			return
		}
		instance = inst
	} else {
		// Без reply: fallback к последнему активному
		active, err := store.GetActiveByUser(h.db, user.ID)
		if err != nil || len(active) == 0 {
			h.sendText(update.Message.Chat.ID, "Нет активных напоминаний")
			return
		}
		// Берём последний по scheduled_at
		instance = active[len(active)-1]
	}

	// Проверяем статус
	if instance.Status != "pending" {
		h.sendText(update.Message.Chat.ID, "Это напоминание уже выполнено")
		return
	}

	// Загружаем reminder для label
	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// SetStatus("done") автоматически проставляет done_at = time.Now()
	if err := store.SetStatus(h.db, instance.ID, "done"); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Перечитываем instance чтобы получить done_at
	updated, err := store.GetInstanceByID(h.db, instance.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Форматируем время из done_at
	doneTime := "??:??"
	if updated.DoneAt != nil {
		doneTime = updated.DoneAt.Format("15:04")
	}

	// NextInstance: создаём следующий в цепочке, если есть
	if err := domain.NextInstance(h.db, updated); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	h.sendText(update.Message.Chat.ID, fmt.Sprintf("✅ %s — записано в %s", reminder.Label, doneTime))
}
