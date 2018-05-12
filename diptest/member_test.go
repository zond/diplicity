package diptest

import (
	"testing"
	"time"

	"github.com/zond/diplicity/game"
)

func TestJoinLeaveGame(t *testing.T) {
	gameDesc := String("test-game")

	env1 := NewEnv().SetUID(String("fake"))
	env2 := NewEnv().SetUID(String("fake"))

	env1.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Variant":            "Classical",
			"Desc":               gameDesc,
			"PhaseLengthMinutes": time.Duration(60),
			"NoMerge":            true,
			"FirstMember": &game.Member{
				NationPreferences: "Austria,England",
			},
		}).Success().
		AssertEq(gameDesc, "Properties", "Desc").
		Find("Austria,England", []string{"Properties", "Members"}, []string{"NationPreferences"})

	t.Run("TestJoiningExistingGame", func(t *testing.T) {
		env2.GetRoute(game.IndexRoute).Success().
			Follow("open-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("join", "Links").Body(map[string]interface{}{
			"NationPreferences": "England,Austria",
		}).Success().
			Find("England,Austria", []string{"Properties", "NationPreferences"})

		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Find("England,Austria", []string{"Properties", "Members"}, []string{"NationPreferences"})
	})

	t.Run("TestGameAlias", func(t *testing.T) {
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Find(env1.GetUID(), []string{"Properties", "Members"}, []string{"User", "Id"}).
			AssertEq("", "GameAlias")
		alias := String("alias")
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("update-membership", "Links").Body(map[string]interface{}{
			"GameAlias":         alias,
			"NationPreferences": "Russia,Turkey",
		}).Success().
			AssertEq(alias, "Properties", "GameAlias").
			AssertEq("Russia,Turkey", "Properties", "NationPreferences")
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Find(env1.GetUID(), []string{"Properties", "Members"}, []string{"User", "Id"}).
			AssertEq(alias, "GameAlias").
			AssertEq("Russia,Turkey", "NationPreferences")
		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Find(env1.GetUID(), []string{"Properties", "Members"}, []string{"User", "Id"}).
			AssertEq("", "GameAlias").
			AssertEq("", "NationPreferences")
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
