package game

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	Created = iota
	Started
	Finished
)

const (
	GameKind   = "Game"
	MemberKind = "Member"
)

var GameResource = &Resource{
	Load:   loadGame,
	Create: createGame,
}

type Game struct {
	ID    *datastore.Key
	State int
	Desc  string
}

func (g *Game) Item(r Request) *Item {
	return NewItem(g).SetDesc([][]string{[]string{g.Desc}}).AddLink(r.NewLink(GameResource.Link("self", Load, g.ID.Encode())))
}

func createGame(w ResponseWriter, r Request) (*Game, error) {
	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	game := &Game{}
	err := json.NewDecoder(r.Req().Body).Decode(game)
	if err != nil {
		return nil, err
	}
	game.State = Created

	ctx := appengine.NewContext(r.Req())

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game.ID, err = datastore.Put(ctx, datastore.NewKey(ctx, GameKind, "", 0, nil), game)
		if err != nil {
			return err
		}
		member := &Member{
			UserID:    user.Id,
			GameID:    game.ID,
			GameState: game.State,
		}
		_, err := datastore.Put(ctx, datastore.NewKey(ctx, MemberKind, member.UserID, 0, game.ID), member)
		return err
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return game, nil
	return game, nil
}

func loadGame(w ResponseWriter, r Request) (*Game, error) {
	id, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	ctx := appengine.NewContext(r.Req())

	game := &Game{}
	if err := datastore.Get(ctx, id, game); err != nil {
		if err == datastore.ErrNoSuchEntity {
			http.Error(w, "not found", 404)
			return nil, nil
		}
		return nil, err
	}

	game.ID = id
	return game, nil
}

func (g *Game) UpdateMembers(ctx context.Context) error {
	members := []Member{}
	ids, err := datastore.NewQuery(MemberKind).Ancestor(g.ID).GetAll(ctx, &members)
	if err != nil {
		return err
	}
	for index, member := range members {
		member.GameID = g.ID
		member.GameState = g.State
		if _, err := datastore.Put(ctx, ids[index], member); err != nil {
			return err
		}
	}
	return nil
}

type Member struct {
	UserID    string
	GameID    *datastore.Key
	GameState int
}

func SetupRouter(r *mux.Router) {
	HandleResource(r, GameResource)
}
