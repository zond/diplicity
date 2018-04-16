package game

import (
	"net/http"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	gameResultKind = "GameResult"
)

type GameScore struct {
	UserId string
	Member godip.Nation
	SCs    int
	Score  float64
}

type GameResults []GameResult

type GameResult struct {
	GameID            *datastore.Key
	SoloWinnerMember  godip.Nation
	SoloWinnerUser    string
	DIASMembers       []godip.Nation
	DIASUsers         []string
	NMRMembers        []godip.Nation
	NMRUsers          []string
	EliminatedMembers []godip.Nation
	EliminatedUsers   []string
	AllUsers          []string
	Scores            []GameScore
	Rated             bool
	Private           bool
	CreatedAt         time.Time
}

func (g *GameResult) AssignScores() {
	if g.SoloWinnerMember != "" {
		for i := range g.Scores {
			if g.Scores[i].Member == g.SoloWinnerMember {
				g.Scores[i].Score = 100
			} else {
				g.Scores[i].Score = 0
			}
		}
	} else {
		scoreSum := float64(0)
		for i := range g.Scores {
			g.Scores[i].Score = float64(g.Scores[i].SCs * g.Scores[i].SCs)
			scoreSum += g.Scores[i].Score
		}
		ratio := 100 / scoreSum
		for i := range g.Scores {
			g.Scores[i].Score = g.Scores[i].Score * ratio
		}
	}
}

func GameResultID(ctx context.Context, gameID *datastore.Key) *datastore.Key {
	return datastore.NewKey(ctx, gameResultKind, "result", 0, gameID)
}

func (g *GameResult) ID(ctx context.Context) *datastore.Key {
	return GameResultID(ctx, g.GameID)
}

func (g *GameResult) Save(ctx context.Context) error {
	_, err := datastore.Put(ctx, g.ID(ctx), g)
	return err
}

func loadGameResult(w ResponseWriter, r Request) (*GameResult, error) {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	gameResultID := GameResultID(ctx, gameID)

	gameResult := &GameResult{}
	if err := datastore.Get(ctx, gameResultID, gameResult); err != nil {
		return nil, err
	}

	return gameResult, nil
}

var GameResultResource = &Resource{
	Load:     loadGameResult,
	FullPath: "/Game/{game_id}/GameResult",
}

func (g *GameResult) Item(r Request) *Item {
	return NewItem(g).SetName("game-result").AddLink(r.NewLink(GameResultResource.Link("self", Load, []string{"game_id", g.GameID.Encode()})))
}
