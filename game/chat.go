package game

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	messageKind = "Message"
	channelKind = "Channel"
)

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
	channelsItem := NewItem(channelItems).SetName("channels").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListChannelsRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	channelsItem.AddLink(r.NewLink(MessageResource.Link("message", Create, []string{"game_id", gameID.Encode()})))
	return channelsItem
}

type Channel struct {
	GameID    *datastore.Key
	Members   Nations
	NMessages int
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
	messagesItem := NewItem(messageItems).SetName("messages").AddLink(r.NewLink(Link{
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
	Body           string `methods:"POST"`
	CreatedAt      time.Time
}

func (m *Message) Item(r Request) *Item {
	return NewItem(m).SetName(string(m.Sender))
}

func createMessage(w ResponseWriter, r Request) (*Message, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	member := &Member{}
	err = datastore.GetMulti(ctx, []*datastore.Key{gameID, memberID}, []interface{}{game, member})
	if err != nil {
		return nil, err
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
		http.Error(w, "can only send messages to member channels", 403)
		return nil, nil
	}

	for _, channelMember := range message.ChannelMembers {
		if !Nations(variants.Variants[game.Variant].Nations).Includes(channelMember) {
			http.Error(w, "unknown channel member", 400)
			return nil, nil
		}
	}

	channelID, err := ChannelID(ctx, gameID, message.ChannelMembers)
	if err != nil {
		return nil, err
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		channel := &Channel{}
		if err := datastore.Get(ctx, channelID, channel); err == datastore.ErrNoSuchEntity {
			channel.GameID = gameID
			channel.Members = message.ChannelMembers
			channel.NMessages = 0
		}
		if message.ID, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, messageKind, channelID), message); err != nil {
			return err
		}
		channel.NMessages += 1
		_, err = datastore.Put(ctx, channelID, channel)
		return err
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return message, nil
}

func listMessages(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	channelMembers := Nations{}
	channelMembers.FromString(r.Vars()["channel_members"])

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return err
	}

	member := &Member{}
	if err := datastore.Get(ctx, memberID, member); err != nil {
		return err
	}

	if !channelMembers.Includes(member.Nation) {
		http.Error(w, "can only list member channels", 403)
		return nil
	}

	channelID, err := ChannelID(ctx, gameID, channelMembers)
	if err != nil {
		return err
	}

	messages := Messages{}
	if _, err := datastore.NewQuery(messageKind).Ancestor(channelID).GetAll(ctx, &messages); err != nil {
		return err
	}

	w.SetContent(messages.Item(r, gameID, channelMembers))
	return nil
}

func listChannels(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return err
	}

	var nation dip.Nation

	game := &Game{}
	member := &Member{}
	err = datastore.GetMulti(ctx, []*datastore.Key{gameID, memberID}, []interface{}{game, member})
	if err == nil {
		nation = member.Nation
	} else if merr, ok := err.(appengine.MultiError); ok {
		if merr[0] != nil {
			return merr[0]
		}
	} else {
		return err
	}

	channels := Channels{}
	if nation == "" {
		channelID, err := ChannelID(ctx, gameID, variants.Variants[game.Variant].Nations)
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

	w.SetContent(channels.Item(r, gameID))
	return nil
}
