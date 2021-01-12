package main

import (
	"context"
	"flag"
	"fmt"
	telegrambotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
	"math/rand"
	"time"

	"cryptoangelcalcbot/src/tgbot"
	"github.com/go-redis/redis/v8"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	tgbotToken := flag.String("token", "", "Telegram bot token")
	redisDsn := flag.String("redis", "localhost:6379", "Redis DSN address")
	flag.Parse()

	if *tgbotToken == "" {
		log.Fatalln(`missing requested parameter "token"`)
		return
	}

	tgbotapi, err := telegrambotapi.NewBotAPI(*tgbotToken)
	if err != nil {
		log.Panic(err)
	}
	tgbotapi.Debug = false

	rdb := redis.NewClient(&redis.Options{
		Addr: *redisDsn,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Panic(err)
	}

	u := telegrambotapi.NewUpdate(0)
	u.Timeout = 120

	updates, err := tgbotapi.GetUpdatesChan(u)

	fmt.Println("bot is started .....")

	bot := tgbot.NewTgBot(rdb, tgbotapi)

	for update := range updates {
		log.Println("incoming update", update)
		go bot.HandleUpdate(update)
	}
}
