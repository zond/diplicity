package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/delayed"
	"github.com/zond/godip"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"

	hungarianAlgorithm "github.com/oddg/hungarian-algorithm"
	. "github.com/zond/goaeoas"
)

const (
	gameKind           = "Game"
	MAX_PHASE_DEADLINE = 30 * 24 * 60
)

const (
	RandomAllocation AllocationMethod = iota
	PreferenceAllocation
)

func init() {
	rand.Seed(time.Now().UnixNano())

	asyncStartGameFunc = NewDelayFunc("game-asyncStartGame", asyncStartGame)
}

var (
	asyncStartGameFunc *DelayFunc

	noConfigError      = errors.New("user has no config")
	noGameError        = errors.New("game does not exist")
	fromAddressPattern = "replies+%s@diplicity-engine.appspotmail.com"
	fromAddressReg     = regexp.MustCompile("^replies\\+([^@]+)@diplicity-engine.appspotmail.com")
	noreplyFromAddr    = "noreply@oort.se"
	noreplyFromName    = "Diplicity"

	GameResource *Resource
)

func init() {
	gameListerParams := []string{
		"cursor",
		"limit",
		"variant",
		"min-reliability",
		"min-quickness",
		"max-hater",
		"max-hated",
		"min-rating",
		"max-rating",
		"only-private",
		"nation-allocation",
		"phase-length-minutes",
		"conference-chat-disabled",
		"group-chat-disabled",
		"private-chat-disabled",
	}
	GameResource = &Resource{
		Load:   loadGame,
		Delete: gameMasterDeleteGame,
		Create: createGame,
		Update: gameMasterUpdateGame,
		Listers: []Lister{
			{
				Path:        "/Games/Open",
				Route:       openGamesHandler.route,
				Handler:     openGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Started",
				Route:       startedGamesHandler.route,
				Handler:     startedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Finished",
				Route:       finishedGamesHandler.route,
				Handler:     finishedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Mastered/Staging",
				Route:       masteredStagingGamesHandler.route,
				Handler:     masteredStagingGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Mastered/Started",
				Route:       masteredStartedGamesHandler.route,
				Handler:     masteredStartedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/Mastered/Finished",
				Route:       masteredFinishedGamesHandler.route,
				Handler:     masteredFinishedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/My/Staging",
				Route:       myStagingGamesHandler.route,
				Handler:     myStagingGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/My/Started",
				Route:       myStartedGamesHandler.route,
				Handler:     myStartedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/My/Finished",
				Route:       myFinishedGamesHandler.route,
				Handler:     myFinishedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/{user_id}/Staging",
				Route:       otherMemberStagingGamesHandler.route,
				Handler:     otherMemberStagingGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/{user_id}/Started",
				Route:       otherMemberStartedGamesHandler.route,
				Handler:     otherMemberStartedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
			{
				Path:        "/Games/{user_id}/Finished",
				Route:       otherMemberFinishedGamesHandler.route,
				Handler:     otherMemberFinishedGamesHandler.handle,
				QueryParams: gameListerParams,
			},
		},
	}
}

type AllocationMethod int

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
	backend     *delayed.Function
}

func NewDelayFunc(queue string, backend interface{}) *DelayFunc {
	if queue == "" {
		panic(fmt.Errorf("Can't create DelayFunc without queue name!"))
	}
	typ := reflect.TypeOf(backend)
	if typ.Kind() != reflect.Func {
		panic(fmt.Errorf("Can't create DelayFunc with non Func %#v", backend))
	}
	df := &DelayFunc{
		queue:       queue,
		backend:     delayed.Func(queue, backend),
		backendType: typ,
	}
	return df
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

func (g Games) Len() int {
	return len(g)
}

func (g Games) Less(i, j int) bool {
	if g[i].NMembers != g[j].NMembers {
		if g[i].NMembers > g[j].NMembers {
			return true
		}
		return false
	}
	return g[i].CreatedAt.Before(g[j].CreatedAt)
}

func (g Games) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}

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

type filterReason int

const (
	toCreate filterReason = iota
	toJoin
)

func (g *Games) RemoveFiltered(reason filterReason, userStats *UserStats, actuallyRemove bool) [][]string {
	failedRequirements := make([][]string, len(*g))
	newGames := make(Games, 0, len(*g))
	for i := range *g {
		game := &(*g)[i]
		game.FailedRequirements = nil
		addFailure := func(failure string) {
			failedRequirements[i] = append(failedRequirements[i], failure)
			game.FailedRequirements = append(game.FailedRequirements, failure)
		}
		if game.MaxHated != 0 && userStats.Hated > game.MaxHated {
			addFailure("Hated")
		}
		if game.MaxHater != 0 && userStats.Hater > game.MaxHater {
			addFailure("Hater")
		}
		if game.MaxRating != 0 && userStats.TrueSkill.Rating > game.MaxRating {
			addFailure("MaxRating")
		}
		if game.MinRating != 0 && userStats.TrueSkill.Rating < game.MinRating {
			addFailure("MinRating")
		}
		if game.MinReliability != 0 && userStats.Reliability < game.MinReliability {
			addFailure("MinReliability")
		}
		if game.MinQuickness != 0 && userStats.Quickness < game.MinQuickness {
			addFailure("MinQuickness")
		}
		if game.GameMasterEnabled && game.RequireGameMasterInvitation && reason == toJoin && !game.IsInvitedByGameMaster(userStats.User.Email) {
			addFailure("InvitationNeeded")
		}
		if len(game.FailedRequirements) == 0 && actuallyRemove {
			newGames = append(newGames, *game)
		}
	}
	if actuallyRemove {
		*g = newGames
	}
	return failedRequirements
}

func (g *Games) RemoveBanned(ctx context.Context, uid string, actuallyRemove bool) ([][]Ban, error) {
	// Empty slice of bans per game.
	gameBans := make([][]Ban, len(*g))

	// Create a slice of ban IDs, and for each ban ID add the index into *g for the game the ban came from.
	banIDs := []*datastore.Key{}
	gameIndices := []int{}
	for gameIndex := range *g {
		game := &(*g)[gameIndex]
		game.ActiveBans = nil
		// You can't be banned from your own game mastered game.
		if game.GameMaster.Id != uid {
			for _, member := range game.Members {
				banID, err := BanID(ctx, []string{uid, member.User.Id})
				if err != nil {
					return nil, err
				}
				gameIndices = append(gameIndices, gameIndex)
				banIDs = append(banIDs, banID)
			}
			if game.GameMaster.Id != "" {
				banID, err := BanID(ctx, []string{uid, game.GameMaster.Id})
				if err != nil {
					return nil, err
				}
				gameIndices = append(gameIndices, gameIndex)
				banIDs = append(banIDs, banID)
			}
		}
	}
	// If we found no possible bans, just return the empty slice of bans.
	if len(banIDs) == 0 {
		return gameBans, nil
	}

	bans := make([]Ban, len(banIDs))
	err := datastore.GetMulti(ctx, banIDs, bans)

	if err == nil {
		// If we succeeded with all loads (all bans existed, unlikely), then add each ban we found to the correct position in gameBans.
		for banIndex := range bans {
			gameBans[gameIndices[banIndex]] = append(gameBans[gameIndices[banIndex]], bans[banIndex])
			(*g)[gameIndices[banIndex]].ActiveBans = append((*g)[gameIndices[banIndex]].ActiveBans, bans[banIndex])
		}
	} else {
		// If we didn't find ANYTHING, just return the empty slice of bans.
		if err == datastore.ErrNoSuchEntity {
			return gameBans, nil
		}

		// If the error isn't a multi error, then eject.
		merr, ok := err.(appengine.MultiError)
		if !ok {
			return nil, err
		}

		for banIndex, serr := range merr {
			// Go through the errors, and add each ban that was loaded successfully to the correct position in gameBans.
			if serr == nil {
				gameBans[gameIndices[banIndex]] = append(gameBans[gameIndices[banIndex]], bans[banIndex])
				(*g)[gameIndices[banIndex]].ActiveBans = append((*g)[gameIndices[banIndex]].ActiveBans, bans[banIndex])
			} else if serr != datastore.ErrNoSuchEntity {
				// If the error wasn't a 'not found', eject.
				return nil, err
			}
		}
	}

	// Build a new slice of games, if we want to remove.
	if actuallyRemove {
		newGames := Games{}
		for gameIndex, game := range *g {
			if len(gameBans[gameIndex]) == 0 {
				newGames = append(newGames, game)
			}
		}
		*g = newGames
	}
	return gameBans, nil
}

func (g Games) Item(r Request, user *auth.User, cursor *datastore.Cursor, limit int, name string, desc []string, route string) *Item {
	gameItems := make(List, len(g))
	for i := range g {
		g[i].Redact(user, r)
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
	Mustered bool // Game has mustered all players.
	Closed   bool // Game is no longer joinable.
	Finished bool // Game has reached its end.

	Desc                          string           `methods:"POST,PUT" datastore:",noindex"`
	Variant                       string           `methods:"POST"`
	PhaseLengthMinutes            time.Duration    `methods:"POST,PUT"`
	NonMovementPhaseLengthMinutes time.Duration    `methods:"POST,PUT"`
	MaxHated                      float64          `methods:"POST"`
	MaxHater                      float64          `methods:"POST"`
	MinRating                     float64          `methods:"POST"`
	MaxRating                     float64          `methods:"POST"`
	MinReliability                float64          `methods:"POST"`
	MinQuickness                  float64          `methods:"POST"`
	Private                       bool             `methods:"POST"`
	NoMerge                       bool             `methods:"POST"`
	DisableConferenceChat         bool             `methods:"POST,PUT"`
	DisableGroupChat              bool             `methods:"POST,PUT"`
	DisablePrivateChat            bool             `methods:"POST,PUT"`
	NationAllocation              AllocationMethod `methods:"POST"`
	Anonymous                     bool             `methods:"POST"`
	LastYear                      int              `methods:"POST,PUT"`
	SkipMuster                    bool             `methods:"POST,PUT"`
	ChatLanguageISO639_1          string           `methods:"POST,PUT"`
	GameMasterEnabled             bool             `methods:"POST"`
	RequireGameMasterInvitation   bool             `methods:"POST,PUT"`

	GameMasterInvitations GameMasterInvitations
	GameMaster            auth.User

	NMembers int
	Members  Members
	StartETA time.Time

	NewestPhaseMeta []PhaseMeta

	ActiveBans         []Ban    `datastore:"-"`
	FailedRequirements []string `datastore:"-"`
	FirstMember        *Member  `datastore:"-" json:",omitempty" methods:"POST"`

	CreatedAt   time.Time
	CreatedAgo  time.Duration `datastore:"-" ticker:"true"`
	StartedAt   time.Time
	StartedAgo  time.Duration `datastore:"-" ticker:"true"`
	FinishedAt  time.Time
	FinishedAgo time.Duration `datastore:"-" ticker:"true"`
}

func (g *Game) Save() ([]datastore.Property, error) {
	return datastore.SaveStruct(g)
}
func (g *Game) Load(props []datastore.Property) error {
	err := datastore.LoadStruct(g, props)
	if _, is := err.(*datastore.ErrFieldMismatch); is {
		err = nil
	}
	return err
}

func (g *Game) canMergeInto(o *Game, avoid *auth.User) bool {
	if g.NoMerge || o.NoMerge {
		return false
	}
	if g.Started || o.Started {
		return false
	}
	if g.Closed || o.Closed {
		return false
	}
	if g.Finished || o.Finished {
		return false
	}
	if g.Private || o.Private {
		return false
	}
	if g.Variant != o.Variant {
		return false
	}
	if g.PhaseLengthMinutes != o.PhaseLengthMinutes {
		return false
	}
	if g.NonMovementPhaseLengthMinutes != o.NonMovementPhaseLengthMinutes {
		return false
	}
	if g.MaxHated != o.MaxHated {
		return false
	}
	if g.MaxHater != o.MaxHater {
		return false
	}
	if g.MinRating != o.MinRating {
		return false
	}
	if g.MaxRating != o.MaxRating {
		return false
	}
	if g.MinReliability != o.MinReliability {
		return false
	}
	if g.MinQuickness != o.MinQuickness {
		return false
	}
	if g.DisableConferenceChat != o.DisableConferenceChat {
		return false
	}
	if g.DisableGroupChat != o.DisableGroupChat {
		return false
	}
	if g.DisablePrivateChat != o.DisablePrivateChat {
		return false
	}
	if g.NationAllocation != o.NationAllocation {
		return false
	}
	if g.Anonymous != o.Anonymous {
		return false
	}
	if g.GameMasterEnabled || o.GameMasterEnabled {
		return false
	}
	if g.NMembers+o.NMembers > len(variants.Variants[g.Variant].Nations) {
		return false
	}
	if g.LastYear != o.LastYear {
		return false
	}
	if g.SkipMuster != o.SkipMuster {
		return false
	}
	if g.ChatLanguageISO639_1 != o.ChatLanguageISO639_1 {
		return false
	}
	for _, member := range o.Members {
		if member.User.Id == avoid.Id {
			return false
		}
	}
	return true
}

func (g *Game) Refresh() {
	if !g.CreatedAt.IsZero() {
		g.CreatedAgo = g.CreatedAt.Sub(time.Now())
	}
	if !g.StartedAt.IsZero() {
		g.StartedAgo = g.StartedAt.Sub(time.Now())
	}
	if !g.FinishedAt.IsZero() {
		g.FinishedAgo = g.FinishedAt.Sub(time.Now())
	}
}

func (g *Game) abbrMatchesNations(abbr godip.Nation) int {
	matches := 0
	for _, m := range g.Members {
		if strings.Index(string(m.Nation), string(abbr)) == 0 {
			matches++
		}
	}
	return matches
}

func (g *Game) AbbrNats(nats Nations) Nations {
	if len(nats) == len(variants.Variants[g.Variant].Nations) {
		return Nations{"Everyone"}
	}
	result := make(Nations, len(nats))
	for i, nat := range nats {
		result[i] = g.AbbrNat(nat)
	}
	return result
}

func (g *Game) AbbrNat(nat godip.Nation) godip.Nation {
	if len(nat) < 2 {
		return nat
	}
	runes := []rune(string(nat))
	for i := 1; i < len(runes); i++ {
		if g.abbrMatchesNations(godip.Nation(runes[:i])) == 1 {
			return godip.Nation(runes[:i])
		}
	}
	return nat
}

func (g *Game) DescFor(nat godip.Nation) string {
	for _, m := range g.Members {
		if m.Nation == nat && m.GameAlias != "" {
			return m.GameAlias
		}
	}
	return g.Desc
}

func (g *Game) GetMemberByNation(nation godip.Nation) (*Member, bool) {
	for i := range g.Members {
		if g.Members[i].Nation == nation {
			return &g.Members[i], true
		}
	}
	return nil, false
}

func (g *Game) GetMemberByUserId(userID string) (*Member, bool) {
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

func (g *Game) IsInvitedByGameMaster(email string) bool {
	for _, invitation := range g.GameMasterInvitations {
		if invitation.Email == email {
			return true
		}
	}
	return false
}

func (g *Game) HasReplaceableMember() bool {
	for _, member := range g.Members {
		if member.Replaceable {
			return true
		}
	}
	return false
}

func (g *Game) Joinable(user *auth.User) bool {
	if len(g.ActiveBans) > 0 || len(g.FailedRequirements) > 0 {
		return false
	}
	if g.Closed || g.NMembers >= len(variants.Variants[g.Variant].Nations) {
		if g.GameMasterEnabled && g.HasReplaceableMember() && (!g.RequireGameMasterInvitation || g.IsInvitedByGameMaster(user.Email)) {
			return true
		}
		return false
	} else {
		if g.GameMasterEnabled && g.RequireGameMasterInvitation && !g.IsInvitedByGameMaster(user.Email) {
			return false
		}
		return true
	}
}

func (g *Game) Item(r Request) *Item {
	gameItem := NewItem(g).SetName(g.Desc).AddLink(r.NewLink(GameResource.Link("self", Load, []string{"id", g.ID.Encode()})))
	user, ok := r.Values()["user"].(*auth.User)
	if ok {
		if _, isMember := g.GetMemberByUserId(user.Id); isMember {
			if g.Leavable() {
				gameItem.AddLink(r.NewLink(MemberResource.Link("leave", Delete, []string{"game_id", g.ID.Encode(), "user_id", user.Id})))
			}
			gameItem.AddLink(r.NewLink(MemberResource.Link("update-membership", Update, []string{"game_id", g.ID.Encode(), "user_id", user.Id})))
		} else {
			if g.Joinable(user) {
				gameItem.AddLink(r.NewLink(MemberResource.Link("join", Create, []string{"game_id", g.ID.Encode()})))
			}
		}
		gameItem.AddLink(r.NewLink(Link{
			Rel:         "channels",
			Route:       ListChannelsRoute,
			RouteParams: []string{"game_id", g.ID.Encode()},
		}))
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
		if user.Id == g.GameMaster.Id {
			gameItem.AddLink(r.NewLink(GameResource.Link("update-game", Update, []string{"id", g.ID.Encode()})))
			if !g.Started {
				gameItem.AddLink(r.NewLink(GameResource.Link("delete-game", Delete, []string{"id", g.ID.Encode()})))
			}
			gameItem.AddLink(r.NewLink(GameMasterInvitationResource.Link(
				"invite-user",
				Create,
				[]string{
					"game_id", g.ID.Encode(),
				},
			)))
			if len(g.NewestPhaseMeta) == 1 && !g.NewestPhaseMeta[0].Resolved {
				gameItem.AddLink(r.NewLink(GameMasterEditNewestPhaseDeadlineAtResource.Link(
					"edit-newest-phase-deadline-at",
					Create,
					[]string{
						"game_id",
						g.ID.Encode(),
						"phase_ordinal",
						fmt.Sprint(g.NewestPhaseMeta[0].PhaseOrdinal),
					},
				)))
			}
			uniqueInvitationEmail := map[string]bool{}
			for _, invitation := range g.GameMasterInvitations {
				uniqueInvitationEmail[invitation.Email] = true
			}
			for email := range uniqueInvitationEmail {
				gameItem.AddLink(r.NewLink(GameMasterInvitationResource.Link(
					"uninvite-"+email,
					Delete,
					[]string{
						"game_id", g.ID.Encode(),
						"email", email,
					},
				)))
			}
			uniqueMemberIds := map[string]bool{}
			for _, member := range g.Members {
				if member.User.Id != "" {
					uniqueMemberIds[member.User.Id] = true
				}
			}
			for id := range uniqueMemberIds {
				gameItem.AddLink(r.NewLink(MemberResource.Link("kick-"+id, Delete, []string{"game_id", g.ID.Encode(), "user_id", id})))
			}
		}
	}
	return gameItem
}

func (g *Game) DBSave(ctx context.Context) error {
	g.NMembers = len(g.Members)
	if g.Started {
		g.StartETA = g.StartedAt
	} else if len(g.Members) > 1 {
		requiredSpots := float64(len(variants.Variants[g.Variant].Nations))
		emptySpots := requiredSpots - float64(len(g.Members))
		rate := (float64(len(g.Members)) - 1) / float64(time.Now().UnixNano()-g.CreatedAt.UnixNano())
		timeLeft := time.Duration(float64(time.Nanosecond) * (emptySpots / rate))
		g.StartETA = time.Now().Add(timeLeft)
	} else {
		g.StartETA = time.Date(2525, time.January, 1, 0, 0, 0, 0, time.UTC)
	}

	var err error
	if g.ID == nil {
		g.ID, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, gameKind, nil), g)
	} else {
		_, err = datastore.Put(ctx, g.ID, g)
	}
	return err
}

func merge(ctx context.Context, r Request, game *Game, user *auth.User) (*Game, error) {
	games := Games{}
	gameIDs, err := datastore.NewQuery(gameKind).
		Filter("Started=", false).
		Filter("Closed=", false).
		Filter("Finished=", false).
		Filter("Private=", false).
		Filter("NoMerge=", false).
		Filter("GameMaster.Id=", "").
		Filter("Variant=", game.Variant).
		Filter("PhaseLengthMinutes=", game.PhaseLengthMinutes).
		Filter("MaxHated=", game.MaxHated).
		Filter("MaxHater=", game.MaxHater).
		Filter("MinRating=", game.MinRating).
		Filter("MaxRating=", game.MaxRating).
		Filter("MinReliability=", game.MinReliability).
		Filter("MinQuickness=", game.MinQuickness).
		Filter("DisableConferenceChat=", game.DisableConferenceChat).
		Filter("DisableGroupChat=", game.DisableGroupChat).
		Filter("DisablePrivateChat=", game.DisablePrivateChat).
		Filter("NationAllocation=", game.NationAllocation).
		Filter("Anonymous=", game.Anonymous).
		Filter("SkipMuster=", game.SkipMuster).
		Filter("ChatLanguageISO639_1=", game.ChatLanguageISO639_1).
		GetAll(ctx, &games)
	if err != nil {
		return nil, err
	}
	for idx, id := range gameIDs {
		games[idx].ID = id
	}
	sort.Sort(games)

	games.RemoveBanned(ctx, user.Id, true)

	for _, otherGame := range games {
		if game.canMergeInto(&otherGame, user) {
			member := game.FirstMember
			if member == nil {
				member = &Member{}
			}
			if joinedGame, _, err := createMemberHelper(ctx, r, otherGame.ID, user, member); err != nil {
				return nil, err
			} else {
				return joinedGame, nil
			}
		}
	}
	return nil, nil
}

func gameMasterDeleteGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	game := &Game{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game = &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return HTTPErr{"non existing game", http.StatusPreconditionFailed}
		}
		game.ID = gameID

		if game.GameMaster.Id != user.Id {
			return HTTPErr{"unauthorized", http.StatusUnauthorized}
		}

		if game.Started {
			return HTTPErr{"game has already started", http.StatusPreconditionFailed}
		}

		userIDs := []string{}
		for _, member := range game.Members {
			userIDs = append(userIDs, member.User.Id)
		}
		if err := UpdateUserStatsASAP(ctx, userIDs); err != nil {
			return err
		}

		return datastore.Delete(ctx, gameID)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return game, nil
}

func createGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	game := &Game{}
	err := Copy(game, r, "POST")
	if err != nil {
		return nil, err
	}
	if game.FirstMember == nil {
		game.FirstMember = &Member{}
	}
	if _, found := variants.Variants[game.Variant]; !found {
		return nil, HTTPErr{"unknown variant", http.StatusBadRequest}
	}
	if game.PhaseLengthMinutes < 1 {
		return nil, HTTPErr{"no games with zero or negative phase deadline allowed", http.StatusBadRequest}
	}
	if game.PhaseLengthMinutes > MAX_PHASE_DEADLINE {
		return nil, HTTPErr{"no games with more than 30 day deadlines allowed", http.StatusBadRequest}
	}
	if game.GameMasterEnabled {
		if !game.Private {
			return nil, HTTPErr{"only private games can have game master", http.StatusBadRequest}
		}
		game.GameMaster = *user
	}
	game.CreatedAt = time.Now()

	if !game.NoMerge && !game.Private {
		mergedWith, err := merge(ctx, r, game, user)
		if err != nil {
			return nil, err
		}
		if mergedWith != nil {
			w.WriteHeader(http.StatusTeapot)
			return nil, nil
		}
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		userStats := &UserStats{}
		if err := datastore.Get(ctx, UserStatsID(ctx, user.Id), userStats); err == datastore.ErrNoSuchEntity {
			userStats.UserId = user.Id
			userStats.User = *user
		} else if err != nil {
			return err
		}
		filtered := Games{*game}
		if failedRequirements := filtered.RemoveFiltered(toCreate, userStats, false); len(failedRequirements[0]) > 0 {
			return HTTPErr{fmt.Sprintf("Can't create game, failed own requirements: %+v", failedRequirements[0]), http.StatusPreconditionFailed}
		}
		if err := game.DBSave(ctx); err != nil {
			return err
		}
		if !game.GameMasterEnabled {
			game.Members = []Member{
				{
					User:              *user,
					GameAlias:         game.FirstMember.GameAlias,
					NationPreferences: game.FirstMember.NationPreferences,
					NewestPhaseState: PhaseState{
						GameID: game.ID,
					},
				},
			}
			if err := UpdateUserStatsASAP(ctx, []string{user.Id}); err != nil {
				return err
			}
		}
		return game.DBSave(ctx)
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, err
	}

	return game, nil
}

func (g *Game) Redact(viewer *auth.User, r Request) {
	if viewer.Id == g.GameMaster.Id {
		return
	}
	if !g.Finished && ((g.Private && g.Anonymous) || (!g.Private && g.DisablePrivateChat && g.DisableGroupChat && g.DisableConferenceChat)) {
		for index := range g.Members {
			if g.Members[index].User.Id == viewer.Id {
				g.Members[index].Redact(viewer, g.Mustered && g.Started)
			} else {
				g.Members[index].Anonymize(r)
			}
		}
	} else {
		for index := range g.Members {
			g.Members[index].Redact(viewer, g.Mustered && g.Started)
		}
	}
}

type Preferer interface {
	Preferences() godip.Nations
}

type Preferers interface {
	Each(func(int, Preferer))
	Len() int
}

func AllocateNations(preferers Preferers, nations godip.Nations) ([]godip.Nation, error) {
	validNation := map[godip.Nation]bool{}
	for _, nation := range nations {
		validNation[nation] = true
	}
	costs := make([][]int, preferers.Len())
	preferers.Each(func(memberIdx int, preferer Preferer) {
		// For each player, create a cost map.
		costMap := map[godip.Nation]int{}
		for _, nation := range preferer.Preferences() {
			// For each valid nation preference, give it a cost of the current size of the cost map.
			if validNation[nation] {
				costMap[nation] = len(costMap)
			}
		}
		// Create a cost array for the player.
		memberCosts := make([]int, len(nations))
		for _, nationIdx := range rand.Perm(len(nations)) {
			nation := nations[nationIdx]
			// For each nation, add a new cost if we don't already have one.
			if _, found := costMap[nation]; !found {
				costMap[nation] = len(costMap)
			}
			// Add that cost to the cost array.
			memberCosts[nationIdx] = costMap[nation]
		}
		costs[memberIdx] = memberCosts
	})
	solution, err := hungarianAlgorithm.Solve(costs)
	if err != nil {
		return nil, err
	}
	result := make([]godip.Nation, len(nations))
	for memberIdx := range result {
		result[memberIdx] = nations[solution[memberIdx]]
	}
	return result, nil
}

func (g *Game) ValidNation(nation godip.Nation) bool {
	for _, variantNation := range variants.Variants[g.Variant].Nations {
		if nation == variantNation {
			return true
		}
	}
	return false
}

func (g *Game) AllocateNations(ctx context.Context) error {
	variant := variants.Variants[g.Variant]

	// All nations we need to allocate
	nationsNeedingAllocationMap := map[godip.Nation]bool{}
	for _, nation := range variant.Nations {
		nationsNeedingAllocationMap[nation] = true
	}

	// Check all invitations for preallocations
	preallocatedEmailsMap := map[string]godip.Nation{}
	for _, invitation := range g.GameMasterInvitations {
		if invitation.Nation != "" && g.ValidNation(invitation.Nation) {
			preallocatedEmailsMap[invitation.Email] = invitation.Nation
		}
	}

	membersNeedingNations := []*Member{}

	// For each member
	for memberIdx := range g.Members {
		member := &g.Members[memberIdx]
		// If there is a prealloc for this member
		if prealloc, found := preallocatedEmailsMap[member.User.Email]; found {
			// Allocate it, and remove the allocated nation
			log.Infof(ctx, "preallocating %v to %v", prealloc, member.User.Email)
			member.Nation = prealloc
			delete(nationsNeedingAllocationMap, prealloc)
		} else {
			// Store this member as needing a nation
			membersNeedingNations = append(membersNeedingNations, member)
		}
	}

	// Collect the nations needing allocation in a new slice
	nationsNeedingAllocation := []godip.Nation{}
	for nation := range nationsNeedingAllocationMap {
		nationsNeedingAllocation = append(nationsNeedingAllocation, nation)
	}

	if len(nationsNeedingAllocation) != len(membersNeedingNations) {
		return fmt.Errorf("wtf? how did this even happen?")
	}

	if len(nationsNeedingAllocation) == 0 {
		return nil
	}

	if g.NationAllocation == RandomAllocation {
		for memberIndex, nationIndex := range rand.Perm(len(nationsNeedingAllocation)) {
			membersNeedingNations[memberIndex].Nation = nationsNeedingAllocation[nationIndex]
		}
	} else if g.NationAllocation == PreferenceAllocation {
		members := Members{}
		for memberIdx := range membersNeedingNations {
			members = append(members, *membersNeedingNations[memberIdx])
		}
		alloc, err := AllocateNations(members, nationsNeedingAllocation)
		if err != nil {
			return fmt.Errorf("AllocateNations(%+v, %+v): %v", members, nationsNeedingAllocation, err)
		}
		for memberIdx := range membersNeedingNations {
			membersNeedingNations[memberIdx].Nation = alloc[memberIdx]
		}
	} else {
		return fmt.Errorf("unknown allocation method %v, pick %v or %v", g.NationAllocation, RandomAllocation, PreferenceAllocation)
	}
	return nil
}

func asyncStartGame(ctx context.Context, gameID *datastore.Key, host string) error {
	log.Infof(ctx, "asyncStartGame(..., %v, %q)", gameID, host)

	err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		g := &Game{}
		if err := datastore.Get(ctx, gameID, g); err != nil {
			log.Errorf(ctx, "datastore.Get(..., %v, %v): %v; hope datastore will get fixed", gameID, g, err)
			return err
		}
		g.ID = gameID

		variant := variants.Variants[g.Variant]
		if len(g.Members) != len(variant.Nations) {
			log.Warningf(ctx, "Variant %v has %v nations, game %v has %v nations, someone must have dropped out before we got here?", g.Variant, len(variant.Nations), g.ID, len(g.Members))
			return nil
		}

		s, err := variant.Start()
		if err != nil {
			log.Errorf(ctx, "variant.Start(): %v; fix godip", err)
			return err
		}

		g.Started = true
		g.StartedAt = time.Now()
		g.Closed = true
		if err := g.AllocateNations(ctx); err != nil {
			log.Errorf(ctx, "g.AllocateNations(): %v; fix it?", err)
			return err
		}
		if g.SkipMuster {
			g.Mustered = true
		}

		phase := NewPhase(s, g.ID, 1, host)
		// To ensure we don't get 0 phase length games.
		if g.PhaseLengthMinutes == 0 {
			g.PhaseLengthMinutes = MAX_PHASE_DEADLINE
		}
		if (!g.Mustered || phase.Type != godip.Movement) && g.NonMovementPhaseLengthMinutes != 0 {
			phase.DeadlineAt = phase.CreatedAt.Add(time.Minute * g.NonMovementPhaseLengthMinutes)
		} else {
			phase.DeadlineAt = phase.CreatedAt.Add(time.Minute * g.PhaseLengthMinutes)
		}

		toSave := []interface{}{
			phase,
		}

		phaseID, err := phase.ID(ctx)
		if err != nil {
			log.Errorf(ctx, "phase.ID(...): %v", err)
			return err
		}
		keys := []*datastore.Key{
			phaseID,
		}

		for idx := range g.Members {
			options := godip.Options{}
			if g.Mustered {
				options = s.Phase().Options(s, g.Members[idx].Nation)
				profile, counts := s.GetProfile()
				for k, v := range profile {
					log.Debugf(ctx, "Profiling state: %v => %v, %v", k, v, counts[k])
				}
			}
			zippedOptions, err := zipOptions(ctx, options)
			if err != nil {
				log.Errorf(ctx, "zipOptions(..., %+v): %v", options, err)
				return err
			}

			messages := ""
			if g.Mustered {
				messages = strings.Join(s.Phase().Messages(s, g.Members[idx].Nation), ",")
			}
			phaseState := &PhaseState{
				GameID:        g.ID,
				Nation:        g.Members[idx].Nation,
				PhaseOrdinal:  phase.PhaseOrdinal,
				NoOrders:      len(options) == 0,
				Messages:      messages,
				ZippedOptions: zippedOptions,
				Note:          fmt.Sprintf("Created by Diplicity at %v due to game start.", time.Now()),
			}
			phaseStateID, err := phaseState.ID(ctx)
			if err != nil {
				log.Errorf(ctx, "phaseState.ID(...): %v", err)
				return err
			}

			g.Members[idx].NewestPhaseState = *phaseState

			toSave = append(toSave, phaseState)
			keys = append(keys, phaseStateID)
		}

		if err = phase.Recalc(); err != nil {
			log.Errorf(ctx, "phase.Recalc(): %v", err)
			return err
		}
		g.NewestPhaseMeta = []PhaseMeta{phase.PhaseMeta}
		phase.PhaseMeta.UnitsJSON = ""
		phase.PhaseMeta.SCsJSON = ""
		if g.NewestPhaseMeta[0].UnitsJSON == "" || g.NewestPhaseMeta[0].SCsJSON == "" {
			msg := fmt.Sprintf("Sanity check failed, game JSON data is empty while we wanted it populated!")
			log.Errorf(ctx, msg)
			return fmt.Errorf(msg)
		}

		toSave = append(toSave, g)
		keys = append(keys, gameID)

		if _, err := datastore.PutMulti(ctx, keys, toSave); err != nil {
			log.Errorf(ctx, "datastore.PutMulti(..., %+v, %+v): %v; hope datastore gets fixed", keys, toSave, err)
			return err
		}

		if err := phase.ScheduleResolution(ctx); err != nil {
			log.Errorf(ctx, "phase.ScheduleResolution(...): %v; hope datastore gets fixed", err)
			return err
		}
		log.Infof(ctx, "%v has a %d minutes phase length, scheduled resolve", PP(g), g.PhaseLengthMinutes)

		memberIds := make([]string, len(g.Members))
		for i, member := range g.Members {
			memberIds[i] = member.User.Id
		}
		if err := sendPhaseNotificationsToUsersFunc.EnqueueIn(ctx, 0, host, g.ID, phase.PhaseOrdinal, memberIds); err != nil {
			log.Errorf(
				ctx,
				"sendPhaseNotificationsToUserFunc.EnqueueIn(..., 0, %q, %v, %v, %+v): %v; hope datastore gets fixed",
				host,
				g.ID,
				phase.PhaseOrdinal,
				memberIds,
				err,
			)
			return err
		}

		uids := make([]string, len(g.Members))
		for i, m := range g.Members {
			uids[i] = m.User.Id
		}
		if err := UpdateUserStatsASAP(ctx, uids); err != nil {
			log.Errorf(ctx, "UpdateUserStatsASAP(..., %+v): %v; hope datastore gets fixed", uids, err)
			return err
		}
		gameDesc := ""
		if g.Desc != "" {
			gameDesc = fmt.Sprintf("the %v game %v", variant.Name, g.Desc)
		} else {
			gameDesc = fmt.Sprintf("this %v game", variant.Name)
		}
		greetingBody := ""
		if g.Mustered {
			greetingBody = fmt.Sprintf("Welcome to %v. Have fun!", gameDesc)
		} else {
			greetingBody = fmt.Sprintf("Welcome to %v. Before the game starts properly, all players must first declare themselves ready to play by checking 'ready to resolve'. If anyone doesn't do this within %v (before %v), they will be ejected from the game (and all other staging games) and it will re-enter the staging state. Have fun!", gameDesc, phase.DeadlineAt.Sub(time.Now()).Round(time.Minute), phase.DeadlineAt.Format(time.RFC822))
		}
		members := make([]string, len(variant.Nations))
		for idx := range variant.Nations {
			members[idx] = string(variant.Nations[idx])
		}
		if err := AsyncSendMsgFunc.EnqueueIn(
			ctx, 0,
			g.ID,
			DiplicitySender,
			members,
			greetingBody,
			host,
		); err != nil {
			log.Errorf(ctx, "AsyncSendMsgFunc(..., %v, %v, %+v, %q, %q): %v; fix it?", g.ID, DiplicitySender, variant.Nations, greetingBody, host, err)
			return err
		}

		return nil
	}, &datastore.TransactionOptions{XG: true})
	if err != nil {
		log.Errorf(ctx, "Unable to commit transaction: %v; retrying", err)
		return err
	}

	log.Infof(ctx, "asyncStartGame(..., %v, %q): *** SUCCESS ***", gameID, host)

	return nil
}

func gameMasterUpdateGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	game := &Game{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game = &Game{}
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID

		if game.GameMaster.Id != user.Id {
			return HTTPErr{"unauthorized", http.StatusUnauthorized}
		}

		if err := Copy(game, r, "PUT"); err != nil {
			return err
		}

		if _, err := datastore.Put(ctx, gameID, game); err != nil {
			return err
		}

		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return game, nil
}

func loadGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	game := &Game{}
	userStats := &UserStats{}
	if err := datastore.GetMulti(ctx,
		[]*datastore.Key{gameID, UserStatsID(ctx, user.Id)},
		[]interface{}{game, userStats},
	); err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			if merr[0] == nil && merr[1] == datastore.ErrNoSuchEntity {
				userStats.UserId = user.Id
				userStats.User = *user
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

	game.Redact(user, r)

	game.Refresh()

	filtered := Games{*game}
	if _, err = filtered.RemoveBanned(ctx, user.Id, false); err != nil {
		return nil, err
	}
	filtered.RemoveFiltered(toJoin, userStats, false)

	return &filtered[0], nil
}
