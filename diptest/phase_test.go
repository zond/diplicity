package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestStartGame(t *testing.T) {
	gameDesc := String("test-game")

	envs := []*Env{
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
	}

	envs[0].GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]string{
		"Variant": "Classical",
		"Desc":    gameDesc,
	}).Success().
		AssertStringEq(gameDesc, "Properties", "Desc")

	for _, env := range envs[1:] {
		env.GetRoute(game.IndexRoute).Success().
			Follow("open-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
			Follow("join", "Links").Success()
	}

	envs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
		Follow("phases", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

	testOrders(gameDesc, envs)
}
