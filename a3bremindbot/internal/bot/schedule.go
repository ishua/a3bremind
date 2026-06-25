package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleSchedule обрабатывает /schedule и /schedule tomorrow.
func (h *Handler) handleSchedule(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
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

	// Парсим аргумент: может быть "tomorrow"
	parts := strings.Fields(update.Message.Text)
	now := time.Now().In(loc)
	date := now

	if len(parts) > 1 && strings.ToLower(parts[1]) == "tomorrow" {
		date = now.Add(24 * time.Hour)
	}

	instances, err := store.GetInstancesByUserAndDay(h.db, user.ID, date, loc)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if len(instances) == 0 {
		h.sendText(update.Message.Chat.ID, "Нет напоминаний на сегодня")
		return
	}

	// Группируем по ReminderID
	type instanceGroup struct {
		Label     string
		Instances []store.ReminderInstance
	}

	groupMap := make(map[string]*instanceGroup)
	var groupOrder []string // сохраняем порядок

	for _, inst := range instances {
		if _, ok := groupMap[inst.ReminderID]; !ok {
			reminder, err := store.GetByID(h.db, inst.ReminderID)
			if err != nil {
				continue
			}
			groupMap[inst.ReminderID] = &instanceGroup{
				Label:     reminder.Label,
				Instances: nil,
			}
			groupOrder = append(groupOrder, inst.ReminderID)
		}
		groupMap[inst.ReminderID].Instances = append(groupMap[inst.ReminderID].Instances, inst)
	}

	// Формируем ответ
	dateStr := "на сегодня"
	if len(parts) > 1 && strings.ToLower(parts[1]) == "tomorrow" {
		dateStr = "на завтра"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 Расписание %s:\n\n", dateStr))

	for _, reminderID := range groupOrder {
		group := groupMap[reminderID]
		sb.WriteString(fmt.Sprintf("%s\n", group.Label))
		for _, inst := range group.Instances {
			scheduledInLoc := inst.ScheduledAt.In(loc)
			timeStr := scheduledInLoc.Format("15:04")
			icon := statusIcon(inst.Status)
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, timeStr))
		}
		sb.WriteString("\n")
	}

	h.sendText(update.Message.Chat.ID, strings.TrimSpace(sb.String()))
}

// statusIcon возвращает иконку для статуса Instance.
func statusIcon(status string) string {
	switch status {
	case "pending":
		return "⏳"
	case "done":
		return "✅"
	case "missed":
		return "❌"
	case "skipped":
		return "⏭️"
	default:
		return "❓"
	}
}
