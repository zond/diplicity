package game

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/zond/go-fcm"
	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/delay"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"
	"google.golang.org/appengine/urlfetch"
)

const (
	fcmConfKind = "FCMConf"
	prodKey     = "prod"
)

func init() {
	fcmNotifyFunc = delay.Func("game-fcmNotify", fcmNotify)
}

var (
	fcmNotifyFunc   *delay.Function
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

func FCMNotify(ctx context.Context, delay time.Duration, notif *fcm.NotificationPayload, data interface{}, tokens []string) error {
	t, err := fcmNotifyFunc.Task(delay, notif, data, tokens)
	if err != nil {
		return err
	}
	t.Delay = delay
	_, err = taskqueue.Add(ctx, t, "game-fcmNotify")
	return err
}

func fcmNotify(ctx context.Context, lastDelay time.Duration, notif *fcm.NotificationPayload, data interface{}, tokens []string) error {
	log.Infof(ctx, "fcmNotify(..., %v, %v, %+v)", spew.Sdump(notif), spew.Sdump(data), tokens)

	newTokens := []string{}
	for _, tok := range tokens {
		if tok != "" {
			newTokens = append(newTokens, tok)
		}
	}
	tokens = newTokens

	fcmConf, err := getFCMConf(ctx)
	if err != nil {
		// Safe to retry, nothing got sent.
		log.Errorf(ctx, "Unable to get FCMConf: %v; fix getFCMConf or hope datastore gets fixed", err)
		return err
	}

	client := fcm.NewFcmClient(fcmConf.ServerKey)
	client.SetHTTPClient(urlfetch.Client(ctx))
	client.AppendDevices(tokens)
	if notif != nil {
		client.SetNotificationPayload(notif)
	}
	if data != nil {
		client.SetMsgData(data)
	}

	resp, err := client.Send()
	if err != nil {
		// Safe to retry, nothing got sent probably.
		log.Errorf(ctx, "%v unable to send: %v", spew.Sdump(client), err)
		return err
	}

	log.Infof(ctx, "Sent %v, received %v, %v in response", spew.Sdump(client), spew.Sdump(resp), err)

	if resp.StatusCode == 401 {
		// Safe to retry, we will just keep delaying incrementally until the auth gets fixed.
		msg := fmt.Sprintf("%v unable to send due to 401: %v; fix your authentication", spew.Sdump(client), spew.Sdump(resp))
		log.Errorf(ctx, msg)
		return fmt.Errorf(msg)
	}

	if resp.StatusCode == 400 {
		// Can't retry, our payload is fucked up.
		log.Errorf(ctx, "%v unable to send due to 400: %v; unable to recover", spew.Sdump(client), spew.Sdump(resp))
		return nil
	}

	idsToRetry := tokens
	if resp.StatusCode > 199 && resp.StatusCode < 299 {
		// Now we have to take care what we retry - retries might lead to duplicates.
		idsToUpdate := map[string]string{}
		idsToRemove := map[string]string{}
		idsToRetry = []string{}

		for i, result := range resp.Results {
			if newID, found := result["registration_id"]; found {
				idsToUpdate[tokens[i]] = newID
			}
			if errMsg, found := result["error"]; found {
				switch errMsg {
				case "InvalidRegistration":
					fallthrough
				case "NotRegistered":
					fallthrough
				case "MismatchSenderId":
					log.Errorf(ctx, "Token %q got %q, will remove it.", tokens[i], errMsg)
					idsToRemove[tokens[i]] = errMsg
				case "Unavailable":
					// Can be retried, it's supposed to be.
					fallthrough
				case "InternalServerError":
					// Can be retried, it's supposed to be.
					log.Errorf(ctx, "Token %q got %q, will retry.", tokens[i], errMsg)
					idsToRetry = append(idsToRetry, tokens[i])
				case "DeviceMessageRateExceeded":
					fallthrough
				case "TopicsMessageRateExceeded":
					fallthrough
				case "MissingRegistration":
					fallthrough
				case "InvalidTtl":
					fallthrough
				case "InvalidPackageName":
					log.Errorf(ctx, "Token %q got %q, wtf?", tokens[i], errMsg)
				case "MessageTooBig":
					log.Errorf(ctx, "Token %q got %q, SEND SMALLER MESSAGES DAMNIT!", tokens[i], errMsg)
				case "InvalidDataKey":
					log.Errorf(ctx, "Token %q got %q, SEND CORRECT MESSAGES DAMNIT!", tokens[i], errMsg)
				}
			}
		}
		// remove and update tokens here
	}

	if len(idsToRetry) > 0 {
		// Right, we still have something to retry, but might also have a Retry-After header.
		// First, assume we just double the old delay (or 1 sec).
		delay := lastDelay * 2
		if delay < time.Second {
			delay = time.Second
		}
		// Then, try to honor the Retry-After header.
		if n, err := strconv.ParseInt(resp.RetryAfter, 10, 64); err == nil {
			delay = time.Duration(n) * time.Minute
		} else if at, err := time.Parse(time.RFC1123, resp.RetryAfter); err == nil {
			delay = at.Sub(time.Now())
		}
		// Finally, try to schedule again. If we can't then fuckall we'll try again with the entire payload.
		if err := FCMNotify(ctx, delay, notif, data, tokens); err != nil {
			log.Errorf(ctx, "Unable to schedule retry of %v, %v to %+v in %v: %v", spew.Sdump(notif), spew.Sdump(data), tokens, delay, err)
			return err
		}
	}

	log.Infof(ctx, "fcmNotify(...) *** SUCCESS ***")

	return nil
}
