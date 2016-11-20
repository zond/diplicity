package game

import (
	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
)

const (
	userStatsKind = "UserStats"
)

var (
	UpdateUserStatsFunc *DelayFunc
)

func init() {
	UpdateUserStatsFunc = NewDelayFunc("game-updateUserStats", updateUserStats)
}

func updateUserStats(ctx context.Context, deltas []UserStats) error {
	log.Infof(ctx, "updateUserStats(..., %v)", PP(deltas))

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		userStats := &UserStats{}
		if err := datastore.Get(ctx, UserStatsID(ctx, deltas[0].UserId), userStats); err == datastore.ErrNoSuchEntity {
			userStats.UserId = deltas[0].UserId
		} else if err != nil {
			log.Errorf(ctx, "Unable to load stats for %q: %v; hope datastore gets fixed", deltas[0].UserId, err)
			return err
		}
		userStats.Apply(deltas[0])
		if _, err := datastore.Put(ctx, userStats.ID(ctx), userStats); err != nil {
			log.Errorf(ctx, "Unable to store stats %v: %v; hope datastore gets fixed", userStats, err)
			return err
		}
		if len(deltas) > 1 {
			if err := UpdateUserStatsFunc.EnqueueIn(ctx, 0, deltas[1:]); err != nil {
				log.Errorf(ctx, "Unable to enqueue updating rest: %v; hope datastore gets fixed", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit update tx: %v", err)
		return err
	}

	log.Infof(ctx, "updateUserStats(..., %v) *** SUCCESS ***", PP(deltas))

	return nil
}

type UserStats struct {
	UserId        string
	StartedGames  int
	FinishedGames int
	OwnedBans     int
	SharedBans    int
	Hated         float64
	Hater         float64
}

func (u *UserStats) Apply(o UserStats) {
	u.StartedGames += o.StartedGames
	u.FinishedGames += o.FinishedGames
	u.OwnedBans += o.OwnedBans
	u.SharedBans += o.SharedBans
	if u.StartedGames > 0 {
		u.Hater = float64(u.OwnedBans) / float64(u.StartedGames)
		u.Hated = float64(u.SharedBans-u.OwnedBans) / float64(u.StartedGames)
	}
}

func UserStatsID(ctx context.Context, userId string) *datastore.Key {
	return datastore.NewKey(ctx, userStatsKind, userId, 0, nil)
}

func (u *UserStats) ID(ctx context.Context) *datastore.Key {
	return UserStatsID(ctx, u.UserId)
}
