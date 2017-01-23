package game

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/go-fcm"
	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	"gopkg.in/sendgrid/sendgrid-go.v2"

	dvars "github.com/zond/diplicity/variants"
	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

var (
	timeoutResolvePhaseFunc           *DelayFunc
	sendPhaseNotificationsToUsersFunc *DelayFunc
	sendPhaseNotificationsToFCMFunc   *DelayFunc
	sendPhaseNotificationsToMailFunc  *DelayFunc
	PhaseResource                     *Resource
)

func init() {
	timeoutResolvePhaseFunc = NewDelayFunc("game-timeoutResolvePhase", timeoutResolvePhase)
	sendPhaseNotificationsToUsersFunc = NewDelayFunc("game-sendPhaseNotificationsToUsers", sendPhaseNotificationsToUsers)
	sendPhaseNotificationsToFCMFunc = NewDelayFunc("game-sendPhaseNotificationsToFCM", sendPhaseNotificationsToFCM)
	sendPhaseNotificationsToMailFunc = NewDelayFunc("game-sendPhaseNotificationsToMail", sendPhaseNotificationsToMail)

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

func getPhaseNotificationContext(ctx context.Context, host, scheme string, gameID *datastore.Key, phaseOrdinal int64, userId string) (*phaseNotificationContext, error) {
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
			if merr[2] == datastore.ErrNoSuchEntity {
				log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
				return nil, noConfigError
			}
			log.Errorf(ctx, "Unable to load game, phase, user and user config: %v; hope datastore gets fixed", err)
			return nil, err
		} else {
			log.Errorf(ctx, "Unable to load game, phase, user and user config: %v; hope datastore gets fixed", err)
			return nil, err
		}
	}
	res.game.ID = gameID

	isMember := false
	res.member, isMember = res.game.GetMember(userId)
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
	res.mapURL.Scheme = scheme

	res.mailData = map[string]interface{}{
		"phaseMeta": res.phase.PhaseMeta,
		"game":      res.game,
		"user":      res.user,
		"mapLink":   res.mapURL.String(),
	}
	res.fcmData = map[string]interface{}{
		"type":      "phase",
		"gameID":    res.game.ID,
		"phaseMeta": res.phase.PhaseMeta,
	}

	return res, nil
}

func sendPhaseNotificationsToMail(ctx context.Context, host, scheme string, gameID *datastore.Key, phaseOrdinal int64, userId string) error {
	log.Infof(ctx, "sendPhaseNotificationsToMail(..., %q, %q, %v, %v, %q)", host, scheme, gameID, phaseOrdinal, userId)

	msgContext, err := getPhaseNotificationContext(ctx, host, scheme, gameID, phaseOrdinal, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get msg notification context: %v; fix getPhaseNotificationContext or hope datastore gets fixed", err)
		return err
	}

	if !msgContext.userConfig.MailConfig.Enabled {
		log.Infof(ctx, "%q hasn't enabled mail notifications for mail, will skip sending notification", userId)
		return nil
	}

	sendGridConf, err := GetSendGrid(ctx)
	if err != nil {
		log.Errorf(ctx, "Unable to load sendgrid API key: %v; upload one or hope datastore gets fixed", err)
		return err
	}

	unsubscribeURL, err := auth.GetUnsubscribeURL(ctx, router, host, scheme, userId)
	if err != nil {
		log.Errorf(ctx, "Unable to create unsubscribe URL for %q: %v; fix auth.GetUnsubscribeURL", userId, err)
		return err
	}

	msgContext.mailData["unsubscribeURL"] = unsubscribeURL.String()

	msg := sendgrid.NewMail()
	msg.SetText(fmt.Sprintf(
		"%s has a new phase: %s.\n\nVisit %s to stop receiving email like this.",
		msgContext.game.Desc,
		msgContext.mapURL.String(),
		unsubscribeURL.String()))
	msg.SetSubject(fmt.Sprintf("%s %d, %s", msgContext.phase.Season, msgContext.phase.Year, msgContext.phase.Type))
	msg.AddHeader("List-Unsubscribe", fmt.Sprintf("<%s>", unsubscribeURL.String()))

	msgContext.userConfig.MailConfig.MessageConfig.Customize(ctx, msg, msgContext.mailData)

	recipEmail, err := mail.ParseAddress(msgContext.user.Email)
	if err != nil {
		log.Errorf(ctx, "Unable to parse email address of %v: %v; unable to recover, exiting", PP(msgContext.user), err)
		return nil
	}
	msg.AddRecipient(recipEmail)
	msg.AddToName(string(msgContext.member.Nation))

	msg.SetFrom(noreplyFromAddr)

	client := sendgrid.NewSendGridClientWithApiKey(sendGridConf.APIKey)
	client.Client = urlfetch.Client(ctx)
	if err := client.Send(msg); err != nil {
		log.Errorf(ctx, "Unable to send %v: %v; hope sendgrid gets fixed", msg, err)
		return err
	}
	log.Infof(ctx, "Successfully sent %v", PP(msg))

	log.Infof(ctx, "sendPhaseNotificationsToMail(..., %q, %q, %v, %v, %q) *** SUCCESS ***", host, scheme, gameID, phaseOrdinal, userId)

	return nil
}

func sendPhaseNotificationsToFCM(ctx context.Context, host, scheme string, gameID *datastore.Key, phaseOrdinal int64, userId string, finishedTokens map[string]struct{}) error {
	log.Infof(ctx, "sendPhaseNotificationsToFCM(..., %q, %q, %v, %v, %q, %+v)", host, scheme, gameID, phaseOrdinal, userId, finishedTokens)

	msgContext, err := getPhaseNotificationContext(ctx, host, scheme, gameID, phaseOrdinal, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
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
			Title:       fmt.Sprintf("%s %d, %s", msgContext.phase.Season, msgContext.phase.Year, msgContext.phase.Type),
			Body:        fmt.Sprintf("%s has a new phase.", msgContext.game.Desc),
			Tag:         "diplicity-engine-new-phase",
			ClickAction: msgContext.mapURL.String(),
		}

		fcmToken.PhaseConfig.Customize(ctx, notificationPayload, msgContext.mailData)

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
				if err := sendPhaseNotificationsToFCMFunc.EnqueueIn(ctx, 0, host, scheme, gameID, phaseOrdinal, userId, finishedTokens); err != nil {
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

	log.Infof(ctx, "sendPhaseNotificationsToFCM(..., %q, %q, %v, %v, %q, %+v) *** SUCCESS ***", host, scheme, gameID, phaseOrdinal, userId, finishedTokens)

	return nil
}

func sendPhaseNotificationsToUsers(ctx context.Context, host, scheme string, gameID *datastore.Key, phaseOrdinal int64, uids []string) error {
	log.Infof(ctx, "sendPhaseNotificationsToUsers(..., %q, %q, %v, %v, %+v)", host, scheme, gameID, phaseOrdinal, uids)

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := sendPhaseNotificationsToFCMFunc.EnqueueIn(ctx, 0, host, scheme, gameID, phaseOrdinal, uids[0], map[string]struct{}{}); err != nil {
			log.Errorf(ctx, "Unable to enqueue sending to %q: %v; hope datastore gets fixed", uids[0], err)
			return err
		}
		if err := sendPhaseNotificationsToMailFunc.EnqueueIn(ctx, 0, host, scheme, gameID, phaseOrdinal, uids[0]); err != nil {
			log.Errorf(ctx, "Unable to enqueue sending mail to %q: %v; hope datastore gets fixed", uids[0], err)
			return err
		}
		if len(uids) > 1 {
			if err := sendPhaseNotificationsToUsersFunc.EnqueueIn(ctx, 0, host, scheme, gameID, phaseOrdinal, uids[1:]); err != nil {
				log.Errorf(ctx, "Unable to enqueue sending to rest: %v; hope datastore gets fixed", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit send tx: %v", err)
		return err
	}

	log.Infof(ctx, "sendPhaseNotificationsToUsers(..., %q, %q, %v, %v, %+v) *** SUCCESS ***", host, scheme, gameID, phaseOrdinal, uids)

	return nil
}

func timeoutResolvePhase(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) error {
	log.Infof(ctx, "timeoutResolvePhase(..., %v, %v)", gameID, phaseOrdinal)

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
			log.Errorf(ctx, "datastore.GetMulti(..., %v, %v): %v; hope datastore will get fixed", keys, values, err)
			return err
		}
		game.ID = gameID

		phaseStates := PhaseStates{}

		if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &phaseStates); err != nil {
			log.Errorf(ctx, "Unable to query phase states for %v/%v: %v; hope datastore will get fixed", gameID, phaseID, err)
			return err
		}

		return (&PhaseResolver{
			Context:       ctx,
			Game:          game,
			Phase:         phase,
			PhaseStates:   phaseStates,
			TaskTriggered: true,
		}).Act()
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit resolve tx: %v", err)
		return err
	}

	log.Infof(ctx, "timeoutResolvePhase(..., %v, %v): *** SUCCESS ***", gameID, phaseOrdinal)

	return nil
}

type PhaseResolver struct {
	Context       context.Context
	Game          *Game
	Phase         *Phase
	PhaseStates   PhaseStates
	TaskTriggered bool
}

func (p *PhaseResolver) SCCounts(s *state.State) map[dip.Nation]int {
	res := map[dip.Nation]int{}
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

	if p.TaskTriggered && p.Phase.DeadlineAt.After(time.Now()) {
		log.Infof(p.Context, "Resolution postponed to %v by %v; rescheduling task", p.Phase.DeadlineAt, PP(p.Phase))
		return p.Phase.ScheduleResolution(p.Context)
	}

	if p.Phase.Resolved {
		log.Infof(p.Context, "Already resolved; %v; skipping resolution", PP(p.Phase))
		return nil
	}

	// Roll forward the game state.

	log.Infof(p.Context, "PhaseStates at resolve time: %v", PP(p.PhaseStates))

	orderMap, err := p.Phase.Orders(p.Context)
	if err != nil {
		log.Errorf(p.Context, "Unable to load orders for %v: %v; fix phase.Orders or hope datastore will get fixed", PP(p.Phase), err)
		return err
	}
	log.Infof(p.Context, "Orders at resolve time: %v", PP(orderMap))

	variant := variants.Variants[p.Game.Variant]
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

	// Finish and save old phase.

	p.Phase.Resolved = true
	if err := p.Phase.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save old phase %v: %v; hope datastore gets fixed", PP(p.Phase), err)
		return err
	}

	// Create the new phase.

	newPhase := NewPhase(s, p.Phase.GameID, p.Phase.PhaseOrdinal+1, p.Phase.Host, p.Phase.Scheme)
	if p.Game.PhaseLengthMinutes > 0 {
		newPhase.DeadlineAt = time.Now().Add(time.Minute * p.Game.PhaseLengthMinutes)
	}

	// Check if we can roll forward again, and potentially create new phase states.

	// Prepare some data to collect.
	allReady := true          // All nations are ready to resolve the new phase as well.
	var soloWinner dip.Nation // The nation, if any, reaching solo victory.
	var soloWinnerUser string
	quitters := map[dip.Nation]quitter{} // One per nation that wants to quit, with either dias or eliminated.
	newPhaseStates := PhaseStates{}      // The new phase states to save if we want to prepare resolution of a new phase.
	oldPhaseResult := &PhaseResult{      // A result object for the old phase to simplify collecting user scoped stats.
		GameID:       p.Phase.GameID,
		PhaseOrdinal: p.Phase.PhaseOrdinal,
	}

	for i := range p.Game.Members {
		member := &p.Game.Members[i]

		newPhaseState := &PhaseState{
			GameID:       p.Phase.GameID,
			PhaseOrdinal: newPhase.PhaseOrdinal,
			Nation:       member.Nation,
		}

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
		newOptions := len(s.Phase().Options(s, member.Nation))
		if scCounts[member.Nation] == 0 {
			wasEliminated = true
			// Overwrite DIAS with eliminated, you can't be part of a DIAS if you are eliminated...
			quitters[member.Nation] = quitter{
				state:  eliminatedState,
				member: member,
			}
		} else if scCounts[member.Nation] >= variant.SoloSupplyCenters {
			if soloWinner != "" {
				msg := fmt.Sprintf("Found that %q has >= variant.SoloSupplyCenters (%d) SCs, but %q was already marked as solo winner? WTF?; fix godip?", member.Nation, variant.SoloSupplyCenters, soloWinner)
				log.Errorf(p.Context, msg)
				return fmt.Errorf(msg)
			}
			log.Infof(p.Context, "Found that %q has >= variant.SoloSupplyCenters (%d) SCs, marking %q as solo winner", member.Nation, variant.SoloSupplyCenters, member.Nation)
			soloWinner = member.Nation
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
		autoProbation := wasOnProbation || (!hadOrders && !wasReady)
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

		// If the next phase state is non-default, we must save and append it.
		if autoReady || autoDIAS {
			newPhaseState = &PhaseState{
				GameID:         p.Phase.GameID,
				PhaseOrdinal:   newPhase.PhaseOrdinal,
				Nation:         member.Nation,
				ReadyToResolve: autoReady,
				WantsDIAS:      autoDIAS,
				OnProbation:    autoProbation,
				Note:           fmt.Sprintf("Auto generated due to phase change at %v/%v: %s", p.Phase.GameID, p.Phase.PhaseOrdinal, stateString),
			}
		}

		member.NewestPhaseState = *newPhaseState
		newPhaseStates = append(newPhaseStates, *newPhaseState)
		oldPhaseResult.AllUsers = append(oldPhaseResult.AllUsers, member.User.Id)
	}

	log.Infof(p.Context, "Calculated key metrics: allReady: %v, soloWinner: %q, quitters: %v", allReady, soloWinner, PP(quitters))

	// Check if the game should end.

	if soloWinner != "" || len(quitters) > len(variant.Nations)-1 {
		log.Infof(p.Context, "soloWinner: %q, quitters: %v => game needs to end", soloWinner, PP(quitters))
		// Just to ensure we don't try to resolve it again, even by mistake.
		newPhase.Resolved = true
	}

	// Save the old phase result.

	if err := oldPhaseResult.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save old phase result %v: %v; hope datastore gets fixed", PP(oldPhaseResult), err)
		return err
	}

	// Save the new phase.

	if err := newPhase.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save new Phase %v: %v; hope datastore will get fixed", PP(newPhase), err)
		return err
	}

	if err = newPhase.Recalc(); err != nil {
		return err
	}
	p.Game.NewestPhaseMeta = []PhaseMeta{newPhase.PhaseMeta}

	if newPhase.Resolved {

		// Finish the game and store a game result if the new phase is already resolved.

		log.Infof(p.Context, "New phase is already resolved, marking game as finished and stopping early")
		p.Game.Finished = true
		p.Game.FinishedAt = time.Now()
		p.Game.Closed = true

		diasMembers := []dip.Nation{}
		diasUsers := []string{}
		nmrMembers := []dip.Nation{}
		nmrUsers := []string{}
		eliminatedMembers := []dip.Nation{}
		eliminatedUsers := []string{}
		scores := []GameScore{}

		for _, member := range p.Game.Members {
			var state quitState
			quitter, isQuitter := quitters[member.Nation]
			if isQuitter {
				state = quitter.state
			}

			switch state {
			case diasState:
				diasMembers = append(diasMembers, member.Nation)
				diasUsers = append(diasUsers, member.User.Id)
			case nmrState:
				nmrMembers = append(nmrMembers, member.Nation)
				nmrUsers = append(nmrUsers, member.User.Id)
			case eliminatedState:
				eliminatedMembers = append(eliminatedMembers, member.Nation)
				eliminatedUsers = append(eliminatedUsers, member.User.Id)
			}

			scores = append(scores, GameScore{
				UserId: member.User.Id,
				Member: member.Nation,
				SCs:    scCounts[member.Nation],
			})
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
			Rated:             false,
			CreatedAt:         time.Now(),
		}
		gameResult.AssignScores()
		if err := gameResult.Save(p.Context); err != nil {
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
			if err := p.Act(); err != nil {
				log.Errorf(p.Context, "Unable to continue rolling forward: %v; fix the resolver!", err)
				return err
			}

			log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** delegated to new resolver due to immediate resolution ***", p.Phase.GameID, p.Phase.PhaseOrdinal)

			return nil

		} else {

			// Otherwise, schedule new phase resolution if necessary.

			if p.Game.PhaseLengthMinutes > 0 {
				if err := newPhase.ScheduleResolution(p.Context); err != nil {
					log.Errorf(p.Context, "Unable to schedule resolution for %v: %v; fix ScheduleResolution or hope datastore gets fixed", PP(newPhase), err)
					return err
				}
				log.Infof(p.Context, "%v has phase length of %v minutes, scheduled new resolve", PP(p.Game), p.Game.PhaseLengthMinutes)
			} else {
				log.Infof(p.Context, "%v has a zero phase length, skipping resolve scheduling", PP(p.Game))
			}
		}
	}

	// Notify about the new phase.

	if err := newPhase.NotifyMembers(p.Context, p.Game); err != nil {
		log.Errorf(p.Context, "Unable to enqueue notification to game members: %v; hope datastore will get fixed", err)
		return err
	}

	if p.Game.Finished {
		// Enqueue updating of ratings, which will in turn update user stats.

		if err := UpdateGlickosASAP(p.Context); err != nil {
			log.Errorf(p.Context, "Unable to enqueue updating of ratings: %v; hope datastore gets fixed", err)
			return err
		}

	} else {
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
	Province dip.Province
	Unit     dip.Unit
}

type SC struct {
	Province dip.Province
	Owner    dip.Nation
}

type Dislodger struct {
	Province  dip.Province
	Dislodger dip.Province
}

type Dislodged struct {
	Province  dip.Province
	Dislodged dip.Unit
}

type Bounce struct {
	Province   dip.Province
	BounceList string
}

type Resolution struct {
	Province   dip.Province
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

type PhaseMeta struct {
	PhaseOrdinal   int64
	Season         dip.Season
	Year           int
	Type           dip.PhaseType
	Resolved       bool
	DeadlineAt     time.Time
	NextDeadlineIn time.Duration `datastore:"-" ticker:"true"`
	UnitsJSON      string        `datastore:",noindex"`
	SCsJSON        string        `datastore:",noindex"`
}

func (p *PhaseMeta) Refresh() {
	if !p.DeadlineAt.IsZero() {
		p.NextDeadlineIn = p.DeadlineAt.Sub(time.Now())
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
	GameID      *datastore.Key
	Units       []UnitWrapper
	SCs         []SC
	Dislodgeds  []Dislodged
	Dislodgers  []Dislodger
	Bounces     []Bounce
	Resolutions []Resolution
	Host        string
	Scheme      string
}

func (p *Phase) toVariantsPhase(variant string, orderMap map[dip.Nation]map[dip.Province][]string) *dvars.Phase {
	units := map[dip.Province]dip.Unit{}
	for _, unit := range p.Units {
		units[unit.Province] = unit.Unit
	}
	scs := map[dip.Province]dip.Nation{}
	for _, sc := range p.SCs {
		scs[sc.Province] = sc.Owner
	}
	dislodgeds := map[dip.Province]dip.Unit{}
	for _, d := range p.Dislodgeds {
		dislodgeds[d.Province] = d.Dislodged
	}
	dislodgers := map[dip.Province]dip.Province{}
	for _, d := range p.Dislodgers {
		dislodgers[d.Province] = d.Dislodger
	}
	bounces := map[dip.Province]map[dip.Province]bool{}
	for _, b := range p.Bounces {
		provBounces, found := bounces[b.Province]
		if !found {
			provBounces = map[dip.Province]bool{}
		}
		for _, el := range strings.Split(b.BounceList, ",") {
			provBounces[dip.Province(el)] = true
		}
		bounces[b.Province] = provBounces
	}
	resolutions := map[dip.Province]string{}
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

	return timeoutResolvePhase(ctx, gameID, phaseOrdinal)
}

func loadPhase(w ResponseWriter, r Request) (*Phase, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
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

	member, isMember := game.GetMember(user.Id)
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
	if isMember && !p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "options",
			Route:       ListOptionsRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
		phaseItem.AddLink(r.NewLink(OrderResource.Link("create-order", Create, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)})))
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
	return timeoutResolvePhaseFunc.EnqueueAt(ctx, p.DeadlineAt, p.GameID, p.PhaseOrdinal)
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

func (p *Phase) Save(ctx context.Context) error {
	key, err := p.ID(ctx)
	if err != nil {
		return err
	}
	p.PhaseMeta.UnitsJSON = ""
	p.PhaseMeta.SCsJSON = ""
	_, err = datastore.Put(ctx, key, p)
	return err
}

func NewPhase(s *state.State, gameID *datastore.Key, phaseOrdinal int64, host, scheme string) *Phase {
	current := s.Phase()
	p := &Phase{
		PhaseMeta: PhaseMeta{
			PhaseOrdinal: phaseOrdinal,
			Season:       current.Season(),
			Year:         current.Year(),
			Type:         current.Type(),
		},
		GameID: gameID,
		Host:   host,
		Scheme: scheme,
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

func (p *Phase) NotifyMembers(ctx context.Context, game *Game) error {
	memberIds := make([]string, len(game.Members))
	for i, member := range game.Members {
		memberIds[i] = member.User.Id
	}
	if len(memberIds) == 0 {
		return nil
	}
	return sendPhaseNotificationsToUsersFunc.EnqueueIn(ctx, 0, p.Host, p.Scheme, p.GameID, p.PhaseOrdinal, memberIds)
}

func listOptions(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
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

	member, isMember := game.GetMember(user.Id)
	if !isMember {
		return HTTPErr{"can only load options for member games", 404}
	}

	state, err := phase.State(ctx, variants.Variants[game.Variant], nil)
	if err != nil {
		return err
	}

	w.SetContent(NewItem(state.Phase().Options(state, member.Nation)).SetName("options").SetDesc([][]string{
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

func (p *Phase) Orders(ctx context.Context) (map[dip.Nation]map[dip.Province][]string, error) {
	phaseID, err := PhaseID(ctx, p.GameID, p.PhaseOrdinal)
	if err != nil {
		return nil, err
	}

	orders := []Order{}
	if _, err := datastore.NewQuery(orderKind).Ancestor(phaseID).GetAll(ctx, &orders); err != nil {
		return nil, err
	}

	orderMap := map[dip.Nation]map[dip.Province][]string{}
	for _, order := range orders {
		nationMap, found := orderMap[order.Nation]
		if !found {
			nationMap = map[dip.Province][]string{}
			orderMap[order.Nation] = nationMap
		}
		nationMap[dip.Province(order.Parts[0])] = order.Parts[1:]
	}

	return orderMap, nil
}

func (p *Phase) State(ctx context.Context, variant variants.Variant, orderMap map[dip.Nation]map[dip.Province][]string) (*state.State, error) {
	parsedOrders, err := variant.ParseOrders(orderMap)
	if err != nil {
		return nil, err
	}

	units := map[dip.Province]dip.Unit{}
	for _, unit := range p.Units {
		units[unit.Province] = unit.Unit
	}

	supplyCenters := map[dip.Province]dip.Nation{}
	for _, sc := range p.SCs {
		supplyCenters[sc.Province] = sc.Owner
	}

	dislodgeds := map[dip.Province]dip.Unit{}
	for _, dislodged := range p.Dislodgeds {
		dislodgeds[dislodged.Province] = dislodged.Dislodged
	}

	dislodgers := map[dip.Province]dip.Province{}
	for _, dislodger := range p.Dislodgers {
		dislodgers[dislodger.Province] = dislodger.Dislodger
	}

	bounces := map[dip.Province]map[dip.Province]bool{}
	for _, bounce := range p.Bounces {
		bounceMap := map[dip.Province]bool{}
		for _, prov := range strings.Split(bounce.BounceList, ",") {
			bounceMap[dip.Province(prov)] = true
		}
		bounces[bounce.Province] = bounceMap
	}

	return variant.Blank(variant.Phase(p.Year, p.Season, p.Type)).Load(units, supplyCenters, dislodgeds, dislodgers, bounces, parsedOrders), nil
}

func renderPhaseMap(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
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

	var nation dip.Nation

	if member, found := game.GetMember(user.Id); found {
		nation = member.Nation
	}

	foundOrders, err := phase.Orders(ctx)
	if err != nil {
		return err
	}

	ordersToDisplay := map[dip.Nation]map[dip.Province][]string{}
	for nat, orders := range foundOrders {
		log.Infof(ctx, "%#v == %#v => %v", nat, nation, nat == nation)
		if nat == nation || phase.Resolved {
			ordersToDisplay[nat] = orders
		}
	}

	vPhase := phase.toVariantsPhase(game.Variant, ordersToDisplay)

	return dvars.RenderPhaseMap(w, r, vPhase)
}

func listPhases(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	game := &Game{}
	if err := datastore.Get(ctx, gameID, game); err != nil {
		return err
	}
	member, isMember := game.GetMember(user.Id)
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
	}

	w.SetContent(phases.Item(r, gameID))
	return nil
}
