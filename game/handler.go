package game

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	maxLimit = 64
)

const (
	OpenGamesRoute       = "OpenGames"
	StartedGamesRoute    = "StartedGames"
	FinishedGamesRoute   = "FinishedGames"
	MyStagingGamesRoute  = "MyStagingGames"
	MyStartedGamesRoute  = "MyStartedGames"
	MyFinishedGamesRoute = "MyFinishedGames"
	ListOrdersRoute      = "ListOrders"
	ListPhasesRoute      = "ListPhases"
	ListOptionsRoute     = "ListOptions"
)

type gamesHandler struct {
	query   *datastore.Query
	name    string
	desc    []string
	route   string
	private bool
}

type gamesReq struct {
	ctx   context.Context
	user  *auth.User
	iter  *datastore.Iterator
	limit int
}

func (r *gamesReq) cursor(err error) (*datastore.Cursor, error) {
	if err == nil {
		curs, err := r.iter.Cursor()
		if err != nil {
			return nil, err
		}
		return &curs, nil
	}
	if err == datastore.Done {
		return nil, nil
	}
	return nil, err
}

func (h *gamesHandler) prepare(w ResponseWriter, r Request) (*gamesReq, error) {
	req := &gamesReq{
		ctx: appengine.NewContext(r.Req()),
	}

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}
	req.user = user

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}
	req.limit = int(limit)

	q := h.query
	if h.private {
		q = q.Filter("User.Id=", user.Id).KeysOnly()
	}

	if variantFilter := r.Req().URL.Query().Get("variant"); variantFilter != "" {
		if h.private {
			q = q.Filter("GameData.Variant=", variantFilter)
		} else {
			q = q.Filter("Variant=", variantFilter)
		}
	}

	cursor := r.Req().URL.Query().Get("cursor")
	if cursor == "" {
		req.iter = q.Run(req.ctx)
		return req, nil
	}

	decoded, err := datastore.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	req.iter = q.Start(decoded).Run(req.ctx)
	return req, nil
}

func (h *gamesHandler) handle(w ResponseWriter, r Request) error {
	if h.private {
		return h.handlePrivate(w, r)
	}
	return h.handlePublic(w, r)
}

func (h *gamesHandler) handlePublic(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r)
	if err != nil {
		return err
	}

	games := Games{}
	for err == nil && len(games) < req.limit {
		game := Game{}
		game.ID, err = req.iter.Next(&game)
		if err == nil {
			games = append(games, game)
		}
	}

	curs, err := req.cursor(err)
	if err != nil {
		return err
	}

	w.SetContent(games.Item(r, req.user, curs, req.limit, h.name, h.desc, h.route))
	return nil
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r)
	if err != nil {
		return err
	}

	memberIDs := []*datastore.Key{}
	for err == nil && len(memberIDs) < req.limit {
		var memberID *datastore.Key
		memberID, err = req.iter.Next(nil)
		if err == nil {
			memberIDs = append(memberIDs, memberID)
		}
	}

	curs, err := req.cursor(err)
	if err != nil {
		return err
	}

	gameIDs := make([]*datastore.Key, len(memberIDs))
	for index, key := range memberIDs {
		gameIDs[index] = key.Parent()
	}

	games := make(Games, len(gameIDs))
	if err := datastore.GetMulti(req.ctx, gameIDs, games); err != nil {
		return err
	}
	for index, id := range gameIDs {
		games[index].ID = id
	}

	w.SetContent(games.Item(r, req.user, curs, req.limit, h.name, h.desc, h.route))
	return nil
}

var (
	finishedGamesHandler = gamesHandler{
		query:   datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-CreatedAt"),
		name:    "finished-games",
		desc:    []string{"Finished games", "Finished games, sorted with newest first."},
		route:   FinishedGamesRoute,
		private: false,
	}
	startedGamesHandler = gamesHandler{
		query:   datastore.NewQuery(gameKind).Filter("Started=", true).Order("CreatedAt"),
		name:    "started-games",
		desc:    []string{"Started games", "Started games, sorted with oldest first."},
		route:   StartedGamesRoute,
		private: false,
	}
	openGamesHandler = gamesHandler{
		query:   datastore.NewQuery(gameKind).Filter("Closed=", false).Order("-NMembers").Order("CreatedAt"),
		name:    "open-games",
		desc:    []string{"Open games", "Open games, sorted with fullest and oldest first."},
		route:   OpenGamesRoute,
		private: false,
	}
	myFinishedGamesHandler = gamesHandler{
		query:   datastore.NewQuery(memberKind).Filter("GameData.Finished=", true).Order("-GameData.CreatedAt"),
		name:    "my-finished-games",
		desc:    []string{"My finished games", "Finished games I'm a member of, sorted with newest first."},
		route:   MyFinishedGamesRoute,
		private: true,
	}
	myStartedGamesHandler = gamesHandler{
		query:   datastore.NewQuery(memberKind).Filter("GameData.Started=", true).Order("GameData.NextDeadlineAt"),
		name:    "my-started-games",
		desc:    []string{"My started games", "Started games I'm a member of, sorted with closest deadline first."},
		route:   MyStartedGamesRoute,
		private: true,
	}
	myStagingGamesHandler = gamesHandler{
		query:   datastore.NewQuery(memberKind).Filter("GameData.Started=", false).Order("-GameData.NMembers").Order("GameData.CreatedAt"),
		name:    "my-staging-games",
		desc:    []string{"My staging games", "Unstarted games I'm a member of, sorted with fullest and oldest first."},
		route:   MyStagingGamesRoute,
		private: true,
	}
)

func SetupRouter(r *mux.Router) {
	HandleResource(r, GameResource)
	HandleResource(r, MemberResource)
	HandleResource(r, PhaseResource)
	HandleResource(r, OrderResource)
	Handle(r, "/Game/{game_id}/Phases", []string{"GET"}, ListPhasesRoute, listPhases)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Orders", []string{"GET"}, ListOrdersRoute, listOrders)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	Handle(r, "/games/Open", []string{"GET"}, OpenGamesRoute, openGamesHandler.handle)
	Handle(r, "/Games/Started", []string{"GET"}, StartedGamesRoute, startedGamesHandler.handle)
	Handle(r, "/Games/Finished", []string{"GET"}, FinishedGamesRoute, finishedGamesHandler.handle)
	Handle(r, "/Games/My/Staging", []string{"GET"}, MyStagingGamesRoute, myStagingGamesHandler.handle)
	Handle(r, "/Games/My/Started", []string{"GET"}, MyStartedGamesRoute, myStartedGamesHandler.handle)
	Handle(r, "/Games/My/Finished", []string{"GET"}, MyFinishedGamesRoute, myFinishedGamesHandler.handle)
}
