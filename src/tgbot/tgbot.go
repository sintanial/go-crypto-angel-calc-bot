package tgbot

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/json-iterator/go"
	"go.uber.org/zap"
	"regexp"
	"strconv"
	"strings"
)

const StateSetRiskPercentage = "set_risk_percentage"
const StateGetOfferVolume = "get_offer_volume"

type TgBot struct {
	rds     *redis.Client
	tgapi   *tgbotapi.BotAPI
	lg      *zap.Logger
	support string
}

func NewTgBot(rds *redis.Client, tgapi *tgbotapi.BotAPI, lg *zap.Logger, support string) *TgBot {
	return &TgBot{rds: rds, tgapi: tgapi, lg: lg, support: support}
}

func (self *TgBot) HandleUpdate(update tgbotapi.Update) {
	if update.Message != nil {
		if update.Message.IsCommand() {
			cmd := update.Message.Command()
			if cmd == "start" {
				self.OnStartCommand(update)
				return
			} else if cmd == "newriskpercent" {
				self.OnRequestRiskPercentage(update)
				return
			}
		} else if checkIsCryptoAngelMessage.MatchString(update.Message.Text) {
			self.OnGetCryptoAngelOffer(update)
			return
		} else {
			state, data, err := self.getCurrentState(update.Message.Chat.ID)
			if err != nil {
				self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed get current state from redis", err)
				return
			}

			if state == StateSetRiskPercentage {
				self.OnResponseRiskPercentage(update)
				return
			} else if state == StateGetOfferVolume {
				self.OnRequestCalc(update, data)
				return
			} else {
				self.sendTgDefaultMessage(update.Message.Chat.ID)
				return
			}
		}
	}
}

func (self *TgBot) OnStartCommand(update tgbotapi.Update) {
	text := "Привет трейдер. " +
		"\nБот поможет тебе определиться с объёмом сделки в зависимости от твоего депозита и процента риска."

	risk, err := self.getRiskPercentage(update.Message.From.ID)
	if err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed get risk percentage from redis", err)
		return
	}

	if risk == 0 {
		text += "\nДля начала давай укажем процент риска."
	}

	self.sendTgMessage(update.Message.Chat.ID, text)

	if risk == 0 {
		self.OnRequestRiskPercentage(update)
	} else {
		self.sendTgDefaultMessage(update.Message.Chat.ID)
	}
}

type CurrentState struct {
	State string `json:"state"`
	Data  string `json:"data"`
}

func (self *TgBot) OnRequestRiskPercentage(update tgbotapi.Update) {
	percentage, err := self.getRiskPercentage(update.Message.From.ID)
	if err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed get risk percentage from redis", err)
		return
	}

	if err := self.setCurrentState(update.Message.Chat.ID, StateSetRiskPercentage, ""); err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed set current state to redis", err)
		return
	}

	self.sendTgMessage(update.Message.Chat.ID, fmt.Sprintf("Твой текущий процент риска: *%.2f*%%.\n\nУкажи новый процент", percentage))
}

var clearNumberRegex = regexp.MustCompile(`[^0-9.]`)

func (self *TgBot) OnResponseRiskPercentage(update tgbotapi.Update) {
	text := strings.Replace(update.Message.Text, ",", ".", -1)
	text = clearNumberRegex.ReplaceAllString(text, "")

	percent, err := strconv.ParseFloat(text, 64)
	if err != nil {
		self.sendTgMessage(update.Message.Chat.ID, "Неверный формат данных, должно быть число")
		return
	}

	if err := self.setRiskPercentage(update.Message.From.ID, percent); err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed set risk percentage to redis", err)
		return
	}

	self.delCurrentState(update.Message.Chat.ID)

	self.sendTgMessage(update.Message.Chat.ID, "Ваш процент обновлен.")
	self.sendTgMessage(update.Message.Chat.ID, "Отправь мне сообщение из канала CRYPTO-ANGEL и я подскажу с объёмом сделки")
}

var checkIsCryptoAngelMessage = regexp.MustCompile("USDT.*?(Лонг|Шорт).*?")

var parseCryptoCode = regexp.MustCompile(`([\w]*USDT)`)
var parseInputRangePrice = regexp.MustCompile(`Диапазон.*?(\d+).*?-.*?(\d+)`)
var parseStopPrice = regexp.MustCompile(`Стоп.*?(\d+\.?\d*).*?\(-(\d+\.?\d*).*?\)`)

//var parseTargetPrice = regexp.MustCompile(`Цель.*?(\d+\.?\d*).*?(\d+\.?\d*).*?`)

type CryptoAngelOffer struct {
	CryptoCode     string  `json:"crypto_code"`
	MinRangePrice  float64 `json:"min_range_price"`
	MaxRangePrice  float64 `json:"max_range_price"`
	StopPrice      float64 `json:"stop_price"`
	StopPercentage float64 `json:"stop_percentage"`
}

func (self *TgBot) OnGetCryptoAngelOffer(update tgbotapi.Update) {
	text := update.Message.Text
	cryptoCodeSubmatch := parseCryptoCode.FindAllStringSubmatch(text, -1)
	cryptoCode := strings.Replace(cryptoCodeSubmatch[0][1], "USDT", "", -1)

	rangePriceSubmatch := parseInputRangePrice.FindAllStringSubmatch(text, -1)
	minRangePrice, _ := strconv.ParseFloat(rangePriceSubmatch[0][1], 64)
	maxRangePrice, _ := strconv.ParseFloat(rangePriceSubmatch[0][2], 64)

	stopPriceSubmatch := parseStopPrice.FindAllStringSubmatch(text, -1)
	stopPrice, _ := strconv.ParseFloat(stopPriceSubmatch[0][1], 64)
	stopPercentage, _ := strconv.ParseFloat(stopPriceSubmatch[0][2], 64)

	offer := CryptoAngelOffer{
		CryptoCode:     cryptoCode,
		MinRangePrice:  minRangePrice,
		MaxRangePrice:  maxRangePrice,
		StopPrice:      stopPrice,
		StopPercentage: stopPercentage,
	}

	data, err := jsoniter.MarshalToString(offer)
	if err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed marshal offer", err)
		return
	}

	if err := self.setCurrentState(update.Message.Chat.ID, StateGetOfferVolume, data); err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed set current state in redis", err)
		return
	}

	self.sendTgMessage(update.Message.Chat.ID, "Укажите размер депозита")
}

func (self *TgBot) OnRequestCalc(update tgbotapi.Update, data string) {
	var offer CryptoAngelOffer
	if err := jsoniter.UnmarshalFromString(data, &offer); err != nil {
		self.delCurrentState(update.Message.Chat.ID)
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed unmarshal crypto angel offer", err)
		return
	}

	text := strings.Replace(update.Message.Text, ",", ".", -1)
	text = clearNumberRegex.ReplaceAllString(text, "")
	deposit, err := strconv.ParseFloat(text, 64)
	if err != nil {
		self.sendTgMessage(update.Message.Chat.ID, "Неверный формат данных, должно быть число")
		return
	}

	risk, err := self.getRiskPercentage(update.Message.From.ID)
	if err != nil {
		self.sendTgInternalErrorMessage(update.Message.Chat.ID, "failed get risk percentage from redis", err)
		return
	}

	positionVolume := deposit / offer.StopPercentage
	minTokenVolume := positionVolume / offer.MaxRangePrice
	maxTokenVolume := positionVolume / offer.MinRangePrice

	var replyText string

	replyText += fmt.Sprintf("Для открытия позиции \n\n")
	replyText += fmt.Sprintf("Объём средств: *$%.2f*\n\n", positionVolume)
	replyText += fmt.Sprintf("Кол-во токенов (с риском *%.2f%%*):\n\n", risk)
	replyText += fmt.Sprintf("При цене входа *$%.2f*:  *%v* `%.4f`\n", offer.MaxRangePrice, offer.CryptoCode, minTokenVolume*risk)
	replyText += fmt.Sprintf("При цене входа *$%.2f*:  *%v* `%.4f`", offer.MinRangePrice, offer.CryptoCode, maxTokenVolume*risk)

	self.sendTgMessage(update.Message.Chat.ID, replyText)
}

func (self *TgBot) sendTgMessage(chatId int64, message string) {
	msg := tgbotapi.NewMessage(chatId, message)
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := self.tgapi.Send(msg); err != nil {
		self.lg.Warn("failed send telegram message", zap.Error(err))
	}
}

func (self *TgBot) sendTgDefaultMessage(chatId int64) {
	self.sendTgMessage(chatId,
		"Отправь мне сообщение из канала CRYPTO-ANGEL и я подскажу с объёмом сделки",
	)
}

func (self *TgBot) sendTgInternalErrorMessage(chatId int64, log string, err error) {
	self.lg.Error(log, zap.Error(err))
	msg := "Что то пошло не так, попробуйте снова"

	if self.support != "" {
		msg += " или обратитесь в чат @" + self.support
	}

	self.sendTgMessage(chatId, msg)
}

func (self *TgBot) setRiskPercentage(userId int, percent float64) error {
	return self.rds.Set(context.Background(), fmt.Sprintf("risk_percentage:%v", userId), percent, -1).Err()
}

func (self *TgBot) getRiskPercentage(userId int) (float64, error) {
	data, err := self.rds.Get(context.Background(), fmt.Sprintf("risk_percentage:%v", userId)).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		} else {
			return 0, err
		}
	}

	return strconv.ParseFloat(data, 64)
}

func (self *TgBot) setCurrentState(chatId int64, state string, data string) error {
	raw, err := jsoniter.MarshalToString(CurrentState{
		State: state,
		Data:  data,
	})

	if err != nil {
		return err
	}

	return self.rds.Set(context.Background(), fmt.Sprintf("current_state:%v", chatId), raw, -1).Err()
}

func (self *TgBot) getCurrentState(chatId int64) (string, string, error) {
	raw, err := self.rds.Get(context.Background(), fmt.Sprintf("current_state:%v", chatId)).Result()
	if err != nil {
		if err == redis.Nil {
			return "", "", nil
		} else {
			return "", "", err
		}
	}

	var state CurrentState

	if err := jsoniter.UnmarshalFromString(raw, &state); err != nil {
		return "", "", err
	}

	return state.State, state.Data, nil
}

func (self *TgBot) delCurrentState(chatId int64) {
	self.rds.Del(context.Background(), fmt.Sprintf("current_state:%v", chatId))
}
