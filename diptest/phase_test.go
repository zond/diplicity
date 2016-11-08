package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

var (
	startedGameDesc string
	startedGameEnvs []*Env
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
		Body(map[string]interface{}{
		"Variant":            "Classical",
		"Desc":               gameDesc,
		"PhaseLengthMinutes": 60 * 24,
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

	startedGameDesc = gameDesc
	startedGameEnvs = envs
	t.Run("TestOrders", testOrders)
	t.Run("TestOptions", testOptions)
	t.Run("TestChat", testChat)
	t.Run("TestPhaseState", testPhaseState)
	t.Run("TestReadyResolution", testReadyResolution)
}

func testReadyResolution(t *testing.T) {
	t.Run("TestResolve", func(t *testing.T) {
		for _, env := range startedGameEnvs {
			g := env.GetRoute(game.IndexRoute).Success().
				Follow("my-started-games", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
			nat := g.Find([]string{"Properties", "Members"}, []string{"User", "Id"}, env.GetUID()).GetValue("Nation")

			order := []string{"", "Move", ""}
			switch nat {
			case "Austria":
				order[0], order[2] = "bud", "rum"
			case "England":
				order[0], order[2] = "lon", "nth"
			case "France":
				order[0], order[2] = "par", "bur"
			case "Germany":
				order[0], order[2] = "kie", "hol"
			case "Italy":
				order[0], order[2] = "nap", "ion"
			case "Turkey":
				order[0], order[2] = "con", "bul"
			case "Russia":
				order[0], order[2] = "stp", "bot"
			}

			g.Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring").
				Follow("create-order", "Links").Body(map[string]interface{}{
				"Parts": order,
			}).Success()

			g.Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring").
				Follow("phase-states", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Note"}, "").
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      false,
			}).Success()
		}
	})

	g := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)

	t.Run("TestNewPhase", func(t *testing.T) {
		p := g.Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Type"}, "Retreat")

		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "rum")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "nth")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bur")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "hol")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "ion")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bul")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bot")
	})

	t.Run("TestOldPhase", func(t *testing.T) {
		p := g.Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Type"}, "Movement")
		p.Follow("orders", "Links").Success().
			AssertLen(7, "Properties").
			AssertNotFind([]string{"Properties"}, []string{"Link", "Rel"}, "delete").
			AssertNotFind([]string{"Properties"}, []string{"Link", "Rel"}, "update")
		p.Follow("phase-states", "Links").Success().
			AssertLen(7, "Properties").
			AssertNotFind([]string{"Properties"}, []string{"Link", "Rel"}, "update")

	})
}
