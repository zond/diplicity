package game

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/memcache"

	. "github.com/zond/goaeoas"
)

const (
	userStatsKind          = "UserStats"
	userRatingHistogramKey = "userRatingsHistogram"
)

var (
	UpdateUserStatsFunc *DelayFunc
	updateUserStatFunc  *DelayFunc

	UserStatsResource *Resource
)

func init() {
	UpdateUserStatsFunc = NewDelayFunc("game-updateUserStats", updateUserStats)
	updateUserStatFunc = NewDelayFunc("game-updateUserStat", updateUserStat)

	userStatsListerParams := []string{"limit", "cursor"}
	UserStatsResource = &Resource{
		Load:     loadUserStats,
		FullPath: "/User/{user_id}/Stats",
		Listers: []Lister{
			{
				Path:        "/Users/TopRated",
				Route:       ListTopRatedPlayersRoute,
				Handler:     topRatedPlayersHandler.handle,
				QueryParams: userStatsListerParams,
			},
			{
				Path:        "/Users/TopReliable",
				Route:       ListTopReliablePlayersRoute,
				Handler:     topReliablePlayersHandler.handle,
				QueryParams: userStatsListerParams,
			},
			{
				Path:        "/Users/TopHated",
				Route:       ListTopHatedPlayersRoute,
				Handler:     topHatedPlayersHandler.handle,
				QueryParams: userStatsListerParams,
			},
			{
				Path:        "/Users/TopHater",
				Route:       ListTopHaterPlayersRoute,
				Handler:     topHaterPlayersHandler.handle,
				QueryParams: userStatsListerParams,
			},
			{
				Path:        "/Users/TopQuick",
				Route:       ListTopQuickPlayersRoute,
				Handler:     topQuickPlayersHandler.handle,
				QueryParams: userStatsListerParams,
			},
		},
	}

}

func UpdateUserStatsASAP(ctx context.Context, uids []string) error {
	if appengine.IsDevAppServer() {
		return UpdateUserStatsFunc.EnqueueIn(ctx, 0, uids)
	}
	return UpdateUserStatsFunc.EnqueueIn(ctx, time.Second*10, uids)
}

func updateUserStat(ctx context.Context, userId string) error {
	log.Infof(ctx, "updateUserStat(..., %q)", userId)

	if userId == "" {
		return nil
	}

	userStats := &UserStats{
		UserId: userId,
	}
	if err := userStats.UserStatsNumbers.Recalculate(ctx, false, userId); err != nil {
		log.Errorf(ctx, "Unable to recalculate public user stats %v: %v; fix UserStatsNumbers#Recalculate", PP(userStats), err)
		return err
	}
	if err := userStats.PrivateStats.Recalculate(ctx, true, userId); err != nil {
		log.Errorf(ctx, "Unable to recalculate private user stats %v: %v; fix UserStatsNumbers#Recalculate", PP(userStats), err)
		return err
	}
	latestTrueSkill, err := GetTrueSkill(ctx, userId)
	if err != nil {
		log.Errorf(ctx, "Unable to get latest TrueSkill for %q: %v; fix GetTrueSkill", userId, err)
		return err
	}
	userStats.TrueSkill = *latestTrueSkill
	user := &auth.User{}
	if err := datastore.Get(ctx, auth.UserID(ctx, userId), user); err != nil {
		log.Errorf(ctx, "Unable to load user for %q: %v; hope datastore gets fixed", userId, err)
		return err
	}
	userStats.User = *user
	if _, err := datastore.Put(ctx, userStats.ID(ctx), userStats); err != nil {
		log.Errorf(ctx, "Unable to store stats %v: %v; hope datastore gets fixed", userStats, err)
		return err
	}

	log.Infof(ctx, "updateUserStat(..., %q) *** SUCCESS ***", userId)

	return nil
}

func updateUserStats(ctx context.Context, origUids []string) error {
	log.Infof(ctx, "updateUserStats(..., %v)", PP(origUids))

	if len(origUids) == 0 {
		log.Infof(ctx, "updateUserStats(..., %v) *** NO UIDS ***", PP(origUids))
		return nil
	}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		uids := make([]string, len(origUids))
		copy(uids, origUids)
		for i := 0; i < 4 && len(uids) > 0; i++ {
			nextUid := uids[0]
			uids = uids[1:]
			if err := updateUserStatFunc.EnqueueIn(ctx, 0, nextUid); err != nil {
				log.Errorf(ctx, "Unable to enqueue updating first stat: %v; hope datastore gets fixed", err)
				return err
			}
		}
		if len(uids) > 0 {
			if err := UpdateUserStatsFunc.EnqueueIn(ctx, 0, uids); err != nil {
				log.Errorf(ctx, "Unable to enqueue updating rest: %v; hope datastore gets fixed", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit update tx: %v", err)
		return err
	}

	log.Infof(ctx, "updateUserStats(..., %v) *** SUCCESS ***", PP(origUids))

	return nil
}

type UserStatsSlice []UserStats

func (u UserStatsSlice) Item(r Request, cursor *datastore.Cursor, limit int64, name string, desc []string, route string) *Item {
	statsItems := make(List, len(u))
	for i := range u {
		statsItems[i] = u[i].Item(r)
	}
	statsItem := NewItem(statsItems).SetName(name).SetDesc([][]string{
		desc,
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: route,
	}))
	if cursor != nil {
		statsItem.AddLink(r.NewLink(Link{
			Rel:   "next",
			Route: route,
			QueryParams: url.Values{
				"cursor": []string{cursor.String()},
				"limit":  []string{fmt.Sprint(limit)},
			},
		}))
	}
	return statsItem
}

type UserStatsNumbers struct {
	JoinedGames   int
	StartedGames  int
	FinishedGames int

	SoloGames       int
	DIASGames       int
	EliminatedGames int
	DroppedGames    int

	NMRPhases    int
	ActivePhases int
	ReadyPhases  int
	Reliability  float64
	Quickness    float64

	OwnedBans  int
	SharedBans int
	Hated      float64
	Hater      float64
}

type UserStats struct {
	UserId string

	UserStatsNumbers

	PrivateStats UserStatsNumbers

	TrueSkill TrueSkill

	User auth.User
}

func (u *UserStats) Load(props []datastore.Property) error {
	err := datastore.LoadStruct(u, props)
	if _, is := err.(*datastore.ErrFieldMismatch); is {
		err = nil
	}
	return err
}

func (u *UserStats) Save() ([]datastore.Property, error) {
	return datastore.SaveStruct(u)
}

func devUserStatsUpdate(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		return fmt.Errorf("only accessible in local dev mode")
	}

	userStats := &UserStats{}
	if err := json.NewDecoder(r.Req().Body).Decode(userStats); err != nil {
		return err
	}

	if _, err := datastore.Put(ctx, UserStatsID(ctx, r.Vars()["user_id"]), userStats); err != nil {
		return err
	}

	return nil
}

func loadUserStats(w ResponseWriter, r Request) (*UserStats, error) {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	userStats := &UserStats{}
	if err := datastore.Get(ctx, UserStatsID(ctx, r.Vars()["user_id"]), userStats); err == datastore.ErrNoSuchEntity {
		userStats.UserId = r.Vars()["user_id"]
	} else if err != nil {
		return nil, err
	}

	higherRatedCount, err := datastore.NewQuery(userStatsKind).Filter("TrueSkill.Rating>", userStats.TrueSkill.Rating).Count(ctx)
	if err != nil {
		return nil, err
	}

	userStats.TrueSkill.HigherRatedCount = higherRatedCount

	return userStats, nil
}

func (u *UserStats) Item(r Request) *Item {
	u.User.Email = ""
	return NewItem(u).SetName("user-stats").
		AddLink(r.NewLink(UserStatsResource.Link("self", Load, []string{"user_id", u.UserId}))).
		AddLink(r.NewLink(Link{
			Rel:         "finished-games",
			Route:       ListOtherFinishedGamesRoute,
			RouteParams: []string{"user_id", u.UserId},
		})).
		AddLink(r.NewLink(Link{
			Rel:         "staging-games",
			Route:       ListOtherStagingGamesRoute,
			RouteParams: []string{"user_id", u.UserId},
		})).
		AddLink(r.NewLink(Link{
			Rel:         "started-games",
			Route:       ListOtherStartedGamesRoute,
			RouteParams: []string{"user_id", u.UserId},
		}))
}

func (u *UserStatsNumbers) Recalculate(ctx context.Context, private bool, userId string) error {
	var err error
	if u.JoinedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.StartedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", userId).Filter("Started=", true).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.FinishedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", userId).Filter("Finished=", true).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}

	if u.SoloGames, err = datastore.NewQuery(gameResultKind).Filter("SoloWinnerUser=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.DIASGames, err = datastore.NewQuery(gameResultKind).Filter("DIASUsers=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.EliminatedGames, err = datastore.NewQuery(gameResultKind).Filter("EliminatedUsers=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.DroppedGames, err = datastore.NewQuery(gameResultKind).Filter("NMRUsers=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}

	if u.NMRPhases, err = datastore.NewQuery(phaseResultKind).Filter("NMRUsers=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.ActivePhases, err = datastore.NewQuery(phaseResultKind).Filter("ActiveUsers=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	if u.ReadyPhases, err = datastore.NewQuery(phaseResultKind).Filter("ReadyUsers=", userId).Filter("Private=", private).Count(ctx); err != nil {
		return err
	}
	u.Reliability = float64(u.ReadyPhases+u.ActivePhases) / float64(u.NMRPhases+1)
	u.Quickness = float64(u.ReadyPhases) / float64(u.ActivePhases+u.NMRPhases+1)

	if u.OwnedBans, err = datastore.NewQuery(banKind).Filter("OwnerIds=", userId).Count(ctx); err != nil {
		return err
	}
	if u.SharedBans, err = datastore.NewQuery(banKind).Filter("UserIds=", userId).Count(ctx); err != nil {
		return err
	}
	u.Hater = float64(u.OwnedBans) / float64(u.StartedGames+1)
	u.Hated = float64(u.SharedBans-u.OwnedBans) / float64(u.StartedGames+1)
	return nil
}

func UserStatsID(ctx context.Context, userId string) *datastore.Key {
	return datastore.NewKey(ctx, userStatsKind, userId, 0, nil)
}

func (u *UserStats) Redact() {
	u.User.Email = ""
}

func (u *UserStats) ID(ctx context.Context) *datastore.Key {
	return UserStatsID(ctx, u.UserId)
}

type UserRatingHistogram struct {
	FirstBucketRating int
	Counts            []int
}

func (uh *UserRatingHistogram) Item(r Request) *Item {
	return NewItem(uh).SetName("UserRatingHistogram")
}

func getUserRatingHistogram(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	histogram := &UserRatingHistogram{}
	_, err := memcache.JSON.Get(ctx, userRatingHistogramKey, histogram)
	if err == nil {
		w.SetContent(histogram.Item(r))
		return nil
	} else if err != nil && err != memcache.ErrCacheMiss {
		return err
	}

	games := Games{}
	if _, err := datastore.NewQuery(gameKind).Filter("Finished=", false).GetAll(ctx, &games); err != nil {
		return err
	}

	userIds := map[string]bool{}
	for _, game := range games {
		for _, member := range game.Members {
			if member.User.Id != "" {
				userIds[member.User.Id] = true
			}
		}
	}
	userStatsIDs := make([]*datastore.Key, 0, len(userIds))
	for userId := range userIds {
		userStatsIDs = append(userStatsIDs, UserStatsID(ctx, userId))
	}

	userStatsIDsToUse := make([]*datastore.Key, 0, 1000)
	for _, idx := range rand.Perm(len(userStatsIDs)) {
		userStatsIDsToUse = append(userStatsIDsToUse, userStatsIDs[idx])
		if len(userStatsIDsToUse) == 1000 {
			break
		}
	}
	userStats := make([]UserStats, len(userStatsIDsToUse))

	if err := datastore.GetMulti(ctx, userStatsIDsToUse, userStats); err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			for _, serr := range merr {
				if serr != nil && serr != datastore.ErrNoSuchEntity {
					return err
				}
			}
		} else if err == datastore.ErrNoSuchEntity {
			err = nil
		} else {
			return err
		}
	}

	minRating := math.MaxFloat64
	maxRating := -math.MaxFloat64
	for _, stats := range userStats {
		minRating = math.Min(minRating, stats.TrueSkill.Rating)
		maxRating = math.Max(maxRating, stats.TrueSkill.Rating)
	}
	if minRating > maxRating {
		minRating, maxRating = 0, 0
	}

	histogram.FirstBucketRating = int(math.Floor(minRating))
	histogram.Counts = make([]int, int(math.Floor(maxRating)-math.Floor(minRating))+1)
	for _, stats := range userStats {
		histogram.Counts[int(math.Floor(stats.TrueSkill.Rating))-histogram.FirstBucketRating] += 1
	}

	if err := memcache.JSON.Set(ctx, &memcache.Item{
		Key:        userRatingHistogramKey,
		Object:     histogram,
		Expiration: time.Hour * 24,
	}); err != nil {
		return err
	}

	w.SetContent(histogram.Item(r))
	return nil
}
