package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestJoinLeaveGame(t *testing.T) {
	gameDesc := String("test-game")

	env1 := NewEnv().SetUID(String("fake"))
	env2 := NewEnv().SetUID(String("fake"))

	env1.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]string{
		"Variant": "Classical",
		"Desc":    gameDesc,
	}).Success().
		AssertEq(gameDesc, "Properties", "Desc")

	t.Run("TestJoiningExistingGame", func(t *testing.T) {
		env2.GetRoute(game.IndexRoute).Success().
			Follow("open-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("join", "Links").Success()

		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("TestAllLeavingAndDestroyingGame", func(t *testing.T) {
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("leave", "Links").Success()

		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("leave", "Links").Success()

		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}
