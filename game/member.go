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
	dip "github.com/zond/godip/common"
)

const (
	memberKind = "Member"
)

var MemberResource = &Resource{
	Create:     createMember,
	Delete:     deleteMember,
	CreatePath: "/Game/{game_id}/Member",
	FullPath:   "/Game/{game_id}/Member/{user_id}",
}

type Member struct {
	GameData GameData
	User     auth.User
	Nation   dip.Nation
}

func (m *Member) Item(r Request) *Item {
	return NewItem(m).SetName(m.User.Name)
}

func (m *Member) Redact() {
	m.User.Email = ""
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

func deleteMember(w ResponseWriter, r Request) (*Member, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	if user.Id != r.Vars()["user_id"] {
		return nil, fmt.Errorf("can only delete yourself")
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, r.Vars()["user_id"])
	if err != nil {
		return nil, err
	}

	member := &Member{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, memberID, member); err != nil {
			return err
		}
		if !member.GameData.Leavable() {
			return fmt.Errorf("game not leavable")
		}
		if err := datastore.Delete(ctx, memberID); err != nil {
			return err
		}
		game := &Game{}
		if err := datastore.Get(ctx, memberID.Parent(), game); err != nil {
			return err
		}
		game.ID = memberID.Parent()
		newMembers := []Member{}
		for _, member := range game.Members {
			if member.User.Id != member.User.Id {
				newMembers = append(newMembers, member)
			}
		}
		if len(newMembers) == 0 {
			return datastore.Delete(ctx, memberID.Parent())
		}
		game.Members = newMembers
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
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
		game.ID = gameID
		if !game.Joinable() {
			return fmt.Errorf("game not joinable")
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
		game.Members = append(game.Members, *member)
		if len(game.Members) == len(variants.Variants[game.Variant].Nations) {
			if err := game.Start(ctx); err != nil {
				return err
			}
		}
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return member, nil
}
