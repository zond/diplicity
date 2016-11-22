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

		env.GetRoute(game.MyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("TestLeaveAndDestroyGame", func(t *testing.T) {
		env.GetRoute(game.OpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("leave", "Links").Success()

		env.GetRoute(game.MyStagingGamesRoute).Success().
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

}

func TestGameLists(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	t.Run("TestWithoutFilters", func(t *testing.T) {
		env.GetRoute(game.MyStagingGamesRoute).Success()
		env.GetRoute(game.MyStartedGamesRoute).Success()
		env.GetRoute(game.MyFinishedGamesRoute).Success()
		env.GetRoute(game.OpenGamesRoute).Success()
		env.GetRoute(game.StartedGamesRoute).Success()
		env.GetRoute(game.FinishedGamesRoute).Success()
	})
	qp := url.Values{
		"variant": []string{"Classical"},
	}
	t.Run("TestWithVariantFilter", func(t *testing.T) {
		env.GetRoute(game.MyStagingGamesRoute).QueryParams(qp).Success()
		env.GetRoute(game.MyStartedGamesRoute).QueryParams(qp).Success()
		env.GetRoute(game.MyFinishedGamesRoute).QueryParams(qp).Success()
		env.GetRoute(game.OpenGamesRoute).QueryParams(qp).Success()
		env.GetRoute(game.StartedGamesRoute).QueryParams(qp).Success()
		env.GetRoute(game.FinishedGamesRoute).QueryParams(qp).Success()
	})
}
