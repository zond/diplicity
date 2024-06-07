package game

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/variants"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine/v2"
	"google.golang.org/appengine/v2/datastore"
	"google.golang.org/appengine/v2/log"

	dipVariants "github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
)

var (
	router                   = mux.NewRouter()
	reScoreFunc              *DelayFunc
	reSaveFunc               *DelayFunc
	reGameResultFunc         *DelayFunc
	ejectMemberFunc          *DelayFunc
	recalculateDIASUsersFunc *DelayFunc
	updateAllUserStatsFunc   *DelayFunc
	containerGenerators      = map[string]func() interface{}{
		gameKind:        func() interface{} { return &Game{} },
		gameResultKind:  func() interface{} { return &GameResult{} },
		phaseResultKind: func() interface{} { return &PhaseResult{} },
	}

	AllocationResource *Resource
)

func init() {
	reSaveFunc = NewDelayFunc("game-reSave", reSave)
	reScoreFunc = NewDelayFunc("game-reScore", reScore)
	reGameResultFunc = NewDelayFunc("game-reGameResult", reGameResult)
	ejectMemberFunc = NewDelayFunc("game-ejectMember", ejectMember)
	recalculateDIASUsersFunc = NewDelayFunc("game-reCalculateDIASUsers", recalculateDIASUsers)
	updateAllUserStatsFunc = NewDelayFunc("game-updateAllUserStats", updateAllUserStats)
	AllocationResource = &Resource{
		Create:      createAllocation,
		RenderLinks: true,
	}
}

const (
	maxLimit                    = 128
	MAX_STAGING_GAME_INACTIVITY = 30 * 24 * time.Hour
	DiplicitySender             = "Diplicity"
)

const (
	GetSWJSRoute                        = "GetSWJS"
	GetMainJSRoute                      = "GetMainJS"
	ConfigureRoute                      = "AuthConfigure"
	IndexRoute                          = "Index"
	ListOpenGamesRoute                  = "ListOpenGames"
	ListStartedGamesRoute               = "ListStartedGames"
	ListFinishedGamesRoute              = "ListFinishedGames"
	ListMasteredStagingGamesRoute       = "ListMasteredStagingGames"
	ListMasteredStartedGamesRoute       = "ListMasteredStartedGames"
	ListMasteredFinishedGamesRoute      = "ListMasteredFinishedGames"
	ListMyStagingGamesRoute             = "ListMyStagingGames"
	ListMyStartedGamesRoute             = "ListMyStartedGames"
	ListMyFinishedGamesRoute            = "ListMyFinishedGames"
	ListOtherStagingGamesRoute          = "ListOtherStagingGames"
	ListOtherStartedGamesRoute          = "ListOtherStartedGames"
	ListOtherFinishedGamesRoute         = "ListOtherFinishedGames"
	ListOrdersRoute                     = "ListOrders"
	ListPhasesRoute                     = "ListPhases"
	ListPhaseStatesRoute                = "ListPhaseStates"
	ListGameStatesRoute                 = "ListGameStates"
	ListOptionsRoute                    = "ListOptions"
	ListChannelsRoute                   = "ListChannels"
	ListMessagesRoute                   = "ListMessages"
	ListBansRoute                       = "ListBans"
	ListTopRatedPlayersRoute            = "ListTopRatedPlayers"
	ListTopReliablePlayersRoute         = "ListTopReliablePlayers"
	ListTopHatedPlayersRoute            = "ListTopHatedPlayers"
	ListTopHaterPlayersRoute            = "ListTopHaterPlayers"
	ListTopQuickPlayersRoute            = "ListTopQuickPlayers"
	ListFlaggedMessagesRoute            = "ListFlaggedMessages"
	ListGameResultTrueSkillsRoute       = "ListGameResultTrueSkills"
	DevResolvePhaseTimeoutRoute         = "DevResolvePhaseTimeout"
	DevUserStatsUpdateRoute             = "DevUserStatsUpdate"
	ReceiveMailRoute                    = "ReceiveMail"
	RenderPhaseMapRoute                 = "RenderPhaseMap"
	ReGameResultRoute                   = "ReGameResult"
	ReScoreRoute                        = "ReScore"
	ReRateTrueSkillsRoute               = "ReRateTrueSkills"
	UpdateAllUserStatsRoute             = "UpdateAllUserStats"
	DeleteTrueSkillsRoute               = "DeleteTrueSkills"
	GlobalStatsRoute                    = "GlobalStats"
	RssRoute                            = "Rss"
	ReSaveRoute                         = "ReSave"
	AllocateNationsRoute                = "AllocateNations"
	ReapInactiveWaitingPlayersRoute     = "ReapInactiveWaitingPlayersRoute"
	TestReapInactiveWaitingPlayersRoute = "TestReapInactiveWaitingPlayersRoute"
	ReScheduleRoute                     = "ReSchedule"
	ReScheduleAllBrokenRoute            = "ReScheduleAllBroken"
	ReScheduleAllRoute                  = "ReScheduleAll"
	RemoveDIASFromSoloGamesRoute        = "RemoveDIASFromSoloGamesRoute"
	ReComputeAllDIASUsersRoute          = "ReComputeAllDIASUsers"
	SendSystemMessageRoute              = "SendSystemMessage"
	RemoveZippedOptionsRoute            = "RemoveZippedOptions"
	CorroboratePhaseRoute               = "CorroboratePhase"
	CreateAndCorroborateRoute           = "CreateAndCorroborate"
	GetUserRatingHistogramRoute         = "GetUserRatingHistogram"
	GlobalSystemMessageRoute            = "GlobalSystemMessage"
	MusterAllRunningGamesRoute          = "MusterAllRunningGames"
	MusterAllFinishedGamesRoute         = "MusterAllFinishedGame"
	FindBadlyResetGamesRoute            = "FindBadlyResetGames"
	FixBrokenlyMusteredGamesRoute       = "FixBrokenlyMusteredGames"
	FindBrokenNewestPhaseMetaRoute      = "FindBrokenNewestPhaseMeta"
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
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
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

type handlerScope int

const (
	scopeMember handlerScope = iota
	scopeOtherIsMember
	scopePublic
	scopeGameMaster
)

type handlerJoinability int

const (
	joinabilityOpen handlerJoinability = iota
	joinabilityClosed
)

type gamesHandler struct {
	query       *datastore.Query
	name        string
	desc        []string
	route       string
	scope       handlerScope
	joinability handlerJoinability
}

type gamesReq struct {
	ctx                context.Context
	w                  ResponseWriter
	r                  Request
	user               *auth.User
	userStats          *UserStats
	iter               *datastore.Iterator
	limit              int
	h                  *gamesHandler
	detailFilters      []func(g *Game) bool
	viewerStatsFilter  bool
	viewerBanFilter    bool
	viewerFilterRemove bool
}

/*
 * handle uses the generating and filtering setup done by the gamesHandler
 * to generate the next batch (according to cursor and limit)
 * of games to return.
 */
func (req *gamesReq) handle() error {
	var err error
	games := make(Games, 0, req.limit)
	for err == nil && len(games) < req.limit {
		var nextBatch Games
		nextBatch, err = req.h.fetch(req.iter, req.limit-len(games))
		// Remove those not matching programmatic filters.
		nextBatch.RemoveCustomFiltered(req.detailFilters)
		// Mark failed requirements for games if required.
		if req.viewerStatsFilter {
			nextBatch.RemoveFiltered(toJoin, req.userStats, req.viewerFilterRemove)
		}
		// Mark bans for games if required, and remove them if required.
		if req.viewerBanFilter {
			if _, filtErr := nextBatch.RemoveBanned(req.ctx, req.user.Id, req.viewerFilterRemove); filtErr != nil {
				return filtErr
			}
		}
		games = append(games, nextBatch...)
	}
	if err != nil && err != datastore.Done {
		return err
	}

	curs, err := req.cursor(err)
	if err != nil {
		return err
	}

	req.w.SetContent(games.Item(req.r, req.user, curs, req.limit, req.h.name, req.h.desc, req.h.route))
	return nil
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

func (req *gamesReq) boolFilter(fieldName, paramName string, q *datastore.Query) *datastore.Query {
	parm := req.r.Req().URL.Query().Get(paramName)
	if parm == "" {
		return q
	}

	return q.Filter(fmt.Sprintf("%s=", fieldName), parm == "true")
}

func (req *gamesReq) intervalFilter(ctx context.Context, fieldName, paramName string) func(*Game) bool {
	parm := req.r.Req().URL.Query().Get(paramName)
	if parm == "" {
		return nil
	}

	parts := strings.Split(parm, ":")
	if len(parts) != 2 {
		return nil
	}

	var min *float64 = nil
	var max *float64 = nil

	mi, err := strconv.ParseFloat(parts[0], 64)
	if err == nil {
		min = &mi
	}

	ma, err := strconv.ParseFloat(parts[1], 64)
	if err == nil {
		max = &ma
	}

	if min == nil && max == nil {
		return nil
	}

	return func(g *Game) bool {
		cmpField := reflect.ValueOf(g).Elem().FieldByName(fieldName)
		var cmp float64
		if cmpField.Kind() == reflect.Float32 || cmpField.Kind() == reflect.Float64 {
			cmp = cmpField.Float()
		} else {
			cmp = float64(cmpField.Int())
		}
		if min != nil && cmp < *min {
			return false
		}
		if max != nil && cmp > *max {
			return false
		}
		return true
	}
}

/*
 * WARNING: If you add filtering here, you should both add it to the gameListerParams in game.go
 *          and add some testing in diptest/game_test.go/TestGameListFilters and /TestIndexCreation.
 */
func (h *gamesHandler) handle(w ResponseWriter, r Request) error {
	req := &gamesReq{
		ctx:                appengine.NewContext(r.Req()),
		w:                  w,
		r:                  r,
		h:                  h,
		viewerStatsFilter:  h.joinability == joinabilityOpen,
		viewerBanFilter:    h.joinability == joinabilityOpen,
		viewerFilterRemove: h.joinability == joinabilityOpen && h.scope == scopePublic,
	}

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}
	req.user = user

	userStats := &UserStats{}
	if err := datastore.Get(req.ctx, UserStatsID(req.ctx, user.Id), userStats); err == datastore.ErrNoSuchEntity {
		userStats.UserId = user.Id
	} else if err != nil {
		return err
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
	switch h.scope {
	case scopeMember:
		q = q.Filter("Members.User.Id=", user.Id)
	case scopeOtherIsMember:
		q = q.Filter("Members.User.Id=", r.Vars()["user_id"])
	case scopePublic:
		q = q.Filter("Private=", false)
	case scopeGameMaster:
		q = q.Filter("GameMaster.Id=", user.Id)
	default:
		return HTTPErr{fmt.Sprintf("unrecognized scope %v", h.scope), http.StatusInternalServerError}
	}

	apiLevel := auth.APILevel(r)
	req.detailFilters = append(req.detailFilters, func(g *Game) bool {
		if launchLevel, found := variants.LaunchSchedule[g.Variant]; found {
			return apiLevel >= launchLevel
		}
		return true
	})
	if variantFilter := uq.Get("variant"); variantFilter != "" {
		q = q.Filter("Variant=", variantFilter)
	}
	if allocFilter := uq.Get("nation-allocation"); allocFilter != "" {
		wantedAlloc, err := strconv.Atoi(allocFilter)
		if err == nil {
			q = q.Filter("NationAllocation=", wantedAlloc)
		}
	}
	q = req.boolFilter("DisableConferenceChat", "conference-chat-disabled", q)
	q = req.boolFilter("DisableGroupChat", "group-chat-disabled", q)
	q = req.boolFilter("DisablePrivateChat", "private-chat-disabled", q)
	q = req.boolFilter("Private", "only-private", q)
	if f := req.intervalFilter(req.ctx, "PhaseLengthMinutes", "phase-length-minutes"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "NonMovementPhaseLengthMinutes", "non-movement-phase-length-minutes"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "MinReliability", "min-reliability"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "MinQuickness", "min-quickness"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "MaxHater", "max-hater"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "MaxHated", "max-hated"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "MinRating", "min-rating"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}
	if f := req.intervalFilter(req.ctx, "MaxRating", "max-rating"); f != nil {
		req.detailFilters = append(req.detailFilters, f)
	}

	cursor := uq.Get("cursor")
	if cursor == "" {
		req.iter = q.Run(req.ctx)
		return req.handle()
	}

	decoded, err := datastore.DecodeCursor(cursor)
	if err != nil {
		return err
	}
	req.iter = q.Start(decoded).Run(req.ctx)
	return req.handle()
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

func addGamesHandlerLink(r Request, item *Item, handler *gamesHandler) *Item {
	return item.AddLink(r.NewLink(Link{
		Rel:   handler.name,
		Route: handler.route,
	}))
}

var (
	finishedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-FinishedAt"),
		name:        "finished-games",
		desc:        []string{"Finished games", "Public finished games, sorted with newest first."},
		route:       ListFinishedGamesRoute,
		scope:       scopePublic,
		joinability: joinabilityClosed,
	}
	startedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("-StartedAt"),
		name:        "started-games",
		desc:        []string{"Started games", "Public started games, sorted with oldest first."},
		route:       ListStartedGamesRoute,
		scope:       scopePublic,
		joinability: joinabilityClosed,
	}
	openGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Closed=", false).Order("StartETA"),
		name:        "open-games",
		desc:        []string{"Open games", "Public open games, sorted with those expected to start soonest first."},
		route:       ListOpenGamesRoute,
		scope:       scopePublic,
		joinability: joinabilityOpen,
	}
	myFinishedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-FinishedAt"),
		name:        "my-finished-games",
		desc:        []string{"My finished games", "Finished games you are a member of, sorted with newest first."},
		route:       ListMyFinishedGamesRoute,
		scope:       scopeMember,
		joinability: joinabilityClosed,
	}
	myStartedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("-StartedAt"),
		name:        "my-started-games",
		desc:        []string{"My started games", "Started games you are a member of, sorted with oldest first."},
		route:       ListMyStartedGamesRoute,
		scope:       scopeMember,
		joinability: joinabilityClosed,
	}
	myStagingGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", false).Order("StartETA"),
		name:        "my-staging-games",
		desc:        []string{"My staging games", "Unstarted games you are a member of, sorted with those expected to start soonest first."},
		route:       ListMyStagingGamesRoute,
		scope:       scopeMember,
		joinability: joinabilityClosed,
	}
	masteredStagingGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", false).Order("StartETA"),
		name:        "mastered-staging-games",
		desc:        []string{"Mastered staging games", "Unstarted games you are game master of, sorted with those expected to start soonest first."},
		route:       ListMasteredStagingGamesRoute,
		scope:       scopeGameMaster,
		joinability: joinabilityOpen,
	}
	masteredFinishedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-FinishedAt"),
		name:        "mastered-finished-games",
		desc:        []string{"Mastered finished games", "Finished games you are game master of, sorted with newest first."},
		route:       ListMasteredFinishedGamesRoute,
		scope:       scopeGameMaster,
		joinability: joinabilityClosed,
	}
	masteredStartedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("-StartedAt"),
		name:        "mastered-started-games",
		desc:        []string{"Mastered started games", "Started games you are game master of, sorted with oldest first."},
		route:       ListMasteredStartedGamesRoute,
		scope:       scopeGameMaster,
		joinability: joinabilityOpen,
	}
	otherMemberStagingGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", false).Order("StartETA"),
		name:        "other-member-staging-games",
		desc:        []string{"Other member staging games", "Unstarted games someone else is a member of, sorted with those expected to start soonest first."},
		route:       ListOtherStagingGamesRoute,
		scope:       scopeOtherIsMember,
		joinability: joinabilityOpen,
	}
	otherMemberFinishedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-FinishedAt"),
		name:        "other-member-finished-games",
		desc:        []string{"Other member finished games", "Finished games someone else is a member of, sorted with newest first."},
		route:       ListOtherFinishedGamesRoute,
		scope:       scopeOtherIsMember,
		joinability: joinabilityClosed,
	}
	otherMemberStartedGamesHandler = &gamesHandler{
		query:       datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("-StartedAt"),
		name:        "other-member-started-games",
		desc:        []string{"Other member started games", "Started games someone else is a member of, sorted with oldest first."},
		route:       ListOtherStartedGamesRoute,
		scope:       scopeOtherIsMember,
		joinability: joinabilityOpen,
	}
	topRatedPlayersHandler = userStatsHandler{
		query: datastore.NewQuery(userStatsKind).Order("-TrueSkill.Rating"),
		name:  "top-rated-players",
		desc:  []string{"Top rated alayers", "Players sorted by TrueSkill rating"},
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

type AllocationMember struct {
	Prefs  godip.Nations `methods:"POST"`
	Result godip.Nation
}

func (a AllocationMember) Preferences() godip.Nations {
	result := godip.Nations{}
	for _, preference := range a.Prefs {
		result = append(result, godip.Nation(preference))
	}
	return result
}

type AllocationMembers []AllocationMember

func (a AllocationMembers) Len() int {
	return len(a)
}

func (a AllocationMembers) Each(f func(int, Preferer)) {
	for idx, member := range a {
		f(idx, member)
	}
}

type Allocation struct {
	Members AllocationMembers `methods:"POST"`
	Variant string            `methods:"POST"`
}

func (a *Allocation) Item(r Request) *Item {
	allocationItem := NewItem(a).SetName("Allocation")
	return allocationItem
}

func createAllocation(w ResponseWriter, r Request) (*Allocation, error) {
	ctx := appengine.NewContext(r.Req())

	a := &Allocation{}
	err := Copy(a, r, "POST")
	if err != nil {
		return nil, err
	}
	variant, found := dipVariants.Variants[a.Variant]
	if !found {
		return nil, HTTPErr{fmt.Sprintf("variant %q not found", a.Variant), http.StatusNotFound}
	}
	log.Infof(ctx, "Allocating for %+v, %+v", a, variant.Nations)
	alloc, err := AllocateNations(a.Members, variant.Nations)
	if err != nil {
		return nil, err
	}
	for memberIdx := range a.Members {
		a.Members[memberIdx].Result = alloc[memberIdx]
	}
	return a, nil
}

type configuration struct {
	OAuth                 *auth.OAuth
	FCMConf               *FCMConf
	SendGrid              *auth.SendGrid
	Superusers            *auth.Superusers
	DiscordBotCredentials *auth.DiscordBotCredentials
}

func handleConfigure(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())
	log.Infof(ctx, "handleConfigure called")
	fmt.Printf("handleConfigure called")

	conf := &configuration{}
	log.Infof(ctx, "handleConfigure called with %+v", conf)
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
		if err := auth.SetSendGrid(ctx, conf.SendGrid); err != nil {
			return err
		}
	}
	if conf.Superusers != nil {
		if err := auth.SetSuperusers(ctx, conf.Superusers); err != nil {
			return err
		}
	}
	fmt.Printf("DiscordBotCredentials: %+v", conf.DiscordBotCredentials)
	log.Infof(ctx, "DiscordBotCredentials: %+v", conf.DiscordBotCredentials)
	if conf.DiscordBotCredentials != nil {
		if err := auth.SetDiscordBotCredentials(ctx, conf.DiscordBotCredentials); err != nil {
			return err
		}
	}
	return nil
}

func reGameResult(ctx context.Context, withRepair bool, counter int, valid int, invalid int, cursorString string) error {
	log.Infof(ctx, "reGameResult(..., %v, %v, %v, %v, %q)", withRepair, counter, valid, invalid, cursorString)

	q := datastore.NewQuery(gameResultKind)
	if cursorString != "" {
		cursor, err := datastore.DecodeCursor(cursorString)
		if err != nil {
			return err
		}
		q = q.Start(cursor)
	}
	iterator := q.Run(ctx)

	gameResult := &GameResult{}
	if _, err := iterator.Next(gameResult); err == datastore.Done {
		log.Infof(ctx, "reGameResult(..., %v, %v, %v, %v, %q) is DONE", withRepair, counter, valid, invalid, cursorString)
		return nil
	} else if err != nil {
		return err
	}

	game := &Game{ID: gameResult.GameID}
	if err := datastore.Get(ctx, gameResult.GameID, game); err != nil {
		return err
	}

	if err := gameResult.Validate(game); err != nil {
		log.Errorf(ctx, "Loaded invalid GameResult: %v", err)
		if withRepair {
			if err = gameResult.Repair(ctx, game); err != nil {
				log.Errorf(ctx, "Unable to repair GameResult: %v", err)
				return err
			}
			log.Infof(ctx, "Reparied the game result!")
		}
		invalid += 1
	} else {
		valid += 1
	}

	cursor, err := iterator.Cursor()
	if err != nil {
		return err
	}
	return reGameResultFunc.EnqueueIn(ctx, 0, withRepair, counter+1, valid, invalid, cursor.String())
}

func updateAllUserStats(ctx context.Context, counter int, cursorString string) error {
	log.Infof(ctx, "updateAllUserStats(..., %v, %q)", counter, cursorString)

	q := datastore.NewQuery(userStatsKind).KeysOnly()
	if cursorString != "" {
		cursor, err := datastore.DecodeCursor(cursorString)
		if err != nil {
			return err
		}
		q = q.Start(cursor)
	}
	iterator := q.Run(ctx)

	id, err := iterator.Next(nil)
	if err == datastore.Done {
		log.Infof(ctx, "updateAllUserStats(..., %v, %q) is DONE", counter, cursorString)
		return nil
	} else if err != nil {
		return err
	}

	if err := updateUserStat(ctx, id.StringID()); err != nil {
		return err
	}

	cursor, err := iterator.Cursor()
	if err != nil {
		return err
	}
	return updateAllUserStatsFunc.EnqueueIn(ctx, 0, counter+1, cursor.String())
}

func reScore(ctx context.Context, counter int, cursorString string) error {
	log.Infof(ctx, "reScore(..., %v, %q)", counter, cursorString)

	q := datastore.NewQuery(gameResultKind)
	if cursorString != "" {
		cursor, err := datastore.DecodeCursor(cursorString)
		if err != nil {
			return err
		}
		q = q.Start(cursor)
	}
	iterator := q.Run(ctx)

	gameResult := &GameResult{}
	if _, err := iterator.Next(gameResult); err == datastore.Done {
		log.Infof(ctx, "reScore(..., %v, %q) is DONE", counter, cursorString)
		return nil
	} else if err != nil {
		return err
	}

	gameResult.AssignScores()

	game := &Game{ID: gameResult.GameID}
	if err := datastore.Get(ctx, gameResult.GameID, game); err != nil {
		return err
	}

	if err := gameResult.DBSave(ctx, game); err != nil {
		return err
	}

	cursor, err := iterator.Cursor()
	if err != nil {
		return err
	}
	return reScoreFunc.EnqueueIn(ctx, 0, counter+1, cursor.String())
}

func reSave(ctx context.Context, kind string, counter int, cursorString string) error {
	log.Infof(ctx, "reSave(..., %q, %v, %q)", kind, counter, cursorString)

	containerGenerator, found := containerGenerators[kind]
	if !found {
		return fmt.Errorf("Kind %q not supported by reSave", kind)
	}

	batchSize := 20

	q := datastore.NewQuery(kind).KeysOnly()
	if cursorString != "" {
		cursor, err := datastore.DecodeCursor(cursorString)
		if err != nil {
			return err
		}
		q = q.Start(cursor)
	}
	iterator := q.Run(ctx)

	processed := 0
	containerID, err := iterator.Next(nil)
	for ; err == nil && processed < batchSize; containerID, err = iterator.Next(nil) {
		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			container := containerGenerator()
			if err := datastore.Get(ctx, containerID, container); err != nil {
				return err
			}
			val := reflect.ValueOf(container)
			if field := val.Elem().FieldByName("ID"); field.IsValid() && reflect.TypeOf(containerID).AssignableTo(field.Type()) {
				field.Set(reflect.ValueOf(containerID))
			}
			typ := reflect.TypeOf(container)
			meth, ok := typ.MethodByName("Save")
			if ok && meth.Type.NumIn() == 2 && reflect.TypeOf(ctx).AssignableTo(meth.Type.In(1)) {
				out := meth.Func.Call([]reflect.Value{val, reflect.ValueOf(ctx)})
				if len(out) > 0 {
					if out[len(out)-1].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
						errVal := out[len(out)-1]
						if !errVal.IsNil() {
							return errVal.Interface().(error)
						}
					}
				}
				log.Infof(ctx, "Processed %v via Save(ctx)", containerID)
			} else {
				if _, err := datastore.Put(ctx, containerID, container); err != nil {
					return err
				}
				log.Infof(ctx, "Processed %v via datastore.Put(ctx, ...)", containerID)
			}
			return nil
		}, &datastore.TransactionOptions{XG: false}); err != nil {
			log.Errorf(ctx, "Failed to process %v: %v", containerID, err)
			return err
		}
		processed++
		counter++
	}

	if err == nil {
		cursor, err := iterator.Cursor()
		if err != nil {
			return err
		}
		if err := reSaveFunc.EnqueueIn(ctx, 0, kind, counter, cursor.String()); err != nil {
			return err
		}
	} else if err != datastore.Done {
		return err
	}

	return nil
}

func handleReGameResult(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	return reGameResultFunc.EnqueueIn(ctx, 0, r.Req().URL.Query().Get("with-repair") == "true", 0, 0, 0, "")
}

func handleUpdateAllUserStats(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	return updateAllUserStatsFunc.EnqueueIn(ctx, 0, 0, "")
}

func handleReScore(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	return reScoreFunc.EnqueueIn(ctx, 0, 0, "")
}

func handleReSave(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	kind := r.Req().URL.Query().Get("kind")

	_, found := containerGenerators[kind]
	if !found {
		return fmt.Errorf("Kind %q not supported by reSave", kind)
	}

	return reSaveFunc.EnqueueIn(ctx, 0, kind, 0, "")
}

func reScheduleAll(w ResponseWriter, r Request, onlyBroken bool) error {
	ctx := appengine.NewContext(r.Req())

	log.Infof(ctx, "reScheduleAll(..., %v)", onlyBroken)

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	log.Infof(ctx, "Authorized!")

	games := Games{}
	ids, err := datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx, id := range ids {
		games[idx].ID = id
	}
	log.Infof(ctx, "Found %v started and unfinished games.", len(games))
	for _, game := range games {
		if len(game.NewestPhaseMeta) > 0 {
			if !onlyBroken || (game.NewestPhaseMeta[0].DeadlineAt.Before(time.Now()) && !game.NewestPhaseMeta[0].Resolved) {
				log.Infof(ctx, "Rescheduling %+v", game)
				if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
					phaseID, err := PhaseID(ctx, game.ID, game.NewestPhaseMeta[0].PhaseOrdinal)
					if err != nil {
						return err
					}
					phase := &Phase{}
					if err := datastore.Get(ctx, phaseID, phase); err != nil {
						return err
					}
					return phase.ScheduleResolution(ctx)
				}, &datastore.TransactionOptions{XG: true}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func handleReScheduleAllBroken(w ResponseWriter, r Request) error {
	return reScheduleAll(w, r, true)
}

func handleReScheduleAll(w ResponseWriter, r Request) error {
	return reScheduleAll(w, r, false)
}

func handleRemoveDIASFromSoloGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	gameResults := GameResults{}
	ids, err := datastore.NewQuery(gameResultKind).GetAll(ctx, &gameResults)
	if err != nil {
		return err
	}
	weirdResults := 0
	uidsToUpdateMap := map[string]bool{}
	for idx := range gameResults {
		gameResult := &gameResults[idx]
		if len(gameResult.DIASMembers) > 0 && gameResult.SoloWinnerMember != "" {
			weirdResults += 1
			for _, diasUid := range gameResult.DIASUsers {
				uidsToUpdateMap[diasUid] = true
			}
			gameResult.DIASMembers = nil
		}
	}
	log.Infof(ctx, "Found %v weird results with DIASMembers _and_ a SoloWinnerMember", weirdResults)
	if _, err := datastore.PutMulti(ctx, ids, gameResults); err != nil {
		return err
	}
	log.Infof(ctx, "Removed DIASMembers from %v results", weirdResults)
	if weirdResults > 0 {
		uidsToUpdate := make([]string, 0, len(uidsToUpdateMap))
		for uidToUpdate := range uidsToUpdateMap {
			uidsToUpdate = append(uidsToUpdate, uidToUpdate)
		}
		if err := UpdateUserStatsASAP(ctx, uidsToUpdate); err != nil {
			return err
		}
		log.Infof(ctx, "Enqueued updating of user stats for %v users", len(uidsToUpdate))
	}
	return nil
}

func diasUsersQuery() *datastore.Query {
	return datastore.NewQuery(userStatsKind).Filter("DIASGames>", 0).KeysOnly()
}

func handleReComputeAllDIASUsers(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	cursor, err := diasUsersQuery().Run(ctx).Cursor()
	if err != nil {
		return err
	}

	return recalculateDIASUsersFunc.EnqueueIn(ctx, 0, cursor.String())
}

func recalculateDIASUsers(ctx context.Context, encodedCursor string) error {
	log.Infof(ctx, "recalculateDIASUsers(..., %#v) called", encodedCursor)

	cursor, err := datastore.DecodeCursor(encodedCursor)
	if err != nil {
		log.Errorf(ctx, "Unable to decode cursor %#v: %v", encodedCursor, err)
		return err
	}

	iterator := diasUsersQuery().Start(cursor).Run(ctx)

	idsToUpdate := []string{}
	for i := 0; i < 50 && err == nil; i++ {
		var id *datastore.Key
		id, err = iterator.Next(nil)
		log.Infof(ctx, "Next produced %v, %v", id, err)
		if err == nil {
			idsToUpdate = append(idsToUpdate, id.StringID())
		}
	}
	log.Infof(ctx, "Found %+v, %v", idsToUpdate, err)
	if err != nil && err != datastore.Done {
		log.Errorf(ctx, "Unable to iterate to next user stat: %v", err)
		return err
	}
	if len(idsToUpdate) > 0 {
		if err := UpdateUserStatsASAP(ctx, idsToUpdate); err != nil {
			log.Errorf(ctx, "Unable to enqueue user stat update: %v", err)
			return err
		}
	}
	if err == nil {
		cursor, err = iterator.Cursor()
		if err != nil {
			log.Errorf(ctx, "Unable to encode new cursor: %v", err)
			return err
		}
		if err := recalculateDIASUsersFunc.EnqueueIn(ctx, 0, cursor.String()); err != nil {
			log.Errorf(ctx, "Unable to enqueue for new cursor: %v", err)
			return err
		}
	}
	log.Infof(ctx, "recalculateDIASUsers(..., %#v) done", encodedCursor)
	return nil
}

var (
	badlyResetGameMembersByGameIDString = map[string][]struct {
		Nation godip.Nation
		Email  string
	}{}
)

func handleFindBadlyResetGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	for gameIDString, resetMembers := range badlyResetGameMembersByGameIDString {
		gameID, err := datastore.DecodeKey(gameIDString)
		if err != nil {
			return err
		}
		log.Infof(ctx, "Looking at broken game %v", gameID)
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID

		madeChanges := false
		for _, resetMember := range resetMembers {
			log.Infof(ctx, "Looking at user %+v", resetMember)
			foundNation := false
			for _, nat := range dipVariants.Variants[game.Variant].Nations {
				if nat == resetMember.Nation {
					foundNation = true
					break
				}
			}
			if !foundNation {
				return fmt.Errorf("No nation for %+v found among %+v", resetMember, dipVariants.Variants[game.Variant].Nations)
			}
			users := []auth.User{}
			_, err := datastore.NewQuery(auth.UserKind).Filter("Email=", resetMember.Email).GetAll(ctx, &users)
			if err != nil {
				return err
			}
			if len(users) != 1 {
				return fmt.Errorf("Found %+v with Email %q, wtf?", users, resetMember.Email)
			}
			user := &users[0]
			hasUser := false
			var foundMember *Member
			for idx := range game.Members {
				if game.Members[idx].User.Id == user.Id {
					hasUser = true
				}
				if game.Members[idx].Nation == resetMember.Nation {
					foundMember = &game.Members[idx]
				}
			}
			if hasUser {
				log.Infof(ctx, "%+v already found among %+v", resetMember, game.Members)
				continue
			}
			if foundMember == nil {
				return fmt.Errorf("Didn't found a member of the right nation for %+v among %+v, what's wrong?", resetMember, game.Members)
			}
			foundMember.User = *user
			madeChanges = true
			log.Infof(ctx, "Successfully reinstated %+v among %+v", resetMember, game.Members)
		}
		members := game.Members

		if madeChanges {
			if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
				game = &Game{}
				if err := datastore.Get(ctx, gameID, game); err != nil {
					return err
				}
				game.ID = gameID
				game.Members = members

				phases := Phases{}
				if _, err := datastore.NewQuery(phaseKind).Ancestor(gameID).GetAll(ctx, &phases); err != nil {
					return err
				}
				var lastPhase *Phase
				for idx := range phases {
					if lastPhase == nil || phases[idx].PhaseOrdinal > lastPhase.PhaseOrdinal {
						lastPhase = &phases[idx]
					}
				}
				lastPhase.DeadlineAt = time.Now().Add(time.Hour * 24 * 3)
				if err := lastPhase.ScheduleResolution(ctx); err != nil {
					return err
				}
				log.Infof(ctx, "Schedule %v to resolve at %v", gameID, lastPhase.DeadlineAt)

				game.NewestPhaseMeta = []PhaseMeta{lastPhase.PhaseMeta}

				if _, err := datastore.Put(ctx, gameID, game); err != nil {
					return err
				}
				log.Infof(ctx, "Successfully saved %v with the reinstated members and a new NewestPhaseMeta (%+v)", gameID, game.NewestPhaseMeta)
				return nil
			}, &datastore.TransactionOptions{XG: true}); err != nil {
				return err
			}
		}

	}
	return nil
}

func handleFixBrokenlyMusteredGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	games := Games{}
	ids, err := datastore.NewQuery(gameKind).Filter("Finished=", true).Filter("Mustered=", true).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx := range games {
		games[idx].ID = ids[idx]
		result := &GameResult{}
		resultID := GameResultID(ctx, games[idx].ID)
		if err := datastore.Get(ctx, resultID, result); err != nil {
			log.Errorf(ctx, "unable to load game result: %v", err)
			return err
		}
		correctMembers := map[godip.Nation]*Member{}

		for _, score := range result.Scores {
			correctMember := &Member{
				Nation: score.Member,
			}
			correctMembers[score.Member] = correctMember

			if err := datastore.Get(ctx, auth.UserID(ctx, score.UserId), &correctMember.User); err != nil {
				return err
			}

			phaseID, err := PhaseID(ctx, games[idx].ID, games[idx].NewestPhaseMeta[0].PhaseOrdinal)
			if err != nil {
				log.Errorf(ctx, "unable to create phase id: %v", err)
				return err
			}
			phaseStateID, err := PhaseStateID(ctx, phaseID, score.Member)
			if err != nil {
				log.Errorf(ctx, "unable to create phase state id: %v", err)
				return err
			}

			datastore.Get(ctx, phaseStateID, &correctMember.NewestPhaseState)
		}

		correctNations := dipVariants.Variants[games[idx].Variant].Nations
		if len(correctNations) != len(correctMembers) {
			return fmt.Errorf("Generated correct members %+v are of different length than variant nations %+v", correctMembers, correctNations)
		}
		for _, nat := range correctNations {
			if _, found := correctMembers[nat]; !found {
				return fmt.Errorf("Generated correct members %+v doesn't contain correct nation %q", correctMembers, nat)
			}
		}

		isOK := true
		for _, member := range games[idx].Members {
			correctMember, found := correctMembers[member.Nation]
			if !found {
				isOK = false
				break
			}
			if correctMember.Nation != member.Nation {
				isOK = false
				break
			}
			if correctMember.User.Id != member.User.Id {
				isOK = false
				break
			}
		}

		if isOK {
			log.Infof(ctx, "%v seems to have the correct members. Phew!", games[idx].ID)
		} else {
			log.Infof(ctx, "%v doesn't have the correct members!!! Gah!", games[idx].ID)
			games[idx].Members = nil
			for _, correctMember := range correctMembers {
				games[idx].Members = append(games[idx].Members, *correctMember)
			}
			if len(games[idx].Members) != len(correctNations) {
				return fmt.Errorf("New generated members %+v doesn't have the same length as correct variant nations %+v", games[idx].Members, correctNations)
			}
			if _, err := datastore.Put(ctx, games[idx].ID, &games[idx]); err != nil {
				log.Errorf(ctx, "Unable to store game %+v: %v", games[idx], err)
				return err
			}
			log.Infof(ctx, "%v is updated with the correct members!", games[idx].ID)
		}
	}
	return nil
}

func handleMusterAllFinishedGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	games := Games{}
	ids := []*datastore.Key{}
	saveGamesMustered := func() error {
		for idx := range games {
			if !games[idx].Finished {
				return fmt.Errorf("%+v isn't finished, wtf?", games[idx])
			}
			games[idx].Mustered = true
		}
		if _, err := datastore.PutMulti(ctx, ids, games); err != nil {
			return err
		}
		log.Infof(ctx, "Saved %v finished games as mustered", len(games))
		games = Games{}
		ids = []*datastore.Key{}
		return nil
	}

	iterator := datastore.NewQuery(gameKind).Filter("Finished=", true).Run(ctx)
	count := 0
	for {
		game := &Game{}
		id, err := iterator.Next(game)
		if err == datastore.Done {
			break
		}
		if err != nil {
			return err
		}
		if !game.Mustered {
			game.ID = id
			games = append(games, *game)
			ids = append(ids, id)
			if len(games) == 50 {
				if err := saveGamesMustered(); err != nil {
					return err
				}
			}
		}
		count += 1
		if count%100 == 0 {
			log.Infof(ctx, "Looked at %v games", count)
		}
	}
	return saveGamesMustered()
}

func handleFindBrokenNewestPhaseMeta(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	games := Games{}
	ids, err := datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx := range games {
		games[idx].ID = ids[idx]
		newestPhase, err := newestPhaseForGame(ctx, games[idx].ID)
		if err != nil {
			return err
		}
		if len(games[idx].NewestPhaseMeta) == 0 && newestPhase == nil {
			log.Infof(ctx, "%v is supposed to be started and not finished, but doesn't have a phase?", games[idx].ID.Encode())
			continue
		}
		if (len(games[idx].NewestPhaseMeta) == 0) != (newestPhase == nil) {
			log.Infof(ctx, "%v has %v NewestPhaseMeta's, and we found a newestPhase %p", games[idx].ID.Encode(), len(games[idx].NewestPhaseMeta), newestPhase)
			continue
		}
		if games[idx].NewestPhaseMeta[0].PhaseOrdinal != newestPhase.PhaseOrdinal {
			log.Infof(ctx, "%v has NewestPhaseMeta with ordinal %v, and the newest phase we could find for it had ordinal %v", games[idx].ID.Encode(), games[idx].NewestPhaseMeta[0].PhaseOrdinal, newestPhase.PhaseOrdinal)
			continue
		}
		if !games[idx].Finished && newestPhase.Resolved {
			log.Infof(ctx, "%v isn't finished, but it's newest phase is resolved", games[idx].ID.Encode())
			continue
		}
		log.Infof(ctx, "%v is good!", games[idx].ID)
	}
	return nil
}

func newestPhaseForGame(ctx context.Context, gameID *datastore.Key) (*Phase, error) {
	phases := Phases{}
	if _, err := datastore.NewQuery(phaseKind).Ancestor(gameID).GetAll(ctx, &phases); err != nil {
		return nil, err
	}
	var newestPhase *Phase
	for idx := range phases {
		if newestPhase == nil || newestPhase.PhaseOrdinal < phases[idx].PhaseOrdinal {
			newestPhase = &phases[idx]
		}
	}
	return newestPhase, nil
}

func handleMusterAllRunningGames(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	games := Games{}
	ids, err := datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx := range ids {
		games[idx].ID = ids[idx]
	}

	for idx := range games {
		if games[idx].Mustered {
			log.Infof(ctx, "Not mustering %v since it's already mustered", games[idx].ID)
			continue
		}
		channels := Channels{}
		_, err := datastore.NewQuery(channelKind).Ancestor(games[idx].ID).GetAll(ctx, &channels)
		if err != nil {
			return err
		}
		var foundMessage *Message
		for _, channel := range channels {
			if channel.LatestMessage.Sender == godip.Nation(DiplicitySender) && strings.Contains(channel.LatestMessage.Body, "Welcome to") {
				foundMessage = &channel.LatestMessage
				break
			}
		}
		if foundMessage != nil {
			log.Infof(ctx, "Not mustering %v since I found %+v", games[idx].ID, foundMessage)
			continue
		}
		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			game := &Game{}
			if err := datastore.Get(ctx, games[idx].ID, game); err != nil {
				return err
			}
			game.Mustered = true
			_, err := datastore.Put(ctx, games[idx].ID, game)
			return err
		}, &datastore.TransactionOptions{XG: true}); err != nil {
			return err
		}
		log.Infof(ctx, "Forcefully mustered %v since it was started, not mustered, not finished, and didn't have the welcome message.", games[idx].ID)
	}

	return nil
}

func handleGlobalSystemMessage(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	games := Games{}
	ids, err := datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx := range ids {
		games[idx].ID = ids[idx]

		newMessage := &Message{
			GameID:         games[idx].ID,
			ChannelMembers: dipVariants.Variants[games[idx].Variant].Nations,
			Sender:         DiplicitySender,
			Body:           r.Req().FormValue("body"),
		}

		if r.Req().FormValue("really") == "yes" {
			if err := createMessageHelper(ctx, r.Req().Host, newMessage); err != nil {
				return err
			}
			log.Infof(ctx, "Successfully sent %+v", newMessage)
		} else {
			log.Infof(ctx, "Would have sent %+v", newMessage)
		}
	}

	return nil
}

func handleSendSystemMessage(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}
	game := &Game{}
	if err := datastore.Get(ctx, gameID, game); err != nil {
		return err
	}

	recipients := strings.Split(r.Vars()["recipients"], ",")
	sort.Sort(sort.StringSlice(recipients))
	recipientNations := Nations{}
	for _, recipient := range recipients {
		if !Nations(dipVariants.Variants[game.Variant].Nations).Includes(godip.Nation(recipient)) {
			return HTTPErr{"unknown recipient", http.StatusNotFound}
		}
		recipientNations = append(recipientNations, godip.Nation(recipient))
	}

	newMessage := &Message{
		GameID:         gameID,
		ChannelMembers: recipientNations,
		Sender:         DiplicitySender,
		Body:           r.Req().FormValue("body"),
	}

	return createMessageHelper(ctx, r.Req().Host, newMessage)
}

func handleRemoveZippedOptions(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	log.Infof(ctx, "handleRemoveZippedOptions(...)")

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	gameIDs, err := datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).KeysOnly().GetAll(ctx, nil)
	if err != nil {
		return err
	}
	for _, gameID := range gameIDs {
		log.Infof(ctx, "Looking at %v", gameID)
		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			game := &Game{}
			if err := datastore.Get(ctx, gameID, game); err != nil {
				return err
			}
			game.ID = gameID
			if !game.Started || game.Finished {
				log.Infof(ctx, "Is finished, or not started. Odd.")
				return nil
			}
			for idx := range game.Members {
				game.Members[idx].NewestPhaseState.ZippedOptions = nil
			}
			phaseStates := PhaseStates{}
			phaseStateIDs, err := datastore.NewQuery(phaseStateKind).Ancestor(gameID).Filter("PhaseOrdinal=", game.NewestPhaseMeta[0].PhaseOrdinal).GetAll(ctx, &phaseStates)
			if err != nil {
				return err
			}
			for idx := range phaseStates {
				phaseStates[idx].ZippedOptions = nil
			}
			toSave := []interface{}{game}
			for idx := range phaseStates {
				toSave = append(toSave, &phaseStates[idx])
			}
			keys := []*datastore.Key{gameID}
			keys = append(keys, phaseStateIDs...)
			if _, err := datastore.PutMulti(ctx, keys, toSave); err != nil {
				return err
			}
			log.Infof(ctx, "Successfully cleaned zipped options from %v", gameID)
			return nil
		}, &datastore.TransactionOptions{XG: false}); err != nil {
			return err
		}
	}
	log.Infof(ctx, "handleRemoveZippedOptions(...) DONE")
	return nil
}

func handleReSchedule(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		user, ok := r.Values()["user"].(*auth.User)
		if !ok {
			return HTTPErr{"unauthenticated", http.StatusUnauthorized}
		}

		superusers, err := auth.GetSuperusers(ctx)
		if err != nil {
			return err
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{"unauthorized", http.StatusForbidden}
		}
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
		if err != nil {
			return err
		}
		game := &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		if game.Finished {
			log.Infof(ctx, "%v is already finished, not doing anything more", gameID)
			return nil
		}
		newestPhase, err := newestPhaseForGame(ctx, gameID)
		if err != nil {
			return err
		}
		if newestPhase == nil {
			log.Infof(ctx, "%v has no phases, can't re-schedule.", gameID)
			return nil
		}
		if len(game.NewestPhaseMeta) == 0 {
			log.Infof(ctx, "%v has no NewestPhaseMeta, but we found phase %v. Fixing.", gameID, newestPhase.PhaseOrdinal)
		} else if game.NewestPhaseMeta[0].PhaseOrdinal != newestPhase.PhaseOrdinal {
			log.Infof(ctx, "%v has NewestPhaseMeta %v, but we found phase %v. Fixing.", gameID, game.NewestPhaseMeta[0].PhaseOrdinal, newestPhase.PhaseOrdinal)
		}
		if newestPhase.Resolved {
			log.Infof(ctx, "%v has a newest phase that is already resolved, fixing.", gameID)
			newestPhase.Resolved = false
			phaseID, err := PhaseID(ctx, gameID, newestPhase.PhaseOrdinal)
			if err != nil {
				return err
			}
			if _, err := datastore.Put(ctx, phaseID, newestPhase); err != nil {
				return err
			}
		}
		game.NewestPhaseMeta = []PhaseMeta{newestPhase.PhaseMeta}
		if _, err := datastore.Put(ctx, gameID, game); err != nil {
			return err
		}
		if err := newestPhase.ScheduleResolution(ctx); err != nil {
			return err
		}
		log.Infof(ctx, "Successfully fixed any NewestPhaseMeta- or latest phase already resolved-problems with %v, and rescheduled it to resolve at %v", gameID, newestPhase.DeadlineAt)
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return err
	}
	return nil
}

func handleTestReapInactiveWaitingPlayers(w ResponseWriter, r Request) error {
	if !appengine.IsDevAppServer() {
		return HTTPErr{"unauthorized", http.StatusForbidden}
	}
	return handleReapInactiveWaitingPlayers(w, r)
}

func handleReapInactiveWaitingPlayers(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	games := Games{}
	ids, err := datastore.NewQuery(gameKind).Filter("Started=", false).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	log.Infof(ctx, "Found %v staging games", len(games))
	for idx, id := range ids {
		games[idx].ID = id
	}

	userIdMap := map[string]bool{}
	for _, game := range games {
		for _, member := range game.Members {
			userIdMap[member.User.Id] = true
		}
	}
	log.Infof(ctx, "Found %v users waiting for these games", len(userIdMap))

	userIds := make([]*datastore.Key, 0, len(userIdMap))
	for userId := range userIdMap {
		userIds = append(userIds, auth.UserID(ctx, userId))
	}

	users := make([]auth.User, len(userIds))
	if err := datastore.GetMulti(ctx, userIds, users); err != nil {
		return err
	}

	userMap := map[string]auth.User{}
	for idx, user := range users {
		userMap[userIds[idx].StringID()] = user
	}

	minValidUntil := time.Now().Add(-MAX_STAGING_GAME_INACTIVITY)
	if paramInactivity := r.Req().URL.Query().Get("max-staging-game-inactivity"); paramInactivity != "" {
		parsed, err := strconv.Atoi(paramInactivity)
		if err != nil {
			return err
		}
		minValidUntil = time.Now().Add(time.Duration(-parsed) * time.Second)
	}
	log.Infof(ctx, "Going to eject users with ValidUntil < %v from these games", minValidUntil)

	count := 0
	for _, game := range games {
		for _, member := range game.Members {
			if userMap[member.User.Id].ValidUntil.Before(minValidUntil) {
				log.Infof(ctx, "%q has ValidUntil older than %v, ejecting from %v (%v)", member.User.Email, minValidUntil, game.ID, game.Desc)
				if err := ejectMemberFunc.EnqueueIn(ctx, 0, game.ID, member.User.Id); err != nil {
					return err
				}
				count++
			}
		}
	}
	log.Infof(ctx, "Ejected %v users from waiting games", count)

	return nil
}

func ejectMember(ctx context.Context, gameID *datastore.Key, userId string) error {
	log.Infof(ctx, "ejectMember(..., %v, %v)", gameID, userId)

	if _, err := deleteMemberHelper(ctx, gameID, deleteMemberRequest{systemReq: true, toRemoveId: userId}, true); err != nil {
		log.Errorf(ctx, "deleteMemberHelper(..., %v, %v, %v, true): %v; hope datastore gets fixed", gameID, userId, userId, err)
		return err
	}

	log.Infof(ctx, "ejectMember(..., %v, %v): *** SUCCESS ***", gameID, userId)

	return nil
}

func SetupRouter(r *mux.Router) {
	router = r
	Handle(r, "/_reap-inactive-waiting-players", []string{"GET"}, ReapInactiveWaitingPlayersRoute, handleReapInactiveWaitingPlayers)
	Handle(r, "/_test_reap-inactive-waiting-players", []string{"GET"}, TestReapInactiveWaitingPlayersRoute, handleTestReapInactiveWaitingPlayers)
	Handle(r, "/_re-save", []string{"GET"}, ReSaveRoute, handleReSave)
	Handle(r, "/_configure", []string{"POST"}, ConfigureRoute, handleConfigure)
	Handle(r, "/_delete-true-skills", []string{"GET"}, DeleteTrueSkillsRoute, handleDeleteTrueSkills)
	Handle(r, "/_re-rate-true-skills", []string{"GET"}, ReRateTrueSkillsRoute, handleReRateTrueSkills)
	Handle(r, "/_re-score", []string{"GET"}, ReScoreRoute, handleReScore)
	Handle(r, "/_update-all-user-stats", []string{"GET"}, UpdateAllUserStatsRoute, handleUpdateAllUserStats)
	Handle(r, "/_re-game-result", []string{"GET"}, ReGameResultRoute, handleReGameResult)
	Handle(r, "/Game/{game_id}/_re-schedule", []string{"GET"}, ReScheduleRoute, handleReSchedule)
	Handle(r, "/_fix-brokenly-mustered-games", []string{"GET"}, FixBrokenlyMusteredGamesRoute, handleFixBrokenlyMusteredGames)
	Handle(r, "/_find-broken-newest-phase-meta", []string{"GET"}, FindBrokenNewestPhaseMetaRoute, handleFindBrokenNewestPhaseMeta)
	Handle(r, "/_muster-all-running-games", []string{"GET"}, MusterAllRunningGamesRoute, handleMusterAllRunningGames)
	Handle(r, "/_muster-all-finished-games", []string{"GET"}, MusterAllFinishedGamesRoute, handleMusterAllFinishedGames)
	Handle(r, "/_find-badly-reset-games", []string{"GET"}, FindBadlyResetGamesRoute, handleFindBadlyResetGames)
	Handle(r, "/_re-schedule-all-broken", []string{"GET"}, ReScheduleAllBrokenRoute, handleReScheduleAllBroken)
	Handle(r, "/_re-schedule-all", []string{"GET"}, ReScheduleAllRoute, handleReScheduleAll)
	Handle(r, "/_remove-zipped-options", []string{"GET"}, RemoveZippedOptionsRoute, handleRemoveZippedOptions)
	Handle(r, "/_remove-dias-from-solo-games", []string{"GET"}, RemoveDIASFromSoloGamesRoute, handleRemoveDIASFromSoloGames)
	Handle(r, "/_global-system-message", []string{"POST"}, GlobalSystemMessageRoute, handleGlobalSystemMessage)
	Handle(r, "/Game/{game_id}/Channel/{recipients}/_system-message", []string{"POST"}, SendSystemMessageRoute, handleSendSystemMessage)
	Handle(r, "/_re-compute-all-dias-users", []string{"GET"}, ReComputeAllDIASUsersRoute, handleReComputeAllDIASUsers)
	Handle(r, "/_ah/mail/{recipient}", []string{"POST"}, ReceiveMailRoute, receiveMail)
	Handle(r, "/", []string{"GET"}, IndexRoute, handleIndex)
	Handle(r, "/Game/{game_id}/GameResults/TrueSkills", []string{"GET"}, ListGameResultTrueSkillsRoute, listGameResultTrueSkills)
	Handle(r, "/Game/{game_id}/Channels", []string{"GET"}, ListChannelsRoute, listChannels)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/_dev_resolve_timeout", []string{"GET"}, DevResolvePhaseTimeoutRoute, devResolvePhaseTimeout)
	Handle(r, "/User/{user_id}/Stats/_dev_update", []string{"PUT"}, DevUserStatsUpdateRoute, devUserStatsUpdate)
	// TODO(zond): Remove this when the Android client no longer uses the old API.
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/PhaseState/{nation}", []string{"PUT"}, "deprecatedUpdatePhaseState",
		func(w ResponseWriter, r Request) error {
			phaseState, err := updatePhaseState(w, r)
			if err != nil {
				return err
			}
			w.SetContent(phaseState.Item(r))
			return nil
		})
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Map", []string{"GET"}, RenderPhaseMapRoute, renderPhaseMap)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Corroborate", []string{"GET"}, CorroboratePhaseRoute, corroboratePhase)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/CreateAndCorroborate", []string{"POST"}, CreateAndCorroborateRoute, createAndCorroborate)
	Handle(r, "/GlobalStats", []string{"GET"}, GlobalStatsRoute, handleGlobalStats)
	Handle(r, "/Rss", []string{"GET"}, RssRoute, handleRss)
	Handle(r, "/Users/Ratings/Histogram", []string{"GET"}, GetUserRatingHistogramRoute, getUserRatingHistogram)
	HandleResource(r, ForumMailResource)
	HandleResource(r, GameResource)
	HandleResource(r, AllocationResource)
	HandleResource(r, GameMasterInvitationResource)
	HandleResource(r, GameMasterEditNewestPhaseDeadlineAtResource)
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
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/7.9.2/firebase.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/7.9.2/firebase-app.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/7.9.2/firebase-messaging.js")
		head.AddEl("link", "rel", "stylesheet", "style", "text/css", "href", "/css/bootstrap.css")
		head.AddEl("script", "src", "/js/main.js")
		head.AddEl("link", "rel", "manifest", "href", "/js/manifest.json")
		return nil
	})
}
