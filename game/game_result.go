package game

import (
	"fmt"
	"math"
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
	UserId      string
	Member      godip.Nation
	SCs         int
	Score       float64
	Explanation string
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

// AssignScores uses http://windycityweasels.org/tribute-scoring-system/
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
		// Find board topper size, number of survivors, and number of SCs in total.
		numSCs := 0
		topperSize := 0
		survivors := 0
		for i := range g.Scores {
			if g.Scores[i].SCs > topperSize {
				topperSize = g.Scores[i].SCs
			}
			if g.Scores[i].SCs > 0 {
				survivors += 1
			}
			numSCs += g.Scores[i].SCs
		}

		// Minimum number of SCs required to top the board is ceil(number of SCs / number of players) + 1 (ceil(34 / 7) + 1 = 5 + 1 = 6).
		minTopperSize := int(math.Ceil(float64(numSCs)/float64(len(g.Scores))) + 1)
		// Tribute is one for each SCs over minimum topper size.
		tributePerSurvivor := 0
		if topperSize > minTopperSize {
			tributePerSurvivor = topperSize - minTopperSize
		}
		// Score per SC is 34 / number of SCs.
		scorePerSC := 34.0 / float64(numSCs)

		// Find toppers, and assign survival and SC scores, and find tribute sum.
		tributeSum := 0.0
		topperNations := map[godip.Nation]bool{}
		for i := range g.Scores {
			survivalPart := 66.0 / float64(survivors)
			scPart := scorePerSC * float64(g.Scores[i].SCs)
			g.Scores[i].Explanation = fmt.Sprintf("Survival:%v\nSupply centers:%v\n", survivalPart, scPart)
			g.Scores[i].Score = scPart + survivalPart
			if g.Scores[i].SCs == topperSize {
				topperNations[g.Scores[i].Member] = true
			} else {
				if g.Scores[i].Score > float64(tributePerSurvivor) {
					tributeSum += float64(tributePerSurvivor)
					g.Scores[i].Explanation += fmt.Sprintf("Tribute:%v", tributePerSurvivor)
					g.Scores[i].Score -= float64(tributePerSurvivor)
				} else {
					tributeSum += g.Scores[i].Score
					g.Scores[i].Explanation += fmt.Sprintf("Tribute:%v", g.Scores[i].Score)
					g.Scores[i].Score = 0
				}
			}
		}

		topperShare := float64(tributeSum) / float64(len(topperNations))

		// Distribute tribute.
		for i := range g.Scores {
			if topperNations[g.Scores[i].Member] {
				g.Scores[i].Explanation += fmt.Sprintf("Tribute:%v", topperShare)
				g.Scores[i].Score += topperShare
			}
		}
	}

	sum := 0.0
	for i := range g.Scores {
		sum += g.Scores[i].Score
	}
	if int(sum*10000) != 1000000 {
		panic(fmt.Errorf("Tribute algorithm not implemented correctly, wanted sum of scores to be 100, but got %v: %+v", sum, g.Scores))
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
