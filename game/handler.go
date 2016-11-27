package game

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"

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
	ListBansRoute               = "ListBans"
	ListTopRatedPlayersRoute    = "ListTopRatedPlayers"
	ListTopReliablePlayersRoute = "ListTopReliablePlayers"
	ListTopHatedPlayersRoute    = "ListTopHatedPlayers"
	ListTopHaterPlayersRoute    = "ListTopHaterPlayers"
	ListTopQuickPlayersRoute    = "ListTopQuickPlayers"
	ListFlaggedMessagesRoute    = "ListFlaggedMessages"
	DevResolvePhaseTimeoutRoute = "DevResolvePhaseTimeout"
	DevUserStatsUpdateRoute     = "DevUserStatsUpdate"
	ReceiveMailRoute            = "ReceiveMail"
)

type userStatsHandler struct {
	query *datastore.Query
	name  string
	desc  []string
	route string
}

func (h *userStatsHandler) handle(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
	}

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}

	var iter *datastore.Iterator

	cursor := r.Req().URL.Query().Get("cursor")
	if cursor == "" {
		iter = h.query.Run(ctx)
	} else {
		decoded, err := datastore.DecodeCursor(cursor)
		if err != nil {
			return err
		}
		iter = h.query.Start(decoded).Run(ctx)
	}

	stats := UserStatsSlice{}
	for err == nil && len(stats) < int(limit) {
		stat := &UserStats{}
		_, err = iter.Next(stat)
		if err == nil {
			stats = append(stats, *stat)
		}
	}

	var cursP *datastore.Cursor
	if err == nil {
		curs, err := iter.Cursor()
		if err != nil {
			return err
		}
		cursP = &curs
	}

	w.SetContent(stats.Item(r, cursP, limit, h.name, h.desc, h.route))

	return nil
}

type gamesHandler struct {
	query   *datastore.Query
	name    string
	desc    []string
	route   string
	private bool
}

type gamesReq struct {
	ctx           context.Context
	w             ResponseWriter
	r             Request
	user          *auth.User
	userStats     *UserStats
	iter          *datastore.Iterator
	limit         int
	h             *gamesHandler
	detailFilters []func(g *Game) bool
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

func (req *gamesReq) intervalFilter(fieldName, paramName string) func(*Game) bool {
	parm := req.r.Req().URL.Query().Get(paramName)
	if parm == "" {
		return nil
	}

	parts := strings.Split(parm, ":")
	if len(parts) != 2 {
		return nil
	}

	min, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil
	}

	max, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return nil
	}

	return func(g *Game) bool {
		cmp := reflect.ValueOf(g).Elem().FieldByName(fieldName).Float()
		return cmp >= min && cmp <= max
	}
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

	userStats := &UserStats{}
	if err := datastore.Get(req.ctx, UserStatsID(req.ctx, user.Id), userStats); err == datastore.ErrNoSuchEntity {
		userStats.UserId = user.Id
	} else if err != nil {
		return nil, err
	}
	req.userStats = userStats

	uq := r.Req().URL.Query()
	limit, err := strconv.ParseInt(uq.Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}
	req.limit = int(limit)

	q := h.query
	if private {
		q = q.Filter("Members.User.Id=", user.Id)
	}

	if variantFilter := uq.Get("variant"); variantFilter != "" {
		req.detailFilters = append(req.detailFilters, func(g *Game) bool {
			return g.Variant == variantFilter
		})
	}
	if f := req.intervalFilter("MinReliability", "min-reliability"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter("MinQuickness", "min-quickness"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter("MaxHater", "max-hater"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter("MaxHated", "max-hated"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter("MinRating", "min-rating"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter("MaxRating", "max-rating"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}

	cursor := uq.Get("cursor")
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

func (h *gamesHandler) fetch(iter *datastore.Iterator, max int) (Games, error) {
	var err error
	result := make(Games, 0, max)
	for err == nil && len(result) < max {
		game := Game{}
		game.ID, err = iter.Next(&game)
		if err == nil {
			result = append(result, game)
		}
	}
	return result, err
}

func (req *gamesReq) handle(private bool) error {
	var err error
	games := make(Games, 0, req.limit)
	for err == nil && len(games) < req.limit {
		var nextBatch Games
		nextBatch, err = req.h.fetch(req.iter, req.limit-len(games))
		nextBatch.RemoveCustomFiltered(req.detailFilters)
		if !private {
			nextBatch.RemoveFiltered(req.userStats)
			if _, filtErr := nextBatch.RemoveBanned(req.ctx, req.user.Id); filtErr != nil {
				return filtErr
			}
		}
		games = append(games, nextBatch...)
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

	return req.handle(false)
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r, true)
	if err != nil {
		return err
	}

	return req.handle(true)
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
	topRatedPlayersHandler = userStatsHandler{
		query: datastore.NewQuery(userStatsKind).Order("-Glicko.PracticalRating"),
		name:  "top-rated-players",
		desc:  []string{"Top rated alayers", "Players sorted by PracticalGlicko (lowest bound of their rating: rating - 2 * deviation)"},
		route: ListTopRatedPlayersRoute,
	}
	topReliablePlayersHandler = userStatsHandler{
		query: datastore.NewQuery(userStatsKind).Order("-Reliability"),
		name:  "top-reliable-players",
		desc:  []string{"Top reliable players", "Players sorted by Reliability"},
		route: ListTopReliablePlayersRoute,
	}
	topHatedPlayersHandler = userStatsHandler{
		query: datastore.NewQuery(userStatsKind).Order("-Hated"),
		name:  "top-hated-players",
		desc:  []string{"Top hated players", "Players sorted by Hated"},
		route: ListTopHatedPlayersRoute,
	}
	topHaterPlayersHandler = userStatsHandler{
		query: datastore.NewQuery(userStatsKind).Order("-Hater"),
		name:  "top-hater-players",
		desc:  []string{"Top hater players", "Players sorted by Hater"},
		route: ListTopHaterPlayersRoute,
	}
	topQuickPlayersHandler = userStatsHandler{
		query: datastore.NewQuery(userStatsKind).Order("-Quickness"),
		name:  "top-quick-players",
		desc:  []string{"Top quick players", "Players sorted by Quickness"},
		route: ListTopQuickPlayersRoute,
	}
)

type configuration struct {
	OAuth    *auth.OAuth
	FCMConf  *FCMConf
	SendGrid *SendGrid
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
	if conf.SendGrid != nil {
		if err := SetSendGrid(ctx, conf.SendGrid); err != nil {
			return err
		}
	}
	return nil
}

func SetupRouter(r *mux.Router) {
	router = r
	Handle(r, "/_configure", []string{"POST"}, ConfigureRoute, handleConfigure)
	Handle(r, "/_ah/mail", []string{"POST"}, ReceiveMailRoute, receiveMail)
	Handle(r, "/", []string{"GET"}, IndexRoute, handleIndex)
	Handle(r, "/Game/{game_id}/Channels", []string{"GET"}, ListChannelsRoute, listChannels)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/_dev_resolve_timeout", []string{"GET"}, DevResolvePhaseTimeoutRoute, devResolvePhaseTimeout)
	Handle(r, "/User/{user_id}/Stats/_dev_update", []string{"PUT"}, DevUserStatsUpdateRoute, devUserStatsUpdate)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	HandleResource(r, GameResource)
	HandleResource(r, MemberResource)
	HandleResource(r, PhaseResource)
	HandleResource(r, OrderResource)
	HandleResource(r, MessageResource)
	HandleResource(r, PhaseStateResource)
	HandleResource(r, GameStateResource)
	HandleResource(r, GameResultResource)
	HandleResource(r, BanResource)
	HandleResource(r, PhaseResultResource)
	HandleResource(r, UserStatsResource)
	HandleResource(r, MessageFlagResource)
	HandleResource(r, FlaggedMessagesResource)
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
