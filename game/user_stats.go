package game

import (
	"time"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
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
	UserId        string
	StartedGames  int
	FinishedGames int
	OwnedBans     int
	SharedBans    int
	Hated         float64
	Hater         float64
}

func (u *UserStats) Recalculate(ctx context.Context) error {
	var err error
	if u.StartedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", u.UserId).Filter("Started=", true).Count(ctx); err != nil {
		return err
	}
	if u.FinishedGames, err = datastore.NewQuery(gameKind).Filter("Members.User.Id=", u.UserId).Filter("Finished=", true).Count(ctx); err != nil {
		return err
	}
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
