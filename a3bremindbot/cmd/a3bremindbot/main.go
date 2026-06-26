package main

import (
	"log/slog"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/bot"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		slog.Error("TELEGRAM_BOT_TOKEN is not set")
		os.Exit(1)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "bot.db"
	}
	db, err := store.InitDB("sqlite", dbPath)
	if err != nil {
		slog.Error("init db", "error", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	botAPI, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		slog.Error("create bot api", "error", err)
		os.Exit(1)
	}

	notifier := bot.NewNotifier(botAPI)
	scheduler := domain.New(db, notifier)
	scheduler.Start()
	defer scheduler.Stop()

	handler := bot.NewHandler(db, botAPI, scheduler)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := botAPI.GetUpdatesChan(u)

	slog.Info("Bot started")

	for update := range updates {
		handler.HandleUpdate(update)
	}
}
