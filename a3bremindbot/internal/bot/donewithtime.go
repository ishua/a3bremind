package bot

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

// handleDoneWithTime обрабатывает "done HH:MM" / "ok HH:MM" / "+ HH:MM".
func (h *Handler) handleDoneWithTime(update tgbotapi.Update) {
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

	// Парсим HH:MM из текста
	text := strings.TrimSpace(update.Message.Text)
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return // не должно случиться из-за isDoneWithTime
	}

	timeStr := parts[len(parts)-1] // последнее слово
	parsed, err := time.ParseInLocation("15:04", timeStr, loc)
	if err != nil {
		return // игнорируем — невалидное время
	}

	now := time.Now().In(loc)
	doneAt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)

	if doneAt.After(now) {
		h.sendText(update.Message.Chat.ID, "Указанное время в будущем. Используй `done` без времени.")
		return
	}

	// doneAt в прошлом — находим активный Instance
	active, err := store.GetActiveByUser(h.db, user.ID)
	if err != nil || len(active) == 0 {
		h.sendText(update.Message.Chat.ID, "Нет активных напоминаний")
		return
	}
	instance := active[len(active)-1]

	// Сохраняем в pendingConfirm
	entry := pendingConfirmEntry{
		InstanceID: instance.ID,
		DoneAt:     doneAt,
	}
	h.pendingConfirm.Store(update.Message.Chat.ID, entry)

	// Таймер на 5 минут — очистка
	time.AfterFunc(5*time.Minute, func() {
		h.pendingConfirm.Delete(update.Message.Chat.ID)
	})

	h.sendText(update.Message.Chat.ID,
		fmt.Sprintf("Записать выполнение в %s? Отправь + для подтверждения.", doneAt.Format("15:04")))
}

// handleConfirmDoneTime обрабатывает подтверждение "+"/"yes"/"y" после done HH:MM.
func (h *Handler) handleConfirmDoneTime(update tgbotapi.Update) {
	val, ok := h.pendingConfirm.LoadAndDelete(update.Message.Chat.ID)
	if !ok {
		// Нет pending confirm — fallback к обычному handleDone
		h.handleDone(update)
		return
	}

	entry := val.(pendingConfirmEntry)

	// Загружаем пользователя
	user, err := store.GetOrCreate(h.db, update.Message.Chat.ID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Загружаем Instance
	instance, err := store.GetInstanceByID(h.db, entry.InstanceID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Не удалось найти напоминание")
		return
	}

	// Проверяем статус
	if instance.Status != "pending" {
		h.sendText(update.Message.Chat.ID, "Это напоминание уже выполнено")
		return
	}

	// Загружаем reminder для label
	reminder, err := store.GetByID(h.db, instance.ReminderID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Устанавливаем done с конкретным временем
	if err := store.SetStatusWithDoneAt(h.db, entry.InstanceID, "done", entry.DoneAt); err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// Перечитываем Instance для NextInstance (DoneAt должен быть проставлен)
	updated, err := store.GetInstanceByID(h.db, entry.InstanceID)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	// NextInstance
	warning, err := domain.NextInstance(h.db, updated)
	if err != nil {
		h.sendText(update.Message.Chat.ID, "Произошла ошибка. Попробуй позже.")
		return
	}

	h.sendText(update.Message.Chat.ID,
		fmt.Sprintf("✅ %s — записано в %s", reminder.Label, entry.DoneAt.Format("15:04")))

	if warning != "" {
		h.sendText(update.Message.Chat.ID, fmt.Sprintf("⚠️ %s — пропустить?", warning))
	}

	// Уведомление о рескедуле
	if reminder.MinGap != nil && updated.DoneAt != nil {
		h.sendRescheduleNotification(update.Message.Chat.ID, user, reminder, updated)
	}
}
