package game

import (
	"fmt"
	"net/http"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine/v2"
	"google.golang.org/appengine/v2/datastore"
	"google.golang.org/appengine/v2/log"

	trueskill "github.com/mafredri/go-trueskill"
	. "github.com/zond/goaeoas"
)

const (
	trueSkillKind = "TrueSkill"
)

var (
	reRateTrueSkillsFunc *DelayFunc
)

func init() {
	reRateTrueSkillsFunc = NewDelayFunc("game-reRateTrueSkills", reRateTrueSkills)
}

func listGameResultTrueSkills(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	trueSkills := TrueSkills{}
	if _, err := datastore.NewQuery(trueSkillKind).Ancestor(gameID).GetAll(ctx, &trueSkills); err != nil {
		return err
	}

	w.SetContent(trueSkills.Item(r, gameID))
	return nil
}

type TrueSkills []TrueSkill

func (t TrueSkills) Item(r Request, gameID *datastore.Key) *Item {
	skillItems := make(List, len(t))
	for i := range t {
		skillItems[i] = t[i].Item(r)
	}
	skillsItem := NewItem(skillItems).SetName("true-skills").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListGameResultTrueSkillsRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	return skillsItem
}

type TrueSkillContent struct {
	GameID    *datastore.Key
	UserId    string
	CreatedAt time.Time
	Member    godip.Nation
	Mu        float64
	Sigma     float64
	Rating    float64
}

type TrueSkill struct {
	TrueSkillContent
	Previous         []TrueSkillContent
	HigherRatedCount int `datastore:"-"`
}

func (t TrueSkill) Item(r Request) *Item {
	return NewItem(t).SetName("true-skill")
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
			TrueSkillContent: TrueSkillContent{
				UserId: userId,
				Mu:     player.Mu(),
				Sigma:  player.Sigma(),
				Rating: ts.TrueSkill(player),
			},
		}, nil
	}
	return &trueSkills[0], nil
}

func (t *TrueSkillContent) ID(ctx context.Context) (*datastore.Key, error) {
	if t.GameID == nil || t.GameID.IntID() == 0 || t.UserId == "" {
		return nil, fmt.Errorf("TrueSkills must have game IDs with non zero int ID, and non empty user IDs")
	}
	return datastore.NewKey(ctx, trueSkillKind, t.UserId, 0, t.GameID), nil
}

func handleDeleteTrueSkills(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	log.Infof(ctx, "handleDeleteTrueSkills(..., ...)")

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

	getFunc := func() ([]*datastore.Key, error) {
		return datastore.NewQuery(trueSkillKind).KeysOnly().Limit(500).GetAll(ctx, nil)
	}
	deleted := 0
	trueSkillIDs, err := getFunc()
	for ; err == nil && len(trueSkillIDs) > 0; trueSkillIDs, err = getFunc() {
		if err := datastore.DeleteMulti(ctx, trueSkillIDs); err != nil {
			return err
		}
		deleted += len(trueSkillIDs)
	}
	if err != nil {
		return err
	}

	log.Infof(ctx, "handleDeleteTrueSkills(..., ...): Deleted %v TrueSkills", deleted)

	return nil
}

func UpdateTrueSkillsASAP(ctx context.Context) error {
	if appengine.IsDevAppServer() {
		return reRateTrueSkillsFunc.EnqueueIn(ctx, 0, 0, "", true, true)
	}
	return reRateTrueSkillsFunc.EnqueueIn(ctx, time.Second*10, 0, "", true, true)
}

func reRateTrueSkills(ctx context.Context, counter int, cursorString string, onlyUnrated bool, updateUserStats bool) error {
	log.Infof(ctx, "reRateTrueSkills(..., %v, %v, %v)", counter, cursorString, onlyUnrated)

	query := datastore.NewQuery(gameResultKind).Filter("Private=", false).Order("CreatedAt")
	if onlyUnrated {
		query = query.Filter("TrueSkillRated=", false)
	}
	if cursorString != "" {
		cursor, err := datastore.DecodeCursor(cursorString)
		if err != nil {
			return err
		}
		query = query.Start(cursor)
	}

	iterator := query.Run(ctx)
	gameResult := &GameResult{}
	if _, err := iterator.Next(gameResult); err != nil {
		if err == datastore.Done {
			log.Infof(ctx, "reRateTrueSkills(..., %v, %v, %v) is DONE", counter, cursorString, onlyUnrated)
			return nil
		}
		log.Errorf(ctx, "iterator.Next(%v): %v", gameResult, err)
		return err
	}

	if err := gameResult.TrueSkillRate(ctx, onlyUnrated, updateUserStats); err != nil {
		return err
	}
	log.Infof(ctx, "reRateTrueSkills(..., %v, %v, %v): Successfully rated %+v", counter, cursorString, onlyUnrated, gameResult)

	cursor, err := iterator.Cursor()
	if err != nil {
		log.Errorf(ctx, "reRateTrueSkills(..., %v, %v, %v): iterator.Cursor(): %v", counter, cursorString, onlyUnrated, err)
		return err
	}

	return reRateTrueSkillsFunc.EnqueueIn(ctx, 0, counter+1, cursor.String(), onlyUnrated, updateUserStats)
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

	return reRateTrueSkillsFunc.EnqueueIn(ctx, 0, 0, "", false, false)
}
