package game

import (
	"fmt"
	"time"

	"github.com/mafredri/go-trueskill"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
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
