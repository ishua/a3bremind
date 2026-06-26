package bot

import (
	"fmt"
	"strings"
	"time"

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

	// SetStatus("done") + GetInstanceByID + NextInstance в одной транзакции
	tx, err := h.db.Begin()
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}
	defer tx.Rollback()

	if err := store.SetStatus(tx, instance.ID, "done"); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Перечитываем instance чтобы получить done_at
	updated, err := store.GetInstanceByID(tx, instance.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// NextInstance: создаём следующий в цепочке, если есть
	warning, err := domain.NextInstance(tx, updated, time.Now())
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if err := tx.Commit(); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Форматируем время из done_at
	doneTime := "??:??"
	if updated.DoneAt != nil {
		doneTime = updated.DoneAt.Format("15:04")
	}

	// Ответ: записано
	h.sendText(update.Message.Chat.ID, fmt.Sprintf("✅ %s — записано в %s", reminder.Label, doneTime))

	// Предупреждение о выходе за полночь
	if warning != "" {
		h.sendText(update.Message.Chat.ID, fmt.Sprintf("⚠️ %s — пропустить?", warning))
	}

	// Уведомление о рескедуле — если MinGap задан и был рескедул
	if reminder.MinGap != nil && updated.DoneAt != nil {
		h.sendRescheduleNotification(update.Message.Chat.ID, user, reminder, updated)
	}
}

// sendRescheduleNotification отправляет уведомление о новом расписании, если времена сдвинулись.
func (h *Handler) sendRescheduleNotification(chatID int64, user store.User, reminder store.Reminder, doneInst store.ReminderInstance) {
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return
	}

	// Вычисляем adjusted times с помощью domain.Reschedule
	now := time.Now()
	adjusted, _ := domain.Reschedule(reminder, *doneInst.DoneAt, doneInst.TimeIndex, loc, now)
	if len(adjusted) == 0 {
		return
	}

	// Проверяем, сдвинулось ли хотя бы одно время относительно исходного
	now = now.In(loc)
	hasShift := false
	adjustedStrs := make([]string, len(adjusted))
	for i, adj := range adjusted {
		adjustedStrs[i] = adj.In(loc).Format("15:04")
		// Сравниваем с исходным временем
		reminderIdx := doneInst.TimeIndex + 1 + i
		if reminderIdx < len(reminder.Times) {
			parsed, _ := time.ParseInLocation("15:04", reminder.Times[reminderIdx], loc)
			original := time.Date(
				now.Year(), now.Month(), now.Day(),
				parsed.Hour(), parsed.Minute(), 0, 0,
				loc,
			)
			if adj.Unix() != original.Unix() {
				hasShift = true
			}
		}
	}

	if !hasShift {
		return
	}

	h.sendText(chatID, fmt.Sprintf("📅 Новое расписание: %s", strings.Join(adjustedStrs, " · ")))
}
