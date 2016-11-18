package game

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/go-fcm"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
	"gopkg.in/sendgrid/sendgrid-go.v2"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	messageKind = "Message"
	channelKind = "Channel"
)

var (
	sendMsgNotificationsToUsersFunc *DelayFunc
	sendMsgNotificationsToFCMFunc   *DelayFunc
	sendMsgNotificationsToMailFunc  *DelayFunc

	noConfigError = errors.New("user has no config")
)

func init() {
	sendMsgNotificationsToUsersFunc = NewDelayFunc("game-sendMsgNotificationsToUsers", sendMsgNotificationsToUsers)
	sendMsgNotificationsToFCMFunc = NewDelayFunc("game-sendMsgNotificationsToFCM", sendMsgNotificationsToFCM)
	sendMsgNotificationsToMailFunc = NewDelayFunc("game-sendMsgNotificationsToMail", sendMsgNotificationsToMail)
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
	data         map[string]interface{}
}

func getMsgNotificationContext(ctx context.Context, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, userId string) (*msgNotificationContext, error) {
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
	res.member, isMember = res.game.GetMember(userId)
	if !isMember {
		log.Errorf(ctx, "%q is not a member of %v, wtf? Exiting.", userId, res.game)
		return nil, noConfigError
	}

	res.data = map[string]interface{}{
		"diplicityGame":    res.game,
		"diplicityChannel": res.channel,
		"diplicityMessage": res.message,
		"diplicityUser":    res.user,
	}

	return res, nil
}

func sendMsgNotificationsToMail(ctx context.Context, reqURL string, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, userId string) error {
	log.Infof(ctx, "sendMsgNotificationsToMail(..., %q, %v, %+v, %v, %q)", reqURL, gameID, channelMembers, messageID, userId)

	sendGridConf, err := GetSendGrid(ctx)
	if err != nil {
		log.Errorf(ctx, "Unable to load sendgrid API key: %v; upload one or hope datastore gets fixed", err)
		return err
	}

	msgContext, err := getMsgNotificationContext(ctx, gameID, channelMembers, messageID, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get msg notification context: %v; fix getMsgNotificationContext or hope datastore gets fixed", err)
		return err
	}

	if !msgContext.userConfig.MailMessageConfig.Enabled {
		log.Infof(ctx, "%q hasn't enabled mail notifications for mail, will skip sending notification", userId)
		return nil
	}

	unsubscribeURL, err := router.Get(auth.UnsubscribeRoute).URL("user_id", userId)
	if err != nil {
		log.Errorf(ctx, "Unable to create unsubscribe URL for %q: %v; fix gorilla muxer?", userId, err)
		return err
	}

	reqU, err := url.Parse(reqURL)
	if err != nil {
		log.Errorf(ctx, "Unable to parse reqURL %q: %v; unable to recover, exiting", reqURL, err)
		return nil
	}
	unsubscribeURL.Host = reqU.Host
	unsubscribeURL.Scheme = reqU.Scheme

	unsubToken, err := auth.EncodeString(ctx, userId)
	if err != nil {
		log.Errorf(ctx, "Unable to create auth token for unsubscribe URL: %v; fix EncodeString or hope datastore gets fixed", err)
		return err
	}

	qp := unsubscribeURL.Query()
	qp.Set("t", unsubToken)
	unsubscribeURL.RawQuery = qp.Encode()

	msg := sendgrid.NewMail()
	msg.SetText(fmt.Sprintf("%s\n\nVisit %s to stop receiving email like this.", msgContext.message.Body, unsubscribeURL.String()))
	msg.SetSubject(fmt.Sprintf("%s: Message from %s", msgContext.message.ChannelMembers.String(), msgContext.message.Sender))
	msg.AddHeader("List-Unsubscribe", fmt.Sprintf("<%s>", unsubscribeURL.String()))

	msgContext.userConfig.MailMessageConfig.Customize(ctx, msg, msgContext.data)

	recipEmail, err := mail.ParseAddress(msgContext.user.Email)
	if err != nil {
		log.Errorf(ctx, "Unable to parse email address of %v: %v; unable to recover, exiting", PP(msgContext.user), err)
		return nil
	}
	msg.AddRecipient(recipEmail)
	msg.AddToName(channelMembers.String())

	fromToken, err := auth.EncodeString(ctx, fmt.Sprintf("%s,%s", userId, messageID.Encode()))
	if err != nil {
		log.Errorf(ctx, "Unable to create auth token for reply address: %v; fix EncodeString or hope datastore gets fixed", err)
		return err
	}

	fromAddress := fmt.Sprintf("replies+%s@diplicity-engine.appspotmail.com", fromToken)
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

	log.Infof(ctx, "sendMsgNotificationsToMail(..., %q, %v, %+v, %v, %q) *** SUCCESS ***", reqURL, gameID, channelMembers, messageID, userId)

	return nil
}

func sendMsgNotificationsToFCM(ctx context.Context, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, userId string, finishedTokens map[string]struct{}) error {
	log.Infof(ctx, "sendMsgNotificationsToFCM(..., %v, %+v, %v, %q, %+v)", gameID, channelMembers, messageID, userId, finishedTokens)

	msgContext, err := getMsgNotificationContext(ctx, gameID, channelMembers, messageID, userId)
	if err == noConfigError {
		log.Infof(ctx, "%q has no configuration, will skip sending notification", userId)
		return nil
	} else if err != nil {
		log.Errorf(ctx, "Unable to get msg notification context: %v; fix getMsgNotificationContext or hope datastore gets fixed", err)
		return err
	}

	dataPayload, err := NewFCMData(msgContext.data)
	if err != nil {
		log.Errorf(ctx, "Unable to encode FCM data payload %v: %v; fix NewFCMData", msgContext.data, err)
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
		notificationPayload := &fcm.NotificationPayload{
			Title:       fmt.Sprintf("%s: Message from %s", msgContext.message.ChannelMembers.String(), msgContext.message.Sender),
			Body:        msgContext.message.Body,
			Tag:         "diplicity-engine-new-message",
			ClickAction: fmt.Sprintf("https://diplicity-engine.appspot.com/Game/%s/Channel/%s/Messages", gameID.Encode(), channelMembers.String()),
		}

		fcmToken.MessageConfig.Customize(ctx, notificationPayload, msgContext.data)

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
				if err := sendMsgNotificationsToFCMFunc.EnqueueIn(ctx, 0, gameID, channelMembers, messageID, userId, finishedTokens); err != nil {
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
		return nil
	}

	log.Infof(ctx, "sendMsgNotificationsToFCM(..., %v, %+v, %v, %q, %+v) *** SUCCESS ***", gameID, channelMembers, messageID, userId, finishedTokens)

	return nil
}

func sendMsgNotificationsToUsers(ctx context.Context, reqURL string, gameID *datastore.Key, channelMembers Nations, messageID *datastore.Key, uids []string) error {
	log.Infof(ctx, "sendMsgNotificationsToUsers(..., %q, %v, %+v, %v, %+v)", reqURL, gameID, channelMembers, messageID, uids)

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := sendMsgNotificationsToFCMFunc.EnqueueIn(ctx, 0, gameID, channelMembers, messageID, uids[0], map[string]struct{}{}); err != nil {
			log.Errorf(ctx, "Unable to enqueue sending FCM to %q: %v; hope datastore gets fixed", uids[0], err)
			return err
		}
		if err := sendMsgNotificationsToMailFunc.EnqueueIn(ctx, 0, reqURL, gameID, channelMembers, messageID, uids[0]); err != nil {
			log.Errorf(ctx, "Unable to enqueue sending mail to %q: %v; hope datastore gets fixed", uids[0], err)
			return err
		}
		if len(uids) > 1 {
			if err := sendMsgNotificationsToUsersFunc.EnqueueIn(ctx, 0, gameID, channelMembers, messageID, uids[1:]); err != nil {
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

	log.Infof(ctx, "sendMsgNotificationsToUsers(..., %q, %v, %+v, %v, %+v) *** SUCCESS ***", reqURL, gameID, channelMembers, messageID, uids)

	return nil
}

type Nations []dip.Nation

func (n *Nations) FromString(s string) {
	parts := strings.Split(s, ",")
	*n = make(Nations, len(parts))
	for i := range parts {
		(*n)[i] = dip.Nation(parts[i])
	}
}

func (n Nations) Includes(m dip.Nation) bool {
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

func (c Channels) Item(r Request, gameID *datastore.Key) *Item {
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
			"Channels tell you how many messages they have, and if you provide the `since` query parameter they will even tell you how many new messages they have received since then.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListChannelsRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	channelsItem.AddLink(r.NewLink(MessageResource.Link("message", Create, []string{"game_id", gameID.Encode()})))
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
	return datastore.NewKey(ctx, channelKind, fmt.Sprintf("%d:%s", gameID.IntID(), members.String()), 0, nil), nil
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

var MessageResource = &Resource{
	Create:     createMessage,
	CreatePath: "/Game/{game_id}/Messages",
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
	}))
	return messagesItem
}

type Message struct {
	ID             *datastore.Key `datastore:"-"`
	GameID         *datastore.Key
	ChannelMembers Nations `methods:"POST"`
	Sender         dip.Nation
	Body           string `methods:"POST" datastore:",noindex"`
	CreatedAt      time.Time
}

func (m *Message) NotifyRecipients(ctx context.Context, reqURL string, channel *Channel, game *Game) error {
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
	unmutedMembers := []dip.Nation{}
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
					if states[index].Nation != m.Sender && !states[index].HasMuted(m.Sender) {
						unmutedMembers = append(unmutedMembers, states[index].Nation)
					}
				} else if serr != datastore.ErrNoSuchEntity {
					return err
				} else {
					unmutedMembers = append(unmutedMembers, states[index].Nation)
				}
			}
		} else if err != datastore.ErrNoSuchEntity {
			return err
		}
	}

	// Use this unmuted list to build a set of user IDs to send the message to.
	sort.Sort(m.ChannelMembers)
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
		return nil
	}

	return sendMsgNotificationsToUsersFunc.EnqueueIn(ctx, 0, reqURL, m.GameID, m.ChannelMembers, m.ID, memberIds)
}

func (m *Message) Item(r Request) *Item {
	return NewItem(m).SetName(string(m.Sender))
}

func createMessage(w ResponseWriter, r Request) (*Message, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
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

	member, found := game.GetMember(user.Id)
	if !found {
		return nil, HTTPErr{"can only create messages in member games", 404}
	}

	message := &Message{}
	if err := Copy(message, r, "POST"); err != nil {
		return nil, err
	}
	message.GameID = gameID
	message.Sender = member.Nation
	message.CreatedAt = time.Now()
	sort.Sort(message.ChannelMembers)

	if !message.ChannelMembers.Includes(member.Nation) {
		return nil, HTTPErr{"can only send messages to member channels", 403}
	}

	for _, channelMember := range message.ChannelMembers {
		if !Nations(variants.Variants[game.Variant].Nations).Includes(channelMember) {
			return nil, HTTPErr{"unknown channel member", 400}
		}
	}

	channelID, err := ChannelID(ctx, gameID, message.ChannelMembers)
	if err != nil {
		return nil, err
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		channel := &Channel{}
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, channelID}, []interface{}{game, channel}); err != nil {
			if merr, ok := err.(appengine.MultiError); ok {
				if merr[0] == nil && merr[1] == datastore.ErrNoSuchEntity {
					channel.GameID = gameID
					channel.Members = message.ChannelMembers
					channel.NMessages = 0
				} else {
					return merr
				}
			} else {
				return err
			}
		}
		game.ID = gameID
		if message.ID, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, messageKind, channelID), message); err != nil {
			return err
		}
		channel.NMessages += 1
		if _, err = datastore.Put(ctx, channelID, channel); err != nil {
			return err
		}

		completeURL := r.Req().URL
		completeURL.Host = r.Req().Host
		if r.Req().TLS == nil {
			completeURL.Scheme = "http"
		} else {
			completeURL.Scheme = "https"
		}
		return message.NotifyRecipients(ctx, completeURL.String(), channel, game)
	}, &datastore.TransactionOptions{XG: true}); err != nil {
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
		return HTTPErr{"unauthorized", 401}
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

	var nation dip.Nation
	mutedNats := map[dip.Nation]struct{}{}
	if member, found := game.GetMember(user.Id); found {
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

	if !channelMembers.Includes(nation) && !isPublic(game.Variant, channelMembers) {
		return HTTPErr{"can only list member channels", 403}
	}

	channelID, err := ChannelID(ctx, gameID, channelMembers)
	if err != nil {
		return err
	}

	messages := Messages{}
	q := datastore.NewQuery(messageKind).Ancestor(channelID)
	if since != nil {
		q = q.Filter("CreatedAt>", *since)
	}
	if _, err := q.Order("-CreatedAt").GetAll(ctx, &messages); err != nil {
		return err
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

func listChannels(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

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

	var nation dip.Nation

	if member, found := game.GetMember(user.Id); found {
		nation = member.Nation
	}

	channels := Channels{}
	if nation == "" {
		channelID, err := ChannelID(ctx, gameID, publicChannel(game.Variant))
		if err != nil {
			return err
		}
		channel := &Channel{}
		if err := datastore.Get(ctx, channelID, channel); err == nil {
			channels = append(channels, *channel)
		} else if err != datastore.ErrNoSuchEntity {
			return err
		}
	} else {
		_, err = datastore.NewQuery(channelKind).Filter("GameID=", gameID).Filter("Members=", nation).GetAll(ctx, &channels)
		if err != nil {
			return err
		}
	}

	if since != nil {
		results := make(chan error)
		for i := range channels {
			go func(c *Channel) {
				results <- c.CountSince(ctx, *since)
			}(&channels[i])
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
	} else {
		for i := range channels {
			channels[i].NMessagesSince.NMessages = channels[i].NMessages
		}
	}

	w.SetContent(channels.Item(r, gameID))
	return nil
}
