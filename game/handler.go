package game

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	maxLimit = 64
)

type gamesHandler struct {
	query *datastore.Query
	name  string
	desc  []string
	route string
}

func (h gamesHandler) handlePublic(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	var iter *datastore.Iterator
	if cursor := r.Req().URL.Query().Get("cursor"); cursor == "" {
		iter = h.query.Run(ctx)
	} else {
		decoded, err := datastore.DecodeCursor(cursor)
		if err != nil {
			return err
		}
		iter = h.query.Start(decoded).Run(ctx)
	}

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}

	games := Games{}
	for err == nil && len(games) < int(limit) {
		game := Game{}
		game.ID, err = iter.Next(&game)
		if err == nil {
			games = append(games, game)
		}
	}

	var cursor *datastore.Cursor
	if err == nil {
		curs, err := iter.Cursor()
		if err != nil {
			return err
		}
		cursor = &curs
	} else if err != datastore.Done {
		return err
	}

	w.SetContent(games.Item(r, cursor, int(limit), h.name, h.desc, h.route))
	return nil
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	var iter *datastore.Iterator
	q := h.query.Filter("User.Id=", user.Id).KeysOnly()
	if cursor := r.Req().URL.Query().Get("cursor"); cursor == "" {
		iter = q.Run(ctx)
	} else {
		decoded, err := datastore.DecodeCursor(cursor)
		if err != nil {
			return err
		}
		iter = q.Start(decoded).Run(ctx)
	}

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}

	memberIDs := []*datastore.Key{}
	for err == nil && len(memberIDs) < int(limit) {
		var memberID *datastore.Key
		memberID, err = iter.Next(nil)
		if err == nil {
			memberIDs = append(memberIDs, memberID)
		}
		log.Printf("found %v, %#v", memberID, err)
	}

	var cursor *datastore.Cursor
	if err == nil {
		curs, err := iter.Cursor()
		if err != nil {
			return err
		}
		cursor = &curs
	} else if err != datastore.Done {
		return err
	}

	gameIDs := make([]*datastore.Key, len(memberIDs))
	for index, key := range memberIDs {
		gameIDs[index] = key.Parent()
	}

	games := make(Games, len(gameIDs))
	if err := datastore.GetMulti(ctx, gameIDs, games); err != nil {
		return err
	}
	for index, id := range gameIDs {
		games[index].ID = id
	}

	w.SetContent(games.Item(r, cursor, int(limit), h.name, h.desc, h.route))
	return nil
}

var (
	finishedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("State=", FinishedState).Order("-CreatedAt"),
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
		query: datastore.NewQuery(memberKind).Filter("GameData.State=", FinishedState).Order("-GameData.CreatedAt"),
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
		route: MyOpenGamesRoute,
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
