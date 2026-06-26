package bot

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleCallback маршрутизирует callback query от inline-кнопок.
func (h *Handler) handleCallback(update tgbotapi.Update) {
	callback := update.CallbackQuery
	if callback == nil {
		return
	}

	// Парсинг callback data: action:instanceID
	data := strings.SplitN(callback.Data, ":", 2)
	if len(data) != 2 {
		h.answerCallback(callback, "Ошибка: неверный формат данных")
		return
	}

	action := data[0]
	instanceID := data[1]

	// Проверяем UUID — должен быть 36 символов
	if len(instanceID) != 36 {
		h.answerCallback(callback, "Ошибка: неверный UUID")
		return
	}

	switch action {
	case "done":
		h.handleCallbackDone(callback, instanceID)
	case "snooze":
		h.handleCallbackSnooze(callback, instanceID)
	case "skip":
		h.handleCallbackSkip(callback, instanceID)
	default:
		h.answerCallback(callback, "Неизвестное действие")
	}
}

// answerCallback отвечает на callback query (показывает всплывающее уведомление).
func (h *Handler) answerCallback(callback *tgbotapi.CallbackQuery, text string) {
	cb := tgbotapi.NewCallback(callback.ID, text)
	if _, err := h.bot.Request(cb); err != nil {
		cb.Text = ""      // пустой ответ, если с текстом не вышло
		h.bot.Request(cb) //nolint:errcheck // ответ без текста — fire-and-forget
	}
}

// editMessageButtons убирает inline-клавиатуру с сообщения.
func (h *Handler) editMessageButtons(chatID int64, messageID int) {
	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, tgbotapi.NewInlineKeyboardMarkup())
	if _, err := h.bot.Request(edit); err != nil {
		slog.Error("edit message buttons", "chat_id", chatID, "message_id", messageID, "error", err)
	}
}

// editMessageText заменяет текст и убирает клавиатуру.
func (h *Handler) editMessageText(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
	if _, err := h.bot.Request(edit); err != nil {
		slog.Error("edit message text", "chat_id", chatID, "message_id", messageID, "error", err)
	}
}

// handleCallbackDone обрабатывает нажатие ✅ Done.
func (h *Handler) handleCallbackDone(callback *tgbotapi.CallbackQuery, instanceID string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID

	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	// Проверяем владельца
	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	user, err := store.GetByTelegramID(h.db, userID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Это не твоё напоминание")
		return
	}

	if instance.Status != "pending" {
		h.answerCallback(callback, "Уже выполнено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	// Выполняем done
	tx, err := h.db.Begin()
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is idiomatic

	if err := store.SetStatus(tx, instance.ID, "done"); err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	updated, err := store.GetInstanceByID(tx, instance.ID)
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	warning, err := domain.NextInstance(tx, updated, time.Now())
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	if err := tx.Commit(); err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	// Форматируем время
	loc, _ := time.LoadLocation(user.Timezone)
	doneTime := "??:??"
	if updated.DoneAt != nil {
		if loc != nil {
			doneTime = updated.DoneAt.In(loc).Format("15:04")
		} else {
			doneTime = updated.DoneAt.Format("15:04")
		}
	}

	text := fmt.Sprintf("✅ %s — записано в %s", reminder.Label, doneTime)
	h.editMessageText(chatID, messageID, text)
	h.answerCallback(callback, "✅ Выполнено!")

	if warning != "" {
		h.sendText(chatID, fmt.Sprintf("⚠️ %s — пропустить?", warning))
	}

	if reminder.MinGap != nil && updated.DoneAt != nil {
		h.sendRescheduleNotification(chatID, user, reminder, updated)
	}
}

// handleCallbackSnooze обрабатывает нажатие ⏰ Snooze.
// Откладывает на дефолтный интервал (RepeatInterval = 15 минут).
func (h *Handler) handleCallbackSnooze(callback *tgbotapi.CallbackQuery, instanceID string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID

	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	user, err := store.GetByTelegramID(h.db, userID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Это не твоё напоминание")
		return
	}

	if instance.Status != "pending" {
		h.answerCallback(callback, "Уже неактуально")
		h.editMessageButtons(chatID, messageID)
		return
	}

	// Сдвигаем scheduled_at на RepeatInterval (15 минут)
	newTime := time.Now().Add(domain.RepeatInterval)
	if err := store.SetInstanceScheduledAt(h.db, instance.ID, newTime); err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	minutes := int(domain.RepeatInterval.Minutes())
	text := fmt.Sprintf("🔇 %s — напомню через %d мин.", reminder.Label, minutes)
	h.editMessageText(chatID, messageID, text)
	h.answerCallback(callback, fmt.Sprintf("⏰ Напомню через %d мин.", minutes))
}

// handleCallbackSkip обрабатывает нажатие ⏭ Skip.
func (h *Handler) handleCallbackSkip(callback *tgbotapi.CallbackQuery, instanceID string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID

	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	user, err := store.GetByTelegramID(h.db, userID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Это не твоё напоминание")
		return
	}

	if instance.Status != "pending" {
		h.answerCallback(callback, "Уже неактуально")
		h.editMessageButtons(chatID, messageID)
		return
	}

	// Транзакция: set status skipped + next instance
	tx, err := h.db.Begin()
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is idiomatic

	if err := store.SetStatus(tx, instance.ID, "skipped"); err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	updated, err := store.GetInstanceByID(tx, instance.ID)
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	_, err = domain.NextInstance(tx, updated, time.Now())
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	if err := tx.Commit(); err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	text := fmt.Sprintf("⏭️ %s — пропущено", reminder.Label)
	h.editMessageText(chatID, messageID, text)
	h.answerCallback(callback, "⏭ Пропущено")
}
