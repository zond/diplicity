package game

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine/v2/datastore"
	"google.golang.org/appengine/v2/log"

	"firebase.google.com/go/v4/messaging"
	newFcm "github.com/appleboy/go-fcm"

	. "github.com/zond/goaeoas"
)

const (
	fcmConfKind = "FCMConf"
	prodKey     = "prod"
)

func init() {
	FCMSendToTokensFunc = NewDelayFunc("game-fcmSendToTokens", fcmSendToTokens)
	manageFCMTokensFunc = NewDelayFunc("game-manageFCMTokens", manageFCMTokens)
}

var (
	FCMSendToTokensFunc *DelayFunc
	manageFCMTokensFunc *DelayFunc
	prodFCMConf         *FCMConf
	prodFCMConfLock     = sync.RWMutex{}
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
			return HTTPErr{"FCMConf already configured", http.StatusBadRequest}
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
	foundConf := &FCMConf{}
	if err := datastore.Get(ctx, getFCMConfKey(ctx), foundConf); err != nil {
		return nil, err
	}
	prodFCMConf = foundConf
	return prodFCMConf, nil
}

func mutateFCMTokens(ctx context.Context, toMutate map[string]map[string]string, mutator func(*auth.FCMToken, string), cont func() error) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		userConfigs := make([]auth.UserConfig, len(toMutate))
		ids := make([]*datastore.Key, 0, len(toMutate))
		for uid := range toMutate {
			ids = append(ids, auth.UserConfigID(ctx, auth.UserID(ctx, uid)))
		}
		if err := datastore.GetMulti(ctx, ids, userConfigs); err != nil {
			return err
		}
		for i := range userConfigs {
			conf := &userConfigs[i]
			userTokens := toMutate[conf.UserId]
			for j := range conf.FCMTokens {
				fcmToken := &conf.FCMTokens[j]
				if data, found := userTokens[fcmToken.Value]; found {
					mutator(fcmToken, data)
				}
			}
		}
		if _, err := datastore.PutMulti(ctx, ids, userConfigs); err != nil {
			return err
		}
		if cont != nil {
			return cont()
		}
		return nil
	}, &datastore.TransactionOptions{XG: true})
}

func splitMap(at int, m map[string]map[string]string) (m1, m2 map[string]map[string]string) {
	m1 = map[string]map[string]string{}
	m2 = map[string]map[string]string{}
	for uid, sm := range m {
		if len(m1) < at {
			m1[uid] = sm
		} else {
			m2[uid] = sm
		}
	}
	return m1, m2
}

func manageFCMTokens(ctx context.Context, tokensToRemove, tokensToUpdate map[string]map[string]string) error {
	log.Infof(ctx, "manageFCMTokens(..., %+v, %+v)", PP(tokensToRemove), PP(tokensToUpdate))

	if len(tokensToRemove) > 0 {
		toRemove, toDelay := splitMap(4, tokensToRemove)
		return mutateFCMTokens(
			ctx,
			toRemove,
			func(tok *auth.FCMToken, errMsg string) {
				tok.Disabled = true
				tok.Note = errMsg
			},
			func() error {
				if len(toDelay) > 0 || len(tokensToUpdate) > 0 {
					return manageFCMTokensFunc.EnqueueIn(ctx, 0, toDelay, tokensToUpdate)
				}
				return nil
			},
		)
	}

	if len(tokensToUpdate) > 0 {
		toUpdate, toDelay := splitMap(4, tokensToUpdate)
		return mutateFCMTokens(
			ctx,
			toUpdate,
			func(tok *auth.FCMToken, newValue string) {
				tok.Note = fmt.Sprintf("Updated from %q at %v due to FCM service indication.", tok.Value, time.Now())
				tok.Value = newValue
			},
			func() error {
				if len(toDelay) > 0 || len(tokensToUpdate) > 0 {
					return manageFCMTokensFunc.EnqueueIn(ctx, 0, tokensToRemove, toDelay)
				}
				return nil
			},
		)
	}

	log.Infof(ctx, "manageFCMTokens(..., %+v, %+v) *** SUCCESS ***", PP(tokensToRemove), PP(tokensToUpdate))

	return nil
}

func fcmSendToTokens(ctx context.Context, lastDelay time.Duration, notification *messaging.Notification, tokens map[string][]string) error {
	log.Infof(ctx, "fcmSendToTokens called")

	tokenStrings := []string{}
	userByToken := map[string]string{}
	for uid, userTokens := range tokens {
		for _, tokenString := range userTokens {
			if tokenString != "" {
				tokenStrings = append(tokenStrings, tokenString)
				userByToken[tokenString] = uid
			} else {
				log.Infof(ctx, "Ignoring empty token for %q", uid)
			}
		}
	}

	if len(tokenStrings) == 0 {
		log.Infof(ctx, "No tokens left, exiting")
		return nil
	}

	log.Infof(ctx, "Creating new FCM client...")
	newClient, err := newFcm.NewClient(
		ctx,
		newFcm.WithCredentialsFile("service-account-key.json"),
	)
	if err != nil {
		log.Errorf(ctx, "Unable to create FCM client: %v", err)
	}
	log.Infof(ctx, "New FCM client created!")

	msg := &messaging.MulticastMessage{
		Tokens:       tokenStrings,
		Notification: notification,
	}

	log.Infof(ctx, "Sending FCM message...")
	newResp, err := newClient.SendMulticast(ctx, msg)
	if err != nil {
		log.Errorf(ctx, "Unable to send FCM message: %v", err)
	}
	log.Infof(ctx, "%d messages were sent successfully", newResp.SuccessCount)

	if newResp.FailureCount > 0 {
		var failedTokens []string
		for i, resp := range newResp.Responses {
			if !resp.Success {
				failedTokens = append(failedTokens, tokenStrings[i])
				log.Infof(ctx, "Failed to send message to token %s: %v", tokenStrings[i], resp.Error)
			}
		}
		log.Infof(ctx, "Failed tokens: %v", failedTokens)
	}

	return nil
}
