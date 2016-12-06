package diptest

import (
	"net/url"
	"testing"

	"github.com/zond/diplicity/game"
)

func TestCreateLeaveGame(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	t.Run("TestCreateGame", func(t *testing.T) {
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]string{
			"Variant": "Classical",
			"Desc":    gameDesc,
		}).Success().
			AssertEq(gameDesc, "Properties", "Desc")

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			AssertNil("Properties", "NewestPhaseMeta")
	})

	t.Run("TestLeaveAndDestroyGame", func(t *testing.T) {
		env.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("leave", "Links").Success()

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}

func TestGameListFilters(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":        "Classical",
		"Desc":           gameDesc,
		"MaxHated":       10,
		"MaxHater":       10,
		"MinReliability": 10,
		"MinQuickness":   10,
		"MinRating":      10,
		"MaxRating":      100,
	}).Failure()
	env.PutRoute(game.DevUserStatsUpdateRoute).RouteParams("user_id", env.GetUID()).Body(map[string]interface{}{
		"UserId":      env.GetUID(),
		"Reliability": 10,
		"Quickness":   10,
		"Hated":       0,
		"Hater":       0,
		"Glicko": &game.Glicko{
			PracticalRating: 20,
		},
	}).Success()
	gameURLString := env.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":        "Classical",
		"Desc":           gameDesc,
		"MaxHated":       10,
		"MaxHater":       10,
		"MinReliability": 10,
		"MinQuickness":   10,
		"MinRating":      10,
		"MaxRating":      100,
	}).Success().
		AssertEq(gameDesc, "Properties", "Desc").
		Find("self", []string{"Links"}, []string{"Rel"}).GetValue("URL").(string)
	gameURL, err := url.Parse(gameURLString)
	if err != nil {
		panic(err)
	}
	gameURL.RawQuery = ""

	env2 := NewEnv().SetUID(String("fake"))

	env2.GetRoute(game.IndexRoute).Success().
		Follow("open-games", "Links").Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	env2.GetURL(gameURL.String()).Success().
		AssertLen(1, "Properties", "FailedRequirements").
		Find("MinRating", []string{"Properties", "FailedRequirements"}, nil)

	env2.PutRoute(game.DevUserStatsUpdateRoute).RouteParams("user_id", env2.GetUID()).Body(map[string]interface{}{
		"UserId":      env.GetUID(),
		"Reliability": 10,
		"Quickness":   10,
		"Hated":       0,
		"Hater":       0,
		"Glicko": &game.Glicko{
			PracticalRating: 20,
		},
	}).Success()

	env2.GetRoute(game.IndexRoute).Success().
		Follow("open-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	env2.GetURL(gameURL.String()).Success().
		AssertNil("Properties", "FailedRequirements")

	for _, f := range []filter{
		{
			"variant",
			"Classical",
			true,
		},
		{
			"variant",
			"blapp",
			false,
		},
		{
			"min-reliability",
			"5:15",
			true,
		},
		{
			"min-reliability",
			"0:5",
			false,
		},
		{
			"min-reliability",
			"15:20",
			false,
		},
		{
			"min-quickness",
			"5:15",
			true,
		},
		{
			"min-quickness",
			"0:5",
			false,
		},
		{
			"min-quickness",
			"15:20",
			false,
		},
		{
			"min-rating",
			"5:15",
			true,
		},
		{
			"min-rating",
			"0:5",
			false,
		},
		{
			"min-rating",
			"15:20",
			false,
		},
		{
			"max-rating",
			"95:115",
			true,
		},
		{
			"max-rating",
			"10:15",
			false,
		},
		{
			"max-rating",
			"125:130",
			false,
		},
		{
			"max-hater",
			"5:15",
			true,
		},
		{
			"max-hater",
			"0:5",
			false,
		},
		{
			"max-hater",
			"15:25",
			false,
		},
		{
			"max-hated",
			"5:15",
			true,
		},
		{
			"max-hated",
			"0:5",
			false,
		},
		{
			"max-hated",
			"15:25",
			false,
		},
	} {
		res := env2.GetRoute(game.ListOpenGamesRoute).QueryParams(url.Values{
			f.name: []string{f.value},
		}).Success()
		if f.wantFind {
			res.Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		} else {
			res.AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		}
	}
}

type filter struct {
	name     string
	value    string
	wantFind bool
}

func TestGameLists(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.ListMyStagingGamesRoute).Success()
	env.GetRoute(game.ListMyStartedGamesRoute).Success()
	env.GetRoute(game.ListMyFinishedGamesRoute).Success()
	env.GetRoute(game.ListOpenGamesRoute).Success()
	env.GetRoute(game.ListStartedGamesRoute).Success()
	env.GetRoute(game.ListFinishedGamesRoute).Success()
}
