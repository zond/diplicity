package game

import (
	"fmt"
	"net/http"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

var MemberResource = &Resource{
	Create:     createMember,
	Load:       loadMember,
	CreatePath: "/Game/{game_id}/Member",
	FullPath:   "/Game/{game_id}/Member/{user_id}",
}

type Member struct {
	GameData GameData
	User     auth.User
}

func (m *Member) Item(r Request) *Item {
	return NewItem(m).SetName(m.User.Name).AddLink(r.NewLink(MemberResource.Link("self", Load, []string{"game_id", m.GameData.ID.Encode(), "user_id", m.User.Id})))
}

func MemberID(ctx context.Context, gameID *datastore.Key, userID string) (*datastore.Key, error) {
	if gameID == nil || userID == "" {
		return nil, fmt.Errorf("members must have games and users")
	}
	return datastore.NewKey(ctx, memberKind, userID, 0, gameID), nil
}

func (m *Member) ID(ctx context.Context) (*datastore.Key, error) {
	return MemberID(ctx, m.GameData.ID, m.User.Id)
}

func (m *Member) Save(ctx context.Context) error {
	key, err := m.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, key, m)
	return err
}

func loadMember(w ResponseWriter, r Request) (*Member, error) {
	ctx := appengine.NewContext(r.Req())

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, r.Vars()["user_id"])
	if err != nil {
		return nil, err
	}

	member := &Member{}
	if err := datastore.Get(ctx, memberID, member); err != nil {
		if err == datastore.ErrNoSuchEntity {
			http.Error(w, "not found", 404)
			return nil, nil
		}
		return nil, err
	}

	return member, nil
}

func createMember(w ResponseWriter, r Request) (*Member, error) {
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

	var member *Member
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		if len(game.Members) >= len(variants.Variants[game.Variant].Nations) {
			return fmt.Errorf("too many members")
		}
		if game.State != OpenState {
			return fmt.Errorf("game not open")
		}
		for _, member := range game.Members {
			if member.User.Id == user.Id {
				return fmt.Errorf("user already member")
			}
		}
		member = &Member{
			User:     *user,
			GameData: game.GameData,
		}
		if err := member.Save(ctx); err != nil {
			return err
		}
		game.Members = append(game.Members, *member)
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return member, nil
}
