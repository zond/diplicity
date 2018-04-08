package game

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
)

var (
	router = mux.NewRouter()
)

const (
	maxLimit = 128
)

const (
	GetSWJSRoute                = "GetSWJS"
	GetMainJSRoute              = "GetMainJS"
	ConfigureRoute              = "AuthConfigure"
	IndexRoute                  = "Index"
	ListOpenGamesRoute          = "ListOpenGames"
	ListStartedGamesRoute       = "ListStartedGames"
	ListFinishedGamesRoute      = "ListFinishedGames"
	ListMyStagingGamesRoute     = "ListMyStagingGames"
	ListMyStartedGamesRoute     = "ListMyStartedGames"
	ListMyFinishedGamesRoute    = "ListMyFinishedGames"
	ListOtherStagingGamesRoute  = "ListOtherStagingGames"
	ListOtherStartedGamesRoute  = "ListOtherStartedGames"
	ListOtherFinishedGamesRoute = "ListOtherFinishedGames"
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
	RenderPhaseMapRoute         = "RenderPhaseMap"
	ReRateRoute                 = "ReRate"
	GlobalStatsRoute            = "GlobalStats"
	FixNewTimestampsRoute       = "FixNewTimestamps"
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
		return HTTPErr{"unauthenticated", 401}
	}

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}

	query := h.query

	cursor := r.Req().URL.Query().Get("cursor")
	if cursor != "" {
		decoded, err := datastore.DecodeCursor(cursor)
		if err != nil {
			return err
		}
		query = query.Start(decoded)
	}

	iter := query.Run(ctx)

	stats := UserStatsSlice{}
	for err == nil && len(stats) < int(limit) {
		stat := &UserStats{}
		_, err = iter.Next(stat)
		if err == nil {
			stats = append(stats, *stat)
		}
	}

	for i := range stats {
		stats[i].Redact()
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
	viewerFilter  bool
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

func (h *gamesHandler) prepare(w ResponseWriter, r Request, userId *string, viewerFilter bool) (*gamesReq, error) {
	req := &gamesReq{
		ctx:          appengine.NewContext(r.Req()),
		w:            w,
		r:            r,
		h:            h,
		viewerFilter: viewerFilter,
	}

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", 401}
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
	if userId != nil {
		q = q.Filter("Members.User.Id=", *userId)
	}

	apiLevel := auth.APILevel(r)
	req.detailFilters = append(req.detailFilters, func(g *Game) bool {
		if launchLevel, found := variants.LaunchSchedule[g.Variant]; found {
			return apiLevel >= launchLevel
		}
		return true
	})
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
		for i := range game.NewestPhaseMeta {
			game.NewestPhaseMeta[i].Refresh()
		}
		game.Refresh()
		if err == nil {
			result = append(result, game)
		}
	}
	return result, err
}

func (req *gamesReq) handle() error {
	var err error
	games := make(Games, 0, req.limit)
	for err == nil && len(games) < req.limit {
		var nextBatch Games
		nextBatch, err = req.h.fetch(req.iter, req.limit-len(games))
		nextBatch.RemoveCustomFiltered(req.detailFilters)
		if req.viewerFilter {
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

func (h *gamesHandler) handlePublic(viewerFilter bool) func(w ResponseWriter, r Request) error {
	return func(w ResponseWriter, r Request) error {
		req, err := h.prepare(w, r, nil, viewerFilter)
		if err != nil {
			return err
		}

		return req.handle()
	}
}

func (h gamesHandler) handleOther(w ResponseWriter, r Request) error {
	userId := r.Vars()["user_id"]

	req, err := h.prepare(w, r, &userId, false)
	if err != nil {
		return err
	}

	return req.handle()
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", 401}
	}

	req, err := h.prepare(w, r, &user.Id, false)
	if err != nil {
		return err
	}

	return req.handle()
}

var (
	finishedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-FinishedAt"),
		name:  "finished-games",
		desc:  []string{"Finished games", "Finished games, sorted with newest first."},
		route: ListFinishedGamesRoute,
	}
	startedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("StartedAt"),
		name:  "started-games",
		desc:  []string{"Started games", "Started games, sorted with oldest first."},
		route: ListStartedGamesRoute,
	}
	// The reason we have both openGamesHandler and stagingGamesHandler is because in theory we could have
	// started games in openGamesHandler - if we had a replacement mechanism.
	openGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Closed=", false).Order("-NMembers").Order("CreatedAt"),
		name:  "open-games",
		desc:  []string{"Open games", "Open games, sorted with fullest and oldest first."},
		route: ListOpenGamesRoute,
	}
	stagingGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", false).Order("-NMembers").Order("CreatedAt"),
		name:  "my-staging-games",
		desc:  []string{"My staging games", "Unstarted games I'm a member of, sorted with fullest and oldest first."},
		route: ListMyStagingGamesRoute,
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
	OAuth      *auth.OAuth
	FCMConf    *FCMConf
	SendGrid   *SendGrid
	Superusers *auth.Superusers
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
	if conf.Superusers != nil {
		if err := auth.SetSuperusers(ctx, conf.Superusers); err != nil {
			return err
		}
	}
	return nil
}

func handleFixNewTimestamps(w ResponseWriter, r Request) error {
	dryRun := r.Req().URL.Query().Get("dry-run") != "false"

	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", 401}
	}

	superusers, err := auth.GetSuperusers(ctx)
	if err != nil {
		return err
	}

	if !superusers.Includes(user.Id) {
		return HTTPErr{"unauthorized", 403}
	}

	gameIDs, err := datastore.NewQuery(gameKind).KeysOnly().GetAll(ctx, nil)
	if err != nil {
		return err
	}

	numProcessed := 0
	for numSeen, gameID := range gameIDs {
		log.Infof(ctx, "Looking at game %v, processed %v", numSeen, numProcessed)
		if dryRun && numSeen > 10 {
			break
		}
		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			game := &Game{}
			if err := datastore.Get(ctx, gameID, game); err != nil {
				return err
			}
			if game.StartedAt.IsZero() {
				phases := Phases{}
				phaseIDs, err := datastore.NewQuery(phaseKind).Ancestor(gameID).GetAll(ctx, &phases)
				if err != nil {
					return nil
				}
				if len(phases) > 0 {
					keys := []*datastore.Key{gameID}
					values := []interface{}{game}
					sort.Sort(phases)
					for i := range phases {
						phase := &phases[i]
						if phase.PhaseOrdinal != int64(i+1) {
							return fmt.Errorf("WTF, the phases aren't sorted properly? Phase %v is %+v", i, phase)
						}
						if phase.CreatedAt.IsZero() {
							// If this is the first phase OR DeadlineAt - prevPhase.CreatedAt > phase length.
							if i == 0 || phase.DeadlineAt.Sub(phases[i-1].CreatedAt) > time.Minute*game.PhaseLengthMinutes {
								phase.CreatedAt = phase.DeadlineAt.Add(-time.Minute * game.PhaseLengthMinutes)
								log.Infof(ctx, "Updating phase %v of game with phase length %v, with DeadlineAt %v and no previous phase within %v to have CreatedAt %v", phase.PhaseOrdinal, time.Minute*game.PhaseLengthMinutes, phase.DeadlineAt, time.Minute*game.PhaseLengthMinutes, phase.CreatedAt)
							} else {
								phase.CreatedAt = phase.DeadlineAt
								log.Infof(ctx, "Updating phase %v of game with phase length %v, with DeadlineAt %v and another phase %v before it to have CreatedAt %v", phase.PhaseOrdinal, time.Minute*game.PhaseLengthMinutes, phase.DeadlineAt, phases[i-1].CreatedAt, phase.CreatedAt)
							}
							if i > 0 {
								phases[i-1].ResolvedAt = phase.CreatedAt
								log.Infof(ctx, "Updating phase %v of game with next phase CreatedAt %v to have ResolvedAt %v", phases[i-1].PhaseOrdinal, phase.CreatedAt, phases[i-1].ResolvedAt)
							}
							keys = append(keys, phaseIDs[i])
							values = append(values, phase)
						}
					}
					if game.Finished {
						lastPhase := &phases[len(phases)-1]
						lastPhase.ResolvedAt = lastPhase.CreatedAt
						log.Infof(ctx, "Updating phase %v of finished game with CreatedAt %v to have ResolvedAt %v", lastPhase.PhaseOrdinal, lastPhase.CreatedAt, lastPhase.ResolvedAt)
					}
					game.StartedAt = phases[0].CreatedAt
					log.Infof(ctx, "Updating game with phase length %v and deadlines between %v and %v to have StartedAt %v", time.Minute*game.PhaseLengthMinutes, phases[0].DeadlineAt, phases[len(phases)-1].DeadlineAt, game.StartedAt)

					if !dryRun {
						if _, err := datastore.PutMulti(ctx, keys, values); err != nil {
							return err
						}
					}
					numProcessed++
				}
			}
			return nil
		}, &datastore.TransactionOptions{XG: false}); err != nil {
			return err
		}
	}

	return nil
}

func SetupRouter(r *mux.Router) {
	router = r
	Handle(r, "/_fix_new_timestamps", []string{"GET"}, FixNewTimestampsRoute, handleFixNewTimestamps)
	Handle(r, "/_configure", []string{"POST"}, ConfigureRoute, handleConfigure)
	Handle(r, "/_re-rate", []string{"GET"}, ReRateRoute, handleReRate)
	Handle(r, "/_ah/mail/{recipient}", []string{"POST"}, ReceiveMailRoute, receiveMail)
	Handle(r, "/", []string{"GET"}, IndexRoute, handleIndex)
	Handle(r, "/Game/{game_id}/Channels", []string{"GET"}, ListChannelsRoute, listChannels)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/_dev_resolve_timeout", []string{"GET"}, DevResolvePhaseTimeoutRoute, devResolvePhaseTimeout)
	Handle(r, "/User/{user_id}/Stats/_dev_update", []string{"PUT"}, DevUserStatsUpdateRoute, devUserStatsUpdate)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Map", []string{"GET"}, RenderPhaseMapRoute, renderPhaseMap)
	Handle(r, "/GlobalStats", []string{"GET"}, GlobalStatsRoute, handleGlobalStats)
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
