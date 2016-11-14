package game

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

func preflight(w http.ResponseWriter, r *http.Request) {
	CORSHeaders(w)
}

var (
	router = mux.NewRouter()
)

const (
	maxLimit = 64
)

const (
	GetManifestJSRoute          = "GetManifestJS"
	GetSWJSRoute                = "GetSWJS"
	GetMainJSRoute              = "GetMainJS"
	ConfigureRoute              = "AuthConfigure"
	IndexRoute                  = "Index"
	OpenGamesRoute              = "OpenGames"
	StartedGamesRoute           = "StartedGames"
	FinishedGamesRoute          = "FinishedGames"
	MyStagingGamesRoute         = "MyStagingGames"
	MyStartedGamesRoute         = "MyStartedGames"
	MyFinishedGamesRoute        = "MyFinishedGames"
	ListOrdersRoute             = "ListOrders"
	ListPhasesRoute             = "ListPhases"
	ListPhaseStatesRoute        = "ListPhaseStates"
	ListGameStatesRoute         = "ListGameStates"
	ListOptionsRoute            = "ListOptions"
	ListChannelsRoute           = "ListChannels"
	ListMessagesRoute           = "ListMessages"
	DevResolvePhaseTimeoutRoute = "DevResolvePhaseTimeout"
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
	w     ResponseWriter
	r     Request
	user  *auth.User
	iter  *datastore.Iterator
	limit int
	h     *gamesHandler
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

func (h *gamesHandler) prepare(w ResponseWriter, r Request, private bool) (*gamesReq, error) {
	req := &gamesReq{
		ctx: appengine.NewContext(r.Req()),
		w:   w,
		r:   r,
		h:   h,
	}

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}
	req.user = user

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}
	req.limit = int(limit)

	q := h.query
	if private {
		q = q.Filter("Members.User.Id=", user.Id)
	}

	if variantFilter := r.Req().URL.Query().Get("variant"); variantFilter != "" {
		q = q.Filter("Variant=", variantFilter)
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

func (req *gamesReq) handle() error {
	var err error
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

	req.w.SetContent(games.Item(req.r, req.user, curs, req.limit, req.h.name, req.h.desc, req.h.route))
	return nil
}

func (h *gamesHandler) handlePublic(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r, false)
	if err != nil {
		return err
	}

	return req.handle()
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r, true)
	if err != nil {
		return err
	}

	return req.handle()
}

var (
	finishedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-CreatedAt"),
		name:  "finished-games",
		desc:  []string{"Finished games", "Finished games, sorted with newest first."},
		route: FinishedGamesRoute,
	}
	startedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("CreatedAt"),
		name:  "started-games",
		desc:  []string{"Started games", "Started games, sorted with oldest first."},
		route: StartedGamesRoute,
	}
	openGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Closed=", false).Order("-NMembers").Order("CreatedAt"),
		name:  "open-games",
		desc:  []string{"Open games", "Open games, sorted with fullest and oldest first."},
		route: OpenGamesRoute,
	}
	stagingGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", false).Order("-NMembers").Order("CreatedAt"),
		name:  "my-staging-games",
		desc:  []string{"My staging games", "Unstarted games I'm a member of, sorted with fullest and oldest first."},
		route: MyStagingGamesRoute,
	}
)

type configuration struct {
	OAuth   *auth.OAuth
	FCMConf *FCMConf
}

func handleConfigure(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf := &configuration{}
	if err := json.NewDecoder(r.Req().Body).Decode(conf); err != nil {
		return err
	}
	if conf.OAuth != nil {
		if err := auth.SetOAuth(ctx, conf.OAuth); err != nil {
			return err
		}
	}
	if conf.FCMConf != nil {
		if err := SetFCMConf(ctx, conf.FCMConf); err != nil {
			return err
		}
	}
	return nil
}

func SetupRouter(r *mux.Router) {
	Handle(r, "/_configure", []string{"POST"}, ConfigureRoute, handleConfigure)
	Handle(r, "/", []string{"GET"}, IndexRoute, handleIndex)
	Handle(r, "/Game/{game_id}/Channel/{channel_members}/Messages", []string{"GET"}, ListMessagesRoute, listMessages)
	Handle(r, "/Game/{game_id}/Channels", []string{"GET"}, ListChannelsRoute, listChannels)
	Handle(r, "/Game/{game_id}/GameStates", []string{"GET"}, ListGameStatesRoute, listGameStates)
	Handle(r, "/Game/{game_id}/Phases", []string{"GET"}, ListPhasesRoute, listPhases)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/_dev_resolve_timeout", []string{"GET"}, DevResolvePhaseTimeoutRoute, devResolvePhaseTimeout)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/PhaseStates", []string{"GET"}, ListPhaseStatesRoute, listPhaseStates)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Orders", []string{"GET"}, ListOrdersRoute, listOrders)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	Handle(r, "/Games/Open", []string{"GET"}, OpenGamesRoute, openGamesHandler.handlePublic)
	Handle(r, "/Games/Started", []string{"GET"}, StartedGamesRoute, startedGamesHandler.handlePublic)
	Handle(r, "/Games/Finished", []string{"GET"}, FinishedGamesRoute, finishedGamesHandler.handlePublic)
	Handle(r, "/Games/My/Staging", []string{"GET"}, MyStagingGamesRoute, stagingGamesHandler.handlePrivate)
	Handle(r, "/Games/My/Started", []string{"GET"}, MyStartedGamesRoute, startedGamesHandler.handlePrivate)
	Handle(r, "/Games/My/Finished", []string{"GET"}, MyFinishedGamesRoute, finishedGamesHandler.handlePrivate)
	HandleResource(r, GameResource)
	HandleResource(r, MemberResource)
	HandleResource(r, PhaseResource)
	HandleResource(r, OrderResource)
	HandleResource(r, MessageResource)
	HandleResource(r, PhaseStateResource)
	HandleResource(r, GameStateResource)
	HandleResource(r, GameResultResource)
	HeadCallback(func(head *Node) error {
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/3.6.0/firebase.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/3.5.2/firebase-app.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/3.5.2/firebase-messaging.js")
		head.AddEl("link", "rel", "stylesheet", "style", "text/css", "href", "/css/bootstrap.css")
		head.AddEl("script", "src", "/js/main.js")
		head.AddEl("link", "rel", "manifest", "href", "/js/manifest.json")
		return nil
	})
}
