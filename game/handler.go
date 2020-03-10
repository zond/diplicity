package game

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/variants"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	dipVariants "github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
)

var (
	router              = mux.NewRouter()
	resaveFunc          *DelayFunc
	ejectMemberFunc     *DelayFunc
	containerGenerators = map[string]func() interface{}{
		gameKind:        func() interface{} { return &Game{} },
		gameResultKind:  func() interface{} { return &GameResult{} },
		phaseResultKind: func() interface{} { return &PhaseResult{} },
	}

	AllocationResource *Resource
)

func init() {
	resaveFunc = NewDelayFunc("game-resave", resave)
	ejectMemberFunc = NewDelayFunc("game-ejectMember", ejectMember)
	AllocationResource = &Resource{
		Create:      createAllocation,
		RenderLinks: true,
	}
}

const (
	maxLimit                    = 128
	MAX_STAGING_GAME_INACTIVITY = 30 * 24 * time.Hour
)

const (
	GetSWJSRoute                        = "GetSWJS"
	GetMainJSRoute                      = "GetMainJS"
	ConfigureRoute                      = "AuthConfigure"
	IndexRoute                          = "Index"
	ListOpenGamesRoute                  = "ListOpenGames"
	ListStartedGamesRoute               = "ListStartedGames"
	ListFinishedGamesRoute              = "ListFinishedGames"
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
	DevResolvePhaseTimeoutRoute         = "DevResolvePhaseTimeout"
	DevUserStatsUpdateRoute             = "DevUserStatsUpdate"
	ReceiveMailRoute                    = "ReceiveMail"
	RenderPhaseMapRoute                 = "RenderPhaseMap"
	ReRateRoute                         = "ReRate"
	GlobalStatsRoute                    = "GlobalStats"
	RssRoute                            = "Rss"
	ResaveRoute                         = "Resave"
	AllocateNationsRoute                = "AllocateNations"
	ReapInactiveWaitingPlayersRoute     = "ReapInactiveWaitingPlayersRoute"
	TestReapInactiveWaitingPlayersRoute = "TestReapInactiveWaitingPlayersRoute"
	ReScheduleRoute                     = "ReSchedule"
	ReScheduleAllBrokenRoute            = "ReScheduleAllBroken"
	ReScheduleAllRoute                  = "ReScheduleAll"
	RemoveDIASFromSoloGamesRoute        = "RemoveDIASFromSoloGamesRoute"
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

type gamesHandler struct {
	query   *datastore.Query
	name    string
	desc    []string
	route   string
	private bool
}

type gamesReq struct {
	ctx               context.Context
	w                 ResponseWriter
	r                 Request
	user              *auth.User
	userStats         *UserStats
	iter              *datastore.Iterator
	limit             int
	h                 *gamesHandler
	detailFilters     []func(g *Game) bool
	viewerStatsFilter bool
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
 * prepare creates a query and a bunch of filters useful when generating the next
 * batch of games to return.
 * WARNING: If you add filtering here, you should both add it to the gameListerParams in game.go
 *          and add some testing in diptest/game_test.go/TestGameListFilters and /TestIndexCreation.
 */
func (h *gamesHandler) prepare(w ResponseWriter, r Request, userId *string, viewerStatsFilter bool) (*gamesReq, error) {
	req := &gamesReq{
		ctx:               appengine.NewContext(r.Req()),
		w:                 w,
		r:                 r,
		h:                 h,
		viewerStatsFilter: viewerStatsFilter,
	}

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
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
	if userId == nil {
		q = q.Filter("Private=", false)
	} else {
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

/*
 * handle uses the generating and filtering setup in prepare
 * to generate the next batch (according to cursor and limit)
 * of games to return.
 */
func (req *gamesReq) handle() error {
	var err error
	games := make(Games, 0, req.limit)
	for err == nil && len(games) < req.limit {
		var nextBatch Games
		nextBatch, err = req.h.fetch(req.iter, req.limit-len(games))
		nextBatch.RemoveCustomFiltered(req.detailFilters)
		if req.viewerStatsFilter {
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

func (h *gamesHandler) handlePublic(viewerStatsFilter bool) func(w ResponseWriter, r Request) error {
	return func(w ResponseWriter, r Request) error {
		req, err := h.prepare(w, r, nil, viewerStatsFilter)
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
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
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
		query: datastore.NewQuery(gameKind).Filter("Started=", true).Filter("Finished=", false).Order("-StartedAt"),
		name:  "started-games",
		desc:  []string{"Started games", "Started games, sorted with oldest first."},
		route: ListStartedGamesRoute,
	}
	// The reason we have both openGamesHandler and stagingGamesHandler is because in theory we could have
	// started games in openGamesHandler - if we had a replacement mechanism.
	openGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Closed=", false).Order("StartETA"),
		name:  "open-games",
		desc:  []string{"Open games", "Open games, sorted with fullest and oldest first."},
		route: ListOpenGamesRoute,
	}
	stagingGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", false).Order("StartETA"),
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
	alloc, err := Allocate(a.Members, variant.Nations)
	if err != nil {
		return nil, err
	}
	for memberIdx := range a.Members {
		a.Members[memberIdx].Result = alloc[memberIdx]
	}
	return a, nil
}

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

func resave(ctx context.Context, kind string, counter int, cursorString string) error {
	log.Infof(ctx, "resave(..., %q, %v, %q)", kind, counter, cursorString)

	containerGenerator, found := containerGenerators[kind]
	if !found {
		return fmt.Errorf("Kind %q not supported by resave", kind)
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
		resaveFunc.EnqueueIn(ctx, 0, kind, counter, cursor.String())
	} else if err != datastore.Done {
		return err
	}

	return nil
}

func handleResave(w ResponseWriter, r Request) error {
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
		return fmt.Errorf("Kind %q not supported by resave", kind)
	}

	resaveFunc.EnqueueIn(ctx, 0, kind, 0, "")

	return nil
}

func reScheduleAll(w ResponseWriter, r Request, onlyBroken bool) error {
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
	ids, err := datastore.NewQuery(gameKind).Filter("Finished=", false).GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx, id := range ids {
		games[idx].ID = id
	}
	log.Infof(ctx, "Found %v unfinished games.", len(games))
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
		if len(game.NewestPhaseMeta) == 0 {
			log.Infof(ctx, "%+v has no phases, can't re-schedule.", game)
			return nil
		}
		phaseID, err := PhaseID(ctx, gameID, game.NewestPhaseMeta[0].PhaseOrdinal)
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
				ejectMemberFunc.EnqueueIn(ctx, 0, game.ID, member.User.Id)
				count++
			}
		}
	}
	log.Infof(ctx, "Ejected %v users from waiting games", count)

	return nil
}

func ejectMember(ctx context.Context, gameID *datastore.Key, userId string) error {
	log.Infof(ctx, "ejectMember(..., %v, %v)", gameID, userId)

	if _, err := deleteMemberHelper(ctx, gameID, userId, true); err != nil {
		log.Errorf(ctx, "deleteMemberHelper(..., %v, %v, true): %v; hope datastore gets fixed", gameID, userId, err)
		return err
	}

	log.Infof(ctx, "ejectMember(..., %v, %v): *** SUCCESS ***", gameID, userId)

	return nil
}

func SetupRouter(r *mux.Router) {
	router = r
	Handle(r, "/_reap-inactive-waiting-players", []string{"GET"}, ReapInactiveWaitingPlayersRoute, handleReapInactiveWaitingPlayers)
	Handle(r, "/_test_reap-inactive-waiting-players", []string{"GET"}, TestReapInactiveWaitingPlayersRoute, handleTestReapInactiveWaitingPlayers)
	Handle(r, "/_re-save", []string{"GET"}, ResaveRoute, handleResave)
	Handle(r, "/_configure", []string{"POST"}, ConfigureRoute, handleConfigure)
	Handle(r, "/_re-rate", []string{"GET"}, ReRateRoute, handleReRate)
	Handle(r, "/_re-schedule", []string{"GET"}, ReScheduleRoute, handleReSchedule)
	Handle(r, "/_re-schedule-all-broken", []string{"GET"}, ReScheduleAllBrokenRoute, handleReScheduleAllBroken)
	Handle(r, "/_re-schedule-all", []string{"GET"}, ReScheduleAllRoute, handleReScheduleAll)
	Handle(r, "/_remove-dias-from-solo-games", []string{"GET"}, RemoveDIASFromSoloGamesRoute, handleRemoveDIASFromSoloGames)
	Handle(r, "/_ah/mail/{recipient}", []string{"POST"}, ReceiveMailRoute, receiveMail)
	Handle(r, "/", []string{"GET"}, IndexRoute, handleIndex)
	Handle(r, "/Game/{game_id}/Channels", []string{"GET"}, ListChannelsRoute, listChannels)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/_dev_resolve_timeout", []string{"GET"}, DevResolvePhaseTimeoutRoute, devResolvePhaseTimeout)
	Handle(r, "/User/{user_id}/Stats/_dev_update", []string{"PUT"}, DevUserStatsUpdateRoute, devUserStatsUpdate)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Map", []string{"GET"}, RenderPhaseMapRoute, renderPhaseMap)
	Handle(r, "/GlobalStats", []string{"GET"}, GlobalStatsRoute, handleGlobalStats)
	Handle(r, "/Rss", []string{"GET"}, RssRoute, handleRss)
	HandleResource(r, GameResource)
	HandleResource(r, AllocationResource)
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
