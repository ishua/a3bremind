package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleListInstances обрабатывает /list instances <reminder_id>.
// Показывает все Instance указанного Reminder за сегодня с UUID, статусом,
// временем и готовой командой /done <uuid> <time>.
func (h *Handler) handleListInstances(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Парсим reminder_id из аргументов
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

	// Загружаем Reminder с проверкой принадлежности пользователю
	reminder, err := store.GetByID(h.db, reminderID)
	if err != nil || reminder.UserID != user.ID {
		h.sendText(update.Message.Chat.ID, "Напоминание не найдено")
		return
	}

	// Загружаем все Instance для этого Reminder
	instances, err := store.GetReminderInstancesByReminder(h.db, reminderID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if len(instances) == 0 {
		h.sendText(update.Message.Chat.ID, "Нет записей для этого напоминания")
		return
	}

	// Фильтруем по сегодняшнему дню в timezone пользователя
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

	// Форматируем вывод
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("💊 %s\n", reminder.Label))

	for _, inst := range todayInstances {
		scheduledStr := inst.ScheduledAt.In(loc).Format("15:04")

		switch inst.Status {
		case "done":
			sb.WriteString(fmt.Sprintf("✅ %s\n", scheduledStr))
		case "missed":
			// Показываем короткий UUID
			shortID := inst.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			sb.WriteString(fmt.Sprintf("❌ %s — %s…\n", scheduledStr, shortID))
		default:
			// pending, skipped и т.д.
			shortID := inst.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			sb.WriteString(fmt.Sprintf("⏳ %s — %s…\n", scheduledStr, shortID))
		}

		// Готовая команда /done
		sb.WriteString(fmt.Sprintf("  `/done %s %s`\n", inst.ID, scheduledStr))
	}

	h.sendMarkdown(update.Message.Chat.ID, strings.TrimSpace(sb.String()))
}
