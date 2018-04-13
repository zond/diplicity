package game

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
)

const (
	userStatsKind = "UserStats"
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
	latestGlicko, err := GetGlicko(ctx, userId)
	if err != nil {
		log.Errorf(ctx, "Unable to get latest Glicko for %q: %v; fix GetGlicko", userId, err)
		return err
	}
	userStats.Glicko = *latestGlicko
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

func updateUserStats(ctx context.Context, uids []string) error {
	log.Infof(ctx, "updateUserStats(..., %v)", PP(uids))

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := updateUserStatFunc.EnqueueIn(ctx, 0, uids[0]); err != nil {
			log.Errorf(ctx, "Unable to enqueue updating first stat: %v; hope datastore gets fixed", err)
			return err
		}
		if len(uids) > 1 {
			if err := UpdateUserStatsFunc.EnqueueIn(ctx, 0, uids[1:]); err != nil {
				log.Errorf(ctx, "Unable to enqueue updating rest: %v; hope datastore gets fixed", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit update tx: %v", err)
		return err
	}

	log.Infof(ctx, "updateUserStats(..., %v) *** SUCCESS ***", PP(uids))

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

	Glicko Glicko
	User   auth.User
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
		return nil, HTTPErr{"unauthenticated", 401}
	}

	userStats := &UserStats{}
	if err := datastore.Get(ctx, UserStatsID(ctx, r.Vars()["user_id"]), userStats); err == datastore.ErrNoSuchEntity {
		userStats.UserId = r.Vars()["user_id"]
	} else if err != nil {
		return nil, err
	}

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
