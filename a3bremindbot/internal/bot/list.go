package bot

import (
	"fmt"
	"strings"

	"github.com/a3bremind/a3bremindbot/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleList обрабатывает /list — показывает все Reminder шаблоны с инлайн-кнопками.
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

	var allButtons []tgbotapi.InlineKeyboardButton

	for _, r := range reminders {
		label := r.Label
		if label == "" {
			label = "(без названия)"
		}

		fmt.Fprintf(&sb, "%s · %s\n", label, r.Repeat)

		if len(r.Times) > 0 {
			timesStr := strings.Join(r.Times, " ")
			if r.MinGap != nil {
				gapMinutes := *r.MinGap
				gapStr := formatGap(gapMinutes)
				fmt.Fprintf(&sb, "  ⏰ %s (gap: %s)\n", timesStr, gapStr)
			} else {
				fmt.Fprintf(&sb, "  ⏰ %s\n", timesStr)
			}
		}

		sb.WriteString("\n")

		allButtons = append(allButtons,
			tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить", "del:"+r.ID),
			tgbotapi.NewInlineKeyboardButtonData("📋 Экземпляры", "instances:"+r.ID),
		)
	}

	// Строим клавиатуру: по 2 кнопки (Удалить + Экземпляры) в ряд для каждого напоминания
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < len(allButtons); i += 2 {
		end := i + 2
		if end > len(allButtons) {
			end = len(allButtons)
		}
		rows = append(rows, allButtons[i:end])
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, strings.TrimSpace(sb.String()))
	msg.ReplyMarkup = &keyboard
	if _, err := h.bot.Send(msg); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка")
	}
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
