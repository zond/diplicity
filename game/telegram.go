package game

import (
	"encoding/json"
	"net/http"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
)

const (
	telegramConfKind = "TelegramConf"
)

type TelegramConf struct {
	BotToken string
}

func getTelegramConfKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, telegramConfKind, prodKey, 0, nil)
}

func SetTelegramConf(ctx context.Context, telegramConf *TelegramConf) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentTelegramConf := &TelegramConf{}
		if err := datastore.Get(ctx, getTelegramConfKey(ctx), currentTelegramConf); err == nil {
			return HTTPErr{"TelegramConf already configured", http.StatusBadRequest}
		}
		if _, err := datastore.Put(ctx, getTelegramConfKey(ctx), telegramConf); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

type TelegramUser struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

type TelegramChat struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Type      string `json:"type"`
}

type TelegramEntity struct {
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
	Type   string `json:"type"`
}

type TelegramMessage struct {
	MessageID int64            `json:"message"`
	From      TelegramUser     `json:"from"`
	Chat      TelegramChat     `json:"chat"`
	Date      int64            `json:"date"`
	Text      string           `json:"text"`
	Entities  []TelegramEntity `json:"entities"`
}

type TelegramUpdate struct {
	UpdateID int64           `json:"update_id"`
	Message  TelegramMessage `json:"message"`
}

func handleTelegramWebhook(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	update := &TelegramUpdate{}
	if err := json.NewDecoder(r.Req().Body).Decode(update); err != nil {
		return err
	}

	log.Infof(ctx, "Got telegram update %+v", update)
	return nil
}
