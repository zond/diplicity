package game

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	OpenState = iota
	ClosedState
	FinishedState
)

const (
	OpenGamesRoute     = "OpenGames"
	ClosedGamesRoute   = "ClosedGames"
	FinishedGamesRoute = "FinishedGames"
)

const (
	gameKind   = "Game"
	memberKind = "Member"
)

var GameResource = &Resource{
	Load:   loadGame,
	Create: createGame,
}

type Game struct {
	ID        *datastore.Key
	State     int
	Desc      string
	NMembers  int
	CreatedAt time.Time
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
	game.State = OpenState
	game.NMembers = 1
	game.CreatedAt = time.Now()

	ctx := appengine.NewContext(r.Req())

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game.ID, err = datastore.Put(ctx, datastore.NewKey(ctx, gameKind, "", 0, nil), game)
		if err != nil {
			return err
		}
		member := &Member{
			UserID:    user.Id,
			GameID:    game.ID,
			GameState: game.State,
		}
		_, err := datastore.Put(ctx, datastore.NewKey(ctx, memberKind, member.UserID, 0, game.ID), member)
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
	ids, err := datastore.NewQuery(memberKind).Ancestor(g.ID).GetAll(ctx, &members)
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

func handleFinishedGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	finishedGames := []Game{}
	if _, err := datastore.NewQuery(gameKind).Filter("State=", FinishedState).Order("CreatedAt").GetAll(ctx, &finishedGames); err != nil {
		return err
	}
	gameItems := make(List, len(finishedGames))
	for index, game := range finishedGames {
		gameItems[index] = game.Item(r)
	}
	content := NewItem(gameItems).SetName("finished-games").SetDesc([][]string{
		[]string{
			"Finished games",
			"Unjoinable, finished games, sorted with oldest first.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: FinishedGamesRoute,
	}))
	w.SetContent(content)
	return nil
}

func handleClosedGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	closedGames := []Game{}
	if _, err := datastore.NewQuery(gameKind).Filter("State=", ClosedState).Order("CreatedAt").GetAll(ctx, &closedGames); err != nil {
		return err
	}
	gameItems := make(List, len(closedGames))
	for index, game := range closedGames {
		gameItems[index] = game.Item(r)
	}
	content := NewItem(gameItems).SetName("closed-games").SetDesc([][]string{
		[]string{
			"Closed games",
			"Unjoinable, unfinished games, sorted with oldest first.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: ClosedGamesRoute,
	}))
	w.SetContent(content)
	return nil
}

func handleOpenGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	openGames := []Game{}
	if _, err := datastore.NewQuery(gameKind).Filter("State=", OpenState).Order("-NMembers").Order("CreatedAt").GetAll(ctx, &openGames); err != nil {
		return err
	}
	gameItems := make(List, len(openGames))
	for index, game := range openGames {
		gameItems[index] = game.Item(r)
	}
	content := NewItem(gameItems).SetName("open-games").SetDesc([][]string{
		[]string{
			"Open games",
			"Joinable, unfinished games, sorted with fullest and oldest first.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: OpenGamesRoute,
	}))
	w.SetContent(content)
	return nil
}

type Member struct {
	UserID    string
	GameID    *datastore.Key
	GameState int
}

func SetupRouter(r *mux.Router) {
	HandleResource(r, GameResource)
	Handle(r, "/games/open", []string{"GET"}, OpenGamesRoute, handleOpenGames)
	Handle(r, "/games/closed", []string{"GET"}, ClosedGamesRoute, handleClosedGames)
	Handle(r, "/games/finished", []string{"GET"}, FinishedGamesRoute, handleFinishedGames)
}
