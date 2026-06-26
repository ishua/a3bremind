package bot

import (
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/domain"
)

// BotAPI — минимальный интерфейс для отправки сообщений через Telegram API.
type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// pendingConfirmEntry holds data for a done HH:MM confirmation request.
type pendingConfirmEntry struct {
	InstanceID string
	DoneAt     time.Time
}

// Handler управляет входящими сообщениями от Telegram.
type Handler struct {
	db             *sql.DB
	bot            BotAPI
	scheduler      *domain.Scheduler
	version        string
	pendingConfirm sync.Map // chatID (int64) -> pendingConfirmEntry
}

// NewHandler создаёт Handler.
func NewHandler(db *sql.DB, bot BotAPI, scheduler *domain.Scheduler, version string) *Handler {
	return &Handler{
		db:        db,
		bot:       bot,
		scheduler: scheduler,
		version:   version,
	}
}

// HandleUpdate маршрутизирует один update от Telegram.
func (h *Handler) HandleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	lower := strings.ToLower(text)

	if update.Message.IsCommand() {
		h.handleCommand(update, text)
		return
	}

	// 1. done HH:MM / ok HH:MM / + HH:MM — с указанием времени (проверяем до обычного done)
	if isDoneWithTime(lower) {
		h.handleDoneWithTime(update)
		return
	}

	// 2. + / yes / y — подтверждение done HH:MM (проверяем до обычного +/done)
	if isConfirmation(lower) {
		if _, ok := h.pendingConfirm.Load(update.Message.Chat.ID); ok {
			h.handleConfirmDoneTime(update)
			return
		}
	}

	// 3. Обычные done / ok / +
	switch lower {
	case "done", "ok", "+":
		h.handleDone(update)
	default:
		// Неизвестный текст — игнорируем
	}
}

// isDoneWithTime проверяет, начинается ли текст с "done ", "ok " или "+ ".
func isDoneWithTime(lower string) bool {
	for _, prefix := range []string{"done ", "ok ", "+ "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// isConfirmation проверяет, является ли текст подтверждением.
func isConfirmation(lower string) bool {
	switch lower {
	case "+", "yes", "y":
		return true
	}
	return false
}

// handleCommand маршрутизирует команды.
func (h *Handler) handleCommand(update tgbotapi.Update, text string) {
	switch {
	case strings.HasPrefix(text, "/start"):
		h.handleStart(update)
	case strings.HasPrefix(text, "/settings"):
		h.handleSettings(update)
	case strings.HasPrefix(text, "/add"):
		h.handleAdd(update)
	case strings.HasPrefix(text, "/schedule"):
		h.handleSchedule(update)
	case strings.HasPrefix(text, "/list instances"):
		h.handleListInstances(update)
	case strings.HasPrefix(text, "/list"):
		h.handleList(update)
	case strings.HasPrefix(text, "/skip"):
		h.handleSkip(update)
	case strings.HasPrefix(text, "/snooze"):
		h.handleSnooze(update)
	case strings.HasPrefix(text, "/pause"):
		h.handlePause(update)
	case strings.HasPrefix(text, "/resume"):
		h.handleResume(update)
	case strings.HasPrefix(text, "/delete"):
		h.handleDelete(update)
	case strings.HasPrefix(text, "/help"):
		h.handleHelp(update)
	case strings.HasPrefix(text, "/done"):
		h.handleDoneByUUID(update)
	default:
		h.sendText(update.Message.Chat.ID, "Неизвестная команда")
	}
}

// sendText отправляет простое текстовое сообщение.
func (h *Handler) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := h.bot.Send(msg); err != nil {
		slog.Error("failed to send telegram message", "chat_id", chatID, "error", err)
	}
}

// sendMarkdown отправляет сообщение с Markdown-парсингом.
func (h *Handler) sendMarkdown(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := h.bot.Send(msg); err != nil {
		slog.Error("failed to send markdown message", "chat_id", chatID, "error", err)
	}
}
