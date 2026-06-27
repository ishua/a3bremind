package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (h *Handler) handleHelp(update tgbotapi.Update) {
	text := "*Доступные команды*\n\n" +
		"/start — начать работу с ботом\n" +
		"/settings timezone Europe/Moscow — установить часовой пояс\n" +
		"/add \"Название\" daily|once [gap:Nh|Nm] HH:MM ... — создать напоминание\n" +
		"/schedule — показать расписание на сегодня\n" +
		"/list — все шаблоны напоминаний\n" +
		"/skip — пропустить активное напоминание\n" +
		"/snooze N — отложить на N минут\n" +
		"/pause — приостановить все напоминания\n" +
		"/resume — возобновить напоминания\n" +
		"/delete ID — удалить напоминание\n" +
		"/help — показать эту справку\n\n" +
		"Несколько времен — для серии напоминаний:\n" +
		"`/add \"Медикамент\" daily 08:00 12:00 20:00`\n\n" +
		"Версия: " + h.version

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := h.bot.Send(msg); err != nil {
		h.sendText(update.Message.Chat.ID, text)
	}
}
