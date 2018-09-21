package game

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/aymerick/raymond"
	"github.com/davecgh/go-spew/spew"
	"github.com/jhillyerd/enmime"
	"github.com/zond/diplicity/auth"
	"github.com/zond/go-fcm"
	"github.com/zond/godip"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	"gopkg.in/sendgrid/sendgrid-go.v2"

	. "github.com/zond/goaeoas"
)

func init() {
	raymond.RegisterHelper("joinNations", func(glue string, nats Nations) string {
		parts := make([]string, len(nats))
		for i, nat := range nats {
			parts[i] = string(nat)
		}
		return strings.Join(parts, glue)
	})
}

const (
	messageKind    = "Message"
	channelKind    = "Channel"
	seenMarkerKind = "SeenMarker"
)

var (
	sendMsgNotificationsToUsersFunc *DelayFunc
	sendMsgNotificationsToFCMFunc   *DelayFunc
	sendMsgNotificationsToMailFunc  *DelayFunc

	MessageResource *Resource
)

func init() {
	sendMsgNotificationsToUsersFunc = NewDelayFunc("game-sendMsgNotificationsToUsers", sendMsgNotificationsToUsers)
	sendMsgNotificationsToFCMFunc = NewDelayFunc("game-sendMsgNotificationsToFCM", sendMsgNotificationsToFCM)
	sendMsgNotificationsToMailFunc = NewDelayFunc("game-sendMsgNotificationsToMail", sendMsgNotificationsToMail)

	MessageResource = &Resource{
		Create:     createMessage,
		CreatePath: "/Game/{game_id}/Messages",
		Listers: []Lister{
			{
				Path:    "/Game/{game_id}/Channel/{channel_members}/Messages",
				Route:   ListMessagesRoute,
				Handler: listMessages,
			},
		},
	}
}

type msgNotificationContext struct {
	channelID    *datastore.Key
	userID       *datastore.Key
	userConfigID *datastore.Key
	game         *Game
	member       *Member
	channel      *Channel
	message      *Message
	user         *auth.User
	userConfig   *auth.UserConfig
	fcmData      map[string]interface{}
	mailData     map[string]interface{}
	mapURL       *url.URL
}

func getMsgNotificationContext(ctx context.Context, host, scheme string, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, userId string) (*msgNotificationContext, error) {
	res := &msgNotificationContext{}

	var err error
	res.channelID, err = ChannelID(ctx, gameID, channelMembers)
	if err != nil {
		log.Errorf(ctx, "ChannelID(..., %v, %v): %v; fix the ChannelID func", gameID, channelMembers, err)
		return nil, err
	}

	res.userID = auth.UserID(ctx, userId)

	res.userConfigID = auth.UserConfigID(ctx, res.userID)

	res.game = &Game{}
	res.channel = &Channel{}
	res.message = &Message{}
	res.user = &auth.User{}
	res.userConfig = &auth.UserConfig{}
	err = datastore.GetMulti(
		ctx,
		[]*datastore.Key{gameID, res.channelID, messageID, res.userConfigID, res.userID},
		[]interface{}{res.game, res.channel, res.message, res.userConfig, res.user},
	)
	if err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			if merr[3] == datastore.ErrNoSuchEntity {
				log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
				return nil, noConfigError
			}
			log.Errorf(ctx, "Unable to load game, channel, message, user and user config: %v; hope datastore gets fixed", err)
			return nil, err
		} else {
			log.Errorf(ctx, "Unable to load game, channel, message, user and user config: %v; hope datastore gets fixed", err)
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

	res.mapURL, err = router.Get(RenderPhaseMapRoute).URL("game_id", res.game.ID.Encode(), "phase_ordinal", fmt.Sprint(res.game.NewestPhaseMeta[0].PhaseOrdinal))
	if err != nil {
		log.Errorf(ctx, "Unable to create map URL for game %v and phase %v: %v; wtf?", res.game.ID, res.game.NewestPhaseMeta[0].PhaseOrdinal, err)
		return nil, err
	}
	res.mapURL.Host = host
	res.mapURL.Scheme = scheme

	res.mailData = map[string]interface{}{
		"game":    res.game,
		"channel": res.channel,
		"message": res.message,
		"user":    res.user,
		"mapLink": res.mapURL.String(),
	}
	res.fcmData = map[string]interface{}{
		"type":    "message",
		"message": res.message,
	}

	return res, nil
}

func sendEmailError(ctx context.Context, to string, errorMessage string) error {
	sendGridConf, err := GetSendGrid(ctx)
	if err != nil {
		return err
	}

	msg := sendgrid.NewMail()
	msg.SetText(fmt.Sprint("Your recent mail to diplicity was not successfully parsed.\n\nAn error message follows.\n\n%v", errorMessage))
	msg.SetSubject("Unsuccessfully parsed")
	msg.AddTo(to)
	msg.SetFrom(noreplyFromAddr)

	client := sendgrid.NewSendGridClientWithApiKey(sendGridConf.APIKey)
	client.Client = urlfetch.Client(ctx)
	if err := client.Send(msg); err != nil {
		return err
	}

	return nil
}

func sendMsgNotificationsToMail(ctx context.Context, host, scheme string, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, userId string) error {
	log.Infof(ctx, "sendMsgNotificationsToMail(..., %q, %q, %v, %+v, %v, %q)", host, scheme, gameID, channelMembers, messageID, userId)

	msgContext, err := getMsgNotificationContext(ctx, host, scheme, gameID, channelMembers, messageID, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get msg notification context: %v; fix getMsgNotificationContext or hope datastore gets fixed", err)
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
	msg.SetText(fmt.Sprintf("%s\n\nVisit %s to stop receiving email like this.\n\nVisit %s to see the latest phase in this game.", msgContext.message.Body, unsubscribeURL.String(), msgContext.mapURL.String()))
	msg.SetSubject(
		fmt.Sprintf(
			"%s: %s => %s",
			msgContext.game.DescFor(msgContext.member.Nation),
			msgContext.game.AbbrNat(msgContext.message.Sender),
			msgContext.game.AbbrNats(msgContext.message.ChannelMembers).String(),
		),
	)
	msg.AddHeader("List-Unsubscribe", fmt.Sprintf("<%s>", unsubscribeURL.String()))

	msgContext.userConfig.MailConfig.MessageConfig.Customize(ctx, msg, msgContext.mailData)

	recipEmail, err := mail.ParseAddress(msgContext.user.Email)
	if err != nil {
		log.Errorf(ctx, "Unable to parse email address of %v: %v; unable to recover, exiting", PP(msgContext.user), err)
		return nil
	}
	msg.AddRecipient(recipEmail)
	msg.AddToName(channelMembers.String())

	fromToken, err := auth.EncodeString(ctx, fmt.Sprintf("%s,%s", msgContext.member.Nation, messageID.Encode()))
	if err != nil {
		log.Errorf(ctx, "Unable to create auth token for reply address: %v; fix EncodeString or hope datastore gets fixed", err)
		return err
	}

	fromAddress := fmt.Sprintf(fromAddressPattern, fromToken)
	fromEmail, err := mail.ParseAddress(fromAddress)
	if err != nil {
		log.Errorf(ctx, "Unable to parse reply email address %q: %v; fix the address generation", fromAddress, err)
		return err
	}
	msg.SetFromEmail(fromEmail)
	msg.SetFromName(string(msgContext.message.Sender))

	client := sendgrid.NewSendGridClientWithApiKey(sendGridConf.APIKey)
	client.Client = urlfetch.Client(ctx)
	if err := client.Send(msg); err != nil {
		log.Errorf(ctx, "Unable to send %v: %v; hope sendgrid gets fixed", msg, err)
		return err
	}
	log.Infof(ctx, "Successfully sent %v", PP(msg))

	log.Infof(ctx, "sendMsgNotificationsToMail(..., %q, %q, %v, %+v, %v, %q) *** SUCCESS ***", host, scheme, gameID, channelMembers, messageID, userId)

	return nil
}

func sendMsgNotificationsToFCM(ctx context.Context, host, scheme string, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, userId string, finishedTokens map[string]struct{}) error {
	log.Infof(ctx, "sendMsgNotificationsToFCM(..., %q, %q, %v, %+v, %v, %q, %+v)", host, scheme, gameID, channelMembers, messageID, userId, finishedTokens)

	msgContext, err := getMsgNotificationContext(ctx, host, scheme, gameID, channelMembers, messageID, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get msg notification context: %v; fix getMsgNotificationContext or hope datastore gets fixed", err)
		return err
	}

	dataPayload, err := NewFCMData(msgContext.fcmData)
	if err != nil {
		log.Errorf(ctx, "Unable to encode FCM data payload %v: %v; fix NewFCMData", msgContext.fcmData, err)
		return err
	}

	if len(msgContext.userConfig.FCMTokens) == 0 {
		log.Infof(ctx, "%q hasn't registered any FCM tokens, will skip sending notifiations", userId)
		return nil
	}

	for _, fcmToken := range msgContext.userConfig.FCMTokens {
		if fcmToken.Disabled || fcmToken.Value == "" {
			continue
		}
		if _, done := finishedTokens[fcmToken.Value]; done {
			continue
		}
		log.Infof(ctx, "Found an FCM token to send to: %v", PP(fcmToken))
		finishedTokens[fcmToken.Value] = struct{}{}
		notificationBody := msgContext.message.Body
		if runes := []rune(notificationBody); len(runes) > 512 {
			notificationBody = string(runes[:512]) + "..."
		}
		notificationPayload := &fcm.NotificationPayload{
			Title: fmt.Sprintf(
				"%s: %s => %s",
				msgContext.game.DescFor(msgContext.member.Nation),
				msgContext.game.AbbrNat(msgContext.message.Sender),
				msgContext.game.AbbrNats(msgContext.message.ChannelMembers).String(),
			),
			Body:        notificationBody,
			Tag:         "diplicity-engine-new-message",
			ClickAction: fmt.Sprintf("%s://%s/Game/%s/Channel/%s/Messages", scheme, host, gameID.Encode(), channelMembers.String()),
		}

		fcmToken.MessageConfig.Customize(ctx, notificationPayload, msgContext.mailData)

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
				if err := sendMsgNotificationsToFCMFunc.EnqueueIn(ctx, 0, host, scheme, gameID, channelMembers, messageID, userId, finishedTokens); err != nil {
					log.Errorf(ctx, "Unable to enqueue sending of rest of notifications: %v; hope datastore gets fixed", err)
					return err
				}
			}

			return nil
		}, &datastore.TransactionOptions{XG: true}); err != nil {
			log.Errorf(ctx, "Unable to commit send tx: %v", err)
			return err
		}
		log.Infof(ctx, "Successfully enqueued sent a notification and enqueued sending the rest, exiting")
		break
	}

	log.Infof(ctx, "sendMsgNotificationsToFCM(..., %q, %q, %v, %+v, %v, %q, %+v) *** SUCCESS ***", host, scheme, gameID, channelMembers, messageID, userId, finishedTokens)

	return nil
}

func sendMsgNotificationsToUsers(ctx context.Context, host, scheme string, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, uids []string) error {
	log.Infof(ctx, "sendMsgNotificationsToUsers(..., %q, %q, %v, %+v, %v, %+v)", host, scheme, gameID, channelMembers, messageID, uids)

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			log.Errorf(ctx, "Unable to load game %v: %v; hope datastore gets fixed", gameID, err)
			return err
		}
		game.ID = gameID
		member, isMember := game.GetMemberByUserId(uids[0])
		if !isMember {
			log.Errorf(ctx, "%v isn't a member of %v, wtf? Giving up.", uids[0], gameID)
			return nil
		}
		channels, err := loadChannels(ctx, game, member.Nation)
		if err != nil {
			log.Errorf(ctx, "Unable to load channels for %v in %v: %v; hope datastore gets fixed", member.Nation, gameID, err)
			return err
		}
		if err := countUnreadMessages(ctx, channels, member.Nation); err != nil {
			log.Errorf(ctx, "Unable to count unread messages for %v in %v; hope datastore gets fixed", member.Nation, gameID, err)
			return err
		}
		total := 0
		for _, channel := range channels {
			total += channel.NMessagesSince.NMessages
		}
		member.UnreadMessages = total
		if err := game.Save(ctx); err != nil {
			log.Errorf(ctx, "Unable to save %v after updating unread messages for %v: %v; hope datastore gets fixed", gameID, member.Nation, err)
			return err
		}

		if err := sendMsgNotificationsToFCMFunc.EnqueueIn(ctx, 0, host, scheme, gameID, channelMembers, messageID, uids[0], map[string]struct{}{}); err != nil {
			log.Errorf(ctx, "Unable to enqueue sending FCM to %q: %v; hope datastore gets fixed", uids[0], err)
			return err
		}
		if err := sendMsgNotificationsToMailFunc.EnqueueIn(ctx, 0, host, scheme, gameID, channelMembers, messageID, uids[0]); err != nil {
			log.Errorf(ctx, "Unable to enqueue sending mail to %q: %v; hope datastore gets fixed", uids[0], err)
			return err
		}
		if len(uids) > 1 {
			if err := sendMsgNotificationsToUsersFunc.EnqueueIn(ctx, 0, host, scheme, gameID, channelMembers, messageID, uids[1:]); err != nil {
				log.Errorf(ctx, "Unable to enqueue sending to rest: %v; hope datastore gets fixed", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit send tx: %v", err)
		return err
	}
	log.Infof(ctx, "Successfully enqueued sending notification to %q, and to rest if there were any", uids[0])

	log.Infof(ctx, "sendMsgNotificationsToUsers(..., %q, %q, %v, %+v, %v, %+v) *** SUCCESS ***", host, scheme, gameID, channelMembers, messageID, uids)

	return nil
}

type Nations []godip.Nation

func (n *Nations) FromString(s string) {
	parts := strings.Split(s, ",")
	*n = make(Nations, len(parts))
	for i := range parts {
		(*n)[i] = godip.Nation(parts[i])
	}
}

func (n Nations) Includes(m godip.Nation) bool {
	for i := range n {
		if n[i] == m {
			return true
		}
	}
	return false
}

func (n Nations) Len() int {
	return len(n)
}

func (n Nations) Less(i, j int) bool {
	return n[i] < n[j]
}

func (n Nations) Swap(i, j int) {
	n[i], n[j] = n[j], n[i]
}

func (n Nations) String() string {
	slice := make([]string, len(n))
	for i := range n {
		slice[i] = string(n[i])
	}
	return strings.Join(slice, ",")
}

type Channels []Channel

func (c Channels) Item(r Request, gameID *datastore.Key, isMember bool) *Item {
	channelItems := make(List, len(c))
	for i := range c {
		channelItems[i] = c[i].Item(r)
	}
	channelsItem := NewItem(channelItems).SetName("channels").SetDesc([][]string{
		[]string{
			"Lazy channels",
			"Channels are created lazily when messages are created for previously non existing channels.",
			"This means that you can write messages to combinations of nations not currently represented by a channel listed here, and the channel will simply be created for you.",
		},
		[]string{
			"Counters",
			"Channels tell you how many messages they have, and how many new since you last loaded messages from them.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListChannelsRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	if isMember {
		channelsItem.AddLink(r.NewLink(MessageResource.Link("message", Create, []string{"game_id", gameID.Encode()})))
	}
	return channelsItem
}

type NMessagesSince struct {
	Since     time.Time
	NMessages int
}

type Channel struct {
	GameID         *datastore.Key
	Members        Nations
	NMessages      int
	NMessagesSince NMessagesSince `datastore:"-"`
}

type SeenMarker struct {
	GameID  *datastore.Key
	Members Nations
	Owner   godip.Nation
	At      time.Time `methods:"POST"`
}

func SeenMarkerID(ctx context.Context, channelID *datastore.Key, owner godip.Nation) (*datastore.Key, error) {
	if channelID == nil || owner == "" {
		return nil, fmt.Errorf("seen markers must have channels and owners")
	}
	return datastore.NewKey(ctx, seenMarkerKind, string(owner), 0, channelID), nil
}

func (s *SeenMarker) ID(ctx context.Context) (*datastore.Key, error) {
	channelID, err := ChannelID(ctx, s.GameID, s.Members)
	if err != nil {
		return nil, err
	}
	return SeenMarkerID(ctx, channelID, s.Owner)
}

func (c *Channel) Item(r Request) *Item {
	sort.Sort(c.Members)
	channelItem := NewItem(c).SetName(c.Members.String())
	channelItem.AddLink(r.NewLink(Link{
		Rel:         "messages",
		Route:       ListMessagesRoute,
		RouteParams: []string{"game_id", c.GameID.Encode(), "channel_members", c.Members.String()},
	}))
	return channelItem
}

func ChannelID(ctx context.Context, gameID *datastore.Key, members Nations) (*datastore.Key, error) {
	if gameID == nil || len(members) < 2 {
		return nil, fmt.Errorf("channels must have games and > 1 members")
	}
	if gameID.IntID() == 0 {
		return nil, fmt.Errorf("gameIDs must have int IDs")
	}
	sort.Sort(members)
	return datastore.NewKey(ctx, channelKind, members.String(), 0, gameID), nil
}

func (c *Channel) ID(ctx context.Context) (*datastore.Key, error) {
	return ChannelID(ctx, c.GameID, c.Members)
}

func (c *Channel) CountSince(ctx context.Context, since time.Time) error {
	channelID, err := ChannelID(ctx, c.GameID, c.Members)
	if err != nil {
		return err
	}
	count, err := datastore.NewQuery(messageKind).Ancestor(channelID).Filter("CreatedAt>", since).Count(ctx)
	if err != nil {
		return err
	}
	c.NMessagesSince.Since = since
	c.NMessagesSince.NMessages = count
	return nil
}

type Messages []Message

func (m Messages) Item(r Request, gameID *datastore.Key, channelMembers Nations) *Item {
	messageItems := make(List, len(m))
	for i := range m {
		messageItems[i] = m[i].Item(r)
	}
	messagesItem := NewItem(messageItems).SetName("messages").SetDesc([][]string{
		[]string{
			"Limiting messages",
			"Messages normally contain all messages for the chosen channel, but if you provide a `since` query parameter they will only contain new messages since that time.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListMessagesRoute,
		RouteParams: []string{"game_id", gameID.Encode(), "channel_members", channelMembers.String()},
	})).AddLink(r.NewLink(MessageFlagResource.Link("flag-messages", Create, []string{"game_id", gameID.Encode(), "channel_members", channelMembers.String()})))
	return messagesItem
}

type Message struct {
	ID             *datastore.Key `datastore:"-"`
	GameID         *datastore.Key
	ChannelMembers Nations `methods:"POST"`
	Sender         godip.Nation
	Body           string `methods:"POST" datastore:",noindex"`
	CreatedAt      time.Time
	Age            time.Duration `datastore:"-" ticker:"true"`
}

func (m *Message) NotifyRecipients(ctx context.Context, host, scheme string, channel *Channel, game *Game) error {
	// Build a slice of game state IDs.
	stateIDs := []*datastore.Key{}
	for _, nat := range m.ChannelMembers {
		stateID, err := GameStateID(ctx, m.GameID, nat)
		if err != nil {
			return err
		}
		stateIDs = append(stateIDs, stateID)
	}

	// Load the game states for this slice.
	states := make(GameStates, len(stateIDs))
	err := datastore.GetMulti(ctx, stateIDs, states)

	// Populate a list of nations that haven't muted the sender (and aren't the sender).
	unmutedMembers := []godip.Nation{}
	if err == nil {
		for _, state := range states {
			if state.Nation != m.Sender && !state.HasMuted(m.Sender) {
				unmutedMembers = append(unmutedMembers, state.Nation)
			}
		}
	} else {
		if merr, ok := err.(appengine.MultiError); ok {
			for index, serr := range merr {
				if serr == nil {
					if m.ChannelMembers[index] != m.Sender && !states[index].HasMuted(m.Sender) {
						unmutedMembers = append(unmutedMembers, states[index].Nation)
					}
				} else if serr != datastore.ErrNoSuchEntity {
					return err
				} else if m.ChannelMembers[index] != m.Sender {
					unmutedMembers = append(unmutedMembers, m.ChannelMembers[index])
				}
			}
		} else if err != datastore.ErrNoSuchEntity {
			return err
		}
	}

	// Use this unmuted list to build a set of user IDs to send the message to.
	memberIds := []string{}
	for _, nat := range unmutedMembers {
		for _, member := range game.Members {
			if member.Nation == nat {
				memberIds = append(memberIds, member.User.Id)
				break
			}
		}
	}

	if len(memberIds) == 0 {
		log.Infof(ctx, "Message had no unmuted recipients, skipping notifications")
		return nil
	}

	if err := sendMsgNotificationsToUsersFunc.EnqueueIn(ctx, 0, host, scheme, m.GameID, m.ChannelMembers, m.ID, memberIds); err != nil {
		log.Errorf(ctx, "Unable to schedule notification tasks: %v", err)
		return err
	}

	log.Infof(ctx, "Successfully scheduled notifications to %+v", memberIds)

	return nil
}

func (m *Message) Item(r Request) *Item {
	return NewItem(m).SetName(string(m.Sender))
}

func createMessageHelper(ctx context.Context, r Request, message *Message) error {
	if strings.TrimSpace(message.Body) == "" {
		return HTTPErr{"can not create empty messages", http.StatusBadRequest}
	}

	message.CreatedAt = time.Now()
	sort.Sort(message.ChannelMembers)

	channelID, err := ChannelID(ctx, message.GameID, message.ChannelMembers)
	if err != nil {
		return err
	}

	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		channel := &Channel{}
		if err := datastore.GetMulti(ctx, []*datastore.Key{message.GameID, channelID}, []interface{}{game, channel}); err != nil {
			if merr, ok := err.(appengine.MultiError); ok {
				if merr[0] == nil && merr[1] == datastore.ErrNoSuchEntity {
					channel.GameID = message.GameID
					channel.Members = message.ChannelMembers
					channel.NMessages = 0
				} else {
					return merr
				}
			} else {
				return err
			}
		}
		game.ID = message.GameID
		if !game.Started {
			return HTTPErr{"game not yet started", http.StatusBadRequest}
		}
		if !game.Finished {
			if game.DisablePrivateChat && len(message.ChannelMembers) == 2 {
				return HTTPErr{"private chat disabled", http.StatusBadRequest}
			}
			if game.DisableGroupChat && len(message.ChannelMembers) > 2 && len(message.ChannelMembers) < len(variants.Variants[game.Variant].Nations) {
				return HTTPErr{"group chat disabled", http.StatusBadRequest}
			}
			if game.DisableConferenceChat && len(message.ChannelMembers) == len(variants.Variants[game.Variant].Nations) {
				return HTTPErr{"conference chat disabled", http.StatusBadRequest}
			}
		}
		if message.ID, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, messageKind, channelID), message); err != nil {
			return err
		}
		channel.NMessages += 1
		if _, err = datastore.Put(ctx, channelID, channel); err != nil {
			return err
		}

		scheme := "http"
		if r.Req().TLS != nil {
			scheme = "https"
		}
		return message.NotifyRecipients(ctx, r.Req().Host, scheme, channel, game)
	}, &datastore.TransactionOptions{XG: true})
}

func createMessage(w ResponseWriter, r Request) (*Message, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	game := &Game{}
	err = datastore.Get(ctx, gameID, game)
	if err != nil {
		return nil, err
	}
	game.ID = gameID

	member, found := game.GetMemberByUserId(user.Id)
	if !found {
		return nil, HTTPErr{"can only create messages in member games", http.StatusNotFound}
	}

	message := &Message{}
	if err := Copy(message, r, "POST"); err != nil {
		return nil, err
	}

	if !message.ChannelMembers.Includes(member.Nation) {
		return nil, HTTPErr{"can only send messages to member channels", http.StatusForbidden}
	}

	for _, channelMember := range message.ChannelMembers {
		if !Nations(variants.Variants[game.Variant].Nations).Includes(channelMember) {
			return nil, HTTPErr{"unknown channel member", http.StatusBadRequest}
		}
	}

	message.GameID = gameID
	message.Sender = member.Nation

	if err := createMessageHelper(ctx, r, message); err != nil {
		return nil, err
	}

	return message, nil
}

func publicChannel(variant string) Nations {
	publicChannel := make(Nations, len(variants.Variants[variant].Nations))
	copy(publicChannel, variants.Variants[variant].Nations)
	sort.Sort(publicChannel)

	return publicChannel
}

func isPublic(variant string, members Nations) bool {
	public := publicChannel(variant)

	sort.Sort(members)

	if len(members) != len(public) {
		return false
	}

	for i := range public {
		if members[i] != public[i] {
			return false
		}
	}

	return true
}

func listMessages(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	channelMembers := Nations{}
	channelMembers.FromString(r.Vars()["channel_members"])

	var since *time.Time
	if sinceParam := r.Req().URL.Query().Get("since"); sinceParam != "" {
		sinceTime, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			return err
		}
		since = &sinceTime
	}

	game := &Game{}
	err = datastore.Get(ctx, gameID, game)
	if err != nil {
		return err
	}
	game.ID = gameID
	if !game.Started {
		w.SetContent((Messages{}).Item(r, gameID, channelMembers))
		return nil
	}

	var nation godip.Nation
	mutedNats := map[godip.Nation]struct{}{}
	if member, found := game.GetMemberByUserId(user.Id); found {
		nation = member.Nation
		gameStateID, err := GameStateID(ctx, gameID, nation)
		if err != nil {
			return err
		}
		gameState := &GameState{}
		if err = datastore.Get(ctx, gameStateID, gameState); err == nil {
			for _, nat := range gameState.Muted {
				mutedNats[nat] = struct{}{}
			}
		} else if err != datastore.ErrNoSuchEntity {
			return err
		}
	}

	if !game.Finished && !channelMembers.Includes(nation) && !isPublic(game.Variant, channelMembers) {
		return HTTPErr{"can only list member channels", http.StatusForbidden}
	}

	channelID, err := ChannelID(ctx, gameID, channelMembers)
	if err != nil {
		return err
	}

	messages := Messages{}
	var seenMarker *SeenMarker
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		q := datastore.NewQuery(messageKind).Ancestor(channelID)
		if since != nil {
			q = q.Filter("CreatedAt>", *since)
		}
		messageIDs, err := q.Order("-CreatedAt").GetAll(ctx, &messages)
		if err != nil {
			return err
		}
		for i := range messages {
			messages[i].ID = messageIDs[i]
			messages[i].Age = time.Now().Sub(messages[i].CreatedAt)
		}
		if nation != "" {
			seenMarkerID, err := SeenMarkerID(ctx, channelID, nation)
			if err != nil {
				return err
			}
			seenMarker = &SeenMarker{}
			if err := datastore.Get(ctx, seenMarkerID, seenMarker); err == datastore.ErrNoSuchEntity {
				err = nil
				seenMarker = nil
			} else if err != nil {
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return err
	}

	if nation != "" && len(messages) > 0 && (seenMarker == nil || seenMarker.At.Before(messages[0].CreatedAt)) {
		seenMarker = &SeenMarker{
			GameID:  gameID,
			Owner:   nation,
			Members: channelMembers,
			At:      messages[0].CreatedAt,
		}
		seenMarkerID, err := seenMarker.ID(ctx)
		if err != nil {
			return err
		}
		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			game := &Game{}
			err = datastore.Get(ctx, gameID, game)
			if err != nil {
				return err
			}
			game.ID = gameID

			member, isMember := game.GetMemberByUserId(user.Id)
			if !isMember {
				return fmt.Errorf("not member of the game?")
			}

			channels, err := loadChannels(ctx, game, nation)
			if err != nil {
				return err
			}
			filteredChannels := Channels{}
			for _, channel := range channels {
				if channel.Members.String() != seenMarker.Members.String() {
					filteredChannels = append(filteredChannels, channel)
				}
			}

			if err := countUnreadMessages(ctx, filteredChannels, nation); err != nil {
				return err
			}

			unread := 0
			for _, channel := range filteredChannels {
				unread += channel.NMessagesSince.NMessages
			}

			member.UnreadMessages = unread
			if err := game.Save(ctx); err != nil {
				return err
			}

			if _, err = datastore.Put(ctx, seenMarkerID, seenMarker); err != nil {
				return err
			}

			return nil
		}, &datastore.TransactionOptions{XG: false}); err != nil {
			return err
		}
	}

	filteredMessages := make(Messages, 0, len(messages))
	for _, msg := range messages {
		if _, isMuted := mutedNats[msg.Sender]; !isMuted {
			filteredMessages = append(filteredMessages, msg)
		}
	}

	w.SetContent(filteredMessages.Item(r, gameID, channelMembers))
	return nil
}

func loadChannels(ctx context.Context, game *Game, viewer godip.Nation) (Channels, error) {
	channels := Channels{}
	if game.Finished {
		_, err := datastore.NewQuery(channelKind).Ancestor(game.ID).GetAll(ctx, &channels)
		if err != nil {
			return nil, err
		}
	} else if game.Started {
		if viewer == "" {
			channelID, err := ChannelID(ctx, game.ID, publicChannel(game.Variant))
			if err != nil {
				return nil, err
			}
			channel := &Channel{}
			if err := datastore.Get(ctx, channelID, channel); err == nil {
				channels = append(channels, *channel)
			} else if err != datastore.ErrNoSuchEntity {
				return nil, err
			}
		} else {
			_, err := datastore.NewQuery(channelKind).Ancestor(game.ID).Filter("Members=", viewer).GetAll(ctx, &channels)
			if err != nil {
				return nil, err
			}
		}
	}
	return channels, nil
}

func countUnreadMessages(ctx context.Context, channels Channels, viewer godip.Nation) error {
	seenMarkerTimes := make([]time.Time, len(channels))

	seenMarkerIDs := make([]*datastore.Key, len(channels))
	for i := range channels {
		channelID, err := channels[i].ID(ctx)
		if err != nil {
			return err
		}
		seenMarkerIDs[i], err = SeenMarkerID(ctx, channelID, viewer)
		if err != nil {
			return err
		}
	}
	seenMarkers := make([]SeenMarker, len(channels))
	err := datastore.GetMulti(ctx, seenMarkerIDs, seenMarkers)
	if err == nil {
		for i := range channels {
			seenMarkerTimes[i] = seenMarkers[i].At
		}
	} else if merr, ok := err.(appengine.MultiError); ok {
		for i, serr := range merr {
			if serr == nil {
				seenMarkerTimes[i] = seenMarkers[i].At
			} else if serr != datastore.ErrNoSuchEntity {
				return err
			}
		}
	} else if err != datastore.ErrNoSuchEntity {
		return err
	}

	results := make(chan error)
	for i := range channels {
		go func(c *Channel, since time.Time) {
			if since.IsZero() {
				c.NMessagesSince.NMessages = c.NMessages
				results <- nil
			} else {
				results <- c.CountSince(ctx, since)
			}
		}(&channels[i], seenMarkerTimes[i])
	}
	merr := appengine.MultiError{}
	for _ = range channels {
		if err := <-results; err != nil {
			merr = append(merr, err)
		}
	}
	if len(merr) > 0 {
		return merr
	}
	return nil
}

func listChannels(w ResponseWriter, r Request) error {
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
	err = datastore.Get(ctx, gameID, game)
	if err != nil {
		return err
	}
	game.ID = gameID

	var nation godip.Nation

	member, isMember := game.GetMemberByUserId(user.Id)
	if isMember {
		nation = member.Nation
	}

	channels, err := loadChannels(ctx, game, nation)
	if err != nil {
		return err
	}

	if isMember {
		if err := countUnreadMessages(ctx, channels, nation); err != nil {
			return err
		}
	} else {
		for i := range channels {
			channels[i].NMessagesSince.NMessages = channels[i].NMessages
		}
	}

	w.SetContent(channels.Item(r, gameID, isMember))
	return nil
}

func receiveMail(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	log.Infof(ctx, "Received incoming email")

	b, err := ioutil.ReadAll(r.Req().Body)
	if err != nil {
		log.Errorf(ctx, "Unable to read body from request %v: %v", spew.Sdump(r.Req()), err)
		return err
	}

	enmsg, err := enmime.ReadEnvelope(bytes.NewBuffer(b))
	if err != nil {
		e := fmt.Sprintf("Unable to parse\n%s\ninto a mime message: %v", string(b), err)
		log.Errorf(ctx, e)
		return fmt.Errorf(e)
	}

	from := enmsg.GetHeader("From")

	toAddressString := enmsg.GetHeader("To")

	toAddress, err := mail.ParseAddress(toAddressString)
	if err != nil {
		e := fmt.Sprintf("Unable to parse recipient address %q: %v", toAddressString, err)
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	match := fromAddressReg.FindStringSubmatch(toAddress.Address)
	if len(match) == 0 {
		e := fmt.Sprintf("Recipient address of %v doesn't match %v.", toAddress, fromAddressReg)
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	fromToken := match[1]
	plainToken, err := auth.DecodeString(ctx, fromToken)
	if err != nil {
		e := fmt.Sprintf("Unable to successfully decrypt token %q in %q: %v.", match[1], match[0], err)
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	parts := strings.Split(plainToken, ",")
	if len(parts) != 2 {
		e := fmt.Sprintf("Decrypted token %q is not two strings joined by ','.", fromToken)
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	fromNation := parts[0]
	messageID, err := datastore.DecodeKey(parts[1])
	if err != nil {
		e := fmt.Sprintf("Unable to decode message ID %q: %v.", parts[1], err)
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	message := &Message{}
	if err := datastore.Get(ctx, messageID, message); err != nil {
		e := fmt.Sprintf("Unable to load original message from datastore, unable to create reply: %v", err)
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	paragraphs := []string{}
	paragraph := []string{}
	for _, line := range strings.Split(enmsg.Text, "\n") {
		paragraph = append(paragraph, line)
		if strings.TrimSpace(line) == "" {
			paragraphs = append(paragraphs, strings.Join(paragraph, "\n"))
			paragraph = []string{}
		}
	}
	paragraphs = append(paragraphs, strings.Join(paragraph, "\n"))

	foundFirstLine := false
	okLines := []string{}
	for _, line := range paragraphs {
		if !foundFirstLine {
			if strings.TrimSpace(line) != "" {
				foundFirstLine = true
			} else {
				continue
			}
		}

		if strings.Contains(line, toAddress.Address) {
			for i := len(okLines); i > 0; i-- {
				if strings.TrimSpace(okLines[i-1]) == "" {
					okLines = okLines[:i-1]
				} else {
					break
				}
			}
			break
		}
		okLines = append(okLines, strings.TrimRightFunc(line, unicode.IsSpace))
	}

	newMessage := &Message{
		GameID:         message.GameID,
		ChannelMembers: message.ChannelMembers,
		Sender:         godip.Nation(fromNation),
		Body:           strings.Join(okLines, "\n"),
	}

	if strings.TrimSpace(newMessage.Body) == "" {
		e := "Unable to send empty message."
		log.Errorf(ctx, e)
		return sendEmailError(ctx, from, e)
	}

	log.Infof(ctx, "Received %v via email", PP(newMessage))

	return createMessageHelper(ctx, r, newMessage)
}
