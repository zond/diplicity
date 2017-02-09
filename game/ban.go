package game

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	banKind = "Ban"
)

var BanResource *Resource

func init() {
	BanResource = &Resource{
		Load:       loadBan,
		Create:     createBan,
		Delete:     deleteBan,
		CreatePath: "/User/{user_id}/Ban",
		FullPath:   "/User/{user_id}/Ban/{banned_id}",
		Listers: []Lister{
			{
				Path:    "/User/{user_id}/Bans",
				Route:   ListBansRoute,
				Handler: listBans,
			},
		},
	}
}

type Bans []Ban

func (b Bans) Item(r Request, userId string) *Item {
	banItems := make(List, len(b))
	for i := range b {
		banItems[i] = b[i].Item(r)
	}
	bansItem := NewItem(banItems).SetName("bans").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListBansRoute,
		RouteParams: []string{"user_id", userId},
	})).AddLink(r.NewLink(BanResource.Link("create", Create, []string{"user_id", userId}))).SetDesc([][]string{
		[]string{
			"Bans",
			"Bans prevent players from seeing or joining each others games. If you never want to risk playing with a given user again, create a ban with both your IDs.",
		},
	})
	return bansItem
}

type Ban struct {
	UserIds  []string `methods:"POST"`
	OwnerIds []string
	Users    []auth.User
}

func (b *Ban) OwnedBy(uid string) bool {
	for _, ownerId := range b.OwnerIds {
		if ownerId == uid {
			return true
		}
	}
	return false
}

func (b *Ban) Item(r Request) *Item {
	user := r.Values()["user"].(*auth.User)

	banItem := NewItem(b)
	if b.OwnedBy(user.Id) {
		bannedId := ""
		for _, userId := range b.UserIds {
			if userId != user.Id {
				bannedId = userId
				break
			}
		}
		banItem.AddLink(r.NewLink(BanResource.Link("unsign", Delete, []string{"user_id", user.Id, "banned_id", bannedId})))
		banItem.AddLink(r.NewLink(BanResource.Link("self", Load, []string{"user_id", user.Id, "banned_id", bannedId})))
	}
	return banItem
}

func BanID(ctx context.Context, userIds []string) (*datastore.Key, error) {
	if len(userIds) != 2 {
		return nil, fmt.Errorf("bans must have exactly 2 user ids")
	}
	sort.Sort(sort.StringSlice(userIds))
	return datastore.NewKey(ctx, banKind, strings.Join(userIds, ","), 0, nil), nil
}

func (b *Ban) ID(ctx context.Context) (*datastore.Key, error) {
	return BanID(ctx, b.UserIds)
}

func (b *Ban) Save(ctx context.Context) error {
	id, err := b.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, id, b)
	return err
}

func loadBan(w ResponseWriter, r Request) (*Ban, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	if r.Vars()["user_id"] != user.Id {
		return nil, HTTPErr{"can only delete owned bans", 403}
	}

	banID, err := BanID(ctx, []string{user.Id, r.Vars()["banned_id"]})
	if err != nil {
		return nil, err
	}

	ban := &Ban{}
	if err = datastore.Get(ctx, banID, ban); err != nil {
		return nil, err
	}

	return ban, nil
}

func deleteBan(w ResponseWriter, r Request) (*Ban, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	if r.Vars()["user_id"] != user.Id {
		return nil, HTTPErr{"can only delete owned bans", 403}
	}

	banID, err := BanID(ctx, []string{user.Id, r.Vars()["banned_id"]})
	if err != nil {
		return nil, err
	}

	ban := &Ban{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, banID, ban); err != nil {
			return err
		}

		wasOwner := false
		newOwners := []string{}
		for _, ownerId := range ban.OwnerIds {
			if ownerId == user.Id {
				wasOwner = true
			} else {
				newOwners = append(newOwners, ownerId)
			}
		}
		if !wasOwner {
			return HTTPErr{"can only delete owned bans", 403}
		}
		ban.OwnerIds = newOwners

		if err := UpdateUserStatsASAP(ctx, ban.UserIds); err != nil {
			return err
		}

		if len(ban.OwnerIds) == 0 {
			return datastore.Delete(ctx, banID)
		}
		return ban.Save(ctx)
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, err
	}

	return ban, nil
}

func listBans(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
	}

	if r.Vars()["user_id"] != user.Id {
		return HTTPErr{"can only list bans containing you", 403}
	}

	bans := Bans{}

	if _, err := datastore.NewQuery(banKind).Filter("UserIds=", user.Id).GetAll(ctx, &bans); err != nil {
		return err
	}

	w.SetContent(bans.Item(r, user.Id))

	return nil
}

func createBan(w ResponseWriter, r Request) (*Ban, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	if r.Vars()["user_id"] != user.Id {
		return nil, HTTPErr{"can only create your own bans", 403}
	}

	ban := &Ban{}
	if err := Copy(ban, r, "POST"); err != nil {
		return nil, err
	}
	wasInBan := false
	for _, userId := range ban.UserIds {
		if userId == user.Id {
			wasInBan = true
		}
	}
	if !wasInBan {
		return nil, HTTPErr{"can only create bans containing yourself", 400}
	}
	userIds := ban.UserIds

	banID, err := ban.ID(ctx)
	if err != nil {
		return nil, err
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, banID, ban); err == datastore.ErrNoSuchEntity {
			ban.UserIds = userIds
			ban.OwnerIds = []string{user.Id}
		} else if err != nil {
			return err
		}

		wasOwner := false
		for _, ownerId := range ban.OwnerIds {
			if ownerId == user.Id {
				wasOwner = true
				break
			}
		}
		if !wasOwner {
			ban.OwnerIds = append(ban.OwnerIds, user.Id)
		}

		if len(ban.OwnerIds) < 1 || len(ban.OwnerIds) > 2 {
			return fmt.Errorf("bans must have 1 or 2 owner ids")
		}

		if err := UpdateUserStatsASAP(ctx, ban.UserIds); err != nil {
			return err
		}

		ban.Users = make([]auth.User, 2)
		userIDs := make([]*datastore.Key, 2)
		for i, id := range userIds {
			userIDs[i] = auth.UserID(ctx, id)
		}
		if err := datastore.GetMulti(ctx, userIDs, ban.Users); err != nil {
			return err
		}

		return ban.Save(ctx)
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, err
	}

	return ban, nil
}
