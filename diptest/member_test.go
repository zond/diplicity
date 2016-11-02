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
		AssertStringEq(gameDesc, "Properties", "Desc")

	env2.GetRoute(game.IndexRoute).Success().
		Follow("open-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
		Follow("join", "Links").Success()

	env2.GetRoute(game.MyStagingGamesRoute).Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)

	env1.GetRoute(game.MyStagingGamesRoute).Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
		Follow("leave", "Links").Success()

	env2.GetRoute(game.MyStagingGamesRoute).Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
		Follow("leave", "Links").Success()

	env1.GetRoute(game.MyStagingGamesRoute).Success().
		AssertNotFind([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)

	env2.GetRoute(game.MyStagingGamesRoute).Success().
		AssertNotFind([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)
}
