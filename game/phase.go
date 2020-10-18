package game

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/go-fcm"
	"github.com/zond/godip"
	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	dvars "github.com/zond/diplicity/variants"
	vrt "github.com/zond/godip/variants/common"

	. "github.com/zond/goaeoas"
)

var (
	asyncResolvePhaseFunc             *DelayFunc
	timeoutResolvePhaseFunc           *DelayFunc
	planPhaseTimeoutFunc              *DelayFunc
	sendPhaseDeadlineWarningFunc      *DelayFunc
	sendPhaseNotificationsToUsersFunc *DelayFunc
	sendPhaseNotificationsToFCMFunc   *DelayFunc
	sendPhaseNotificationsToMailFunc  *DelayFunc
	ejectProbationariesFunc           *DelayFunc
	PhaseResource                     *Resource
)

func init() {
	asyncResolvePhaseFunc = NewDelayFunc("game-asyncResolvePhase", asyncResolvePhase)
	timeoutResolvePhaseFunc = NewDelayFunc("game-timeoutResolvePhase", timeoutResolvePhase)
	sendPhaseNotificationsToUsersFunc = NewDelayFunc("game-sendPhaseNotificationsToUsers", sendPhaseNotificationsToUsers)
	sendPhaseNotificationsToFCMFunc = NewDelayFunc("game-sendPhaseNotificationsToFCM", sendPhaseNotificationsToFCM)
	sendPhaseNotificationsToMailFunc = NewDelayFunc("game-sendPhaseNotificationsToMail", sendPhaseNotificationsToMail)
	ejectProbationariesFunc = NewDelayFunc("game-ejectProbationaries", ejectProbationaries)
	planPhaseTimeoutFunc = NewDelayFunc("game-planPhaseTimeout", planPhaseTimeout)
	sendPhaseDeadlineWarningFunc = NewDelayFunc("game-sendPhaseDeadlineWarning", sendPhaseDeadlineWarning)

	PhaseResource = &Resource{
		Load:     loadPhase,
		FullPath: "/Game/{game_id}/Phase/{phase_ordinal}",
		Listers: []Lister{
			{
				Path:    "/Game/{game_id}/Phases",
				Route:   ListPhasesRoute,
				Handler: listPhases,
			},
		},
	}
}

func zipOptions(ctx context.Context, options interface{}) ([]byte, error) {
	zippedOptionsBuffer := &bytes.Buffer{}
	marshalledOptionsWriter := gzip.NewWriter(zippedOptionsBuffer)
	if err := json.NewEncoder(marshalledOptionsWriter).Encode(options); err != nil {
		log.Errorf(ctx, "While trying to decode zipped options: %v", err)
		return nil, err
	}
	if err := marshalledOptionsWriter.Close(); err != nil {
		log.Errorf(ctx, "While trying to close zipped options: %v", err)
		return nil, err
	}
	return zippedOptionsBuffer.Bytes(), nil
}

func unzipOptions(ctx context.Context, b []byte) (interface{}, error) {
	zippedReader, err := gzip.NewReader(bytes.NewBuffer(b))
	if err != nil {
		log.Warningf(ctx, "While trying to create zipped options reader: %v", err)
		return nil, err
	}
	var opts interface{}
	if err := json.NewDecoder(zippedReader).Decode(&opts); err != nil {
		log.Errorf(ctx, "While trying to read zipped options: %v", err)
		return nil, err
	}
	return opts, nil
}

type phaseNotificationContext struct {
	userID       *datastore.Key
	userConfigID *datastore.Key
	phaseID      *datastore.Key
	memberID     *datastore.Key
	game         *Game
	phase        *Phase
	member       *Member
	user         *auth.User
	userConfig   *auth.UserConfig
	mapURL       *url.URL
	fcmData      map[string]interface{}
	mailData     map[string]interface{}
}

func getPhaseNotificationContext(ctx context.Context, host string, gameID *datastore.Key, phaseOrdinal int64, userId string) (*phaseNotificationContext, error) {
	res := &phaseNotificationContext{}

	var err error
	res.phaseID, err = PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		log.Errorf(ctx, "PhaseID(..., %v, %v): %v; fix the PhaseID func", gameID, phaseOrdinal, err)
		return nil, err
	}

	res.userID = auth.UserID(ctx, userId)

	res.userConfigID = auth.UserConfigID(ctx, res.userID)

	res.game = &Game{}
	res.phase = &Phase{}
	res.user = &auth.User{}
	res.userConfig = &auth.UserConfig{}
	err = datastore.GetMulti(ctx, []*datastore.Key{gameID, res.phaseID, res.userConfigID, res.userID}, []interface{}{res.game, res.phase, res.userConfig, res.user})
	if err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			for idx, serr := range merr {
				if serr != nil {
					if idx == 2 && serr == datastore.ErrNoSuchEntity {
						log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
						return nil, noConfigError
					} else if idx == 0 && serr == datastore.ErrNoSuchEntity {
						log.Warningf(ctx, "Game doesn't exist, this must be a manually deleted game")
						return nil, noGameError
					} else if idx == 1 && serr == datastore.ErrNoSuchEntity {
						log.Infof(ctx, "Phase doesn't exist, this must be a reverted mustering game, faking phase")
						if res.game.Variant == "" {
							log.Errorf(ctx, "Loaded game lacks variant. Errors: %+v", err)
							return nil, err
						}
						variant := variants.Variants[res.game.Variant]
						s, err := variant.Start()
						if err != nil {
							log.Errorf(ctx, "Unable to create state to generate fake phase for notification: %v", err)
							return nil, err
						}
						res.phase = NewPhase(s, gameID, 1, host)
					} else {
						log.Errorf(ctx, "Unable to load game or user: %v; hope datastore gets fixed", err)
						return nil, err
					}
				}
			}
		} else {
			log.Errorf(ctx, "Unable to load game, phase, user and user config: %v; hope datastore gets fixed", err)
			return nil, err
		}
	}
	res.game.ID = gameID

	isMember := false
	res.member, isMember = res.game.GetMemberByUserId(userId)
	if !isMember {
		log.Errorf(ctx, "%q is not a member of %v, wtf? Exiting.", userId, res.game)
		return nil, noConfigError
	}

	res.mapURL, err = router.Get(RenderPhaseMapRoute).URL("game_id", res.game.ID.Encode(), "phase_ordinal", fmt.Sprint(res.phase.PhaseOrdinal))
	if err != nil {
		log.Errorf(ctx, "Unable to create map URL for game %v and phase %v: %v; wtf?", res.game.ID, res.phase.PhaseOrdinal, err)
		return nil, err
	}
	res.mapURL.Host = host
	res.mapURL.Scheme = DefaultScheme

	res.mailData = map[string]interface{}{
		"phaseMeta": res.phase.PhaseMeta,
		"game":      res.game,
		"user":      res.user,
		"mapLink":   res.mapURL.String(),
	}
	res.fcmData = map[string]interface{}{
		"type":      "phase",
		"gameID":    res.game.ID,
		"gameDesc":  res.game.DescFor(res.member.Nation),
		"phaseMeta": res.phase.PhaseMeta,
	}

	return res, nil
}

func ejectProbationaries(ctx context.Context, probationaries []string) error {
	log.Infof(ctx, "ejectProbationaries(..., %+v)", probationaries)

	for _, probationary := range probationaries {
		ids, err := datastore.NewQuery(gameKind).Filter("Private=", false).Filter("Started=", false).Filter("Members.User.Id=", probationary).KeysOnly().GetAll(ctx, nil)
		if err != nil {
			log.Infof(ctx, "Unable to load staging games for %q: %v; hope datastore gets fixed", probationary, err)
			return err
		}
		for _, gameID := range ids {
			if _, err := deleteMemberHelper(ctx, gameID, probationary, true); err != nil {
				log.Infof(ctx, "Unable to delete %q from game %v: %v; fix 'deleteMemberHelper' or hope datastore gets fixed", probationary, gameID, err)
				return err
			}
		}
	}

	log.Infof(ctx, "ejectProbationaries(..., %+v) *** SUCCESS ***", probationaries)

	return nil
}

func sendPhaseNotificationsToMail(ctx context.Context, host string, gameID *datastore.Key, phaseOrdinal int64, userId string) error {
	log.Infof(ctx, "sendPhaseNotificationsToMail(..., %q, %v, %v, %q)", host, gameID, phaseOrdinal, userId)

	msgContext, err := getPhaseNotificationContext(ctx, host, gameID, phaseOrdinal, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err == noGameError {
		log.Warningf(ctx, "%q doesn't exist, giving up", gameID)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get msg notification context: %v; fix getPhaseNotificationContext or hope datastore gets fixed", err)
		return err
	}

	if !msgContext.userConfig.MailConfig.Enabled {
		log.Infof(ctx, "%q hasn't enabled mail notifications for mail, will skip sending notification", userId)
		return nil
	}

	unsubscribeURL, err := auth.GetUnsubscribeURL(ctx, router, host, userId)
	if err != nil {
		log.Errorf(ctx, "Unable to create unsubscribe URL for %q: %v; fix auth.GetUnsubscribeURL", userId, err)
		return err
	}

	msgContext.mailData["unsubscribeURL"] = unsubscribeURL.String()

	msg := &auth.EMail{}
	msg.TextBody = fmt.Sprintf(
		"%s has a new phase: %s\n\nVisit %s to stop receiving email like this.",
		msgContext.game.Desc,
		msgContext.mapURL.String(),
		unsubscribeURL.String())
	msg.Subject = fmt.Sprintf(
		"%s: %s %d, %s",
		msgContext.game.DescFor(msgContext.member.Nation),
		msgContext.phase.Season,
		msgContext.phase.Year,
		msgContext.phase.Type,
	)
	msg.UnsubscribeURL = unsubscribeURL.String()

	msgContext.userConfig.MailConfig.MessageConfig.Customize(ctx, msg, msgContext.mailData)

	recipEmail, err := mail.ParseAddress(msgContext.user.Email)
	if err != nil {
		log.Errorf(ctx, "Unable to parse email address of %v: %v; unable to recover, exiting", PP(msgContext.user), err)
		return nil
	}
	msg.ToAddr = recipEmail.Address
	msg.ToName = string(msgContext.member.Nation)

	msg.FromAddr = noreplyFromAddr
	msg.FromName = noreplyFromName

	if err := msg.Send(ctx); err != nil {
		log.Errorf(ctx, "Unable to send %v: %v; hope sendgrid gets fixed", msg, err)
		return err
	}
	log.Infof(ctx, "Successfully sent %v", PP(msg))

	log.Infof(ctx, "sendPhaseNotificationsToMail(..., %q, %v, %v, %q) *** SUCCESS ***", host, gameID, phaseOrdinal, userId)

	return nil
}

func sendPhaseNotificationsToFCM(ctx context.Context, host string, gameID *datastore.Key, phaseOrdinal int64, userId string, finishedTokens map[string]struct{}) error {
	log.Infof(ctx, "sendPhaseNotificationsToFCM(..., %q, %v, %v, %q, %+v)", host, gameID, phaseOrdinal, userId, finishedTokens)

	msgContext, err := getPhaseNotificationContext(ctx, host, gameID, phaseOrdinal, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err == noGameError {
		log.Warningf(ctx, "%q doesn't exists, giving up", gameID)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get phase notification context: %v; fix getPhaseNotificationContext or hope datastore gets fixed", err)
		return err
	}

	dataPayload, err := NewFCMData(msgContext.fcmData)
	if err != nil {
		log.Errorf(ctx, "Unable to encode FCM data payload %v: %v; fix NewFCMData", msgContext.fcmData, err)
		return err
	}

	for _, fcmToken := range msgContext.userConfig.FCMTokens {
		if fcmToken.Disabled {
			continue
		}
		if _, done := finishedTokens[fcmToken.Value]; done {
			continue
		}
		finishedTokens[fcmToken.Value] = struct{}{}
		notificationPayload := &fcm.NotificationPayload{
			Title: fmt.Sprintf(
				"%s: %s %d, %s",
				msgContext.game.DescFor(msgContext.member.Nation),
				msgContext.phase.Season,
				msgContext.phase.Year,
				msgContext.phase.Type,
			),
			Body:        fmt.Sprintf("%s has a new phase.", msgContext.game.Desc),
			Tag:         "diplicity-engine-new-phase",
			ClickAction: msgContext.mapURL.String(),
		}

		fcmToken.PhaseConfig.Customize(ctx, notificationPayload, msgContext.mailData)
		if fcmToken.MessageConfig.DontSendData {
			dataPayload = nil
		}
		if fcmToken.MessageConfig.DontSendNotification {
			notificationPayload = nil
		}

		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			if err := FCMSendToTokensFunc.EnqueueIn(
				ctx,
				0,
				time.Duration(0),
				notificationPayload,
				dataPayload,
				map[string][]string{
					userId: []string{fcmToken.Value},
				},
			); err != nil {
				log.Errorf(ctx, "Unable to enqueue actual sending of notification to %v/%v: %v; fix FCMSendToUsers or hope datastore gets fixed", userId, fcmToken.Value, err)
				return err
			}

			if len(msgContext.userConfig.FCMTokens) > len(finishedTokens) {
				if err := sendPhaseNotificationsToFCMFunc.EnqueueIn(ctx, 0, host, gameID, phaseOrdinal, userId, finishedTokens); err != nil {
					log.Errorf(ctx, "Unable to enqueue sending of rest of notifications: %v; hope datastore gets fixed", err)
					return err
				}
			}

			return nil
		}, &datastore.TransactionOptions{XG: true}); err != nil {
			log.Errorf(ctx, "Unable to commit send tx: %v", err)
			return err
		}
		log.Infof(ctx, "Successfully sent a notification and enqueued sending the rest, exiting")
		break
	}

	log.Infof(ctx, "sendPhaseNotificationsToFCM(..., %q, %v, %v, %q, %+v) *** SUCCESS ***", host, gameID, phaseOrdinal, userId, finishedTokens)

	return nil
}

func sendPhaseNotificationsToUsers(ctx context.Context, host string, gameID *datastore.Key, phaseOrdinal int64, origUids []string) error {
	log.Infof(ctx, "sendPhaseNotificationsToUsers(..., %q, %v, %v, %+v)", host, gameID, phaseOrdinal, origUids)

	if len(origUids) == 0 {
		log.Infof(ctx, "sendPhaseNotificationsToUsers(..., %q, %v, %v, %+v) *** NO UIDS ***", host, gameID, phaseOrdinal, origUids)
		return nil
	}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		uids := make([]string, len(origUids))
		copy(uids, origUids)
		for i := 0; i < 2 && len(uids) > 0; i++ {
			nextUid := uids[0]
			uids = uids[1:]
			if err := sendPhaseNotificationsToFCMFunc.EnqueueIn(ctx, 0, host, gameID, phaseOrdinal, nextUid, map[string]struct{}{}); err != nil {
				log.Errorf(ctx, "Unable to enqueue sending to %q: %v; hope datastore gets fixed", nextUid, err)
				return err
			}
			if err := sendPhaseNotificationsToMailFunc.EnqueueIn(ctx, 0, host, gameID, phaseOrdinal, nextUid); err != nil {
				log.Errorf(ctx, "Unable to enqueue sending mail to %q: %v; hope datastore gets fixed", nextUid, err)
				return err
			}
		}
		if len(uids) > 0 {
			if err := sendPhaseNotificationsToUsersFunc.EnqueueIn(ctx, 0, host, gameID, phaseOrdinal, uids); err != nil {
				log.Errorf(ctx, "Unable to enqueue sending to rest: %v; hope datastore gets fixed", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit send tx: %v", err)
		return err
	}

	log.Infof(ctx, "sendPhaseNotificationsToUsers(..., %q, %v, %v, %+v) *** SUCCESS ***", host, gameID, phaseOrdinal, origUids)

	return nil
}

func asyncResolvePhase(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) error {
	return resolvePhaseHelper(ctx, gameID, phaseOrdinal, false)
}

func timeoutResolvePhase(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) error {
	return resolvePhaseHelper(ctx, gameID, phaseOrdinal, true)
}

func sendPhaseDeadlineWarning(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64, nation string) error {
	log.Infof(ctx, "sendPhaseDeadlineWarning(..., %v, %v, %v)", gameID, phaseOrdinal, nation)

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		log.Errorf(ctx, "PhaseID(..., %v, %v): %v, %v; fix the PhaseID func", gameID, phaseOrdinal, phaseID, err)
		return err
	}

	game := &Game{}
	phase := &Phase{}
	keys := []*datastore.Key{gameID, phaseID}
	values := []interface{}{game, phase}
	if err := datastore.GetMulti(ctx, keys, values); err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			for idx, serr := range merr {
				if serr != nil {
					if idx == 1 && serr == datastore.ErrNoSuchEntity {
						log.Infof(ctx, "Phase doesn't exist, assuming this is a mustering game that got reverted. Ignoring.")
						return nil
					} else if idx == 0 && serr == datastore.ErrNoSuchEntity {
						log.Warningf(ctx, "Game doesn't exist, assuming this is a manually deleted game, giving up.")
						return nil
					} else {
						log.Errorf(ctx, "datastore.GetMulti(..., %+v, %+v): %v; hope datastore gets fixed", keys, values, err)
						return err
					}
				}
			}
		} else {
			log.Errorf(ctx, "datastore.GetMulti(..., %+v, %+v): %v; hope datastore gets fixed", keys, values, err)
			return err
		}
	}
	log.Infof(ctx, "Loaded game %+v and phase %+v", game, phase)

	member, found := game.GetMemberByNation(godip.Nation(nation))
	if !found {
		log.Errorf(ctx, "game.GetMemberByNation(%v): %v, %v; wtf?", nation, member, found)
		return nil
	}
	log.Infof(ctx, "Found member %+v", member)

	if !game.Finished && !phase.Resolved && !member.NewestPhaseState.ReadyToResolve {
		userConfigKey := auth.UserConfigID(ctx, auth.UserID(ctx, member.User.Id))
		userConfig := &auth.UserConfig{}
		if err := datastore.Get(ctx, userConfigKey, userConfig); err == datastore.ErrNoSuchEntity {
			log.Warningf(ctx, "UserConfig for %v is gone, assuming manual intervention", userConfigKey)
			return nil
		} else if err != nil {
			log.Errorf(ctx, "Unable to load user config for %v: %v", userConfigKey, err)
			return err
		}
		sendAt := phase.DeadlineAt.Add(-time.Minute * time.Duration(userConfig.PhaseDeadlineWarningMinutesAhead))
		now := time.Now()
		if sendAt.Before(now) {
			newMessage := &Message{
				GameID:         gameID,
				ChannelMembers: Nations{godip.Nation(nation), DiplicitySender},
				Sender:         DiplicitySender,
				Body: fmt.Sprintf(
					"This is a reminder that the current phase will resolve in %v (at %v), and you haven't declared that you are ready for the next phase. If you don't declare ready you will lose Quickness score. If you don't declare ready and don't provide any orders you will lose Reliability score, and be evicted from all staging game queues.",
					phase.DeadlineAt.Sub(now).Round(time.Minute),
					phase.DeadlineAt.Format(time.RFC822)),
			}
			if err := createMessageHelper(ctx, phase.Host, newMessage); err != nil {
				log.Errorf(ctx, "createMessageHelper(..., %v, %+v): %v; fix it?", phase.Host, newMessage, err)
				return err
			}
			log.Infof(ctx, "Successfully sent phase deadline warning!")
		} else {
			log.Infof(ctx, "Want to send at %v, which is after now (%v), rescheduling.", sendAt, now)
			if err := sendPhaseDeadlineWarningFunc.EnqueueAt(ctx, sendAt, gameID, phaseOrdinal, nation); err != nil {
				log.Errorf(ctx, "sendPhaseDeadlineWarningFunc.EnqueueAt(..., %v, %v, %v, %v): %v; hope taskqueues get fixed", sendAt, gameID, phaseOrdinal, nation, err)
				return err
			}
		}
	} else {
		log.Infof(ctx, "Game finished, phase resolved, or member already ready to resolve")
	}

	return nil
}

func planPhaseTimeout(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) error {
	log.Infof(ctx, "planPhaseTimeout(..., %v, %v)", gameID, phaseOrdinal)

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		log.Errorf(ctx, "PhaseID(..., %v, %v): %v, %v; fix the PhaseID func", gameID, phaseOrdinal, phaseID, err)
		return err
	}

	game := &Game{}
	phase := &Phase{}
	keys := []*datastore.Key{gameID, phaseID}
	values := []interface{}{game, phase}
	if err := datastore.GetMulti(ctx, keys, values); err != nil {
		log.Errorf(ctx, "datastore.GetMulti(..., %+v, %+v): %v; hope datastore gets fixed", keys, values, err)
		return err
	}

	if err := timeoutResolvePhaseFunc.EnqueueAt(ctx, phase.DeadlineAt, phase.GameID, phase.PhaseOrdinal); err != nil {
		log.Errorf(ctx, "timeoutResolvePhaseFunc.EnqueueAt(..., %v, %v, %v): %v; hope taskqueues get fixed", phase.DeadlineAt, phase.GameID, phase.PhaseOrdinal, err)
		return err
	}
	log.Infof(ctx, "Successfully scheduled phase resolution at %v", phase.DeadlineAt)

	if !game.Mustered {
		return nil
	}

	userConfigKeys := make([]*datastore.Key, len(game.Members))
	for idx := range userConfigKeys {
		userConfigKeys[idx] = auth.UserConfigID(ctx, auth.UserID(ctx, game.Members[idx].User.Id))
	}
	userConfigs := make([]auth.UserConfig, len(game.Members))
	if err := datastore.GetMulti(ctx, userConfigKeys, userConfigs); err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			for _, serr := range merr {
				if serr != nil && serr != datastore.ErrNoSuchEntity {
					log.Errorf(ctx, "datastore.GetMulti(..., %+v, %+v): %v; hope datastore gets fixed", userConfigKeys, userConfigs, err)
					return err
				}
			}
		} else if err != datastore.ErrNoSuchEntity {
			log.Errorf(ctx, "datastore.GetMulti(..., %+v, %+v): %v; hope datastore gets fixed", userConfigKeys, userConfigs, err)
			return err
		}
	}

	for idx, userConfig := range userConfigs {
		if userConfig.PhaseDeadlineWarningMinutesAhead > 0 {
			sendAt := phase.DeadlineAt.Add(-time.Minute * time.Duration(userConfig.PhaseDeadlineWarningMinutesAhead))
			if sendAt.After(time.Now()) {
				nation := string(game.Members[idx].Nation)
				if err := sendPhaseDeadlineWarningFunc.EnqueueAt(ctx, sendAt, gameID, phase.PhaseOrdinal, nation); err != nil {
					log.Errorf(ctx, "sendPhaseDeadlineWarningFunc.EnqueueAt(..., %v, %v, %v, %v): %v; hope taskqueues get fixed", sendAt, gameID, phase.PhaseOrdinal, nation, err)
					return err
				}
				log.Infof(ctx, "Successfully scheduled phase deadline warning for %+v at %v", game.Members[idx], sendAt)
			} else {
				log.Infof(ctx, "User %+v wants phase deadline warning at %v??", game.Members[idx], sendAt)
			}
		} else {
			log.Infof(ctx, "User %+v doesn't want phase deadline warning", game.Members[idx])
		}
	}

	return nil
}

func resolvePhaseHelper(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64, timeoutTriggered bool) error {
	log.Infof(ctx, "resolvePhaseHelper(..., %v, %v, %v)", gameID, phaseOrdinal, timeoutTriggered)

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		log.Errorf(ctx, "PhaseID(..., %v, %v): %v, %v; fix the PhaseID func", gameID, phaseOrdinal, phaseID, err)
		return err
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		phase := &Phase{}
		keys := []*datastore.Key{gameID, phaseID}
		values := []interface{}{game, phase}
		if err := datastore.GetMulti(ctx, keys, values); err != nil {
			if merr, ok := err.(appengine.MultiError); ok {
				for idx, serr := range merr {
					if idx == 1 && serr == datastore.ErrNoSuchEntity {
						log.Warningf(ctx, "Phase is missing, assuming this is manually deleted, giving up")
						return nil
					} else {
						log.Errorf(ctx, "datastore.GetMulti(..., %v, %v): %v; hope datastore will get fixed", keys, values, err)
						return err
					}
				}
			} else {
				log.Errorf(ctx, "datastore.GetMulti(..., %v, %v): %v; hope datastore will get fixed", keys, values, err)
				return err
			}
		}
		game.ID = gameID

		phaseStates := PhaseStates{}

		if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &phaseStates); err != nil {
			log.Errorf(ctx, "Unable to query phase states for %v/%v: %v; hope datastore will get fixed", gameID, phaseID, err)
			return err
		}

		log.Infof(ctx, "resolvePhaseHelper(..., %v, %v, %v) running PhaseResolver.Act()", gameID, phaseOrdinal, timeoutTriggered)
		return (&PhaseResolver{
			Context:          ctx,
			Game:             game,
			Phase:            phase,
			PhaseStates:      phaseStates,
			TimeoutTriggered: timeoutTriggered,
		}).Act()
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit resolve tx: %v", err)
		return err
	}

	log.Infof(ctx, "timeoutResolvePhase(..., %v, %v): *** SUCCESS ***", gameID, phaseOrdinal)

	return nil
}

type PhaseResolver struct {
	Context          context.Context
	Game             *Game
	Phase            *Phase
	PhaseStates      PhaseStates
	TimeoutTriggered bool

	// Don't populate this yourself, it's calculated by the PhaseResolver when you trigger it.
	nonEliminatedUserIds map[string]bool
}

func (p *PhaseResolver) SCCounts(s *state.State) map[godip.Nation]int {
	res := map[godip.Nation]int{}
	for _, nat := range s.SupplyCenters() {
		if nat != "" {
			res[nat] = res[nat] + 1
		}
	}
	return res
}

type quitter struct {
	state  quitState
	member *Member
}

type quitState int

const (
	unknownState quitState = iota
	diasState
	eliminatedState
	nmrState
)

func (p *PhaseResolver) Act() error {
	log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act()", p.Phase.GameID, p.Phase.PhaseOrdinal)

	// Sanity check time and resolution status of the phase.

	if p.TimeoutTriggered && p.Phase.DeadlineAt.After(time.Now()) {
		log.Infof(p.Context, "Resolution postponed to %v by %v; rescheduling task", p.Phase.DeadlineAt, PP(p.Phase))
		return p.Phase.ScheduleResolution(p.Context)
	}

	if p.Phase.Resolved {
		log.Infof(p.Context, "Already resolved; %v; skipping resolution", PP(p.Phase))
		return nil
	}

	variant, found := variants.Variants[p.Game.Variant]
	if !found {
		variantNames := []string{}
		for name := range variants.Variants {
			variantNames = append(variantNames, name)
		}
		log.Error(p.Context, "Variant %#v not recognized? We only know of %+v", p.Game.Variant, names)
	}

	// Make mustering games go back to staging after deleting their phases,
	// all non-ready members, and all phase states - if not everyone is ready.
	// Otherwise just mark the game as mustered, push the deadline, and reschedule.
	if !p.Game.Mustered {

		allMembers := []string{}
		for _, nat := range variant.Nations {
			allMembers = append(allMembers, string(nat))
		}

		// Collect IDs for phase and phase states, in case we need to delete them in a bit.

		allKeys := []*datastore.Key{}
		phaseID, err := p.Phase.ID(p.Context)
		if err != nil {
			log.Errorf(p.Context, "p.Phase.ID(...): %v; fix it?", err)
			return err
		}
		allKeys = append(allKeys, phaseID)

		// Find ready members.

		phaseStateKeys := []*datastore.Key{}
		readyNationMap := map[godip.Nation]bool{}
		for _, phaseState := range p.PhaseStates {
			stateID, err := phaseState.ID(p.Context)
			if err != nil {
				log.Errorf(p.Context, "phaseState.ID(...): %v; fix it?", err)
				return err
			}
			allKeys = append(allKeys, stateID)
			phaseStateKeys = append(phaseStateKeys, stateID)
			if phaseState.ReadyToResolve {
				readyNationMap[phaseState.Nation] = true
			}
		}

		// Find all userIds.
		allUserIds := []string{}
		for _, member := range p.Game.Members {
			allUserIds = append(allUserIds, member.User.Id)
		}

		// Depending on whether everyone is ready...
		if len(readyNationMap) == len(variant.Nations) {
			p.Game.Mustered = true
			if p.Phase.Type != godip.Movement && p.Game.NonMovementPhaseLengthMinutes != 0 {
				p.Phase.DeadlineAt = time.Now().Add(time.Minute * p.Game.NonMovementPhaseLengthMinutes)
			} else {
				p.Phase.DeadlineAt = time.Now().Add(time.Minute * p.Game.PhaseLengthMinutes)
			}
			p.Game.NewestPhaseMeta = []PhaseMeta{p.Phase.PhaseMeta}
			// Delete all the old phase states.
			if err := datastore.DeleteMulti(p.Context, phaseStateKeys); err != nil {
				log.Errorf(p.Context, "datastore.DeleteMulti(..., %+v): %v; hope datastore gets fixed", phaseStateKeys, err)
				return err
			}
			phaseID, err := p.Phase.ID(p.Context)
			if err != nil {
				log.Errorf(p.Context, "p.Phase.ID(...): %v; wtf?", err)
				return err
			}
			toSave := []interface{}{
				p.Game, p.Phase,
			}
			keys := []*datastore.Key{
				p.Game.ID, phaseID,
			}
			// Create the state for this phase.
			s, err := p.Phase.State(p.Context, variant, map[godip.Nation]map[godip.Province][]string{})
			if err != nil {
				log.Errorf(p.Context, "Unable to create godip State for %v: %v; fix godip!", PP(p.Phase), err)
				return err
			}
			// And the phase states for the members.
			for idx := range p.Game.Members {
				options := s.Phase().Options(s, p.Game.Members[idx].Nation)
				profile, counts := s.GetProfile()
				for k, v := range profile {
					log.Debugf(p.Context, "Profiling state: %v => %v, %v", k, v, counts[k])
				}
				zippedOptions, err := zipOptions(p.Context, options)
				if err != nil {
					log.Errorf(p.Context, "zipOptions(..., %+v): %v", options, err)
					return err
				}

				phaseState := &PhaseState{
					GameID:        p.Game.ID,
					PhaseOrdinal:  p.Phase.PhaseOrdinal,
					Nation:        p.Game.Members[idx].Nation,
					NoOrders:      len(options) == 0,
					Messages:      strings.Join(s.Phase().Messages(s, p.Game.Members[idx].Nation), ","),
					ZippedOptions: zippedOptions,
					Note:          fmt.Sprintf("Created by Diplicity at %v due to game muster.", time.Now()),
				}
				phaseStateID, err := phaseState.ID(p.Context)
				if err != nil {
					log.Errorf(p.Context, "phaseState.ID(...): %v", err)
					return err
				}

				p.Game.Members[idx].NewestPhaseState = *phaseState

				toSave = append(toSave, phaseState)
				keys = append(keys, phaseStateID)
			}
			// Save everything.
			if _, err := datastore.PutMulti(p.Context, keys, toSave); err != nil {
				log.Errorf(p.Context, "datastore.PutMulti(..., %+v, %+v): %v; hope datastore gets fixed", keys, toSave, err)
				return err
			}
			// Notify everyone that the game has properly started.
			notificationBody := "All players are ready, and the game has started for real. Be nice to each other, and have fun!"
			if err := AsyncSendMsgFunc.EnqueueIn(
				p.Context, 0,
				p.Phase.GameID,
				DiplicitySender,
				allMembers,
				notificationBody,
				p.Phase.Host,
			); err != nil {
				log.Errorf(p.Context, "AsyncSendMsgFunc(..., %v, %v, %+v, %q, %q): %v; fix it?", p.Phase.GameID, DiplicitySender, variant.Nations, notificationBody, p.Phase.Host, err)
				return err
			}
			log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESSFULLY PROMOTED MUSTERING GAME ***", p.Phase.GameID, p.Phase.PhaseOrdinal)
		} else if len(readyNationMap) == 0 {
			allKeys = append(allKeys, p.Game.ID)
			// Delete the game, the phase, and all it's phase states.
			if err := datastore.DeleteMulti(p.Context, allKeys); err != nil {
				log.Errorf(p.Context, "datastore.DeleteMulti(..., %+v): %v; hope datastore gets fixed", allKeys, err)
				return err
			}
			log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESSFULLY DELETED MUSTERING ABANDONED GAME ***", p.Phase.GameID, p.Phase.PhaseOrdinal)
			return nil
		} else {
			p.Game.Started = false
			p.Game.StartedAt = time.Time{}
			p.Game.Closed = false
			p.Game.NewestPhaseMeta = nil
			newMembers := []Member{}
			probationUids := []string{}
			allUIds := []string{}
			for i := range p.Game.Members {
				member := p.Game.Members[i]
				allUIds = append(allUIds, member.User.Id)
				if readyNationMap[member.Nation] {
					newMembers = append(newMembers, member)
				} else {
					probationUids = append(probationUids, member.User.Id)
				}
			}
			// Make the game have only the ready members.
			p.Game.Members = newMembers
			// Eject anyone not ready from any staging games.
			if err := ejectProbationariesFunc.EnqueueIn(p.Context, 0, probationUids); err != nil {
				log.Errorf(p.Context, "Unable to enqueue ejection of probationaries %+v: %v; hope datastore gets fixed", probationUids, err)
				return err
			}
			// Save the game with the new state of being staging, and no longer closed.
			if err := p.Game.Save(p.Context); err != nil {
				log.Errorf(p.Context, "Unable to save game %v: %v; hope datastore will get fixed", PP(p.Game), err)
				return err
			}
			// Update all users with the new stats about joined/started games.
			if err := UpdateUserStatsASAP(p.Context, allUIds); err != nil {
				log.Errorf(p.Context, "UpdateUserStatsASAP(..., %+v): %v; hope datastore gets fixed", allUIds, err)
				return err
			}
			// Delete the phase and all it's phase states.
			if err := datastore.DeleteMulti(p.Context, allKeys); err != nil {
				log.Errorf(p.Context, "datastore.DeleteMulti(..., %+v): %v; hope datastore gets fixed", allKeys, err)
				return err
			}
			notificationBody := fmt.Sprintf("Unfortunately %v players weren't ready, so the game has re-entered the staging state. Once it has enough players it will re-enter the mustering state again.", len(variant.Nations)-len(readyNationMap))
			if err := AsyncSendMsgFunc.EnqueueIn(
				p.Context, 0,
				p.Phase.GameID,
				DiplicitySender,
				allMembers,
				notificationBody,
				p.Phase.Host,
			); err != nil {
				log.Errorf(p.Context, "AsyncSendMsgFunc(..., %v, %v, %+v, %q, %q): %v; fix it?", p.Phase.GameID, DiplicitySender, variant.Nations, notificationBody, p.Phase.Host, err)
				return err
			}
			log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESSFULLY REVERTED MUSTERING GAME ***", p.Phase.GameID, p.Phase.PhaseOrdinal)
		}
		if err := sendPhaseNotificationsToUsersFunc.EnqueueIn(
			p.Context,
			0,
			p.Phase.Host,
			p.Game.ID,
			p.Phase.PhaseOrdinal,
			allUserIds,
		); err != nil {
			log.Errorf(p.Context, "Unable to enqueue notification to game members: %v; hope datastore will get fixed", err)
			return err
		}
		log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** Notified all members of the new phase state ***", p.Phase.GameID, p.Phase.PhaseOrdinal)
		return nil
	}

	// Clean up old phase states, and populate the nonEliminatedUserIds slice if necessary.

	phaseStateIDs := make([]*datastore.Key, len(p.PhaseStates))
	nonEliminatedUserIds := map[string]bool{}
	for i := range p.PhaseStates {
		if !p.PhaseStates[i].Eliminated {
			member, found := p.Game.GetMemberByNation(p.PhaseStates[i].Nation)
			if !found {
				err := fmt.Errorf("p.Game.GetMemberByNation(%q) found no member; something is horribly wrong", p.PhaseStates[i].Nation)
				log.Errorf(p.Context, err.Error())
				return err
			}
			nonEliminatedUserIds[member.User.Id] = true
		}
		p.PhaseStates[i].ZippedOptions = nil
		phaseStateID, err := p.PhaseStates[i].ID(p.Context)
		if err != nil {
			log.Errorf(p.Context, "Unable to create ID for %v: %v; hope datastore gets fixed", PP(p.PhaseStates[i]), err)
			return err
		}
		phaseStateIDs[i] = phaseStateID
	}
	if _, err := datastore.PutMulti(p.Context, phaseStateIDs, p.PhaseStates); err != nil {
		log.Errorf(p.Context, "Unable to save old phase states %v: %v; hope datastore will get fixed", PP(p.PhaseStates), err)
		return err
	}
	// We don't change the nonEliminatedUserIds map if it's already set.
	// I _think_ (a long time since I implemented this) that it's because
	// we want to make sure that anyone not eliminated _before_ this resolution
	// should still be notified when all the resolutions are finished.
	if p.nonEliminatedUserIds == nil {
		p.nonEliminatedUserIds = nonEliminatedUserIds
	}

	// Roll forward the game state.

	log.Infof(p.Context, "PhaseStates at resolve time: %v", PP(p.PhaseStates))

	orderMap, err := p.Phase.Orders(p.Context)
	if err != nil {
		log.Errorf(p.Context, "Unable to load orders for %v: %v; fix phase.Orders or hope datastore will get fixed", PP(p.Phase), err)
		return err
	}
	log.Infof(p.Context, "Orders at resolve time: %v", PP(orderMap))

	s, err := p.Phase.State(p.Context, variant, orderMap)
	if err != nil {
		log.Errorf(p.Context, "Unable to create godip State for %v: %v; fix godip!", PP(p.Phase), err)
		return err
	}
	if err := s.Next(); err != nil {
		log.Errorf(p.Context, "Unable to roll State forward for %v: %v; fix godip!", PP(p.Phase), err)
		return err
	}
	scCounts := p.SCCounts(s)

	// Set resolutions

	for prov, err := range s.Resolutions() {
		if err == nil {
			p.Phase.Resolutions = append(p.Phase.Resolutions, Resolution{prov, "OK"})
		} else {
			p.Phase.Resolutions = append(p.Phase.Resolutions, Resolution{prov, err.Error()})
		}
	}
	for prov := range s.ForceDisbands() {
		p.Phase.ForceDisbands = append(p.Phase.ForceDisbands, prov)
	}

	// Finish and save old phase.

	p.Phase.Resolved = true
	p.Phase.ResolvedAt = time.Now()
	if err := p.Phase.DBSave(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save old phase %v: %v; hope datastore gets fixed", PP(p.Phase), err)
		return err
	}

	// Create the new phase.

	newPhase := NewPhase(s, p.Phase.GameID, p.Phase.PhaseOrdinal+1, p.Phase.Host)
	newPhase.SoloSCCount = variant.SoloSCCount(s)
	// To make old games work.
	if p.Game.PhaseLengthMinutes == 0 {
		p.Game.PhaseLengthMinutes = MAX_PHASE_DEADLINE
	}
	if newPhase.Type != godip.Movement && p.Game.NonMovementPhaseLengthMinutes != 0 {
		newPhase.DeadlineAt = newPhase.CreatedAt.Add(time.Minute * p.Game.NonMovementPhaseLengthMinutes)
	} else {
		newPhase.DeadlineAt = newPhase.CreatedAt.Add(time.Minute * p.Game.PhaseLengthMinutes)
	}

	// Check if we can roll forward again, and potentially create new phase states.

	// Prepare some data to collect.
	allReady := true                       // All nations are ready to resolve the new phase as well.
	soloWinner := variant.SoloWinner(s)    // The nation, if any, reaching solo victory.
	var soloWinnerUser string              // Will be set to the user of the soloWinner nation.
	quitters := map[godip.Nation]quitter{} // One per nation that wants to quit, with either dias, eliminated, or nmr.
	probationaries := []string{}           // One per user that's on probation.
	newPhaseStates := PhaseStates{}        // The new phase states to save if we want to prepare resolution of a new phase.
	oldPhaseResult := &PhaseResult{        // A result object for the old phase to simplify collecting user scoped stats.
		GameID:       p.Phase.GameID,
		PhaseOrdinal: p.Phase.PhaseOrdinal,
		Private:      p.Game.Private,
	}

	membersWithOptions := map[string]bool{}
	for i := range p.Game.Members {
		member := &p.Game.Members[i]

		// Collect data on each nation.
		_, hadOrders := orderMap[member.Nation]
		wasReady := false
		wantedDIAS := false
		wasOnProbation := false
		wasEliminated := false
		for _, phaseState := range p.PhaseStates {
			if phaseState.Nation == member.Nation {
				wasReady = phaseState.ReadyToResolve
				wantedDIAS = phaseState.WantsDIAS
				if phaseState.WantsDIAS {
					quitters[member.Nation] = quitter{
						state:  diasState,
						member: member,
					}
				}
				wasOnProbation = phaseState.OnProbation
				break
			}
		}
		orderOptions := s.Phase().Options(s, member.Nation)
		newOptions := len(orderOptions)
		if newOptions > 0 {
			membersWithOptions[member.User.Id] = true
		}
		if scCounts[member.Nation] == 0 {
			wasEliminated = true
			// Overwrite DIAS with eliminated, you can't be part of a DIAS if you are eliminated...
			quitters[member.Nation] = quitter{
				state:  eliminatedState,
				member: member,
			}
		} else if member.Nation == soloWinner {
			log.Infof(p.Context, "Marking %q as solo winner", member.Nation)
			soloWinnerUser = member.User.Id
		}

		// Log what we're doing.
		stateString := fmt.Sprintf("wasReady = %v, wantedDIAS = %v, onProbation = %v, hadOrders = %v, newOptions = %v, wasEliminated = %v", wasReady, wantedDIAS, wasOnProbation, hadOrders, newOptions, wasEliminated)
		log.Infof(p.Context, "%v at phase change: %s", member.Nation, stateString)

		// Calculate states for next phase.
		// When a player creates an order, the phase state for that order is updated to 'OnProbation = false'.
		// When a player updates a phase state, it's always set to 'OnProbation = false'.
		// Thus, if the player was on probation last phase, we know they didn't enter orders or update their phase state, and they are safe to put on probation again.
		// The reason for the `||` is that they can still be ready to resolve, due to not having options!
		// (i.e. even someone who is ready to resolve can be on probation)
		// A player should not be on probation once they've been eliminated from the game.
		autoProbation := (wasOnProbation || (!hadOrders && !wasReady)) && !wasEliminated
		if autoProbation {
			probationaries = append(probationaries, member.User.Id)
		}
		autoReady := newOptions == 0 || autoProbation
		autoDIAS := wantedDIAS || autoProbation
		allReady = allReady && autoReady

		// Update the old phase result object.
		if autoProbation {
			// Users on probation get an NMR count.
			oldPhaseResult.NMRUsers = append(oldPhaseResult.NMRUsers, member.User.Id)
		} else if wasReady {
			// Users marked ready get a ready count.
			oldPhaseResult.ReadyUsers = append(oldPhaseResult.ReadyUsers, member.User.Id)
		} else if hadOrders {
			// Users having orders, but not marked as ready to resolve, get an active count.
			oldPhaseResult.ActiveUsers = append(oldPhaseResult.ActiveUsers, member.User.Id)
		}

		// Overwrite DIAS but not eliminated with NMR.
		if q := quitters[member.Nation]; autoProbation && q.state != eliminatedState {
			quitters[member.Nation] = quitter{
				state:  nmrState,
				member: member,
			}
		}

		zippedOptions, err := zipOptions(p.Context, orderOptions)
		if err != nil {
			log.Errorf(p.Context, "Resolved phase %v unable to marshal options for %v: %v; fix this code!", PP(p.Phase), member.Nation, err)
			return err
		}

		newPhaseState := &PhaseState{
			GameID:         p.Phase.GameID,
			PhaseOrdinal:   newPhase.PhaseOrdinal,
			Nation:         member.Nation,
			ReadyToResolve: autoReady,
			NoOrders:       newOptions == 0,
			Eliminated:     wasEliminated,
			WantsDIAS:      autoDIAS,
			OnProbation:    autoProbation,
			Messages:       strings.Join(s.Phase().Messages(s, member.Nation), ","),
			ZippedOptions:  zippedOptions,
			Note:           fmt.Sprintf("Auto generated due to phase change at %v/%v: %s", p.Phase.GameID, p.Phase.PhaseOrdinal, stateString),
		}

		member.NewestPhaseState = *newPhaseState
		newPhaseStates = append(newPhaseStates, *newPhaseState)
		oldPhaseResult.AllUsers = append(oldPhaseResult.AllUsers, member.User.Id)
	}

	log.Infof(p.Context, "Calculated key metrics: allReady: %v, soloWinner: %q, quitters: %v", allReady, soloWinner, PP(quitters))

	// Check if the game should end.

	if soloWinner != "" || len(quitters) > len(variant.Nations)-1 || (p.Game.LastYear != 0 && newPhase.Year > p.Game.LastYear) {
		log.Infof(p.Context, "soloWinner: %q, quitters: %v, lastYear: %v => game needs to end", soloWinner, PP(quitters), p.Game.LastYear)
		// Just to ensure we don't try to resolve it again, even by mistake.
		newPhase.Resolved = true
		newPhase.ResolvedAt = time.Now()
		p.Game.Finished = true
		p.Game.FinishedAt = time.Now()
		p.Game.Closed = true
	}

	// Save the old phase result.

	if err := oldPhaseResult.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save old phase result %v: %v; hope datastore gets fixed", PP(oldPhaseResult), err)
		return err
	}

	// Save the new phase.

	if err := newPhase.DBSave(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save new Phase %v: %v; hope datastore will get fixed", PP(newPhase), err)
		return err
	}

	if err = newPhase.Recalc(); err != nil {
		return err
	}
	p.Game.NewestPhaseMeta = []PhaseMeta{newPhase.PhaseMeta}

	if p.Game.Finished {

		// Store a game result if it is finished.

		diasMembers := []godip.Nation{}
		diasUsers := []string{}
		nmrMembers := []godip.Nation{}
		nmrUsers := []string{}
		eliminatedMembers := []godip.Nation{}
		eliminatedUsers := []string{}
		scores := []GameScore{}

		for _, member := range p.Game.Members {
			var state quitState
			quitter, isQuitter := quitters[member.Nation]
			if isQuitter {
				state = quitter.state
			}

			switch state {
			case nmrState:
				nmrMembers = append(nmrMembers, member.Nation)
				nmrUsers = append(nmrUsers, member.User.Id)
			case eliminatedState:
				eliminatedMembers = append(eliminatedMembers, member.Nation)
				eliminatedUsers = append(eliminatedUsers, member.User.Id)
			default:
				if soloWinner == "" {
					diasMembers = append(diasMembers, member.Nation)
					diasUsers = append(diasUsers, member.User.Id)
				}
			}

			scores = append(scores, GameScore{
				UserId: member.User.Id,
				Member: member.Nation,
				SCs:    scCounts[member.Nation],
			})
		}

		sort.Sort(sort.StringSlice(nmrUsers))
		sort.Sort(sort.StringSlice(oldPhaseResult.NMRUsers))
		if len(nmrUsers) != len(oldPhaseResult.NMRUsers) {
			err := fmt.Errorf("oldPhaseResult %+v doesn't match the NMRUsers %+v we want to store in the GameResult!", oldPhaseResult, nmrUsers)
			log.Errorf(p.Context, err.Error())
			return err
		}
		for idx, nmrUser := range nmrUsers {
			if oldPhaseResult.NMRUsers[idx] != nmrUser {
				err := fmt.Errorf("oldPhaseResult %+v doesn't match the NMRUsers %+v we want to store in the GameResult!", oldPhaseResult, nmrUsers)
				log.Errorf(p.Context, err.Error())
				return err
			}
		}

		gameResult := &GameResult{
			GameID:            p.Game.ID,
			SoloWinnerMember:  soloWinner,
			SoloWinnerUser:    soloWinnerUser,
			DIASMembers:       diasMembers,
			DIASUsers:         diasUsers,
			NMRMembers:        nmrMembers,
			NMRUsers:          nmrUsers,
			EliminatedMembers: eliminatedMembers,
			EliminatedUsers:   eliminatedUsers,
			Scores:            scores,
			AllUsers:          oldPhaseResult.AllUsers,
			TrueSkillRated:    false,
			Private:           p.Game.Private,
			CreatedAt:         time.Now(),
		}
		gameResult.AssignScores()
		if err := gameResult.DBSave(p.Context, p.Game); err != nil {
			log.Errorf(p.Context, "Unable to save game result %v: %v; hope datastore gets fixed", PP(gameResult), err)
			return err
		}

	} else {

		// Otherwise, save the new phase states.

		if len(newPhaseStates) > 0 {
			ids := make([]*datastore.Key, len(newPhaseStates))
			for i := range newPhaseStates {
				id, err := newPhaseStates[i].ID(p.Context)
				if err != nil {
					log.Errorf(p.Context, "Unable to create new phase state ID for %v: %v; fix PhaseState.ID or hope datastore gets fixed", PP(newPhaseStates[i]), err)
					return err
				}
				ids[i] = id
			}
			if _, err := datastore.PutMulti(p.Context, ids, newPhaseStates); err != nil {
				log.Errorf(p.Context, "Unable to save new PhaseStates %v: %v; hope datastore will get fixed", PP(newPhaseStates), err)
				return err
			}
			log.Infof(p.Context, "Saved %v to get things moving", PP(newPhaseStates))
		}

		if allReady {

			// If we can roll forward again, do it and return (to skip enqueueing tasks, which might break datastore if we do it too many times in the same tx).

			log.Infof(p.Context, "Since all players are ready to resolve RIGHT NOW, rolling forward again")

			newPhase.DeadlineAt = time.Now()
			p.Phase = newPhase
			p.PhaseStates = newPhaseStates
			// Note that we are reusing the same resolver, which means the nonEliminatedUserIds will be the same, and not replaced when we Act().
			log.Infof(p.Context, "PhaseResolver running PhaseResolver.Act() due to fast forward")
			if err := p.Act(); err != nil {
				log.Errorf(p.Context, "Unable to continue rolling forward: %v; fix the resolver!", err)
				return err
			}

			log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** delegated to new resolver due to immediate resolution ***", p.Phase.GameID, p.Phase.PhaseOrdinal)

			return nil

		} else {

			// Otherwise, schedule new phase resolution if necessary.

			if err := newPhase.ScheduleResolution(p.Context); err != nil {
				log.Errorf(p.Context, "Unable to schedule resolution for %v: %v; fix ScheduleResolution or hope datastore gets fixed", PP(newPhase), err)
				return err
			}
			log.Infof(
				p.Context, "%v has phase length of %v/%v minutes, scheduled new resolve",
				PP(p.Game),
				p.Game.PhaseLengthMinutes,
				p.Game.NonMovementPhaseLengthMinutes)
		}
	}

	// Notify about the new phase.

	membersToNotify := []string{}
	for _, member := range p.Game.Members {
		if p.nonEliminatedUserIds[member.User.Id] || membersWithOptions[member.User.Id] {
			membersToNotify = append(membersToNotify, member.User.Id)
		}
	}
	if err := sendPhaseNotificationsToUsersFunc.EnqueueIn(
		p.Context,
		0,
		p.Phase.Host,
		p.Game.ID,
		p.Phase.PhaseOrdinal,
		membersToNotify,
	); err != nil {
		log.Errorf(p.Context, "Unable to enqueue notification to game members: %v; hope datastore will get fixed", err)
		return err
	}

	if p.Game.Finished {

		// Clean up last order options from what's cached in the game.

		for i := range p.Game.Members {
			p.Game.Members[i].NewestPhaseState.ZippedOptions = nil
		}

		// Enqueue updating of ratings, which will in turn update user stats.

		if !p.Game.Private {
			if err := UpdateTrueSkillsASAP(p.Context); err != nil {
				log.Errorf(p.Context, "Unable to enqueue updating of TrueSkill ratings: %v; hope datastore gets fixed", err)
				return err
			}
		}

	}

	if !p.Game.Finished || p.Game.Private {

		// Enqueue updating of user stats (for NMR/NonNMR purposes).

		uids := make([]string, len(p.Game.Members))
		for i, m := range p.Game.Members {
			uids[i] = m.User.Id
		}
		if err := UpdateUserStatsASAP(p.Context, uids); err != nil {
			log.Errorf(p.Context, "Unable to enqueue user stats update tasks: %v; hope datastore gets fixed", err)
			return err
		}

	}

	// Eject probationaries from staging games.

	if len(probationaries) > 0 {
		if err := ejectProbationariesFunc.EnqueueIn(p.Context, 0, probationaries); err != nil {
			log.Errorf(p.Context, "Unable to enqueue ejection of probationaries %+v: %v; hope datastore gets fixed", probationaries, err)
			return err
		}
	}

	if err := p.Game.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save game %v: %v; hope datastore will get fixed", PP(p.Game), err)
		return err
	}

	log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESS ***", p.Phase.GameID, p.Phase.PhaseOrdinal)

	return nil
}

const (
	phaseKind        = "Phase"
	memberNationFlag = "member-nation"
)

type UnitWrapper struct {
	Province godip.Province
	Unit     godip.Unit
}

type SC struct {
	Province godip.Province
	Owner    godip.Nation
}

type Dislodger struct {
	Province godip.Province
	// The name of this is crap.
	// The Dislodger struct is used so that
	// Province is the actual dislodger, while
	// Dislodger is the province that was dislodged.
	Dislodger godip.Province
}

type Dislodged struct {
	Province  godip.Province
	Dislodged godip.Unit
}

type Bounce struct {
	Province   godip.Province
	BounceList string
}

type Resolution struct {
	Province   godip.Province
	Resolution string
}

type Phases []Phase

func (p Phases) Item(r Request, gameID *datastore.Key) *Item {
	phaseItems := make(List, len(p))
	for i := range p {
		phaseItems[i] = p[i].Item(r)
	}
	phasesItem := NewItem(phaseItems).SetName("phases").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListPhasesRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	return phasesItem
}

func (p Phases) Len() int {
	return len(p)
}

func (p Phases) Less(i, j int) bool {
	return p[i].PhaseOrdinal < p[j].PhaseOrdinal
}

func (p Phases) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

type PhaseMeta struct {
	PhaseOrdinal   int64
	Season         godip.Season
	Year           int
	Type           godip.PhaseType
	Resolved       bool
	CreatedAt      time.Time
	CreatedAgo     time.Duration `datastore:"-" ticker:"true"`
	ResolvedAt     time.Time
	ResolvedAgo    time.Duration `datastore:"-" ticker:"true"`
	DeadlineAt     time.Time
	NextDeadlineIn time.Duration `datastore:"-" ticker:"true"`
	UnitsJSON      string        `datastore:",noindex"`
	SCsJSON        string        `datastore:",noindex"`
}

func (p *PhaseMeta) Refresh() {
	if !p.DeadlineAt.IsZero() {
		p.NextDeadlineIn = p.DeadlineAt.Sub(time.Now())
	}
	if !p.CreatedAt.IsZero() {
		p.CreatedAgo = p.CreatedAt.Sub(time.Now())
	}
	if !p.ResolvedAt.IsZero() {
		p.ResolvedAgo = p.ResolvedAt.Sub(time.Now())
	}
}

func (p *Phase) Recalc() error {
	b, err := json.Marshal(p.Units)
	if err != nil {
		return err
	}
	p.PhaseMeta.UnitsJSON = string(b)
	b, err = json.Marshal(p.SCs)
	if err != nil {
		return err
	}
	p.PhaseMeta.SCsJSON = string(b)
	return nil
}

type Phase struct {
	PhaseMeta
	GameID            *datastore.Key
	Units             []UnitWrapper
	SCs               []SC
	Dislodgeds        []Dislodged
	Dislodgers        []Dislodger
	ForceDisbands     []godip.Province
	Bounces           []Bounce
	Resolutions       []Resolution
	Host              string
	SoloSCCount       int
	PreliminaryScores GameScores `datastore:"-"`
}

func (p *Phase) Score(nations godip.Nations) {
	scCountByMember := map[godip.Nation]int{}
	for _, nation := range nations {
		scCountByMember[nation] = 0
	}
	for _, sc := range p.SCs {
		scCountByMember[sc.Owner] += 1
	}
	for owner, scCount := range scCountByMember {
		p.PreliminaryScores = append(p.PreliminaryScores, GameScore{
			Member: owner,
			SCs:    scCount,
		})
	}
	p.PreliminaryScores.Assign()
}

func (p *Phase) Load(props []datastore.Property) error {
	err := datastore.LoadStruct(p, props)
	if _, is := err.(*datastore.ErrFieldMismatch); is {
		err = nil
	}
	return err
}

func (p *Phase) Save() ([]datastore.Property, error) {
	return datastore.SaveStruct(p)
}

func (p *Phase) toVariantsPhase(variant string, orderMap map[godip.Nation]map[godip.Province][]string) *dvars.Phase {
	units := map[godip.Province]godip.Unit{}
	for _, unit := range p.Units {
		units[unit.Province] = unit.Unit
	}
	scs := map[godip.Province]godip.Nation{}
	for _, sc := range p.SCs {
		scs[sc.Province] = sc.Owner
	}
	dislodgeds := map[godip.Province]godip.Unit{}
	for _, d := range p.Dislodgeds {
		dislodgeds[d.Province] = d.Dislodged
	}
	dislodgers := map[godip.Province]godip.Province{}
	for _, d := range p.Dislodgers {
		dislodgers[d.Province] = d.Dislodger
	}
	bounces := map[godip.Province]map[godip.Province]bool{}
	for _, b := range p.Bounces {
		provBounces, found := bounces[b.Province]
		if !found {
			provBounces = map[godip.Province]bool{}
		}
		for _, el := range strings.Split(b.BounceList, ",") {
			provBounces[godip.Province(el)] = true
		}
		bounces[b.Province] = provBounces
	}
	resolutions := map[godip.Province]string{}
	for _, res := range p.Resolutions {
		resolutions[res.Province] = res.Resolution
	}
	return &dvars.Phase{
		Variant:       variant,
		Season:        p.Season,
		Year:          p.Year,
		Type:          p.Type,
		Units:         units,
		SupplyCenters: scs,
		Dislodgeds:    dislodgeds,
		Dislodgers:    dislodgers,
		Bounces:       bounces,
		Resolutions:   resolutions,
		Orders:        orderMap,
	}
}

func devResolvePhaseTimeout(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		return fmt.Errorf("only accessible in local dev mode")
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return err
	}

	phase := &Phase{}
	if err := datastore.Get(ctx, phaseID, phase); err != nil {
		return err
	}

	phase.DeadlineAt = time.Now()
	if _, err := datastore.Put(ctx, phaseID, phase); err != nil {
		return err
	}

	for err = timeoutResolvePhase(ctx, gameID, phaseOrdinal); err == datastore.ErrConcurrentTransaction; err = timeoutResolvePhase(ctx, gameID, phaseOrdinal) {
		time.Sleep(time.Second)
	}
	return err
}

func loadPhase(w ResponseWriter, r Request) (*Phase, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
		return nil, err
	}
	game.ID = gameID
	phase.Refresh()
	phase.Score(variants.Variants[game.Variant].Nations)

	member, isMember := game.GetMemberByUserId(user.Id)
	if isMember {
		r.Values()[memberNationFlag] = member.Nation
	}

	return phase, nil
}

func (p *Phase) Item(r Request) *Item {
	phaseItem := NewItem(p).SetName(fmt.Sprintf("%s %d, %s", p.Season, p.Year, p.Type))
	phaseItem.
		AddLink(r.NewLink(PhaseResource.Link("self", Load, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)}))).
		AddLink(r.NewLink(Link{
			Rel:         "map",
			Route:       RenderPhaseMapRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
	_, isMember := r.Values()[memberNationFlag]
	if isMember || p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "orders",
			Route:       ListOrdersRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
	}
	phaseItem.AddLink(r.NewLink(Link{
		Rel:         "corroborate",
		Route:       CorroboratePhaseRoute,
		RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
	}))
	if isMember && !p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "options",
			Route:       ListOptionsRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
		phaseItem.AddLink(r.NewLink(OrderResource.Link("create-order", Create, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)})))
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "create-and-corroborate",
			Method:      "POST",
			Route:       CreateAndCorroborateRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
	}
	if isMember || p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "phase-states",
			Route:       ListPhaseStatesRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
	}
	if p.Resolved {
		phaseItem.AddLink(r.NewLink(PhaseResultResource.Link("phase-result", Load, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)})))
	}
	return phaseItem
}

func (p *Phase) ScheduleResolution(ctx context.Context) error {
	return planPhaseTimeoutFunc.EnqueueIn(ctx, 0, p.GameID, p.PhaseOrdinal)
}

func PhaseID(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) (*datastore.Key, error) {
	if gameID == nil || phaseOrdinal < 0 {
		return nil, fmt.Errorf("phases must have games and ordinals > 0")
	}
	return datastore.NewKey(ctx, phaseKind, "", phaseOrdinal, gameID), nil
}

func (p *Phase) ID(ctx context.Context) (*datastore.Key, error) {
	return PhaseID(ctx, p.GameID, p.PhaseOrdinal)
}

func (p *Phase) DBSave(ctx context.Context) error {
	key, err := p.ID(ctx)
	if err != nil {
		return err
	}
	p.PhaseMeta.UnitsJSON = ""
	p.PhaseMeta.SCsJSON = ""
	_, err = datastore.Put(ctx, key, p)
	return err
}

func NewPhase(s *state.State, gameID *datastore.Key, phaseOrdinal int64, host string) *Phase {
	current := s.Phase()
	p := &Phase{
		PhaseMeta: PhaseMeta{
			PhaseOrdinal: phaseOrdinal,
			Season:       current.Season(),
			Year:         current.Year(),
			Type:         current.Type(),
			CreatedAt:    time.Now(),
		},
		GameID: gameID,
		Host:   host,
	}
	units, scs, dislodgeds, dislodgers, bounces, _ := s.Dump()
	for prov, unit := range units {
		p.Units = append(p.Units, UnitWrapper{prov, unit})
	}
	for prov, nation := range scs {
		p.SCs = append(p.SCs, SC{prov, nation})
	}
	for prov, unit := range dislodgeds {
		p.Dislodgeds = append(p.Dislodgeds, Dislodged{prov, unit})
	}
	for prov, dislodger := range dislodgers {
		p.Dislodgers = append(p.Dislodgers, Dislodger{prov, dislodger})
	}
	for prov, bounceMap := range bounces {
		bounceList := []string{}
		for prov := range bounceMap {
			bounceList = append(bounceList, string(prov))
		}
		p.Bounces = append(p.Bounces, Bounce{prov, strings.Join(bounceList, ",")})
	}
	return p
}

func listOptions(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return err
	}

	game := &Game{}
	phase := &Phase{}
	if err = datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
		return err
	}
	game.ID = gameID

	member, isMember := game.GetMemberByUserId(user.Id)
	if !isMember {
		return HTTPErr{"can only load options for member games", http.StatusNotFound}
	}

	phaseStateID, err := PhaseStateID(ctx, phaseID, member.Nation)
	if err != nil {
		return err
	}

	var options interface{}

	// First try to load pre-cooked options.

	phaseState := &PhaseState{}
	if err := datastore.Get(ctx, phaseStateID, phaseState); err == datastore.ErrNoSuchEntity {
		phaseState.GameID = game.ID
		phaseState.PhaseOrdinal = phaseOrdinal
		phaseState.Nation = member.Nation
	} else if err != nil {
		return err
	} else {
		options, err = unzipOptions(ctx, phaseState.ZippedOptions)
		if err != nil {
			log.Warningf(ctx, "PhaseState %+v has corrupt ZippedOptions for %v: %v", PP(phaseState), member.Nation, err)
		}
	}

	// Then create them on the fly.

	if options == nil {
		state, err := phase.State(ctx, variants.Variants[game.Variant], nil)
		if err != nil {
			return err
		}

		options = state.Phase().Options(state, member.Nation)
		profile, counts := state.GetProfile()
		for k, v := range profile {
			log.Debugf(ctx, "Profiling state: %v => %v, %v", k, v, counts[k])
		}

		// And save them for the future.

		log.Warningf(ctx, "Found PhaseState without ZippedOptions! Saving the generated options.")
		zippedOptions, err := zipOptions(ctx, options)
		if err != nil {
			return err
		}
		phaseState.ZippedOptions = zippedOptions
		if _, err := datastore.Put(ctx, phaseStateID, phaseState); err != nil {
			return err
		}
	}
	w.SetContent(NewItem(options).SetName("options").SetDesc([][]string{
		[]string{
			"Options explained",
			"The options consist of a decision tree where each node represents a decision a player has to make when defining an order.",
			"Each child set contains one or more alternatives of the same decision type, viz. `Province`, `OrderType`, `UnitType` or `SrcProvince`.",
			"To guide the player towards defining an order, present the alternatives for each node, then the sub tree pointed to by `Next`, etc. until a leaf node is reached.",
			"When a leaf is reached, the value nodes between root and leaf contain the list of strings defining an order the server will understand.",
		},
		[]string{
			"Province",
			"`Province` decisions represent picking a province from the game map.",
			"The children of the root of the options tree indicate that the user needs to select which province to define an order for.",
			"The first `Province` option just indicates which province the order is meant for.",
		},
		[]string{
			"OrderType",
			"`OrderType` decisions represent picking a type of order for a province.",
		},
		[]string{
			"UnitType",
			"`UnitType` decisions represent picking a type of unit for an order.",
		},
		[]string{
			"SrcProvince",
			"`SrcProvince` indicates that the value should replace the first `Province` value in the order list without presenting the player with a choice.",
			"This is useful e.g. when the order has a coast as source province, but the click should be accepted in the entire province.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListOptionsRoute,
		RouteParams: []string{"game_id", gameID.Encode(), "phase_ordinal", fmt.Sprint(phaseOrdinal)},
	})))

	return nil
}

func (p *Phase) Orders(ctx context.Context) (map[godip.Nation]map[godip.Province][]string, error) {
	phaseID, err := PhaseID(ctx, p.GameID, p.PhaseOrdinal)
	if err != nil {
		return nil, err
	}

	orders := []Order{}
	if _, err := datastore.NewQuery(orderKind).Ancestor(phaseID).GetAll(ctx, &orders); err != nil {
		return nil, err
	}

	orderMap := map[godip.Nation]map[godip.Province][]string{}
	for _, order := range orders {
		nationMap, found := orderMap[order.Nation]
		if !found {
			nationMap = map[godip.Province][]string{}
			orderMap[order.Nation] = nationMap
		}
		nationMap[godip.Province(order.Parts[0])] = order.Parts[1:]
	}

	return orderMap, nil
}

func (p *Phase) State(ctx context.Context, variant vrt.Variant, orderMap map[godip.Nation]map[godip.Province][]string) (*state.State, error) {
	parsedOrders, err := variant.Parser.ParseAll(orderMap)
	if err != nil {
		return nil, err
	}

	units := map[godip.Province]godip.Unit{}
	for _, unit := range p.Units {
		units[unit.Province] = unit.Unit
	}

	supplyCenters := map[godip.Province]godip.Nation{}
	for _, sc := range p.SCs {
		supplyCenters[sc.Province] = sc.Owner
	}

	dislodgeds := map[godip.Province]godip.Unit{}
	for _, dislodged := range p.Dislodgeds {
		dislodgeds[dislodged.Province] = dislodged.Dislodged
	}

	dislodgers := map[godip.Province]godip.Province{}
	for _, dislodger := range p.Dislodgers {
		dislodgers[dislodger.Province] = dislodger.Dislodger
	}

	bounces := map[godip.Province]map[godip.Province]bool{}
	for _, bounce := range p.Bounces {
		bounceMap := map[godip.Province]bool{}
		for _, prov := range strings.Split(bounce.BounceList, ",") {
			bounceMap[godip.Province(prov)] = true
		}
		bounces[bounce.Province] = bounceMap
	}

	return variant.Blank(variant.Phase(p.Year, p.Season, p.Type)).Load(units, supplyCenters, dislodgeds, dislodgers, bounces, parsedOrders), nil
}

func renderPhaseMap(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return err
	}

	userConfigID := auth.UserConfigID(ctx, auth.UserID(ctx, user.Id))

	game := &Game{}
	phase := &Phase{}
	userConfig := &auth.UserConfig{}
	err = datastore.GetMulti(
		ctx,
		[]*datastore.Key{gameID, phaseID, userConfigID},
		[]interface{}{game, phase, userConfig},
	)
	if err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			if merr[0] != nil || merr[1] != nil || (merr[2] != nil && merr[2] != datastore.ErrNoSuchEntity) {
				return merr
			}
			err = nil
		} else {
			return err
		}
	}
	game.ID = gameID

	var nation godip.Nation

	if member, found := game.GetMemberByUserId(user.Id); found {
		nation = member.Nation
	}

	foundOrders, err := phase.Orders(ctx)
	if err != nil {
		return err
	}

	ordersToDisplay := map[godip.Nation]map[godip.Province][]string{}
	for nat, orders := range foundOrders {
		if nat == nation || phase.Resolved {
			ordersToDisplay[nat] = orders
		}
	}

	vPhase := phase.toVariantsPhase(game.Variant, ordersToDisplay)

	return dvars.RenderPhaseMap(w, r, vPhase, userConfig.Colors)
}

type CorroborateResponse struct {
	Orders          Orders
	Inconsistencies []godip.Inconsistency
}

func (c *CorroborateResponse) Item(r Request, gameID *datastore.Key, phaseOrdinal int) *Item {
	return NewItem(c).SetName("corroboration").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       CorroboratePhaseRoute,
		RouteParams: []string{"game_id", gameID.Encode(), "phase_ordinal", fmt.Sprint(phaseOrdinal)},
	}))
}
func corroboratePhase(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return err
	}

	game := &Game{}
	phase := &Phase{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
		return err
	}
	game.ID = gameID

	member, isMember := game.GetMemberByUserId(user.Id)

	response := CorroborateResponse{}
	if isMember || phase.Resolved {
		query := datastore.NewQuery(orderKind).Ancestor(phaseID)
		if isMember && !phase.Resolved {
			query = query.Filter("Nation=", member.Nation)
		}
		if _, err := query.GetAll(ctx, &response.Orders); err != nil {
			return err
		}

		if isMember {
			orderPartsByProvince := map[godip.Province][]string{}
			for _, order := range response.Orders {
				orderPartsByProvince[godip.Province(order.Parts[0])] = order.Parts[1:]
			}

			variant := variants.Variants[game.Variant]

			s, err := phase.State(ctx, variant, map[godip.Nation]map[godip.Province][]string{
				member.Nation: orderPartsByProvince,
			})
			if err != nil {
				return err
			}

			response.Inconsistencies = s.Corroborate(member.Nation)
		}
	}

	w.SetContent(response.Item(r, gameID, int(phaseOrdinal)))

	return nil
}

func listPhases(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	game := &Game{}
	if err := datastore.Get(ctx, gameID, game); err != nil {
		return err
	}
	member, isMember := game.GetMemberByUserId(user.Id)
	if isMember {
		r.Values()[memberNationFlag] = member.Nation
	}

	phases := Phases{}
	_, err = datastore.NewQuery(phaseKind).Ancestor(gameID).GetAll(ctx, &phases)
	if err != nil {
		return err
	}
	for i := range phases {
		phases[i].Refresh()
		phases[i].Score(variants.Variants[game.Variant].Nations)
	}

	w.SetContent(phases.Item(r, gameID))
	return nil
}
