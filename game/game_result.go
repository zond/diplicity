package game

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/mafredri/go-trueskill"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

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

type GameScores []GameScore

func (gs GameScores) Assign() {
	// Find board topper size, number of survivors, and number of SCs in total.
	numSCs := 0
	topperSize := 0
	survivors := 0
	for i := range gs {
		if gs[i].SCs > topperSize {
			topperSize = gs[i].SCs
		}
		if gs[i].SCs > 0 {
			survivors += 1
		}
		numSCs += gs[i].SCs
	}

	// Minimum number of SCs required to top the board is ceil(number of SCs / number of players) + 1 (ceil(34 / 7) + 1 = 5 + 1 = 6).
	minTopperSize := int(math.Ceil(float64(numSCs)/float64(len(gs))) + 1)
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
	for i := range gs {
		if gs[i].SCs == 0 {
			gs[i].Explanation = "Eliminated:0"
		} else if gs[i].SCs > 0 {
			survivalPart := 66.0 / float64(survivors)
			scPart := scorePerSC * float64(gs[i].SCs)
			gs[i].Explanation = fmt.Sprintf("Survival:%v\nSupply centers:%v\n", survivalPart, scPart)
			gs[i].Score = scPart + survivalPart
			if gs[i].SCs == topperSize {
				topperNations[gs[i].Member] = true
			} else {
				if gs[i].Score > float64(tributePerSurvivor) {
					tributeSum += float64(tributePerSurvivor)
					gs[i].Explanation += fmt.Sprintf("Tribute:%v", -tributePerSurvivor)
					gs[i].Score -= float64(tributePerSurvivor)
				} else {
					tributeSum += gs[i].Score
					gs[i].Explanation += fmt.Sprintf("Tribute:%v", -gs[i].Score)
					gs[i].Score = 0
				}
			}
		}
	}

	topperShare := float64(tributeSum) / float64(len(topperNations))

	// Distribute tribute.
	for i := range gs {
		if topperNations[gs[i].Member] {
			gs[i].Explanation += fmt.Sprintf("Tribute:%v", topperShare)
			gs[i].Score += topperShare
		}
	}

	sum := 0.0
	for i := range gs {
		sum += gs[i].Score
	}
	if int(math.Round(sum*10000)) != 1000000 {
		panic(fmt.Errorf("Tribute algorithm not implemented correctly, wanted sum of scores to be 100, but got %v: %+v", sum, gs))
	}
}

type GameResults []GameResult

type GameResult struct {
	GameID               *datastore.Key
	SoloWinnerMember     godip.Nation
	SoloWinnerUser       string
	DIASMembers          []godip.Nation
	DIASUsers            []string
	NMRMembers           []godip.Nation
	NMRUsers             []string
	EliminatedMembers    []godip.Nation
	EliminatedUsers      []string
	AllUsers             []string
	Scores               GameScores
	Rated                bool
	TrueSkillRated       bool
	TrueSkillProbability float64
	Private              bool
	CreatedAt            time.Time
}

type player struct {
	score  GameScore
	player trueskill.Player
}

type players []player

func (p players) Less(i, j int) bool {
	return p[i].score.Score > p[j].score.Score
}

func (p players) Len() int {
	return len(p)
}

func (p players) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

// TrueSkillRate must be idempotent, because it gets called every n minutes by a cron job,
// and can't run inside a transaction since it updates too many users in large games.
func (g *GameResult) TrueSkillRate(ctx context.Context, onlyUnrated bool) error {
	// Last action of the func is to store this GameResult with TrueSkillRated = true, to avoid
	// repetition.
	if onlyUnrated && g.TrueSkillRated {
		return nil
	}

	// To make it idempotent even if some TrueSkills have been stored, we delete any
	// that exist when we start.
	oldTrueSkillIDs := make([]*datastore.Key, len(g.Scores))
	var err error
	for idx := range g.Scores {
		if oldTrueSkillIDs[idx], err = (&TrueSkill{GameID: g.GameID, UserId: g.Scores[idx].UserId}).ID(ctx); err != nil {
			return err
		}
	}
	if err := datastore.DeleteMulti(ctx, oldTrueSkillIDs); err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			for _, serr := range merr {
				if serr != nil && serr != datastore.ErrNoSuchEntity {
					return err
				}
			}
		} else {
			return err
		}
	}

	// We make a slice of players, consisting of GameScores and TrueSkill players.
	players := make(players, len(g.Scores))
	for idx := range players {
		trueSkill, err := GetTrueSkill(ctx, g.Scores[idx].UserId)
		if err != nil {
			return err
		}
		players[idx] = player{
			score:  g.Scores[idx],
			player: trueskill.NewPlayer(trueSkill.Mu, trueSkill.Sigma),
		}
	}

	// Sort them to get highest scores first.
	sort.Sort(players)

	// Define the relationship between each player on the leaderboard by creating
	// a slice of draw bools, where a draw means this player and the next had the
	// same score.
	draws := make([]bool, len(players)-1)
	for idx := range players[:len(players)-1] {
		draws[idx] = players[idx].score.Score == players[idx+1].score.Score
	}

	// Update the players using TrueSkill.
	tsPlayers := make([]trueskill.Player, len(players))
	for idx := range players {
		tsPlayers[idx] = players[idx].player
	}
	ts := trueskill.New()
	newTSPlayers, prob := ts.AdjustSkillsWithDraws(tsPlayers, draws)
	log.Infof(ctx, "AdjustSkillsWithDraws(%+v, %+v): %+v", tsPlayers, draws, newTSPlayers)

	// Create new TrueSkill entities for this game.
	newTrueSkills := make([]TrueSkill, len(players))
	newTrueSkillIDs := make([]*datastore.Key, len(players))
	userIds := make([]string, len(players))
	for idx := range players {
		userIds[idx] = players[idx].score.UserId
		newTrueSkills[idx] = TrueSkill{
			GameID:    g.GameID,
			UserId:    players[idx].score.UserId,
			CreatedAt: time.Now(),
			Member:    players[idx].score.Member,
			Mu:        newTSPlayers[idx].Mu(),
			Sigma:     newTSPlayers[idx].Sigma(),
			Rating:    ts.TrueSkill(newTSPlayers[idx]),
		}
		if newTrueSkillIDs[idx], err = newTrueSkills[idx].ID(ctx); err != nil {
			return err
		}
	}

	if _, err := datastore.PutMulti(ctx, newTrueSkillIDs, newTrueSkills); err != nil {
		return err
	}

	// Schedule UserStats updates for all users in the game, to let them see their new ratings.
	if err := UpdateUserStatsASAP(ctx, userIds); err != nil {
		return err
	}

	// Save the probability of this outcome, along with the fact that we are now rated.
	g.TrueSkillProbability = prob
	g.TrueSkillRated = true

	_, err = datastore.Put(ctx, g.ID(ctx), g)

	return err
}

// AssignScores uses http://windycityweasels.org/tribute-scoring-system/
func (g *GameResult) AssignScores() {
	if g.SoloWinnerMember != "" {
		for i := range g.Scores {
			if g.Scores[i].Member == g.SoloWinnerMember {
				g.Scores[i].Score = 100
				g.Scores[i].Explanation = "Solo victory:100"
			} else {
				g.Scores[i].Score = 0
				g.Scores[i].Explanation = "Lost to solo victory:0"
			}
		}
	} else {
		g.Scores.Assign()
	}
}

func GameResultID(ctx context.Context, gameID *datastore.Key) *datastore.Key {
	return datastore.NewKey(ctx, gameResultKind, "result", 0, gameID)
}

func (g *GameResult) ID(ctx context.Context) *datastore.Key {
	return GameResultID(ctx, g.GameID)
}

// I have seen some signs that there are broken GameResults in the database. Thus, some validation.
func (g *GameResult) Validate(game *Game) error {
	if !g.GameID.Equal(game.ID) {
		return fmt.Errorf("Invalid GameResult %+v, GameID doesn't match parent %+v", g, game)
	}
	userMap := map[string]bool{}
	for _, member := range game.Members {
		userMap[member.User.Id] = true
	}
	if g.SoloWinnerUser != "" && !userMap[g.SoloWinnerUser] {
		return fmt.Errorf("Invalid GameResult %+v, SoloWinner doesn't match parent %+v", g, game)
	}
	isSubset := func(users []string) bool {
		for _, user := range users {
			if !userMap[user] {
				return false
			}
		}
		return true
	}
	if !isSubset(g.DIASUsers) {
		return fmt.Errorf("Invalid GameResult %+v, DIASUsers don't match parent %+v", g, game)
	}
	if !isSubset(g.EliminatedUsers) {
		return fmt.Errorf("Invalid GameResult %+v, EliminatedUsers don't match parent %+v", g, game)
	}
	if !isSubset(g.AllUsers) {
		return fmt.Errorf("Invalid GameResult %+v, AllUsers don't match parent %+v", g, game)
	}
	return nil
}

func (g *GameResult) Save(ctx context.Context, game *Game) error {
	if err := g.Validate(game); err != nil {
		return err
	}
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
	rval := NewItem(g).SetName("game-result").
		AddLink(r.NewLink(GameResultResource.Link("self", Load, []string{"game_id", g.GameID.Encode()})))
	if g.TrueSkillRated {
		rval = rval.AddLink(r.NewLink(Link{
			Rel:         "true-skills",
			Route:       ListGameResultTrueSkillsRoute,
			RouteParams: []string{"game_id", g.GameID.Encode()},
		}))
	}
	return rval
}
