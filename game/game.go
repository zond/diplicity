package game

import (
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
	OpenGamesRoute       = "OpenGames"
	ClosedGamesRoute     = "ClosedGames"
	FinishedGamesRoute   = "FinishedGames"
	MyOpenGamesRoute     = "MyOpenGames"
	MyClosedGamesRoute   = "MyClosedGames"
	MyFinishedGamesRoute = "MyFinishedGames"
)

const (
	gameKind   = "Game"
	memberKind = "Member"
)

var GameResource = &Resource{
	Load:   loadGame,
	Create: createGame,
}

type Games []Game

func (g Games) Item(r Request, name string, desc []string, route string) *Item {
	gameItems := make(List, len(g))
	for index, game := range g {
		gameItems[index] = game.Item(r)
	}
	return NewItem(gameItems).SetName(name).SetDesc([][]string{
		desc,
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: route,
	}))
}

type Game struct {
	ID             *datastore.Key
	State          int
	Desc           string `methods:"POST,UPDATE"`
	NMembers       int
	NextDeadlineAt time.Time
	CreatedAt      time.Time
}

func (g *Game) Item(r Request) *Item {
	return NewItem(g).SetName(g.Desc).AddLink(r.NewLink(GameResource.Link("self", Load, g.ID.Encode())))
}

type Member struct {
	ID   string
	Game Game
}

func createGame(w ResponseWriter, r Request) (*Game, error) {
	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	game := &Game{}
	err := Copy(game, r.Req().Body, "POST")
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
			ID:   user.Id,
			Game: *game,
		}
		_, err := datastore.Put(ctx, datastore.NewKey(ctx, memberKind, member.ID, 0, game.ID), member)
		return err
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

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
		member.Game = *g
		if _, err := datastore.Put(ctx, ids[index], member); err != nil {
			return err
		}
	}
	return nil
}

type gamesHandler struct {
	query *datastore.Query
	name  string
	desc  []string
	route string
}

func (h gamesHandler) handlePublic(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	games := Games{}
	ids, err := h.query.GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for index, id := range ids {
		games[index].ID = id
	}
	w.SetContent(games.Item(r, h.name, h.desc, h.route))
	return nil
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	ctx := appengine.NewContext(r.Req())

	memberKeys, err := h.query.Filter("ID=", user.Id).KeysOnly().GetAll(ctx, nil)
	if err != nil {
		return err
	}
	gameKeys := make([]*datastore.Key, len(memberKeys))
	for index, key := range memberKeys {
		gameKeys[index] = key.Parent()
	}
	games := make(Games, len(memberKeys))
	if err := datastore.GetMulti(ctx, gameKeys, games); err != nil {
		return err
	}
	for index, id := range gameKeys {
		games[index].ID = id
	}
	w.SetContent(games.Item(r, h.name, h.desc, h.route))
	return nil
}

var (
	finishedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("State=", FinishedState).Order("CreatedAt"),
		name:  "finished-games",
		desc:  []string{"Finished games", "Unjoinable, finished games, sorted with oldest first."},
		route: FinishedGamesRoute,
	}
	closedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("State=", ClosedState).Order("CreatedAt"),
		name:  "closed-games",
		desc:  []string{"Closed games", "Unjoinable, unfinished games, sorted with oldest first."},
		route: ClosedGamesRoute,
	}
	openGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("State=", OpenState).Order("-NMembers").Order("CreatedAt"),
		name:  "open-games",
		desc:  []string{"Open games", "Joinable, unfinished games, sorted with fullest and oldest first."},
		route: OpenGamesRoute,
	}
	myFinishedGamesHandler = gamesHandler{
		query: datastore.NewQuery(memberKind).Filter("Game.State=", FinishedState).Order("Game.CreatedAt"),
		name:  "my-finished-games",
		desc:  []string{"My finished games", "My unjoinable, finished games, sorted with oldest first."},
		route: MyFinishedGamesRoute,
	}
	myClosedGamesHandler = gamesHandler{
		query: datastore.NewQuery(memberKind).Filter("Game.State=", ClosedState).Order("Game.NextDeadlineAt"),
		name:  "my-closed-games",
		desc:  []string{"My closed games", "My unjoinable, unfinished games, sorted with closest deadline first."},
		route: MyClosedGamesRoute,
	}
	myOpenGamesHandler = gamesHandler{
		query: datastore.NewQuery(memberKind).Filter("Game.State=", OpenState).Order("-Game.NMembers").Order("Game.CreatedAt"),
		name:  "my-open-games",
		desc:  []string{"My open games", "My joinable, unfinished games, sorted with fullest and oldest first."},
		route: MyClosedGamesRoute,
	}
)

func SetupRouter(r *mux.Router) {
	HandleResource(r, GameResource)
	Handle(r, "/games/open", []string{"GET"}, OpenGamesRoute, openGamesHandler.handlePublic)
	Handle(r, "/games/closed", []string{"GET"}, ClosedGamesRoute, closedGamesHandler.handlePublic)
	Handle(r, "/games/finished", []string{"GET"}, FinishedGamesRoute, finishedGamesHandler.handlePublic)
	Handle(r, "/games/my/open", []string{"GET"}, MyOpenGamesRoute, myOpenGamesHandler.handlePrivate)
	Handle(r, "/games/my/closed", []string{"GET"}, MyClosedGamesRoute, myClosedGamesHandler.handlePrivate)
	Handle(r, "/games/my/finished", []string{"GET"}, MyFinishedGamesRoute, myFinishedGamesHandler.handlePrivate)
}
