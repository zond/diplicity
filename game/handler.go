package game

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

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

	memberKeys, err := h.query.Filter("User.Id=", user.Id).KeysOnly().GetAll(ctx, nil)
	if err != nil {
		return err
	}
	log.Printf("%v found members %+v", h.query, memberKeys)
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
		query: datastore.NewQuery(memberKind).Filter("GameData.State=", FinishedState).Order("GameData.CreatedAt"),
		name:  "my-finished-games",
		desc:  []string{"My finished games", "Unjoinable, finished games I'm a member of, sorted with oldest first."},
		route: MyFinishedGamesRoute,
	}
	myClosedGamesHandler = gamesHandler{
		query: datastore.NewQuery(memberKind).Filter("GameData.State=", ClosedState).Order("GameData.NextDeadlineAt"),
		name:  "my-closed-games",
		desc:  []string{"My closed games", "Unjoinable, unfinished games I'm a member of, sorted with closest deadline first."},
		route: MyClosedGamesRoute,
	}
	myOpenGamesHandler = gamesHandler{
		query: datastore.NewQuery(memberKind).Filter("GameData.State=", OpenState).Order("-GameData.NMembers").Order("GameData.CreatedAt"),
		name:  "my-open-games",
		desc:  []string{"My open games", "Joinable, unfinished games I'm a member of, sorted with fullest and oldest first."},
		route: MyClosedGamesRoute,
	}
)

func SetupRouter(r *mux.Router) {
	HandleResource(r, GameResource)
	HandleResource(r, MemberResource)
	Handle(r, "/games/open", []string{"GET"}, OpenGamesRoute, openGamesHandler.handlePublic)
	Handle(r, "/games/closed", []string{"GET"}, ClosedGamesRoute, closedGamesHandler.handlePublic)
	Handle(r, "/games/finished", []string{"GET"}, FinishedGamesRoute, finishedGamesHandler.handlePublic)
	Handle(r, "/games/my/open", []string{"GET"}, MyOpenGamesRoute, myOpenGamesHandler.handlePrivate)
	Handle(r, "/games/my/closed", []string{"GET"}, MyClosedGamesRoute, myClosedGamesHandler.handlePrivate)
	Handle(r, "/games/my/finished", []string{"GET"}, MyFinishedGamesRoute, myFinishedGamesHandler.handlePrivate)
}
