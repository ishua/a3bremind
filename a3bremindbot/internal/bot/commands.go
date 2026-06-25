package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleStart обрабатывает /start.
func (h *Handler) handleStart(update tgbotapi.Update) {
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if user.Timezone == "" {
		h.sendText(update.Message.Chat.ID,
			"Привет! Я — бот для напоминаний.\n\n"+
				"Укажи часовой пояс: `/settings timezone Europe/Berlin`")
	} else {
		h.sendText(update.Message.Chat.ID, "С возвращением!")
	}
}

// handleSettings обрабатывает /settings.
func (h *Handler) handleSettings(update tgbotapi.Update) {
	// Парсим: "/settings timezone Europe/Berlin"
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		h.sendText(update.Message.Chat.ID, "Использование: `/settings timezone Europe/Berlin`")
		return
	}

	subcommand := strings.ToLower(parts[1])
	if subcommand != "timezone" {
		h.sendText(update.Message.Chat.ID, "Использование: `/settings timezone Europe/Berlin`")
		return
	}

	if len(parts) < 3 {
		h.sendText(update.Message.Chat.ID, "Использование: `/settings timezone Europe/Berlin`")
		return
	}

	value := parts[2]

	// Валидация timezone
	_, err := time.LoadLocation(value)
	if err != nil {
		h.sendText(update.Message.Chat.ID, fmt.Sprintf("Неверный часовой пояс: %s", value))
		return
	}

	// Получаем или создаём пользователя
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	if err := store.SetTimezone(h.db, user.ID, value); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	h.sendText(update.Message.Chat.ID, fmt.Sprintf("✅ Часовой пояс установлен: %s", value))
}

// handleAdd обрабатывает /add.
func (h *Handler) handleAdd(update tgbotapi.Update) {
	text := update.Message.Text

	label, repeat, timeStr, err := parseAddCommand(text)
	if err != nil {
		h.sendText(update.Message.Chat.ID, err.Error())
		return
	}

	// Проверка: пользователь существует и timezone задана
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
		h.sendText(update.Message.Chat.ID, "Ошибка часового пояса. Попробуй `/settings timezone Europe/Berlin`")
		return
	}

	// Создаём напоминание
	reminder := store.Reminder{
		UserID: user.ID,
		Label:  label,
		Repeat: repeat,
		Times:  []string{timeStr},
	}

	created, err := store.Create(h.db, reminder)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка при создании напоминания.")
		return
	}

	// Вычисляем ScheduledAt = сегодня + HH:MM в timezone пользователя
	now := time.Now().In(loc)
	parsedTime, _ := time.ParseInLocation("15:04", timeStr, loc)
	scheduledAt := time.Date(
		now.Year(), now.Month(), now.Day(),
		parsedTime.Hour(), parsedTime.Minute(), 0, 0,
		loc,
	)

	// Создаём первый Instance
	instance := store.ReminderInstance{
		ReminderID:  created.ID,
		TimeIndex:   0,
		ScheduledAt: scheduledAt,
		Status:      "pending",
	}

	if _, err := store.CreateInstance(h.db, instance); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка при создании напоминания.")
		return
	}

	h.sendText(update.Message.Chat.ID,
		fmt.Sprintf("✅ Напоминание «%s» создано. Первое — сегодня в %s.", label, timeStr))
}

// parseAddCommand парсит команду /add "Label" daily|once HH:MM.
// Возвращает label, repeat, time, ошибку.
func parseAddCommand(text string) (label, repeat, timeStr string, err error) {
	// Убираем "/add" в начале
	rest := strings.TrimSpace(strings.TrimPrefix(text, "/add"))

	// Извлекаем label из кавычек
	if !strings.HasPrefix(rest, "\"") {
		return "", "", "", fmt.Errorf("Использование: `/add \"Label\" daily|once HH:MM`")
	}

	closeQuote := strings.Index(rest[1:], "\"")
	if closeQuote == -1 {
		return "", "", "", fmt.Errorf("Использование: `/add \"Label\" daily|once HH:MM`")
	}

	label = rest[1 : 1+closeQuote]
	afterLabel := strings.TrimSpace(rest[2+closeQuote:])

	// Разбиваем оставшуюся часть на слова
	parts := strings.Fields(afterLabel)
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("Использование: `/add \"Label\" daily|once HH:MM`")
	}

	repeat = strings.ToLower(parts[0])
	if repeat != "daily" && repeat != "once" {
		return "", "", "", fmt.Errorf("Режим должен быть `daily` или `once`")
	}

	timeStr = parts[1]

	// Валидация времени
	if _, err := time.Parse("15:04", timeStr); err != nil {
		return "", "", "", fmt.Errorf("Неверный формат времени. Используй HH:MM (например, 09:00)")
	}

	return label, repeat, timeStr, nil
}
