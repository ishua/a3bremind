package main

import (
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/a3bremind/a3bremindbot/internal/bot"
	"github.com/a3bremind/a3bremindbot/internal/domain"
	"github.com/a3bremind/a3bremindbot/internal/store"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}

	db, err := store.InitDB("sqlite", "bot.db")
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	botAPI, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("create bot api: %v", err)
	}

	notifier := bot.NewNotifier(botAPI)
	scheduler := domain.New(db, notifier)
	scheduler.Start()
	defer scheduler.Stop()

	handler := bot.NewHandler(db, botAPI, scheduler)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := botAPI.GetUpdatesChan(u)

	log.Println("Bot started")

	for update := range updates {
		handler.HandleUpdate(update)
	}
}
