package game

import (
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
)

func init() {
	UpdateUserStatsFunc = NewDelayFunc("game-updateUserStats", updateUserStats)
	updateUserStatFunc = NewDelayFunc("game-updateUserStat", updateUserStat)
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
	if err := userStats.Recalculate(ctx); err != nil {
		return err
	}
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

type UserStats struct {
	UserId string

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

var UserStatsResource = &Resource{
	Load:     loadUserStats,
	FullPath: "/User/{user_id}/Stats",
}

func loadUserStats(w ResponseWriter, r Request) (*UserStats, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	if user.Id != r.Vars()["user_id"] {
		return nil, HTTPErr{"can only load owned stats", 403}
	}

	userStats := &UserStats{}
	if err := datastore.Get(ctx, UserStatsID(ctx, user.Id), userStats); err == datastore.ErrNoSuchEntity {
		userStats.UserId = user.Id
	} else if err != nil {
		return nil, err
	}

	return userStats, nil
}

func (u *UserStats) Item(r Request) *Item {
	return NewItem(u).SetName("user-stats").AddLink(r.NewLink(UserStatsResource.Link("self", Load, []string{"user_id", u.UserId})))
}

func (u *UserStats) Recalculate(ctx context.Context) error {
	var err error
	if u.StartedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", u.UserId).Filter("Started=", true).Count(ctx); err != nil {
		return err
	}
	if u.FinishedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", u.UserId).Filter("Finished=", true).Count(ctx); err != nil {
		return err
	}

	if u.SoloGames, err = datastore.NewQuery(gameResultKind).Filter("SoloWinnerUser=", u.UserId).Count(ctx); err != nil {
		return err
	}
	if u.DIASGames, err = datastore.NewQuery(gameResultKind).Filter("DIASUsers=", u.UserId).Count(ctx); err != nil {
		return err
	}
	if u.EliminatedGames, err = datastore.NewQuery(gameResultKind).Filter("EliminatedUsers=", u.UserId).Count(ctx); err != nil {
		return err
	}
	if u.DroppedGames, err = datastore.NewQuery(gameResultKind).Filter("NMRUsers=", u.UserId).Count(ctx); err != nil {
		return err
	}

	if u.NMRPhases, err = datastore.NewQuery(phaseResultKind).Filter("NMRUsers=", u.UserId).Count(ctx); err != nil {
		return err
	}
	if u.ActivePhases, err = datastore.NewQuery(phaseResultKind).Filter("ActiveUsers=", u.UserId).Count(ctx); err != nil {
		return err
	}
	if u.ReadyPhases, err = datastore.NewQuery(phaseResultKind).Filter("ReadyUsers=", u.UserId).Count(ctx); err != nil {
		return err
	}
	u.Reliability = float64(u.ReadyPhases+u.ActivePhases) / float64(u.ReadyPhases+u.ActivePhases+u.NMRPhases+1)
	u.Quickness = float64(u.ReadyPhases) / float64(u.ReadyPhases+u.ActivePhases+u.NMRPhases+1)

	if u.OwnedBans, err = datastore.NewQuery(banKind).Filter("OwnerIds=", u.UserId).Count(ctx); err != nil {
		return err
	}
	if u.SharedBans, err = datastore.NewQuery(banKind).Filter("UserIds=", u.UserId).Count(ctx); err != nil {
		return err
	}
	u.Hater = float64(u.OwnedBans) / float64(u.StartedGames+1)
	u.Hated = float64(u.SharedBans-u.OwnedBans) / float64(u.StartedGames+1)
	return nil
}

func UserStatsID(ctx context.Context, userId string) *datastore.Key {
	return datastore.NewKey(ctx, userStatsKind, userId, 0, nil)
}

func (u *UserStats) ID(ctx context.Context) *datastore.Key {
	return UserStatsID(ctx, u.UserId)
}
