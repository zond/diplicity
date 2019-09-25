package game

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

var MemberResource = &Resource{
	Create:     createMember,
	Delete:     deleteMember,
	Update:     updateMember,
	CreatePath: "/Game/{game_id}/Member",
	FullPath:   "/Game/{game_id}/Member/{user_id}",
}

type Member struct {
	User              auth.User
	Nation            godip.Nation
	GameAlias         string `methods:"POST,PUT" datastore:",noindex"`
	NationPreferences string `methods:"POST,PUT" datastore:",noindex"`
	NewestPhaseState  PhaseState
	UnreadMessages    int
}

type Members []Member

func (m Members) Len() int {
	return len(m)
}

func (m Members) Each(f func(int, Preferer)) {
	for idx, member := range m {
		f(idx, member)
	}
}

func (m Member) Preferences() godip.Nations {
	result := godip.Nations{}
	for _, preference := range strings.Split(m.NationPreferences, ",") {
		result = append(result, godip.Nation(strings.TrimSpace(preference)))
	}
	return result
}

func (m *Member) Item(r Request) *Item {
	return NewItem(m).SetName(m.User.Name)
}

func (m *Member) Redact(viewer *auth.User, isMember bool, started bool) {
	if !isMember {
		m.User.Email = ""
	}
	if viewer.Id != m.User.Id {
		m.GameAlias = ""
		m.NewestPhaseState = PhaseState{}
		m.UnreadMessages = 0
	}
	if !started && viewer.Id != m.User.Id {
		m.NationPreferences = ""
	}
}

func updateMember(w ResponseWriter, r Request) (*Member, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	if user.Id != r.Vars()["user_id"] {
		return nil, HTTPErr{"can only delete yourself", http.StatusForbidden}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	bodyBytes, err := ioutil.ReadAll(r.Req().Body)
	if err != nil {
		return nil, err
	}
	var member *Member
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return HTTPErr{"non existing game", http.StatusPreconditionFailed}
		}
		game.ID = gameID
		isMember := false
		member, isMember = game.GetMemberByUserId(user.Id)
		if !isMember {
			return HTTPErr{"non existing member", http.StatusNotFound}
		}
		previousPreferences := member.NationPreferences
		if err := CopyBytes(member, r, bodyBytes, "PUT"); err != nil {
			return err
		}
		if game.Started {
			if previousPreferences != member.NationPreferences {
				return HTTPErr{"cannot change nation preferences after game started", http.StatusPreconditionFailed}
			}
		}
		updated := false
		for i := range game.Members {
			if game.Members[i].Nation == member.Nation && game.Members[i].User.Id == member.User.Id {
				game.Members[i] = *member
				updated = true
				break
			}
		}
		if !updated {
			return fmt.Errorf("Sanity check failed, didn't succeed in finding the right member to update? game: %v, member: %v", spew.Sdump(game), spew.Sdump(member))
		}
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return member, nil
}

func deleteMemberHelper(ctx context.Context, gameID *datastore.Key, userId string, idempotent bool) (*Member, error) {
	var member *Member
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return HTTPErr{"non existing game", http.StatusPreconditionFailed}
		}
		game.ID = gameID
		isMember := false
		member, isMember = game.GetMemberByUserId(userId)
		if !isMember {
			if idempotent {
				return nil
			}
			return HTTPErr{"can only leave member games", http.StatusNotFound}
		}
		if !game.Leavable() {
			return HTTPErr{"game not leavable", http.StatusPreconditionFailed}
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

func deleteMember(w ResponseWriter, r Request) (*Member, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	if user.Id != r.Vars()["user_id"] {
		return nil, HTTPErr{"can only delete yourself", http.StatusForbidden}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	return deleteMemberHelper(ctx, gameID, user.Id, false)
}

func createMemberHelper(
	ctx context.Context,
	r Request,
	gameID *datastore.Key,
	user *auth.User,
	member *Member,
) (*Game, *Member, error) {
	var game *Game
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game = &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return HTTPErr{"non existing game", http.StatusPreconditionFailed}
		}
		game.ID = gameID
		isMember := false
		_, isMember = game.GetMemberByUserId(user.Id)
		if isMember {
			return HTTPErr{"user already member", http.StatusBadRequest}
		}
		if !game.Joinable() {
			return HTTPErr{"game not joinable", http.StatusPreconditionFailed}
		}
		member.User = *user
		member.NewestPhaseState = PhaseState{
			GameID: gameID,
		}
		game.Members = append(game.Members, *member)
		if err := game.Save(ctx); err != nil {
			return err
		}
		if len(game.Members) == len(variants.Variants[game.Variant].Nations) {
			scheme := "http"
			if r.Req().TLS != nil {
				scheme = "https"
			}
			if err := asyncStartGameFunc.EnqueueIn(ctx, 0, game.ID, r.Req().Host, scheme); err != nil {
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, nil, err
	}

	return game, member, nil
}

func createMember(w ResponseWriter, r Request) (*Member, error) {
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
	if err := datastore.Get(ctx, gameID, game); err != nil {
		return nil, err
	}
	filterList := Games{*game}
	if _, err := filterList.RemoveBanned(ctx, user.Id); err != nil {
		return nil, err
	}
	if len(filterList) == 0 {
		return nil, HTTPErr{"banned from this game", http.StatusForbidden}
	}

	member := &Member{}
	if err := Copy(member, r, "POST"); err != nil {
		return nil, err
	}

	_, member, err = createMemberHelper(ctx, r, gameID, user, member)
	if err != nil {
		return nil, err
	}

	return member, err
}
