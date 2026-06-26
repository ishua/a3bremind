package bot

import (
	"fmt"
	"strconv"
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

	label, repeat, times, minGap, err := parseAddCommand(text)
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
		Times:  times,
		MinGap: minGap,
	}

	created, err := store.Create(h.db, reminder)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка при создании напоминания.")
		return
	}

	// Вычисляем ScheduledAt = сегодня + HH:MM в timezone пользователя для первого времени
	now := time.Now().In(loc)
	parsedTime, _ := time.ParseInLocation("15:04", times[0], loc)
	scheduledAt := time.Date(
		now.Year(), now.Month(), now.Day(),
		parsedTime.Hour(), parsedTime.Minute(), 0, 0,
		loc,
	)

	forDate := time.Date(
		now.Year(), now.Month(), now.Day(),
		0, 0, 0, 0, loc,
	)

	// Создаём первый Instance
	instance := store.ReminderInstance{
		ReminderID:  created.ID,
		ForDate:     forDate,
		TimeIndex:   0,
		ScheduledAt: scheduledAt,
		Status:      "pending",
	}

	if _, err := store.CreateInstance(h.db, instance); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка при создании напоминания.")
		return
	}

	// Формируем ответ о создании
	if len(times) == 1 {
		h.sendText(update.Message.Chat.ID,
			fmt.Sprintf("✅ Напоминание «%s» создано. Первое — сегодня в %s.", label, times[0]))
	} else {
		gapStr := ""
		if minGap != nil {
			gapStr = fmt.Sprintf(" (gap: %d мин.)", *minGap)
		}
		h.sendText(update.Message.Chat.ID,
			fmt.Sprintf("✅ Напоминание «%s» создано. Времена: %s%s. Первое — сегодня в %s.", label, strings.Join(times, " "), gapStr, times[0]))
	}
}

// parseAddCommand парсит команду /add "Label" daily|once [gap:Nh|Nm] HH:MM [HH:MM ...].
// Возвращает label, repeat, times (один или более), minGap (nil если нет gap), ошибку.
func parseAddCommand(text string) (label, repeat string, times []string, minGap *int, err error) {
	// Убираем "/add" в начале
	rest := strings.TrimSpace(strings.TrimPrefix(text, "/add"))

	// Извлекаем label из кавычек
	if !strings.HasPrefix(rest, "\"") {
		return "", "", nil, nil, fmt.Errorf("Использование: `/add \"Label\" daily|once [gap:Nh|Nm] HH:MM [HH:MM ...]`")
	}

	closeQuote := strings.Index(rest[1:], "\"")
	if closeQuote == -1 {
		return "", "", nil, nil, fmt.Errorf("Использование: `/add \"Label\" daily|once [gap:Nh|Nm] HH:MM [HH:MM ...]`")
	}

	label = rest[1 : 1+closeQuote]
	afterLabel := strings.TrimSpace(rest[2+closeQuote:])

	// Разбиваем оставшуюся часть на слова
	parts := strings.Fields(afterLabel)
	if len(parts) < 2 {
		return "", "", nil, nil, fmt.Errorf("Использование: `/add \"Label\" daily|once [gap:Nh|Nm] HH:MM [HH:MM ...]`")
	}

	repeat = strings.ToLower(parts[0])
	if repeat != "daily" && repeat != "once" {
		return "", "", nil, nil, fmt.Errorf("Режим должен быть `daily` или `once`")
	}

	// Оставшиеся токены после repeat
	tokens := parts[1:]

	// Проверяем, есть ли gap:
	if strings.HasPrefix(strings.ToLower(tokens[0]), "gap:") {
		gapStr := tokens[0][4:] // убираем "gap:"
		gapVal, gapErr := parseGap(gapStr)
		if gapErr != nil {
			return "", "", nil, nil, gapErr
		}
		if gapVal <= 0 {
			return "", "", nil, nil, fmt.Errorf("Gap должен быть положительным (например, gap:1h или gap:30m)")
		}
		minGap = &gapVal
		tokens = tokens[1:] // убираем gap-токен
	}

	// Оставшиеся токены — это времена HH:MM
	if len(tokens) == 0 {
		return "", "", nil, nil, fmt.Errorf("Укажи хотя бы одно время HH:MM (например, 09:00)")
	}

	for _, t := range tokens {
		if _, err := time.Parse("15:04", t); err != nil {
			return "", "", nil, nil, fmt.Errorf("Неверный формат времени: %s. Используй HH:MM (например, 09:00)", t)
		}
	}

	return label, repeat, tokens, minGap, nil
}

// parseGap парсит строку вида "3h" или "30m" в минуты.
func parseGap(s string) (int, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("Неверный формат gap. Используй gap:3h или gap:30m")
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("Неверный формат gap. Используй gap:3h или gap:30m")
	}

	switch unit {
	case 'h':
		return num * 60, nil
	case 'm':
		return num, nil
	default:
		return 0, fmt.Errorf("Неверный формат gap. Используй h (часы) или m (минуты)")
	}
}
