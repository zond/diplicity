package game

import (
	"fmt"
	"time"

	"github.com/Kashomon/goglicko"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
)

const (
	glickoKind = "Glicko"
)

var (
	updateGlickosFunc *DelayFunc
	reRateGlickosFunc *DelayFunc
)

func init() {
	reRateGlickosFunc = NewDelayFunc("game-reRateGlickos", reRateGlickos)
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

func handleReRate(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthenticated", 401}
	}

	superusers, err := auth.GetSuperusers(ctx)
	if err != nil {
		return err
	}

	if !superusers.Includes(user.Id) {
		return HTTPErr{"unauthorized", 403}
	}

	ids, err := datastore.NewQuery(glickoKind).KeysOnly().GetAll(ctx, nil)
	if err != nil {
		return err
	}

	if err := datastore.DeleteMulti(ctx, ids); err != nil {
		return err
	}

	return reRateGlickosFunc.EnqueueIn(ctx, 0, "")
}

func processGlickos(ctx context.Context, gameResult *GameResult, onlyUnrated bool, continuation func(context.Context) error) error {
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
			GameID:          game.ID,
			UserId:          member.User.Id,
			CreatedAt:       gameResult.CreatedAt,
			Member:          member.Nation,
			Rating:          newRating.Rating,
			PracticalRating: newRating.Rating - 2*newRating.Deviation,
			Deviation:       newRating.Deviation,
			Volatility:      newRating.Volatility,
		})
	}

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		gameResultID := gameResult.ID(ctx)

		if err := datastore.Get(ctx, gameResultID, gameResult); err != nil {
			log.Errorf(ctx, "Unable to get game result %v: %v; hope datastore gets fixed", gameResultID, err)
			return err
		}
		if onlyUnrated && gameResult.Rated {
			log.Infof(ctx, "%v got rated while we worked, exiting", PP(gameResult))
			return nil
		}
		gameResult.Rated = true
		if _, err := datastore.Put(ctx, gameResultID, gameResult); err != nil {
			log.Errorf(ctx, "Unable to save game result %v after setting it as rated: %v; hope datastore gets fixed", PP(gameResult), err)
			return err
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

		uids := make([]string, len(newGlickos))
		for i, glicko := range newGlickos {
			uids[i] = glicko.UserId
		}
		if err := UpdateUserStatsASAP(ctx, uids); err != nil {
			log.Errorf(ctx, "Unable to enqueue updating of user stats: %v; hope datastore gets fixed", err)
			return err
		}

		if continuation != nil {
			if err := continuation(ctx); err != nil {
				log.Errorf(ctx, "Unable to run continuation: %v; fix the queue func", err)
				return err
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit rating tx: %v", err)
		return err
	}
	return nil
}

func updateGlickos(ctx context.Context) error {
	log.Infof(ctx, "updateGlickos(...)")

	unratedGameResults := GameResults{}
	_, err := datastore.NewQuery(gameResultKind).Filter("Rated=", false).Filter("Private=", false).Order("CreatedAt").Limit(2).GetAll(ctx, &unratedGameResults)
	if err != nil {
		log.Errorf(ctx, "Unable to load unrated game results: %v; hope datastore gets fixed", err)
		return err
	}

	var continuation func(context.Context) error
	if len(unratedGameResults) > 1 {
		continuation = func(ctx context.Context) error {
			log.Infof(ctx, "Still unrated games left, triggering another updateGlickos")
			return updateGlickosFunc.EnqueueIn(ctx, 0)
		}
	}

	if len(unratedGameResults) > 0 {
		if err := processGlickos(ctx, &unratedGameResults[0], true, continuation); err != nil {
			log.Errorf(ctx, "Unable to process glickos for %v: %v; fix processGlickos", PP(unratedGameResults[0]), err)
			return err
		}
	}

	log.Infof(ctx, "updateGlickos(...) *** SUCCESS ***")

	return nil
}

func reRateGlickos(ctx context.Context, cursor string) error {
	log.Infof(ctx, "reRateGlickos(..., %q)", cursor)

	query := datastore.NewQuery(gameResultKind).Filter("Private=", false).Order("CreatedAt")
	if cursor != "" {
		decoded, err := datastore.DecodeCursor(cursor)
		if err != nil {
			log.Errorf(ctx, "Unable to decode cursor %q: %v; giving up", cursor, err)
			return err
		}
		query = query.Start(decoded)
	}

	iter := query.Run(ctx)

	gameResult := &GameResult{}
	_, err := iter.Next(gameResult)
	if err == datastore.Done {
		log.Infof(ctx, "No more game results to re rate, exiting")
	} else if err != nil {
		log.Errorf(ctx, "Unable to load next game result: %v; hope datastore gets fixed", err)
		return err
	} else {
		nextCursor, err := iter.Cursor()
		if err != nil {
			log.Errorf(ctx, "Unable to get next cursor: %v; hope datastore gets fixed", err)
			return err
		}
		if err := processGlickos(ctx, gameResult, false, func(ctx context.Context) error {
			return reRateGlickosFunc.EnqueueIn(ctx, 0, nextCursor.String())
		}); err != nil {
			log.Errorf(ctx, "Unable to process glickos for %v: %v; fix processGlickos", PP(gameResult), err)
			return err
		}
	}

	log.Infof(ctx, "reRateGlickos(..., %q) *** SUCCESS ***", cursor)

	return nil
}

func GetGlicko(ctx context.Context, userId string) (*Glicko, error) {
	glickos := []Glicko{}
	if _, err := datastore.NewQuery(glickoKind).Filter("UserId=", userId).Order("-CreatedAt").Limit(1).GetAll(ctx, &glickos); err != nil {
		return nil, err
	}
	if len(glickos) == 0 {
		return &Glicko{
			UserId:          userId,
			CreatedAt:       time.Now(),
			Rating:          goglicko.DefaultRat,
			PracticalRating: goglicko.DefaultRat - 2*goglicko.DefaultDev,
			Deviation:       goglicko.DefaultDev,
			Volatility:      goglicko.DefaultVol,
		}, nil
	}
	return &glickos[0], nil
}

type Glicko struct {
	GameID          *datastore.Key
	UserId          string
	CreatedAt       time.Time
	Member          godip.Nation
	Rating          float64
	PracticalRating float64
	Deviation       float64
	Volatility      float64
}

func (g *Glicko) ID(ctx context.Context) (*datastore.Key, error) {
	if g.GameID == nil || g.GameID.IntID() == 0 || g.UserId == "" {
		return nil, fmt.Errorf("glickos must have game IDs with non zero int ID, and non empty user IDs")
	}
	return datastore.NewKey(ctx, glickoKind, g.UserId, 0, g.GameID), nil
}
