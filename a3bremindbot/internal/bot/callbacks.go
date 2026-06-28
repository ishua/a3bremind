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

	// Парсинг callback data: action:id[:payload]
	// - action: "done", "snooze", "skip", "instances", "del", "del_yes", "del_no"
	//   "done_inst", "done_inst_y", "done_inst_n"
	// - id: UUID (36 символов)
	// - payload (опционально): HH:MM
	data := strings.SplitN(callback.Data, ":", 3)
	if len(data) < 2 {
		h.answerCallback(callback, "Ошибка: неверный формат данных")
		return
	}

	action := data[0]
	id := data[1]

	// Для действий с instanceID — проверяем UUID для действий, где id это instanceID
	switch action {
	case "done", "snooze", "skip", "done_time", "done_inst", "done_inst_y", "done_inst_n", "done_now", "done_custom":
		if len(id) != 36 {
			h.answerCallback(callback, "Ошибка: неверный UUID")
			return
		}
	}

	switch action {
	case "done":
		h.handleCallbackDone(callback, id)
	case "done_time":
		h.handleCallbackDoneTime(callback, id)
	case "done_now":
		h.handleCallbackDoneNow(callback, id)
	case "done_custom":
		h.handleCallbackDoneCustom(callback, id)
	case "snooze":
		h.handleCallbackSnooze(callback, id)
	case "skip":
		h.handleCallbackSkip(callback, id)
	case "instances":
		h.handleCallbackInstances(callback, id)
	case "del", "del_yes", "del_no":
		h.handleCallbackDelete(callback, action, id)
	default:
		h.answerCallback(callback, "Неизвестное действие")
	}
}

// answerCallback отвечает на callback query (показывает всплывающее уведомление).
func (h *Handler) answerCallback(callback *tgbotapi.CallbackQuery, text string) {
	cb := tgbotapi.NewCallback(callback.ID, text)
	if _, err := h.bot.Request(cb); err != nil {
		cb.Text = ""
		h.bot.Request(cb) //nolint:errcheck
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

// editMessageWithKeyboard заменяет текст и устанавливает новую клавиатуру.
func (h *Handler) editMessageWithKeyboard(chatID int64, messageID int, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = &keyboard
	if _, err := h.bot.Request(edit); err != nil {
		slog.Error("edit message with keyboard", "chat_id", chatID, "message_id", messageID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Callback: Done (из уведомления)
// ---------------------------------------------------------------------------

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

	if instance.Status != "pending" && instance.Status != "missed" {
		h.answerCallback(callback, "Уже выполнено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}
	defer tx.Rollback() //nolint:errcheck

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

// ---------------------------------------------------------------------------
// Callback: Done at time (из уведомления)
// ---------------------------------------------------------------------------

// handleCallbackDoneTime обрабатывает нажатие ⏰ Done at...
// Сохраняет pending entry и просит пользователя ввести время.
func (h *Handler) handleCallbackDoneTime(callback *tgbotapi.CallbackQuery, instanceID string) {
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

	if instance.Status != "pending" && instance.Status != "missed" {
		h.answerCallback(callback, "Уже выполнено")
		h.editMessageButtons(chatID, messageID)
		return
	}

	entry := pendingConfirmEntry{
		InstanceID: instance.ID,
		State:      stateWaitingTime,
	}
	h.pendingConfirm.Store(chatID, entry)

	time.AfterFunc(5*time.Minute, func() {
		h.pendingConfirm.Delete(chatID)
	})

	h.answerCallback(callback, "")
	h.editMessageWithKeyboard(chatID, messageID,
		fmt.Sprintf("⏰ Введи время выполнения для «%s» (HH:MM)", reminder.Label),
		tgbotapi.NewInlineKeyboardMarkup(),
	)

	// Спрашиваем время в отдельном сообщении (чтобы пользователь мог набрать текст).
	h.sendText(chatID, "⏰ Введи время выполнения в формате HH:MM (например, 14:30)")
}

// ---------------------------------------------------------------------------
// Callback: Done now (из списка экземпляров)
// ---------------------------------------------------------------------------

// handleCallbackDoneNow обрабатывает нажатие ✅ Now HH:MM в списке инстансов.
// Отмечает done с done_at = сейчас, без подтверждения.
func (h *Handler) handleCallbackDoneNow(callback *tgbotapi.CallbackQuery, instanceID string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		return
	}

	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		return
	}

	user, err := store.GetByTelegramID(h.db, userID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Это не твоё напоминание")
		return
	}

	if instance.Status != "pending" && instance.Status != "missed" {
		h.answerCallback(callback, "Уже выполнено")
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}
	defer tx.Rollback() //nolint:errcheck

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

	loc, locErr := time.LoadLocation(user.Timezone)
	if locErr != nil {
		loc = time.UTC
	}
	doneTime := "??:??"
	if updated.DoneAt != nil {
		doneTime = updated.DoneAt.In(loc).Format("15:04")
	}

	h.answerCallback(callback, fmt.Sprintf("✅ Выполнено в %s!", doneTime))
	h.sendText(chatID, fmt.Sprintf("✅ %s — записано в %s", reminder.Label, doneTime))

	if warning != "" {
		h.sendText(chatID, fmt.Sprintf("⚠️ %s — пропустить?", warning))
	}

	if reminder.MinGap != nil && updated.DoneAt != nil {
		h.sendRescheduleNotification(chatID, user, reminder, updated)
	}
}

// ---------------------------------------------------------------------------
// Callback: Done custom time (из списка экземпляров)
// ---------------------------------------------------------------------------

// handleCallbackDoneCustom обрабатывает нажатие ⏰ Set HH:MM в списке инстансов.
// Сохраняет pending entry и просит пользователя ввести время.
func (h *Handler) handleCallbackDoneCustom(callback *tgbotapi.CallbackQuery, instanceID string) {
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID

	instance, err := store.GetInstanceByID(h.db, instanceID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		return
	}

	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.answerCallback(callback, "Напоминание не найдено")
		return
	}

	user, err := store.GetByTelegramID(h.db, callback.From.ID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Это не твоё напоминание")
		return
	}

	if instance.Status != "pending" && instance.Status != "missed" {
		h.answerCallback(callback, "Уже выполнено")
		return
	}

	entry := pendingConfirmEntry{
		InstanceID: instance.ID,
		State:      stateWaitingTime,
	}
	h.pendingConfirm.Store(chatID, entry)

	time.AfterFunc(5*time.Minute, func() {
		h.pendingConfirm.Delete(chatID)
	})

	h.answerCallback(callback, "")
	h.editMessageText(chatID, messageID,
		fmt.Sprintf("⏰ Введи время выполнения для «%s» (HH:MM)", reminder.Label))

	h.sendText(chatID, "⏰ Введи время выполнения в формате HH:MM (например, 14:30)")
}

// ---------------------------------------------------------------------------
// Callback: Snooze
// ---------------------------------------------------------------------------

// handleCallbackSnooze обрабатывает нажатие ⏰ Snooze.
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

// ---------------------------------------------------------------------------
// Callback: Skip
// ---------------------------------------------------------------------------

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

	tx, err := h.db.Begin()
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}
	defer tx.Rollback() //nolint:errcheck

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

// ---------------------------------------------------------------------------
// Callback: Instances — показать список инстансов для напоминания
// ---------------------------------------------------------------------------

// handleCallbackInstances отправляет новое сообщение со списком инстансов.
func (h *Handler) handleCallbackInstances(callback *tgbotapi.CallbackQuery, reminderID string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	user, err := store.GetByTelegramID(h.db, userID)
	if err != nil {
		h.answerCallback(callback, "Пользователь не найден")
		return
	}

	if user.Timezone == "" {
		h.answerCallback(callback, "Сначала укажи часовой пояс")
		return
	}

	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		h.answerCallback(callback, "Ошибка часового пояса")
		return
	}

	reminder, err := store.GetByID(h.db, reminderID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Напоминание не найдено")
		return
	}

	instances, err := store.GetReminderInstancesByReminder(h.db, reminderID)
	if err != nil {
		h.answerCallback(callback, "Произошла ошибка")
		return
	}

	if len(instances) == 0 {
		h.answerCallback(callback, "Нет записей для этого напоминания")
		return
	}

	now := time.Now().In(loc)
	year, month, day := now.Date()
	startOfToday := time.Date(year, month, day, 0, 0, 0, 0, loc)
	endOfToday := startOfToday.Add(24 * time.Hour)

	var todayInstances []store.ReminderInstance
	for _, inst := range instances {
		if (inst.ForDate.Equal(startOfToday) || inst.ForDate.After(startOfToday)) && inst.ForDate.Before(endOfToday) {
			todayInstances = append(todayInstances, inst)
		}
	}

	if len(todayInstances) == 0 {
		h.answerCallback(callback, "Нет записей на сегодня")
		return
	}

	h.answerCallback(callback, "")

	var sb strings.Builder
	fmt.Fprintf(&sb, "💊 %s\n", reminder.Label)

	var buttons []tgbotapi.InlineKeyboardButton
	//nolint:dupl // duplicated in list_instances.go, kept for clarity
	for _, inst := range todayInstances {
		scheduledStr := inst.ScheduledAt.In(loc).Format("15:04")

		switch inst.Status {
		case "done":
			fmt.Fprintf(&sb, "✅ %s\n", scheduledStr)
		case "missed":
			shortID := inst.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fmt.Fprintf(&sb, "❌ %s — %s…\n", scheduledStr, shortID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("✅ Now %s", scheduledStr),
				fmt.Sprintf("done_now:%s", inst.ID),
			))
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("⏰ Set %s", scheduledStr),
				fmt.Sprintf("done_custom:%s", inst.ID),
			))
		default:
			shortID := inst.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fmt.Fprintf(&sb, "⏳ %s — %s…\n", scheduledStr, shortID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("✅ Now %s", scheduledStr),
				fmt.Sprintf("done_now:%s", inst.ID),
			))
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("⏰ Set %s", scheduledStr),
				fmt.Sprintf("done_custom:%s", inst.ID),
			))
		}
	}

	var keyboard tgbotapi.InlineKeyboardMarkup
	if len(buttons) > 0 {
		var rows [][]tgbotapi.InlineKeyboardButton
		// По 2 кнопки в ряд (Now + Set для каждого инстанса — уже пара)
		for i := 0; i < len(buttons); i += 2 {
			end := i + 2
			if end > len(buttons) {
				end = len(buttons)
			}
			rows = append(rows, buttons[i:end])
		}
		keyboard = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}

	msg := tgbotapi.NewMessage(chatID, strings.TrimSpace(sb.String()))
	if len(buttons) > 0 {
		msg.ReplyMarkup = &keyboard
	}
	if _, err := h.bot.Send(msg); err != nil {
		slog.Error("send instances message", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Callback: Delete flow (del → del_yes / del_no)
// ---------------------------------------------------------------------------

// handleCallbackDelete обрабатывает del, del_yes, del_no.
func (h *Handler) handleCallbackDelete(callback *tgbotapi.CallbackQuery, action string, reminderID string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID

	user, err := store.GetByTelegramID(h.db, userID)
	if err != nil {
		h.answerCallback(callback, "Пользователь не найден")
		return
	}

	reminder, err := store.GetByID(h.db, reminderID)
	if err != nil || reminder.UserID != user.ID {
		h.answerCallback(callback, "Напоминание не найдено")
		return
	}

	switch action {
	case "del":
		// Спрашиваем подтверждение
		h.answerCallback(callback, "")
		h.editMessageWithKeyboard(chatID, messageID,
			fmt.Sprintf("Удалить «%s»?", reminder.Label),
			tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Да", "del_yes:"+reminderID),
					tgbotapi.NewInlineKeyboardButtonData("❌ Нет", "del_no:"+reminderID),
				),
			),
		)

	case "del_yes":
		// Удаляем
		if err := store.DeleteReminderInstances(h.db, reminderID); err != nil {
			h.answerCallback(callback, "Произошла ошибка")
			return
		}
		if err := store.Delete(h.db, reminderID); err != nil {
			h.answerCallback(callback, "Произошла ошибка")
			return
		}
		h.editMessageText(chatID, messageID, fmt.Sprintf("🗑 Напоминание «%s» удалено", reminder.Label))
		h.answerCallback(callback, "🗑 Удалено!")

	case "del_no":
		// Отменяем — возвращаем исходные кнопки
		h.answerCallback(callback, "")
		h.editMessageButtons(chatID, messageID)
	}
}

// ---------------------------------------------------------------------------
// Callback: Done from instances list (done_inst → done_inst_y / done_inst_n)
// ---------------------------------------------------------------------------
// (удалены handleCallbackDoneInst/handleCallbackDoneInstConfirm/handleCallbackDoneInstCancel
//  — заменены на handleCallbackDoneNow и handleCallbackDoneCustom выше)
