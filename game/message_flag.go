package game

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	flaggedMessagesKind = "FlaggedMessages"
)

var (
	MessageFlagResource     *Resource
	FlaggedMessagesResource *Resource
)

func init() {
	MessageFlagResource = &Resource{
		Create:     createMessageFlag,
		CreatePath: "/Game/{game_id}/Channel/{channel_members}/MessageFlag",
	}
	FlaggedMessagesResource = &Resource{
		Type: reflect.TypeOf(FlaggedMessages{}),
		Listers: []Lister{
			{
				Path:    "/FlaggedMessages",
				Route:   ListFlaggedMessagesRoute,
				Handler: listFlaggedMessages,
			},
		},
	}
}

type MessageFlag struct {
	GameID         *datastore.Key
	ChannelMembers Nations
	From           time.Time `methods:"POST"`
	To             time.Time `methods:"POST"`
}

func (m *MessageFlag) Item(r Request) *Item {
	return NewItem(m).SetName("message-flag")
}

type FlaggedMessage struct {
	GameID         *datastore.Key
	ChannelMembers string
	Sender         godip.Nation
	Body           string
	CreatedAt      time.Time
	AuthorId       string
}

type FlaggedMessages struct {
	GameID    *datastore.Key
	UserId    string
	Messages  []FlaggedMessage
	CreatedAt time.Time
}

func (f *FlaggedMessages) Item(r Request) *Item {
	return NewItem(f).SetName("flagged-messages")
}

type FlaggedMessagess []FlaggedMessages

func (f FlaggedMessagess) Item(r Request, curs *datastore.Cursor, limit int, userId string) *Item {
	fmItems := make(List, len(f))
	for i := range f {
		fmItems[i] = f[i].Item(r)
	}
	fmsItem := NewItem(fmItems).SetName("flagged-messages").
		SetDesc([][]string{
			[]string{
				"Flagged messages",
				"This lists the messages flagged by users. The intention is to make it easier to browse examples of what others find to be bad behaviour, and ban authors of messages you don't want to see in your own games.",
				"The ban link here is exactly the same as the one in the regular 'bans' view. To make it simpler to ban from the auto generated UI, and to make it easier to understand the intention of this list, it's provided here as well.",
			},
		}).
		AddLink(r.NewLink(BanResource.Link("create-ban", Create, []string{"user_id", userId})))
	if curs != nil {
		fmsItem.AddLink(r.NewLink(Link{
			Rel:   "self",
			Route: ListFlaggedMessagesRoute,
			QueryParams: url.Values{
				"cursor": []string{curs.String()},
				"limit":  []string{fmt.Sprint(limit)},
			},
		}))
	}
	return fmsItem
}

func createMessageFlag(w ResponseWriter, r Request) (*MessageFlag, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	flaggedMessagesID, err := FlaggedMessagesID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	existingFlag := &FlaggedMessages{}
	err = datastore.GetMulti(ctx, []*datastore.Key{gameID, flaggedMessagesID}, []interface{}{game, existingFlag})
	if err == nil {
		return nil, HTTPErr{"can only flag messages once per game", 403}
	}
	merr, ok := err.(appengine.MultiError)
	if !ok {
		return nil, err
	}
	if merr[0] != nil {
		return nil, err
	}
	if merr[1] != datastore.ErrNoSuchEntity {
		return nil, err
	}
	game.ID = gameID

	userByNation := map[godip.Nation]auth.User{}
	for _, member := range game.Members {
		userByNation[member.Nation] = member.User
	}

	_, isMember := game.GetMember(user.Id)
	if !isMember {
		return nil, HTTPErr{"can only flag messages in member games", 403}
	}

	channelMembers := Nations{}
	channelMembers.FromString(r.Vars()["channel_members"])

	channelID, err := ChannelID(ctx, gameID, channelMembers)
	if err != nil {
		return nil, err
	}

	messageFlag := &MessageFlag{}
	if err := Copy(messageFlag, r, "POST"); err != nil {
		return nil, err
	}

	messages := Messages{}
	if _, err := datastore.NewQuery(messageKind).Ancestor(channelID).Filter("CreatedAt>=", messageFlag.From).Filter("CreatedAt<=", messageFlag.To).GetAll(ctx, &messages); err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return nil, HTTPErr{"timestamps matched no messages", 400}
	}

	flaggedMessagess := make([]FlaggedMessage, len(messages))
	for i, message := range messages {
		flaggedMessagess[i] = FlaggedMessage{
			GameID:         gameID,
			ChannelMembers: message.ChannelMembers.String(),
			Sender:         message.Sender,
			Body:           message.Body,
			CreatedAt:      message.CreatedAt,
			AuthorId:       userByNation[message.Sender].Id,
		}
	}

	flaggedMessages := &FlaggedMessages{
		GameID:    gameID,
		UserId:    user.Id,
		Messages:  flaggedMessagess,
		CreatedAt: time.Now(),
	}

	if _, err := datastore.Put(ctx, flaggedMessagesID, flaggedMessages); err != nil {
		return nil, err
	}

	return messageFlag, nil
}

func FlaggedMessagesID(ctx context.Context, gameID *datastore.Key, userId string) (*datastore.Key, error) {
	if gameID == nil || gameID.IntID() == 0 || userId == "" {
		return nil, fmt.Errorf("flagged messages must have non zero game IDs and users")
	}
	return datastore.NewKey(ctx, flaggedMessagesKind, fmt.Sprintf("%d,%s", gameID.IntID(), userId), 0, nil), nil
}

func listFlaggedMessages(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
	}

	limit := maxLimit
	if limitS := r.Req().URL.Query().Get("limit"); limitS != "" {
		if i, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64); err == nil {
			limit = int(i)
		}
	}

	query := datastore.NewQuery(flaggedMessagesKind).Order("-CreatedAt")

	var iter *datastore.Iterator

	cursor := r.Req().URL.Query().Get("cursor")
	if cursor == "" {
		iter = query.Run(ctx)
	} else {
		decoded, err := datastore.DecodeCursor(cursor)
		if err != nil {
			return err
		}
		iter = query.Start(decoded).Run(ctx)
	}

	flaggedMessagess := FlaggedMessagess{}
	var err error
	for len(flaggedMessagess) < limit && err == nil {
		f := FlaggedMessages{}
		_, err = iter.Next(&f)
		if err == nil {
			flaggedMessagess = append(flaggedMessagess, f)
		}
	}

	var cursP *datastore.Cursor
	if err == nil {
		curs, err := iter.Cursor()
		if err != nil {
			return err
		}
		cursP = &curs
	}

	w.SetContent(flaggedMessagess.Item(r, cursP, limit, user.Id))

	return nil
}
