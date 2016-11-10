package game

import (
	"fmt"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
)

const (
	fcmConfKind = "FCMConf"
	prodKey     = "prod"
)

var (
	prodFCMConf     *FCMConf
	prodFCMConfLock = sync.RWMutex{}
)

type FCMConf struct {
	ServerKey string
}

func getFCMConfKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, fcmConfKind, prodKey, 0, nil)
}

func SetFCMConf(ctx context.Context, fcmConf *FCMConf) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentFCMConf := &FCMConf{}
		if err := datastore.Get(ctx, getFCMConfKey(ctx), currentFCMConf); err == nil {
			return fmt.Errorf("FCMConf already configured")
		}
		if _, err := datastore.Put(ctx, getFCMConfKey(ctx), fcmConf); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

func getFCMConf(ctx context.Context) (*FCMConf, error) {
	prodFCMConfLock.RLock()
	if prodFCMConf != nil {
		defer prodFCMConfLock.RUnlock()
		return prodFCMConf, nil
	}
	prodFCMConfLock.RUnlock()
	prodFCMConfLock.Lock()
	defer prodFCMConfLock.Unlock()
	prodFCMConf = &FCMConf{}
	if err := datastore.Get(ctx, getFCMConfKey(ctx), prodFCMConf); err != nil {
		return nil, err
	}
	return prodFCMConf, nil
}
