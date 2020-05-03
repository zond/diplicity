package game

import (
	"net/http"

	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
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
