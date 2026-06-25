package bot

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleList обрабатывает /list — показывает все Reminder шаблоны.
func (h *Handler) handleList(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	reminders, err := store.GetAll(h.db, user.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if len(reminders) == 0 {
		h.sendText(update.Message.Chat.ID, "Нет настроенных напоминаний")
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 Все напоминания:\n\n")

	for _, r := range reminders {
		label := r.Label
		if label == "" {
			label = "(без названия)"
		}

		sb.WriteString(fmt.Sprintf("%s · %s\n", label, r.Repeat))
		sb.WriteString(fmt.Sprintf("  🆔 %s\n", r.ID))

		if len(r.Times) > 0 {
			timesStr := strings.Join(r.Times, " ")
			if r.MinGap != nil {
				gapMinutes := *r.MinGap
				gapStr := formatGap(gapMinutes)
				sb.WriteString(fmt.Sprintf("  ⏰ %s (gap: %s)\n", timesStr, gapStr))
			} else {
				sb.WriteString(fmt.Sprintf("  ⏰ %s\n", timesStr))
			}
		}

		sb.WriteString("\n")
	}

	h.sendText(update.Message.Chat.ID, strings.TrimSpace(sb.String()))
}

// formatGap форматирует минуты в читаемый вид: 180 → "3ч", 30 → "30м", 90 → "1ч 30м".
func formatGap(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dм", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		return fmt.Sprintf("%dч", hours)
	}
	return fmt.Sprintf("%dч %dм", hours, remaining)
}
