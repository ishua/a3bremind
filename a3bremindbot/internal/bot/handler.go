package bot

import (
	"database/sql"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/domain"
)

// BotAPI — минимальный интерфейс для отправки сообщений через Telegram API.
// Реальный *tgbotapi.BotAPI удовлетворяет ему нативно.
type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// Handler управляет входящими сообщениями от Telegram.
type Handler struct {
	db        *sql.DB
	bot       BotAPI
	scheduler *domain.Scheduler
}

// NewHandler создаёт Handler.
func NewHandler(db *sql.DB, bot BotAPI, scheduler *domain.Scheduler) *Handler {
	return &Handler{
		db:        db,
		bot:       bot,
		scheduler: scheduler,
	}
}

// HandleUpdate маршрутизирует один update от Telegram.
func (h *Handler) HandleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	text := strings.TrimSpace(update.Message.Text)

	if update.Message.IsCommand() {
		h.handleCommand(update, text)
		return
	}

	lower := strings.ToLower(text)
	switch lower {
	case "done", "ok", "+":
		h.handleDone(update)
	default:
		// Неизвестный текст — игнорируем
	}
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
	default:
		h.sendText(update.Message.Chat.ID, "Неизвестная команда")
	}
}

// sendText отправляет простое текстовое сообщение.
func (h *Handler) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	h.bot.Send(msg)
}
