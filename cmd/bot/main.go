package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"story-points/internal/bot"
	"story-points/internal/db"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No se encontró archivo .env, se usan variables de entorno del sistema")
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("Falta la variable de entorno BOT_TOKEN")
	}

	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		dsn = "story_points.db"
	}

	database, err := db.InitDB(dsn)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer database.Close()

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("init bot: %v", err)
	}

	log.Printf("Bot autorizado como @%s", api.Self.UserName)

	handler := bot.New(api, database, api.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := api.GetUpdatesChan(u)
	for update := range updates {
		handler.HandleUpdate(update)
	}
}
