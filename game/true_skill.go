package game

import (
	"fmt"
	"net/http"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	trueskill "github.com/mafredri/go-trueskill"
	. "github.com/zond/goaeoas"
)

const (
	trueSkillKind = "TrueSkill"
)

type TrueSkill struct {
	GameID    *datastore.Key
	UserId    string
	CreatedAt time.Time
	Member    godip.Nation
	Mu        float64
	Sigma     float64
	Rating    float64
}

func GetTrueSkill(ctx context.Context, userId string) (*TrueSkill, error) {
	trueSkills := []TrueSkill{}
	if _, err := datastore.NewQuery(trueSkillKind).Filter("UserId=", userId).Order("-CreatedAt").Limit(1).GetAll(ctx, &trueSkills); err != nil {
		return nil, err
	}
	if len(trueSkills) == 0 {
		ts := trueskill.New()
		player := ts.NewPlayer()
		return &TrueSkill{
			UserId: userId,
			Mu:     player.Mu(),
			Sigma:  player.Sigma(),
			Rating: ts.TrueSkill(player),
		}, nil
	}
	return &trueSkills[0], nil
}

func (t *TrueSkill) ID(ctx context.Context) (*datastore.Key, error) {
	if t.GameID == nil || t.GameID.IntID() == 0 || t.UserId == "" {
		return nil, fmt.Errorf("TrueSkills must have game IDs with non zero int ID, and non empty user IDs")
	}
	return datastore.NewKey(ctx, trueSkillKind, t.UserId, 0, t.GameID), nil
}

func handleReRateTrueSkills(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	log.Infof(ctx, "handleReRateTrueSkills(..., ...)")

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

	trueSkillIDs, err := datastore.NewQuery(trueSkillKind).KeysOnly().GetAll(ctx, nil)
	if err != nil {
		return err
	}

	if err := datastore.DeleteMulti(ctx, trueSkillIDs); err != nil {
		return err
	}
	log.Infof(ctx, "Deleted %v TrueSkills", len(trueSkillIDs))

	iterator := datastore.NewQuery(gameResultKind).Filter("Private=", false).Order("CreatedAt").Run(ctx)

	gameResult := &GameResult{}
	seenUserIds := map[string]time.Time{}
	for _, err = iterator.Next(gameResult); err == nil; _, err = iterator.Next(gameResult) {
		for _, score := range gameResult.Scores {
			at, seen := seenUserIds[score.UserId]
			earliestEventualConsistency := at.Add(2 * time.Second)
			if seen && earliestEventualConsistency.After(time.Now()) {
				waitTime := earliestEventualConsistency.Sub(time.Now())
				log.Infof(ctx, "Waiting %v for %v to get a consistent state", waitTime, score.UserId)
				time.Sleep(waitTime)
			}
			seenUserIds[score.UserId] = time.Now()
		}
		if err = gameResult.TrueSkillRate(ctx, false); err != nil {
			return err
		}
		log.Infof(ctx, "Successfully rated %+v", gameResult)
	}

	if err == datastore.Done {
		log.Infof(ctx, "handleTrueSkillRateGameResults(..., ...) is DONE")
		return nil
	}

	return err
}
