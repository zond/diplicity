package game

import (
	"fmt"
	"time"

	"github.com/Kashomon/goglicko"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	dip "github.com/zond/godip/common"
)

const (
	glickoKind = "Glicko"
)

var (
	updateGlickosFunc *DelayFunc
)

func init() {
	updateGlickosFunc = NewDelayFunc("game-updateGlickos", updateGlickos)
}

func UpdateGlickosASAP(ctx context.Context) error {
	if appengine.IsDevAppServer() {
		return updateGlickosFunc.EnqueueIn(ctx, 0)
	}
	return updateGlickosFunc.EnqueueIn(ctx, time.Second*10)
}

func makeRating(userId string, glickos []Glicko) (*goglicko.Rating, error) {
	for _, glicko := range glickos {
		if glicko.UserId == userId {
			return goglicko.NewRating(glicko.Rating, glicko.Deviation, glicko.Volatility), nil
		}
	}
	return nil, fmt.Errorf("No rating for %v found in %v", userId, glickos)
}

func makeOpponentsAndResults(userId string, glickos []Glicko, scores []GameScore) ([]*goglicko.Rating, []goglicko.Result, error) {
	userScore := 0.0
	for _, score := range scores {
		if score.UserId == userId {
			userScore = score.Score
			break
		}
	}

	opponents := []*goglicko.Rating{}
	results := []goglicko.Result{}
	for _, score := range scores {
		if score.UserId != userId {
			rating, err := makeRating(score.UserId, glickos)
			if err != nil {
				return nil, nil, err
			}
			opponents = append(opponents, rating)
			results = append(results, 0.5+goglicko.Result(userScore-score.Score)/200.0)
		}
	}
	if len(opponents) != len(scores)-1 || len(results) != len(scores)-1 {
		return nil, nil, fmt.Errorf("Didn't find exactly as many opponents and results as scores - 1 (opponents: %v, results: %v)", opponents, results)
	}

	return opponents, results, nil
}

func updateGlickos(ctx context.Context) error {
	log.Infof(ctx, "updateGlickos(...)")

	unratedGameResults := datastore.NewQuery(gameResultKind).Filter("Rated=", false).Order("CreatedAt").Run(ctx)
	gameResult := &GameResult{}
	for gameResultID, err := unratedGameResults.Next(gameResult); err == nil; _, err = unratedGameResults.Next(gameResult) {

		game := &Game{}
		if err := datastore.Get(ctx, gameResult.GameID, game); err != nil {
			log.Errorf(ctx, "Unable to load game for %v: %v; hope datastore gets fixed", gameResult, err)
			return err
		}
		game.ID = gameResult.GameID

		glickos := make([]Glicko, len(game.Members))
		done := make(chan error, len(game.Members))
		for i, member := range game.Members {
			go func(i int, member Member) {
				found, err := GetGlicko(ctx, member.User.Id)
				if err == nil {
					glickos[i] = *found
				}
				done <- err
			}(i, member)
		}
		for _ = range game.Members {
			if err := <-done; err != nil {
				log.Errorf(ctx, "Unable to fetch latest glicko for all members: %v; fix GetGlicko or hope datastore gets fixed", err)
				return err
			}
		}

		newGlickos := []Glicko{}
		for _, member := range game.Members {
			rating, err := makeRating(member.User.Id, glickos)
			if err != nil {
				log.Errorf(ctx, "Unable to make a rating for %v with %v: %v; fix makeRating", PP(member), PP(glickos), err)
				return err
			}
			opponents, results, err := makeOpponentsAndResults(member.User.Id, glickos, gameResult.Scores)
			if err != nil {
				log.Errorf(ctx, "Unable to make opponents and scores for %v with %v and %v: %v; fix makeOpponentsAndResults", PP(member), PP(glickos), PP(gameResult.Scores), err)
				return err
			}
			newRating, err := goglicko.CalculateRating(rating, opponents, results)
			if err != nil {
				log.Errorf(ctx, "Unable to calculate new rating for %v with %v, %v and %v: %v; fix goglicko?", PP(member), PP(rating), PP(opponents), PP(results), err)
				return err
			}
			newGlickos = append(newGlickos, Glicko{
				GameID:     game.ID,
				UserId:     member.User.Id,
				CreatedAt:  gameResult.CreatedAt,
				Member:     member.Nation,
				Rating:     newRating.Rating,
				Deviation:  newRating.Deviation,
				Volatility: newRating.Volatility,
			})
		}
		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			if err := datastore.Get(ctx, gameResultID, gameResult); err != nil {
				log.Errorf(ctx, "Unable to get game result %v: %v; hope datastore gets fixed", gameResultID, err)
				return err
			}
			if gameResult.Rated {
				log.Infof(ctx, "%v got rated while we worked, exiting", PP(gameResult))
				return nil
			}
			glickoIDs := make([]*datastore.Key, len(newGlickos))
			for i, glicko := range newGlickos {
				id, err := glicko.ID(ctx)
				if err != nil {
					log.Errorf(ctx, "Unable to create ID for %v: %v; fix Glicko#ID", PP(glicko), err)
					return err
				}
				glickoIDs[i] = id
			}
			if _, err := datastore.PutMulti(ctx, glickoIDs, newGlickos); err != nil {
				log.Errorf(ctx, "Unable to store new glickos %v => %v: %v; hope datastore gets fixed", PP(glickoIDs), PP(newGlickos), err)
				return err
			}
			return nil
		}, &datastore.TransactionOptions{XG: false}); err != nil {
			log.Errorf(ctx, "Unable to commit rating tx: %v", err)
			return err
		}

		gameResult = &GameResult{}
	}

	log.Infof(ctx, "updateGlickos(...) *** SUCCESS ***")

	return nil
}

func GetGlicko(ctx context.Context, userId string) (*Glicko, error) {
	glickos := []Glicko{}
	if _, err := datastore.NewQuery(glickoKind).Filter("UserId=", userId).Order("-CreatedAt").Limit(1).GetAll(ctx, &glickos); err != nil {
		return nil, err
	}
	if len(glickos) == 0 {
		return &Glicko{
			UserId:     userId,
			CreatedAt:  time.Now(),
			Rating:     1500,
			Deviation:  350,
			Volatility: goglicko.DefaultVol,
		}, nil
	}
	return &glickos[0], nil
}

type Glicko struct {
	GameID     *datastore.Key
	UserId     string
	CreatedAt  time.Time
	Member     dip.Nation
	Rating     float64
	Deviation  float64
	Volatility float64
}

func (g *Glicko) ID(ctx context.Context) (*datastore.Key, error) {
	if g.GameID == nil || g.GameID.IntID() == 0 || g.UserId == "" {
		return nil, fmt.Errorf("glickos must have game IDs with non zero int ID, and non empty user IDs")
	}
	return datastore.NewKey(ctx, glickoKind, g.UserId, 0, g.GameID), nil
}
