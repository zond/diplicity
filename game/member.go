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

var MemberResource = &Resource{
	Create:     createMember,
	Delete:     deleteMember,
	CreatePath: "/Game/{game_id}/Member",
	FullPath:   "/Game/{game_id}/Member/{user_id}",
}

type Member struct {
	User   auth.User
	Nation dip.Nation
}

func (m *Member) Item(r Request) *Item {
	return NewItem(m).SetName(m.User.Name)
}

func (m *Member) Redact() {
	m.User.Email = ""
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

	var member *Member
	game := &Game{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID
		isMember := false
		member, isMember = game.GetMember(user.Id)
		if !isMember {
			return fmt.Errorf("can only leave member games")
		}
		if !game.Leavable() {
			return fmt.Errorf("game not leavable")
		}
		newMembers := []Member{}
		for _, oldMember := range game.Members {
			if oldMember.User.Id != member.User.Id {
				newMembers = append(newMembers, oldMember)
			}
		}
		if len(newMembers) == 0 && !game.Started {
			return datastore.Delete(ctx, gameID)
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
	game := &Game{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID
		isMember := false
		member, isMember = game.GetMember(user.Id)
		if isMember {
			return fmt.Errorf("user already member")
		}
		if !game.Joinable() {
			return fmt.Errorf("game not joinable")
		}
		member = &Member{
			User: *user,
		}
		game.Members = append(game.Members, *member)
		if len(game.Members) == len(variants.Variants[game.Variant].Nations) {
			if err := game.Start(ctx); err != nil {
				return err
			}
		}
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, err
	}

	return member, nil
}
