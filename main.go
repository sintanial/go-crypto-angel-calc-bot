package main

import (
	"context"
	"flag"
	"fmt"
	telegrambotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	tgSupportUsername := flag.String("support", "", "Telegram support username")
	flag.Parse()

	if *tgbotToken == "" {
		log.Fatalln(`missing requested parameter "token"`)
		return
	}

	tgbotapi, err := telegrambotapi.NewBotAPI(*tgbotToken)
	if err != nil {
		log.Fatalln("failed create new bot api", "token", *tgbotToken, "err", err)
		return
	}
	tgbotapi.Debug = false

	rdb := redis.NewClient(&redis.Options{
		Addr: *redisDsn,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalln("failed connect to redis server", "dsn", *redisDsn, "err", err)
		return
	}

	u := telegrambotapi.NewUpdate(0)
	u.Timeout = 120

	updates, err := tgbotapi.GetUpdatesChan(u)

	fmt.Println("bot is started .....")

	zapconf := zap.NewDevelopmentConfig()
	zapconf.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapconf.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zaplog, err := zapconf.Build()
	if err != nil {
		log.Fatalln("failed create zap logger")
		return
	}

	bot := tgbot.NewTgBot(rdb, tgbotapi, zaplog, *tgSupportUsername)

	for update := range updates {
		log.Println("incoming update", update)
		go bot.HandleUpdate(update)
	}
}
