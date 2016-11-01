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
	return NewItem(c).SetName(c.Members.String())
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
	sort.Sort(message.ChannelMembers)

	selfMember := false
	for _, channelMember := range message.ChannelMembers {
		if channelMember == member.Nation {
			selfMember = true
		}
		isOK := false
		for _, okNation := range variants.Variants[game.Variant].Nations {
			if channelMember == okNation {
				isOK = true
				break
			}
		}
		if !isOK {
			return nil, fmt.Errorf("unknown channel members")
		}
	}
	if !selfMember {
		return nil, fmt.Errorf("can only send messages to member channels")
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
