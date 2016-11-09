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
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)
	})

	t.Run("TestLeaveAndDestroyGame", func(t *testing.T) {
		env.GetRoute(game.OpenGamesRoute).Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
			Follow("leave", "Links").Success()

		env.GetRoute(game.MyStagingGamesRoute).Success().
			AssertNotFind([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)
	})
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
