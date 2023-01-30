package game

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

var (
	MemberResource               *Resource
	GameMasterInvitationResource *Resource
)

func init() {
	MemberResource = &Resource{
		Create:     createMember,
		Delete:     deleteMember,
		Update:     updateMember,
		CreatePath: "/Game/{game_id}/Member",
		FullPath:   "/Game/{game_id}/Member/{user_id}",
	}
	GameMasterInvitationResource = &Resource{
		Create:     gameMasterCreateInvitation,
		Delete:     gameMasterDeleteInvitation,
		CreatePath: "/Game/{game_id}",
		FullPath:   "/Game/{game_id}/{email}",
	}
}

type GameMasterInvitation struct {
	Email  string       `methods:"POST"`
	Nation godip.Nation `methods:"POST"`
}

func (g *GameMasterInvitation) Item(r Request) *Item {
	allocationItem := NewItem(g)
	return allocationItem
}

type GameMasterInvitations []GameMasterInvitation

type Member struct {
	User              auth.User
	Nation            godip.Nation
	GameAlias         string `methods:"POST,PUT" datastore:",noindex"`
	NationPreferences string `methods:"POST,PUT" datastore:",noindex"`
	NewestPhaseState  PhaseState
	UnreadMessages    int
	Replaceable       bool
	GracePeriodsUsed  int
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

func (m *Member) Anonymize(r Request) {
	m.GameAlias = ""
	m.NationPreferences = ""
	m.UnreadMessages = 0
	m.NewestPhaseState = PhaseState{}
	m.GracePeriodsUsed = 0
	m.User.Email = ""
	m.User.FamilyName = "Doe"
	m.User.GivenName = "John"
	m.User.Gender = ""
	m.User.Hd = ""
	m.User.Id = ""
	m.User.Link = ""
	m.User.Locale = ""
	m.User.Name = "Anonymous"
	m.User.Picture = DefaultScheme + "://" + r.Req().Host + "/img/anon.png"
	m.User.VerifiedEmail = false
	m.User.ValidUntil = time.Time{}
}

func (m *Member) Redact(viewer *auth.User, mustered bool) {
	if !mustered {
		m.Nation = ""
		m.NewestPhaseState.Nation = ""
	}
	if viewer.Id != m.User.Id {
		m.User.Email = ""
		m.GameAlias = ""
		m.NewestPhaseState = PhaseState{}
		m.UnreadMessages = 0
	}
	if !mustered && viewer.Id != m.User.Id {
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
		return game.DBSave(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return member, nil
}

type deleteMemberRequest struct {
	actorId    string
	toRemoveId string
	systemReq  bool
}

func deleteMemberHelper(ctx context.Context, gameID *datastore.Key, delReq deleteMemberRequest, idempotent bool) (*Member, error) {
	var member *Member
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return HTTPErr{"non existing game", http.StatusPreconditionFailed}
		}
		game.ID = gameID

		isMember := false
		member, isMember = game.GetMemberByUserId(delReq.toRemoveId)
		if !isMember {
			if idempotent {
				return nil
			}
			return HTTPErr{"can only remove existing members", http.StatusNotFound}
		}

		if !delReq.systemReq && (!game.Leavable() || delReq.actorId != delReq.toRemoveId) && game.GameMaster.Id != delReq.actorId {
			return HTTPErr{"member not removable, or actor not game master", http.StatusPreconditionFailed}
		}

		if !game.Started {
			newMembers := []Member{}
			for memberIdx := range game.Members {
				oldMember := game.Members[memberIdx]
				if oldMember.User.Id != delReq.toRemoveId {
					newMembers = append(newMembers, oldMember)
				}
			}
			game.Members = newMembers
		} else if !game.Finished {
			member.GameAlias = ""
			member.User = auth.User{
				Name: "Redacted",
			}
			member.Replaceable = true
		} else {
			return HTTPErr{"game is finished", http.StatusPreconditionFailed}
		}

		if err := UpdateUserStatsASAP(ctx, []string{delReq.toRemoveId}); err != nil {
			return err
		}

		if !game.GameMasterEnabled && len(game.Members) == 0 && !game.Started {
			return datastore.Delete(ctx, gameID)
		}

		if err := game.DBSave(ctx); err != nil {
			return err
		}

		return nil
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

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	return deleteMemberHelper(ctx, gameID, deleteMemberRequest{actorId: user.Id, toRemoveId: r.Vars()["user_id"]}, false)
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

		if !game.Joinable(user) {
			return HTTPErr{"game not joinable", http.StatusPreconditionFailed}
		}

		if game.Started {
			replaced := false
			for memberIdx := range game.Members {
				oldMember := &game.Members[memberIdx]
				if oldMember.Replaceable {
					oldMember.User = *user
					oldMember.GameAlias = member.GameAlias
					oldMember.Replaceable = false
					replaced = true
					break
				}
			}
			if !replaced {
				return fmt.Errorf("wtf? how could this even happen?")
			}
		} else {
			member.User = *user
			member.NewestPhaseState = PhaseState{
				GameID: gameID,
			}
			game.Members = append(game.Members, *member)

			if len(game.Members) == len(variants.Variants[game.Variant].Nations) {
				if err := asyncStartGameFunc.EnqueueIn(ctx, 0, game.ID, r.Req().Host); err != nil {
					return err
				}
			}

		}

		if err := game.DBSave(ctx); err != nil {
			return err
		}

		if err := UpdateUserStatsASAP(ctx, []string{user.Id}); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, nil, err
	}

	return game, member, nil
}

func gameMasterDeleteInvitation(w ResponseWriter, r Request) (*GameMasterInvitation, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	gmi := GameMasterInvitation{}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID

		if game.GameMaster.Id != user.Id {
			return HTTPErr{"unauthorized", http.StatusUnauthorized}
		}

		newInvitations := GameMasterInvitations{}
		for _, invitation := range game.GameMasterInvitations {
			if strings.ToLower(TrimSpace(invitation.Email)) != strings.ToLower(TrimSpace(r.Vars()["email"])) {
				newInvitations = append(newInvitations, invitation)
			} else {
				gmi = invitation
				gmi.Email = strings.ToLower(TrimSpace(gmi.Email))
			}
		}
		game.GameMasterInvitations = newInvitations

		if _, err := datastore.Put(ctx, gameID, game); err != nil {
			return err
		}

		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return &gmi, nil
}

func gameMasterCreateInvitation(w ResponseWriter, r Request) (*GameMasterInvitation, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	gmi := &GameMasterInvitation{}
	err = Copy(gmi, r, "POST")
	if err != nil {
		return nil, err
	}

	gmi.Email = strings.ToLower(TrimSpace(gmi.Email))
	if gmi.Email == "" {
		return nil, HTTPErr{"email empty", http.StatusBadRequest}
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID

		if game.GameMaster.Id != user.Id {
			return HTTPErr{"unauthorized", http.StatusUnauthorized}
		}

		if gmi.Nation != "" && !game.ValidNation(gmi.Nation) {
			return HTTPErr{"unrecognized nation in variant", http.StatusBadRequest}
		}

		found := false
		for idx := range game.GameMasterInvitations {
			if game.GameMasterInvitations[idx].Email == gmi.Email {
				found = true
				game.GameMasterInvitations[idx] = *gmi
				break
			}
		}
		if !found {
			game.GameMasterInvitations = append(game.GameMasterInvitations, *gmi)
		}

		if _, err := datastore.Put(ctx, gameID, game); err != nil {
			return err
		}

		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return gmi, nil
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
	if _, err := filterList.RemoveBanned(ctx, user.Id, true); err != nil {
		return nil, err
	}
	if len(filterList) == 0 {
		return nil, HTTPErr{"banned from this game", http.StatusPreconditionFailed}
	}

	userStats := &UserStats{}
	if err := datastore.Get(ctx, UserStatsID(ctx, user.Id), userStats); err == datastore.ErrNoSuchEntity {
		userStats.UserId = user.Id
		userStats.User = *user
	} else if err != nil {
		return nil, err
	}
	filterList.RemoveFiltered(toJoin, userStats, true)
	if len(filterList) == 0 {
		return nil, HTTPErr{"filtered from this game", http.StatusPreconditionFailed}
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
