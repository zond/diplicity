package diptest

import (
	"fmt"
	"testing"

	"github.com/zond/diplicity/game"
)

var (
	startedGameDesc string
	startedGames    []*Result
	startedGameEnvs []*Env
	startedGameNats []string
	startedGameID   string
)

// Not concurrency safe
func withStartedGame(f func()) {
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
		AssertEq(gameDesc, "Properties", "Desc")

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

	startedGameNats = make([]string, len(envs))
	startedGames = make([]*Result, len(envs))
	for i, env := range envs {
		startedGames[i] = env.GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)
		startedGameNats[i] = startedGames[i].Find([]string{"Properties", "Members"}, []string{"User", "Id"}, env.GetUID()).GetValue("Nation").(string)
		startedGameID = startedGames[i].GetValue("Properties", "ID").(string)
	}

	startedGameDesc = gameDesc
	startedGameEnvs = envs
	f()
}

func TestStartGame(t *testing.T) {
	withStartedGame(func() {
		t.Run("TestGameState", testGameState)
		t.Run("TestOrders", testOrders)
		t.Run("TestOptions", testOptions)
		t.Run("TestChat", testChat)
		t.Run("TestPhaseState", testPhaseState)
		t.Run("TestReadyResolution", testReadyResolution)
	})
}

func TestDIASEnding(t *testing.T) {
	withStartedGame(func() {
		t.Run("PreparePhaseStatesWithWantsDIAS", func(t *testing.T) {
			for i, nat := range startedGameNats {
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

				p := startedGames[i].Follow("phases", "Links").Success().
					Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

				p.Follow("create-order", "Links").Body(map[string]interface{}{
					"Parts": order,
				}).Success()

				p.Follow("phase-states", "Links").Success().
					Find([]string{"Properties"}, []string{"Properties", "Note"}, "").
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": true,
					"WantsDIAS":      true,
				}).Success()
			}
		})

		t.Run("VerifyGameFinished", func(t *testing.T) {
			g := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("finished-games", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
			startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("started-games", "Links").Success().
				AssertNotFind([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
			g.Follow("game-result", "Links").Success().
				AssertLen(7, "Properties", "DIASMembers").
				AssertNil("Properties", "NMRMembers").
				AssertNil("Properties", "EliminatedMembers").
				AssertEq("", "Properties", "SoloWinner")
		})

	})
}

func TestTimeoutResolution(t *testing.T) {
	withStartedGame(func() {
		t.Run("PreparePhaseStatesWithNotReadyButHasOrders", func(t *testing.T) {
			for i, nat := range startedGameNats {
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

				p := startedGames[i].Follow("phases", "Links").Success().
					Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

				p.Follow("create-order", "Links").Body(map[string]interface{}{
					"Parts": order,
				}).Success()

				isReady := true
				if i == 0 {
					isReady = false
				} else {
					isReady = true
				}

				p.Follow("phase-states", "Links").Success().
					Find([]string{"Properties"}, []string{"Properties", "Note"}, "").
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": isReady,
					"WantsDIAS":      false,
				}).Success()
			}
		})

		t.Run("TestNoResolve-1", func(t *testing.T) {
			startedGames[0].Follow("phases", "Links").Success().
				AssertNotFind([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 2)
		})

		t.Run("TimeoutResolve-1", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "1").Success()
		})

		t.Run("TestNextPhaseNoProbation", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 3).
				AssertEq(false, "Properties", "Resolved")

			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "rum")
			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "nth")
			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bur")
			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "hol")
			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "ion")
			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bul")
			p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bot")

			p.Follow("phase-states", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[0]).
				AssertEq(false, "Properties", "WantsDIAS").
				AssertEq(false, "Properties", "OnProbation").
				AssertEq(false, "Properties", "ReadyToResolve")

			startedGames[1].Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 3).
				Follow("phase-states", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[1]).
				AssertEq(false, "Properties", "WantsDIAS").
				AssertEq(false, "Properties", "OnProbation").
				AssertEq(false, "Properties", "ReadyToResolve")
		})

		var expectedLocs []string

		t.Run("PreparePhaseStatesNotReadyNoOrders", func(t *testing.T) {
			for i, nat := range startedGameNats {
				expectedLocs = []string{}
				order := []string{"", "Move", ""}

				switch nat {
				case "Austria":
					order[2], order[0] = "bud", "rum"
				case "England":
					order[2], order[0] = "lon", "nth"
				case "France":
					order[2], order[0] = "par", "bur"
				case "Germany":
					order[2], order[0] = "kie", "hol"
				case "Italy":
					order[2], order[0] = "nap", "ion"
				case "Turkey":
					order[2], order[0] = "con", "bul"
				case "Russia":
					order[2], order[0] = "stp/sc", "bot"
				}

				p := startedGames[i].Follow("phases", "Links").Success().
					Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 3)

				hasOrders := true
				if i == 0 {
					hasOrders = false
				} else {
					expectedLocs = append(expectedLocs, order[2])
					hasOrders = true
				}

				if hasOrders {
					p.Follow("create-order", "Links").Body(map[string]interface{}{
						"Parts": order,
					}).Success()
				}
			}
		})

		t.Run("TestNoResolve-2", func(t *testing.T) {
			startedGames[0].Follow("phases", "Links").Success().
				AssertNotFind([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 4)
		})

		t.Run("TimeoutResolve-2", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "3").Success()
		})

		t.Run("TestNextPhaseHasProbation", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 6).
				AssertEq(false, "Properties", "Resolved")

			for _, loc := range expectedLocs {
				p.Find([]string{"Properties", "Units"}, []string{"Province"}, loc)
			}

			p.Follow("phase-states", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[0]).
				AssertEq(true, "Properties", "WantsDIAS").
				AssertEq(true, "Properties", "ReadyToResolve").
				AssertEq(true, "Properties", "OnProbation")

			startedGames[1].Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 6).
				Follow("phase-states", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[1]).
				AssertEq(false, "Properties", "WantsDIAS").
				AssertEq(false, "Properties", "OnProbation").
				AssertEq(false, "Properties", "ReadyToResolve")
		})

		t.Run("TimeoutResolve-3", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "6").Success()
		})

		t.Run("TestGameFinished", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("started-games", "Links").Success().
				AssertNotFind([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
			g := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("finished-games", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
			g.Follow("game-result", "Links").Success().
				AssertNil("Properties", "DIASMembers").
				AssertLen(7, "Properties", "NMRMembers").
				AssertNil("Properties", "EliminatedMembers").
				AssertEq("", "Properties", "SoloWinner")
		})
	})

}

func testReadyResolution(t *testing.T) {
	t.Run("TestResolve", func(t *testing.T) {
		for i, nat := range startedGameNats {
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

			startedGames[i].Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring").
				Follow("create-order", "Links").Body(map[string]interface{}{
				"Parts": order,
			}).Success()

			wantsDIAS := false
			if i == 0 {
				wantsDIAS = true
			} else {
				wantsDIAS = false
			}

			startedGames[i].Follow("phases", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring").
				Follow("phase-states", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Note"}, "").
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      wantsDIAS,
			}).Success()
		}
	})

	t.Run("TestOldPhase", func(t *testing.T) {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 1).
			AssertEq(true, "Properties", "Resolved")
		p.Follow("orders", "Links").Success().
			AssertLen(7, "Properties").
			AssertNotFind([]string{"Properties"}, []string{"Link", "Rel"}, "delete").
			AssertNotFind([]string{"Properties"}, []string{"Link", "Rel"}, "update")
		p.Follow("phase-states", "Links").Success().
			AssertLen(7, "Properties").
			AssertNotFind([]string{"Properties"}, []string{"Link", "Rel"}, "update")
	})
	t.Run("TestSkippedPhase", func(t *testing.T) {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 2).
			AssertEq(true, "Properties", "Resolved")

		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "rum")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "nth")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bur")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "hol")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "ion")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bul")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bot")

		p.Follow("phase-states", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[0]).
			AssertEq(true, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(true, "Properties", "ReadyToResolve")
		p.Follow("phase-states", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[1]).
			AssertEq(false, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(true, "Properties", "ReadyToResolve")
	})
	t.Run("TestNextPhase", func(t *testing.T) {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 3).
			AssertEq(false, "Properties", "Resolved")

		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "rum")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "nth")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bur")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "hol")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "ion")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bul")
		p.Find([]string{"Properties", "Units"}, []string{"Province"}, "bot")

		p.Follow("phase-states", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[0]).
			AssertEq(true, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(false, "Properties", "ReadyToResolve")

		startedGames[1].Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "PhaseOrdinal"}, 3).
			Follow("phase-states", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Nation"}, startedGameNats[1]).
			AssertEq(false, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(false, "Properties", "ReadyToResolve")
	})
	t.Run("TestGameNotFinished", func(t *testing.T) {
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("finished-games", "Links").Success().
			AssertNotFind([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)
	})

}
