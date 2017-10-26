package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/delay"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	gameKind           = "Game"
	sendGridKind       = "SendGrid"
	MAX_PHASE_DEADLINE = 30 * 24 * 60
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var (
	prodSendGrid     *SendGrid
	prodSendGridLock = sync.RWMutex{}

	noConfigError      = errors.New("user has no config")
	fromAddressPattern = "replies+%s@diplicity-engine.appspotmail.com"
	fromAddressReg     = regexp.MustCompile("^replies\\+([^@]+)@diplicity-engine.appspotmail.com")
	noreplyFromAddr    = "noreply@oort.se"

	GameResource *Resource
)

func init() {
	gameListerParams := []string{"variant", "min-reliability", "min-quickness", "max-hater", "max-hated", "min-rating", "max-rating", "limit", "cursor"}
	GameResource = &Resource{
		Load:   loadGame,
		Create: createGame,
		Listers: []Lister{
			{
				Path:        "/Games/Open",
				Route:       ListOpenGamesRoute,
				Handler:     openGamesHandler.handlePublic(true),
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Started",
				Route:       ListStartedGamesRoute,
				Handler:     startedGamesHandler.handlePublic(false),
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Finished",
				Route:       ListFinishedGamesRoute,
				Handler:     finishedGamesHandler.handlePublic(false),
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/My/Staging",
				Route:       ListMyStagingGamesRoute,
				Handler:     stagingGamesHandler.handlePrivate,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/My/Started",
				Route:       ListMyStartedGamesRoute,
				Handler:     startedGamesHandler.handlePrivate,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/My/Finished",
				Route:       ListMyFinishedGamesRoute,
				Handler:     finishedGamesHandler.handlePrivate,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/{user_id}/Staging",
				Route:       ListOtherStagingGamesRoute,
				Handler:     stagingGamesHandler.handleOther,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/{user_id}/Started",
				Route:       ListOtherStartedGamesRoute,
				Handler:     startedGamesHandler.handleOther,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/{user_id}/Finished",
				Route:       ListOtherFinishedGamesRoute,
				Handler:     finishedGamesHandler.handleOther,
				QueryParams: gameListerParams,
			},
		},
	}
}

type SendGrid struct {
	APIKey string
}

func getSendGridKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, sendGridKind, prodKey, 0, nil)
}

func SetSendGrid(ctx context.Context, sendGrid *SendGrid) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentSendGrid := &SendGrid{}
		if err := datastore.Get(ctx, getSendGridKey(ctx), currentSendGrid); err == nil {
			return HTTPErr{"SendGrid already configured", 400}
		}
		if _, err := datastore.Put(ctx, getSendGridKey(ctx), sendGrid); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

func GetSendGrid(ctx context.Context) (*SendGrid, error) {
	prodSendGridLock.RLock()
	if prodSendGrid != nil {
		defer prodSendGridLock.RUnlock()
		return prodSendGrid, nil
	}
	prodSendGridLock.RUnlock()
	prodSendGridLock.Lock()
	defer prodSendGridLock.Unlock()
	foundSendGrid := &SendGrid{}
	if err := datastore.Get(ctx, getSendGridKey(ctx), foundSendGrid); err != nil {
		return nil, err
	}
	prodSendGrid = foundSendGrid
	return prodSendGrid, nil
}

func PP(i interface{}) string {
	b, err := json.MarshalIndent(i, "  ", "  ")
	if err != nil {
		return spew.Sdump(i)
	}
	return string(b)
}

type DelayFunc struct {
	queue       string
	backendType reflect.Type
	backend     *delay.Function
}

func NewDelayFunc(queue string, backend interface{}) *DelayFunc {
	typ := reflect.TypeOf(backend)
	if typ.Kind() != reflect.Func {
		panic(fmt.Errorf("Can't create DelayFunc with non Func %#v", backend))
	}
	return &DelayFunc{
		queue:       queue,
		backend:     delay.Func(queue, backend),
		backendType: typ,
	}
}

func (d *DelayFunc) EnqueueAt(ctx context.Context, taskETA time.Time, args ...interface{}) error {
	for i, arg := range args {
		if !reflect.TypeOf(arg).AssignableTo(d.backendType.In(i + 1)) {
			return fmt.Errorf("Can't delay execution of %v on %q with %+v, arg %v (%#v) is not assignable to %v", d.backendType, d.queue, args, i, arg, d.backendType.In(i+1))
		}
	}
	t, err := d.backend.Task(args...)
	if err != nil {
		return err
	}
	t.ETA = taskETA
	_, err = taskqueue.Add(ctx, t, d.queue)
	return err
}

func (d *DelayFunc) EnqueueIn(ctx context.Context, taskDelay time.Duration, args ...interface{}) error {
	return d.EnqueueAt(ctx, time.Now().Add(taskDelay), args...)
}

type Games []Game

func (g *Games) RemoveCustomFiltered(filters []func(g *Game) bool) {
	newGames := make(Games, 0, len(*g))
	for _, game := range *g {
		isOK := true
		for _, filter := range filters {
			if !filter(&game) {
				isOK = false
				break
			}
		}
		if isOK {
			newGames = append(newGames, game)
		}
	}
	*g = newGames
}

func (g *Games) RemoveFiltered(userStats *UserStats) [][]string {
	failedRequirements := make([][]string, len(*g))
	newGames := make(Games, 0, len(*g))
	for i, game := range *g {
		if game.MaxHated != 0 && userStats.Hated > game.MaxHated {
			failedRequirements[i] = append(failedRequirements[i], "Hated")
			continue
		}
		if game.MaxHater != 0 && userStats.Hater > game.MaxHater {
			failedRequirements[i] = append(failedRequirements[i], "Hater")
			continue
		}
		if game.MaxRating != 0 && userStats.Glicko.PracticalRating > game.MaxRating {
			failedRequirements[i] = append(failedRequirements[i], "MaxRating")
			continue
		}
		if game.MinRating != 0 && userStats.Glicko.PracticalRating < game.MinRating {
			failedRequirements[i] = append(failedRequirements[i], "MinRating")
			continue
		}
		if game.MinReliability != 0 && userStats.Reliability < game.MinReliability {
			failedRequirements[i] = append(failedRequirements[i], "MinReliability")
			continue
		}
		if game.MinQuickness != 0 && userStats.Quickness < game.MinQuickness {
			failedRequirements[i] = append(failedRequirements[i], "MinQuickness")
			continue
		}
		newGames = append(newGames, game)
	}
	*g = newGames
	return failedRequirements
}

func (g *Games) RemoveBanned(ctx context.Context, uid string) ([][]Ban, error) {
	gameBans := make([][]Ban, len(*g))

	banIDs := []*datastore.Key{}
	gameIndices := []int{}
	for gameIndex, game := range *g {
		for _, member := range game.Members {
			banID, err := BanID(ctx, []string{uid, member.User.Id})
			if err != nil {
				return nil, err
			}
			gameIndices = append(gameIndices, gameIndex)
			banIDs = append(banIDs, banID)
		}
	}
	bans := make([]Ban, len(banIDs))
	err := datastore.GetMulti(ctx, banIDs, bans)

	if err == nil {
		*g = Games{}
		return [][]Ban{bans}, nil
	}

	if err == datastore.ErrNoSuchEntity {
		return gameBans, nil
	}

	merr, ok := err.(appengine.MultiError)
	if !ok {
		return nil, err
	}

	for banIndex, serr := range merr {
		if serr == nil {
			gameBans[gameIndices[banIndex]] = append(gameBans[gameIndices[banIndex]], bans[banIndex])
		} else if serr != datastore.ErrNoSuchEntity {
			return nil, err
		}
	}

	newGames := Games{}
	for gameIndex, game := range *g {
		if len(gameBans[gameIndex]) == 0 {
			newGames = append(newGames, game)
		}
	}
	*g = newGames
	return gameBans, nil
}

func (g Games) Item(r Request, user *auth.User, cursor *datastore.Cursor, limit int, name string, desc []string, route string) *Item {
	gameItems := make(List, len(g))
	for i := range g {
		g[i].Redact(user)
		gameItems[i] = g[i].Item(r)
	}
	gamesItem := NewItem(gameItems).SetName(name).SetDesc([][]string{
		desc,
		[]string{
			"Cursor and limit",
			fmt.Sprintf("The list contains at most %d games.", maxLimit),
			"If there are additional matching games, a 'next' link will be available with a 'cursor' query parameter.",
			"Use the 'next' link to list the next batch of matching games.",
			fmt.Sprintf("To list fewer than %d games, add an explicit 'limit' query parameter.", maxLimit),
		},
		[]string{
			"Filters",
			"To show only games matching certain criteria, add query parameter filters.",
			"`variant=X` filters on variant X.",
			"`min-reliability=X:Y` filters on min reliability between X and Y.",
			"`min-quickness=X:Y` filters on min quickness between X and Y.",
			"`max-hated=X:Y` filters on max hated between X and Y.",
			"`max-hater=X:Y` filters on max hater between X and Y.",
			"`min-rating=X:Y` filters on min rating between X and Y.",
			"`max-rating=X:Y` filters on max rating between X and Y.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: route,
	}))
	if cursor != nil {
		gamesItem.AddLink(r.NewLink(Link{
			Rel:   "next",
			Route: route,
			QueryParams: url.Values{
				"cursor": []string{cursor.String()},
				"limit":  []string{fmt.Sprint(limit)},
			},
		}))
	}
	return gamesItem
}

type Game struct {
	ID *datastore.Key `datastore:"-"`

	Started  bool // Game has started.
	Closed   bool // Game is no longer joinable..
	Finished bool // Game has reached its end.

	Desc               string        `methods:"POST" datastore:",noindex"`
	Variant            string        `methods:"POST"`
	PhaseLengthMinutes time.Duration `methods:"POST"`
	MaxHated           float64       `methods:"POST"`
	MaxHater           float64       `methods:"POST"`
	MinRating          float64       `methods:"POST"`
	MaxRating          float64       `methods:"POST"`
	MinReliability     float64       `methods:"POST"`
	MinQuickness       float64       `methods:"POST"`

	NMembers int
	Members  []Member

	NewestPhaseMeta []PhaseMeta

	ActiveBans         []Ban    `datastore:"-"`
	FailedRequirements []string `datastore:"-"`

	CreatedAt  time.Time
	FinishedAt time.Time
}

func (g *Game) abbrMatchesNations(abbr dip.Nation) int {
	matches := 0
	for _, m := range g.Members {
		if strings.Index(string(m.Nation), string(abbr)) == 0 {
			matches++
		}
	}
	return matches
}

func (g *Game) AbbrNats(nats Nations) Nations {
	result := make(Nations, len(nats))
	for i, nat := range nats {
		result[i] = g.AbbrNat(nat)
	}
	return result
}

func (g *Game) AbbrNat(nat dip.Nation) dip.Nation {
	if len(nat) < 2 {
		return nat
	}
	runes := []rune(string(nat))
	for i := 1; i < len(runes); i++ {
		if g.abbrMatchesNations(dip.Nation(runes[:i])) == 1 {
			return dip.Nation(runes[:i])
		}
	}
	return nat
}

func (g *Game) DescFor(nat dip.Nation) string {
	for _, m := range g.Members {
		if m.Nation == nat && m.GameAlias != "" {
			return m.GameAlias
		}
	}
	return g.Desc
}

func (g *Game) GetMember(userID string) (*Member, bool) {
	for i := range g.Members {
		if g.Members[i].User.Id == userID {
			return &g.Members[i], true
		}
	}
	return nil, false
}

func (g *Game) Leavable() bool {
	return !g.Started
}

func (g *Game) Joinable() bool {
	return !g.Closed && g.NMembers < len(variants.Variants[g.Variant].Nations) && len(g.ActiveBans) == 0 && len(g.FailedRequirements) == 0
}

func (g *Game) Item(r Request) *Item {
	gameItem := NewItem(g).SetName(g.Desc).AddLink(r.NewLink(GameResource.Link("self", Load, []string{"id", g.ID.Encode()})))
	user, ok := r.Values()["user"].(*auth.User)
	if ok {
		if _, isMember := g.GetMember(user.Id); isMember {
			if g.Leavable() {
				gameItem.AddLink(r.NewLink(MemberResource.Link("leave", Delete, []string{"game_id", g.ID.Encode(), "user_id", user.Id})))
			}
			gameItem.AddLink(r.NewLink(MemberResource.Link("update-membership", Update, []string{"game_id", g.ID.Encode(), "user_id", user.Id})))
		} else {
			if g.Joinable() {
				gameItem.AddLink(r.NewLink(MemberResource.Link("join", Create, []string{"game_id", g.ID.Encode()})))
			}
		}
		if g.Started {
			gameItem.AddLink(r.NewLink(Link{
				Rel:         "channels",
				Route:       ListChannelsRoute,
				RouteParams: []string{"game_id", g.ID.Encode()},
			}))
		}
		if g.Started {
			gameItem.AddLink(r.NewLink(Link{
				Rel:         "phases",
				Route:       ListPhasesRoute,
				RouteParams: []string{"game_id", g.ID.Encode()},
			}))
		}
		if g.Finished {
			gameItem.AddLink(r.NewLink(GameResultResource.Link("game-result", Load, []string{"game_id", g.ID.Encode()})))
		}
		if g.Started {
			gameItem.AddLink(r.NewLink(Link{
				Rel:         "game-states",
				Route:       ListGameStatesRoute,
				RouteParams: []string{"game_id", g.ID.Encode()},
			}))
		}
	}
	return gameItem
}

func (g *Game) Save(ctx context.Context) error {
	g.NMembers = len(g.Members)

	var err error
	if g.ID == nil {
		g.ID, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, gameKind, nil), g)
	} else {
		_, err = datastore.Put(ctx, g.ID, g)
	}
	return err
}

func createGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	game := &Game{}
	err := Copy(game, r, "POST")
	if err != nil {
		return nil, err
	}
	if _, found := variants.Variants[game.Variant]; !found {
		return nil, HTTPErr{"unknown variant", 400}
	}
	if game.PhaseLengthMinutes < 1 {
		return nil, HTTPErr{"no games with zero or negative phase deadline allowed", 400}
	}
	if game.PhaseLengthMinutes > MAX_PHASE_DEADLINE {
		return nil, HTTPErr{"no games with more than 30 day deadlines allowed", 400}
	}
	game.CreatedAt = time.Now()

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		userStats := &UserStats{}
		if err := datastore.Get(ctx, UserStatsID(ctx, user.Id), userStats); err == datastore.ErrNoSuchEntity {
			userStats.UserId = user.Id
		} else if err != nil {
			return err
		}
		filtered := Games{*game}
		if failedRequirements := filtered.RemoveFiltered(userStats); len(failedRequirements[0]) > 0 {
			return HTTPErr{fmt.Sprintf("Can't create game, failed own requirements: %+v", failedRequirements[0]), 412}
		}
		if err := game.Save(ctx); err != nil {
			return err
		}
		member := Member{
			User: *user,
		}
		game.Members = []Member{member}
		if err := game.Save(ctx); err != nil {
			return err
		}
		game.Members[0].NewestPhaseState = PhaseState{
			GameID: game.ID,
		}
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, err
	}

	return game, nil
}

func (g *Game) Redact(viewer *auth.User) {
	_, isMember := g.GetMember(viewer.Id)
	for index := range g.Members {
		g.Members[index].Redact(viewer, isMember)
	}
}

func (g *Game) Start(ctx context.Context, r Request) error {
	variant := variants.Variants[g.Variant]
	s, err := variant.Start()
	if err != nil {
		return err
	}

	g.Started = true
	g.Closed = true
	for memberIndex, nationIndex := range rand.Perm(len(variants.Variants[g.Variant].Nations)) {
		g.Members[memberIndex].Nation = variants.Variants[g.Variant].Nations[nationIndex]
	}

	scheme := "http"
	if r.Req().TLS != nil {
		scheme = "https"
	}
	phase := NewPhase(s, g.ID, 1, r.Req().Host, scheme)
	// To make old games work.
	if g.PhaseLengthMinutes == 0 {
		g.PhaseLengthMinutes = MAX_PHASE_DEADLINE
	}
	phase.DeadlineAt = time.Now().Add(time.Minute * g.PhaseLengthMinutes)
	if err := phase.Save(ctx); err != nil {
		return err
	}

	if err = phase.Recalc(); err != nil {
		return err
	}
	g.NewestPhaseMeta = []PhaseMeta{phase.PhaseMeta}

	if err := phase.ScheduleResolution(ctx); err != nil {
		return err
	}
	log.Infof(ctx, "%v has a %d minutes phase length, scheduled resolve", PP(g), g.PhaseLengthMinutes)

	if err := phase.NotifyMembers(ctx, g); err != nil {
		return err
	}

	uids := make([]string, len(g.Members))
	for i, m := range g.Members {
		uids[i] = m.User.Id
	}
	if err := UpdateUserStatsASAP(ctx, uids); err != nil {
		return err
	}

	return nil
}

func loadGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	game := &Game{}
	userStats := &UserStats{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, UserStatsID(ctx, user.Id)}, []interface{}{game, userStats}); err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			if merr[0] == nil && merr[1] == datastore.ErrNoSuchEntity {
				userStats.UserId = user.Id
				err = nil
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	game.ID = gameID
	for i := range game.NewestPhaseMeta {
		game.NewestPhaseMeta[i].Refresh()
	}

	game.Redact(user)

	filtered := Games{*game}
	activeBans, err := filtered.RemoveBanned(ctx, user.Id)
	if err != nil {
		return nil, err
	}
	game.ActiveBans = activeBans[0]

	filtered = Games{*game}
	game.FailedRequirements = filtered.RemoveFiltered(userStats)[0]

	return game, nil
}
