package bot

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/a3bremind/a3bremindbot/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleListInstances обрабатывает /list instances <reminder_id>.
// handleListInstances обрабатывает /list instances <reminder_id>.
// Показывает все Instance указанного Reminder за сегодня с инлайн-кнопками.
func (h *Handler) handleListInstances(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	parts := strings.Fields(text)
	if len(parts) < 3 {
		h.sendText(update.Message.Chat.ID, "Использование: /list instances <reminder_id>")
		return
	}
	reminderID := parts[2]

	if user.Timezone == "" {
		h.sendText(update.Message.Chat.ID, "Сначала укажи часовой пояс: /settings timezone Europe/Berlin")
		return
	}

	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Ошибка часового пояса.")
		return
	}

	reminder, err := store.GetByID(h.db, reminderID)
	if err != nil || reminder.UserID != user.ID {
		h.sendText(update.Message.Chat.ID, "Напоминание не найдено")
		return
	}

	instances, err := store.GetReminderInstancesByReminder(h.db, reminderID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if len(instances) == 0 {
		h.sendText(update.Message.Chat.ID, "Нет записей для этого напоминания")
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
		h.sendText(update.Message.Chat.ID, "Нет записей на сегодня для этого напоминания")
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "💊 %s\n", reminder.Label)

	var buttons []tgbotapi.InlineKeyboardButton
	//nolint:dupl // duplicated in callbacks.go, kept for clarity
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

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, strings.TrimSpace(sb.String()))
	if len(buttons) > 0 {
		var rows [][]tgbotapi.InlineKeyboardButton
		for i := 0; i < len(buttons); i += 2 {
			end := i + 2
			if end > len(buttons) {
				end = len(buttons)
			}
			rows = append(rows, buttons[i:end])
		}
		msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	if _, err := h.bot.Send(msg); err != nil {
		slog.Error("send list instances", "error", err)
	}
}
