package diptest

import (
	"fmt"
	"math"
	"testing"

	"github.com/kr/pretty"
	"github.com/zond/diplicity/game"
)

var (
	startedGameDesc     string
	startedGames        []*Result
	startedGameEnvs     []*Env
	startedGameNats     []string
	startedGameIdxByNat map[string]int
	startedGameID       string
)

type memberOrders struct {
	nat  string
	ord  [][]string
	dias bool
}

type orderSet []memberOrders

func (set orderSet) execute(phaseOrdinal int) {
	for _, member := range set {
		gameIdx := startedGameIdxByNat[member.nat]
		phase := startedGameEnvs[gameIdx].GetRoute("Phase.Load").RouteParams("game_id", startedGameID, "phase_ordinal", fmt.Sprint(phaseOrdinal)).Success()
		for _, ord := range member.ord {
			phase.Follow("create-order", "Links").Body(map[string]interface{}{
				"Parts": ord,
			}).Success()
			fmt.Printf("o")
		}
		if member.dias {
			phase.Follow("phase-states", "Links").Success().
				Find(phaseOrdinal, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"WantsDIAS": true,
			}).Success()
		}
	}
	for _, env := range startedGameEnvs {
		WaitForEmptyQueue("game-asyncResolvePhase")
		phase := env.GetRoute("Phase.Load").RouteParams("game_id", startedGameID, "phase_ordinal", fmt.Sprint(phaseOrdinal)).Success()
		if !phase.GetValue("Properties", "Resolved").(bool) {
			phase.Follow("phase-states", "Links").Success().
				Find(phaseOrdinal, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
			}).Success()
		}
	}
	WaitForEmptyQueue("game-asyncResolvePhase")
}

type orderSets []orderSet

// Not concurrency safe
func withStartedGame(f func()) {
	withStartedGameOpts(nil, f)
}

// Not concurrency safe
func withStartedGameOpts(conf func(m map[string]interface{}), f func()) {
	withStartedGameOptsAndOrders(conf, nil, f)
}

func withStartedGameOptsAndOrders(conf func(m map[string]interface{}), orders orderSets, f func()) {
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

	opts := map[string]interface{}{
		"Variant":            "Classical",
		"NoMerge":            true,
		"Desc":               gameDesc,
		"PhaseLengthMinutes": 60 * 24,
	}
	if conf != nil {
		conf(opts)
	}
	envs[0].GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(opts).Success().
		AssertEq(gameDesc, "Properties", "Desc")
	gameID := envs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-staging-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)

	for _, env := range envs[1:] {
		env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(map[string]interface{}{}).Success()
	}

	WaitForEmptyQueue("game-asyncStartGame")

	envs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
		AssertEq(false, "Properties", "Mustered").
		Follow("phases", "Links").Success().
		Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

	startedGameNats = make([]string, len(envs))
	startedGames = make([]*Result, len(envs))
	startedGameIdxByNat = map[string]int{}
	for _, env := range envs {
		g := env.GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		g.Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
			Follow("phase-states", "Links").Success().
			Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
			Follow("update", "Links").Body(map[string]interface{}{"ReadyToResolve": true}).Success()
		startedGameID = g.GetValue("Properties", "ID").(string)
	}

	WaitForEmptyQueue("game-asyncResolvePhase")

	for i, env := range envs {
		startedGames[i] = env.GetRoute("Game.Load").RouteParams("id", startedGameID).Success()
		startedGameNats[i] = startedGames[i].
			Find(env.GetUID(), []string{"Properties", "Members"}, []string{"User", "Id"}).GetValue("Nation").(string)
		if startedGameNats[i] == "" {
			panic("Should have a nation here")
		}
		startedGameIdxByNat[startedGameNats[i]] = i
	}

	envs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
		AssertEq(true, "Properties", "Mustered")
	startedGames[0].Follow("phases", "Links").Success().
		Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

	startedGameEnvs = envs
	startedGameDesc = gameDesc

	executed := false
	for phaseOrdinalMinus1, set := range orders {
		set.execute(phaseOrdinalMinus1 + 1)
		fmt.Print("p")
		executed = true
	}
	if executed {
		fmt.Println()
	}

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
		t.Run("TestBanEfficacy", testBanEfficacy)
		t.Run("TestMessageFlagging", testMessageFlagging)
	})
}

var russianSoloOrders = orderSets{
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"stp",
					"Move",
					"bot",
				},
				{
					"sev",
					"Move",
					"bla",
				},
				{
					"mos",
					"Move",
					"ukr",
				},
				{
					"war",
					"Move",
					"gal",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"bot",
					"Move",
					"swe",
				},
				{
					"ukr",
					"Move",
					"rum",
				},
				{
					"bla",
					"Move",
					"bul",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"stp/nc",
					"Build",
					"Fleet",
				},
				{
					"war",
					"Build",
					"Army",
				},
				{
					"sev",
					"Build",
					"Army",
				},
			},
		},
	},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"swe",
					"Move",
					"den",
				},
				{
					"stp",
					"Move",
					"nwy",
				},
				{
					"war",
					"Move",
					"pru",
				},
				{
					"gal",
					"Move",
					"bud",
				},
				{
					"rum",
					"Support",
					"gal",
					"bud",
				},
				{
					"bul",
					"Move",
					"bla",
				},
				{
					"sev",
					"Move",
					"arm",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"rum",
					"Move",
					"ser",
				},
				{
					"bla",
					"Support",
					"arm",
					"ank",
				},
				{
					"arm",
					"Move",
					"ank",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"stp/nc",
					"Build",
					"Fleet",
				},
				{
					"war",
					"Build",
					"Army",
				},
				{
					"sev",
					"Build",
					"Army",
				},
				{
					"mos",
					"Build",
					"Army",
				},
			},
		},
	},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"stp",
					"Move",
					"nwy",
				},
				{
					"nwy",
					"Move",
					"nth",
				},
				{
					"den",
					"Move",
					"bal",
				},
				{
					"war",
					"Move",
					"gal",
				},
				{
					"mos",
					"Move",
					"war",
				},
				{
					"bud",
					"Support",
					"ser",
					"tri",
				},
				{
					"ser",
					"Move",
					"tri",
				},
				{
					"sev",
					"Move",
					"rum",
				},
				{
					"ank",
					"Support",
					"bla",
					"con",
				},
				{
					"bla",
					"Move",
					"con",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"nwy",
					"Move",
					"nrg",
				},
				{
					"bal",
					"Support",
					"pru",
					"ber",
				},
				{
					"pru",
					"Move",
					"ber",
				},
				{
					"war",
					"Move",
					"sil",
				},
				{
					"gal",
					"Move",
					"vie",
				},
				{
					"bud",
					"Support",
					"gal",
					"vie",
				},
				{
					"rum",
					"Move",
					"ser",
				},
				{
					"con",
					"Support",
					"ank",
					"smy",
				},
				{
					"ank",
					"Move",
					"smy",
				},
			},
		},
	},
	{},
	{},
	{
		{
			nat: "Russia",
			ord: [][]string{
				{
					"nrg",
					"Move",
					"edi",
				},
				{
					"nth",
					"Support",
					"nrg",
					"edi",
				},
				{
					"bal",
					"Move",
					"kie",
				},
				{
					"ber",
					"Support",
					"bal",
					"kie",
				},
				{
					"vie",
					"Move",
					"tyr",
				},
				{
					"bud",
					"Move",
					"vie",
				},
				{
					"con",
					"Move",
					"aeg",
				},
			},
		},
	},
	{},
}

func TestSoloEnding(t *testing.T) {
	withStartedGameOptsAndOrders(nil, russianSoloOrders, func() {
		orderSet{
			{
				nat:  "England",
				dias: true,
			},
		}.execute(len(russianSoloOrders) + 1)
		g := startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success()
		g.AssertBoolEq(true, "Properties", "Finished")
		g.Follow("game-result", "Links").Success().AssertNil("Properties", "DIASMembers").AssertEq("Russia", "Properties", "SoloWinnerMember")
		WaitForEmptyQueue("game-reRateTrueSkills")
		WaitForEmptyQueue("game-updateUserStats")
		WaitForEmptyQueue("game-updateUserStat")
		for idx, env := range startedGameEnvs {
			wantedScore := 0.0
			wantedRating := 10.0
			if startedGameNats[idx] == "Russia" {
				wantedScore = 100
				wantedRating = 12.0
			}
			env.GetRoute("GameResult.Load").RouteParams("game_id", startedGameID).Success().
				Find(env.GetUID(), []string{"Properties", "Scores"}, []string{"UserId"}).
				AssertEq(wantedScore, "Score")
			if foundRating := env.GetRoute("UserStats.Load").RouteParams("user_id", env.GetUID()).Success().
				GetValue("Properties", "TrueSkill", "Rating").(float64); math.Round(foundRating) != wantedRating {
				t.Errorf("Got rating %v for %v, wanted %v", foundRating, startedGameNats[idx], wantedRating)
			}
		}
	})
}

func TestPreliminaryScores(t *testing.T) {
	withStartedGame(func() {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})
		p.Find("Germany", []string{"Properties", "PreliminaryScores"}, []string{"Member"}).Find(14.064935064935066, []string{"Score"})
	})
}

func TestLastYearEnding(t *testing.T) {
	withStartedGameOpts(func(opts map[string]interface{}) {
		opts["LastYear"] = 1901
	}, func() {
		for i, nat := range startedGameNats {
			p := startedGames[i].Follow("phases", "Links").Success().
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

			p.Follow("phase-states", "Links").Success().
				Find(nat, []string{"Properties"}, []string{"Properties", "Nation"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
			}).Success()
		}
		WaitForEmptyQueue("game-asyncResolvePhase")
		for i, nat := range startedGameNats {
			p := startedGames[i].Follow("phases", "Links").Success().
				Find("Fall", []string{"Properties"}, []string{"Properties", "Season"})

			p.Follow("phase-states", "Links").Success().
				Find(nat, []string{"Properties"}, []string{"Properties", "Nation"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
			}).Success()
		}
		WaitForEmptyQueue("game-asyncResolvePhase")
		startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().AssertEq(true, "Properties", "Finished")
	})
}

var phaseMessageTestOrders = orderSets{
	{
		{
			nat: "Austria",
			ord: [][]string{
				{
					"vie",
					"Move",
					"tyr",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Austria",
			ord: [][]string{
				{
					"tyr",
					"Move",
					"ven",
				},
			},
		},
		{
			nat: "Italy",
			ord: [][]string{
				{
					"ven",
					"Move",
					"pie",
				},
			},
		},
	},
	{},
}

func TestPhaseMessages(t *testing.T) {
	withStartedGameOptsAndOrders(nil, phaseMessageTestOrders, func() {
		for nat, idx := range startedGameIdxByNat {
			member := startedGameEnvs[idx].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(nat, []string{"Properties", "Members"}, []string{"Nation"})
			if startedGameNats[idx] == "Austria" {
				member.AssertEq("MayBuild:1", "NewestPhaseState", "Messages")
			} else if startedGameNats[idx] == "Italy" {
				member.AssertEq("MustDisband:1", "NewestPhaseState", "Messages")
			} else {
				member.AssertEq("MayBuild:0", "NewestPhaseState", "Messages")
			}
		}
	})
}

var phaseLengthTestOrders = orderSets{
	{
		{
			nat: "Austria",
			ord: [][]string{
				{
					"vie",
					"Move",
					"tyr",
				},
			},
		},
	},
	{},
	{
		{
			nat: "Austria",
			ord: [][]string{
				{
					"tyr",
					"Move",
					"ven",
				},
			},
		},
		{
			nat: "Austria",
			ord: [][]string{
				{
					"tri",
					"Support",
					"tyr",
					"ven",
				},
			},
		},
	},
	{},
}

func TestPhaseLengths(t *testing.T) {
	withStartedGameOpts(func(opts map[string]interface{}) {
		opts["PhaseLengthMinutes"] = 60
		opts["NonMovementPhaseLengthMinutes"] = 30
	}, func() {
		if nextIn := startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			GetValue("Properties", "NewestPhaseMeta").([]interface{})[0].(map[string]interface{})["NextDeadlineIn"].(float64) / 1000000000 / 60; nextIn > 61 || nextIn < 59 {
			t.Errorf("Wanted 60 minutes, got %v", nextIn)
		}

		for phaseOrdinalMinus1, set := range phaseLengthTestOrders {
			set.execute(phaseOrdinalMinus1 + 1)
		}
		fmt.Println()
		WaitForEmptyQueue("game-asyncResolvePhase")

		if nextIn := startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			GetValue("Properties", "NewestPhaseMeta").([]interface{})[0].(map[string]interface{})["NextDeadlineIn"].(float64) / 1000000000 / 60; nextIn > 31 || nextIn < 29 {
			t.Errorf("Wanted 30 minutes, got %v", nextIn)
		}

	})
}

func TestDIASEnding(t *testing.T) {
	withStartedGame(func() {
		WaitForEmptyQueue("game-updateUserStats")
		statsPreviously := startedGameEnvs[0].GetRoute("UserStats.Load").RouteParams("user_id", startedGameEnvs[0].GetUID()).Success().
			GetValue("Properties").(map[string]interface{})
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
					Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

				p.Follow("create-order", "Links").Body(map[string]interface{}{
					"Parts": order,
				}).Success()

				p.Follow("phase-states", "Links").Success().
					Find(startedGameNats[i], []string{"Properties"}, []string{"Properties", "Nation"}).
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": true,
					"WantsDIAS":      true,
				}).Success()
			}
		})

		WaitForEmptyQueue("game-asyncResolvePhase")

		t.Run("VerifyGameFinished", func(t *testing.T) {
			g := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("finished-games", "Links").Success().
				Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			g.Find(2, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
				AssertEq(true, "Resolved")
			startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("started-games", "Links").Success().
				AssertNotFind(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			res := g.Follow("game-result", "Links").Success().
				AssertLen(7, "Properties", "DIASMembers").
				AssertLen(7, "Properties", "DIASUsers").
				AssertNil("Properties", "NMRMembers").
				AssertNil("Properties", "NMRUsers").
				AssertNil("Properties", "EliminatedMembers").
				AssertNil("Properties", "EliminatedUsers").
				AssertEq("", "Properties", "SoloWinnerMember").
				AssertEq("", "Properties", "SoloWinnerUser")
			for i := range startedGameEnvs {
				res.Find(startedGameNats[i], []string{"Properties", "DIASMembers"}, nil)
				res.Find(startedGameEnvs[i].GetUID(), []string{"Properties", "DIASUsers"}, nil)
			}
		})

		t.Run("VerifyStatsChanged", func(t *testing.T) {
			WaitForEmptyQueue("game-reRateTrueSkills")
			WaitForEmptyQueue("game-updateUserStats")
			statsAfter := startedGameEnvs[0].GetRoute("UserStats.Load").RouteParams("user_id", startedGameEnvs[0].GetUID()).Success().
				GetValue("Properties").(map[string]interface{})
			privateStatsPrev := statsPreviously["PrivateStats"]
			privateStatsAfter := statsAfter["PrivateStats"]
			delete(statsPreviously, "PrivateStats")
			delete(statsAfter, "PrivateStats")
			delete(statsPreviously, "User")
			delete(statsAfter, "User")
			if diff := pretty.Diff(privateStatsPrev, privateStatsAfter); len(diff) > 0 {
				panic(fmt.Errorf("Private stats changed after resolution: %+v", diff))
			}
			if diff := pretty.Diff(statsPreviously, statsAfter); len(diff) == 0 {
				panic(fmt.Errorf("Public stats didn't change after resolution"))
			}

		})

		t.Run("TestPrivateGames", func(t *testing.T) {
			gameDesc := String("test-game")

			startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("create-game", "Links").Body(map[string]interface{}{
				"Variant":            "Classical",
				"NoMerge":            true,
				"Desc":               gameDesc,
				"Private":            true,
				"PhaseLengthMinutes": 60,
			}).Success()
			gameID := ""
			t.Run("TestVisibility", func(t *testing.T) {
				gameID = startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
					Follow("my-staging-games", "Links").Success().
					Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
					GetValue("Properties", "ID").(string)

				startedGameEnvs[1].GetRoute(game.IndexRoute).Success().
					Follow("open-games", "Links").Success().
					AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			})
			t.Run("TestStatsVisibility", func(t *testing.T) {
				WaitForEmptyQueue("game-reRateTrueSkills")
				WaitForEmptyQueue("game-updateUserStats")
				statsPreviously := startedGameEnvs[0].GetRoute("UserStats.Load").RouteParams("user_id", startedGameEnvs[0].GetUID()).Success().
					GetValue("Properties").(map[string]interface{})
				for _, env := range startedGameEnvs[1:] {
					env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
						Follow("join", "Links").Body(map[string]interface{}{}).Success()
				}
				WaitForEmptyQueue("game-asyncStartGame")
				startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
					Follow("my-started-games", "Links").Success().
					Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
					Follow("phases", "Links").Success().
					Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})
				for _, env := range startedGameEnvs {
					p := env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
						Follow("phases", "Links").Success().
						Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

					p.Follow("phase-states", "Links").Success().
						Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
						Follow("update", "Links").Body(map[string]interface{}{
						"ReadyToResolve": true,
					}).Success()
				}
				WaitForEmptyQueue("game-asyncResolvePhase")
				for _, env := range startedGameEnvs {
					p := env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
						Follow("phases", "Links").Success().
						Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

					p.Follow("phase-states", "Links").Success().
						Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
						Follow("update", "Links").Body(map[string]interface{}{
						"ReadyToResolve": true,
						"WantsDIAS":      true,
					}).Success()
				}
				WaitForEmptyQueue("game-asyncResolvePhase")
				g := startedGameEnvs[0].GetRoute(game.ListMyFinishedGamesRoute).Success().
					Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
				g.Find(2, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
					AssertEq(true, "Resolved")
				WaitForEmptyQueue("game-updateUserStats")
				statsAfter := startedGameEnvs[0].GetRoute("UserStats.Load").RouteParams("user_id", startedGameEnvs[0].GetUID()).Success().
					GetValue("Properties").(map[string]interface{})
				privateStatsPrev := statsPreviously["PrivateStats"]
				privateStatsAfter := statsAfter["PrivateStats"]
				delete(statsPreviously, "PrivateStats")
				delete(statsAfter, "PrivateStats")
				delete(statsPreviously["TrueSkill"].(map[string]interface{}), "HigherRatedCount")
				delete(statsAfter["TrueSkill"].(map[string]interface{}), "HigherRatedCount")
				delete(statsPreviously, "User")
				delete(statsAfter, "User")
				if diff := pretty.Diff(statsPreviously, statsAfter); len(diff) > 0 {
					panic(fmt.Errorf("Public stats changed after resolution: %+v", diff))
				}
				if diff := pretty.Diff(privateStatsPrev, privateStatsAfter); len(diff) == 0 {
					panic(fmt.Errorf("Private stats didn't change after resolution"))
				}
			})
		})

	})
}

func TestTimeoutResolution(t *testing.T) {
	withStartedGame(func() {
		gameDesc := String("game-desc")
		t.Run("CreateStagingGamePlayer0", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.IndexRoute).
				Success().Follow("create-game", "Links").
				Body(map[string]interface{}{
					"Variant":            "Classical",
					"NoMerge":            true,
					"Desc":               gameDesc,
					"PhaseLengthMinutes": 60 * 24,
				}).Success()
		})
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
					Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

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
					Find(startedGameNats[i], []string{"Properties"}, []string{"Properties", "Nation"}).
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": isReady,
					"WantsDIAS":      false,
				}).Success()
			}
		})

		t.Run("TestNewestPhaseState-1", func(t *testing.T) {
			startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
				Find(startedGameID, []string{"NewestPhaseState", "GameID"})
			startedGameEnvs[1].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[1], []string{"Properties", "Members"}, []string{"Nation"}).
				Find(startedGameID, []string{"NewestPhaseState", "GameID"})
			startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[1], []string{"Properties", "Members"}, []string{"Nation"}).
				AssertNil("NewestPhaseState", "GameID")
			startedGameEnvs[1].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
				AssertNil("NewestPhaseState", "GameID")
		})

		t.Run("TestNoResolve-1", func(t *testing.T) {
			startedGames[0].Follow("phases", "Links").Success().
				AssertNotFind(2, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"})
			startedGames[0].Follow("self", "Links").Success().
				Find(1, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
				AssertEq(false, "Resolved")
		})

		t.Run("TimeoutResolve-1", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "1").Success()
		})

		t.Run("TestNewestPhaseState-1", func(t *testing.T) {
			startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
				Find(startedGameID, []string{"NewestPhaseState", "GameID"})
			startedGameEnvs[1].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[1], []string{"Properties", "Members"}, []string{"Nation"}).
				Find(startedGameID, []string{"NewestPhaseState", "GameID"})
			startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[1], []string{"Properties", "Members"}, []string{"Nation"}).
				AssertNil("NewestPhaseState", "GameID")
			startedGameEnvs[1].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
				AssertNil("NewestPhaseState", "GameID")
		})

		t.Run("TestStagingGamePlayer0Present", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.ListOpenGamesRoute).Success().
				Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		})

		t.Run("TestNextPhaseNoProbation", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(false, "Properties", "Resolved")

			startedGames[0].Follow("self", "Links").Success().
				Find(3, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
				AssertEq(false, "Resolved")

			p.Find("rum", []string{"Properties", "Units"}, []string{"Province"})
			p.Find("nth", []string{"Properties", "Units"}, []string{"Province"})
			p.Find("bur", []string{"Properties", "Units"}, []string{"Province"})
			p.Find("hol", []string{"Properties", "Units"}, []string{"Province"})
			p.Find("ion", []string{"Properties", "Units"}, []string{"Province"})
			p.Find("bul", []string{"Properties", "Units"}, []string{"Province"})
			p.Find("bot", []string{"Properties", "Units"}, []string{"Province"})

			p.Follow("phase-states", "Links").Success().
				Find(startedGameNats[0], []string{"Properties"}, []string{"Properties", "Nation"}).
				AssertEq(false, "Properties", "WantsDIAS").
				AssertEq(false, "Properties", "OnProbation").
				AssertEq(false, "Properties", "ReadyToResolve")

			startedGames[1].Follow("phases", "Links").Success().
				Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				Follow("phase-states", "Links").Success().
				Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
				AssertEq(false, "Properties", "WantsDIAS").
				AssertEq(false, "Properties", "OnProbation").
				AssertEq(false, "Properties", "ReadyToResolve")
		})

		t.Run("OptionsAfterPhaseResolution", func(t *testing.T) {
			prov := ""
			switch startedGameNats[0] {
			case "Austria":
				prov = "rum"
			case "England":
				prov = "nth"
			case "France":
				prov = "bur"
			case "Germany":
				prov = "hol"
			case "Italy":
				prov = "ion"
			case "Turkey":
				prov = "bul"
			case "Russia":
				prov = "bot"
			}
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(false, "Properties", "Resolved")

			p.Follow("options", "Links").Success().
				AssertEq("SrcProvince", "Properties", prov, "Next", "Hold", "Next", prov, "Type")
		})

		t.Run("TestOldPhase-1", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(1, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved").
				AssertLen(22, "Properties", "Resolutions")

			pr := p.Follow("phase-result", "Links").Success().
				AssertLen(6, "Properties", "ReadyUsers").
				AssertNil("Properties", "NMRUsers").
				AssertLen(1, "Properties", "ActiveUsers")
			for i, env := range startedGameEnvs {
				if i == 0 {
					pr.Find(env.GetUID(), []string{"Properties", "ActiveUsers"}, nil)
				} else {
					pr.Find(env.GetUID(), []string{"Properties", "ReadyUsers"}, nil)
				}
			}
		})

		t.Run("TestOldPhase-2", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(2, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved")
			pr := p.Follow("phase-result", "Links").Success().
				AssertLen(7, "Properties", "ReadyUsers").
				AssertNil("Properties", "NMRUsers").
				AssertNil("Properties", "ActiveUsers")
			for _, env := range startedGameEnvs {
				pr.Find(env.GetUID(), []string{"Properties", "ReadyUsers"}, nil)
			}
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
					Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"})

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
				AssertNotFind(4, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"})

			startedGames[0].Follow("self", "Links").Success().
				Find(3, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
				AssertEq(false, "Resolved")

		})

		t.Run("TimeoutResolve-2", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "3").Success()
		})

		t.Run("TestNextPhaseHasProbation", func(t *testing.T) {
			startedGames[0].Follow("self", "Links").Success().
				Find(6, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
				AssertEq(false, "Resolved")

			p := startedGames[0].Follow("phases", "Links").Success().
				Find(6, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(false, "Properties", "Resolved").
				AssertNil("Properties", "Resolutions")

			for _, loc := range expectedLocs {
				p.Find(loc, []string{"Properties", "Units"}, []string{"Province"})
			}

			p.Follow("phase-states", "Links").Success().
				Find(startedGameNats[0], []string{"Properties"}, []string{"Properties", "Nation"}).
				AssertEq(true, "Properties", "WantsDIAS").
				AssertEq(true, "Properties", "ReadyToResolve").
				AssertEq(true, "Properties", "OnProbation")

			startedGames[1].Follow("phases", "Links").Success().
				Find(6, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				Follow("phase-states", "Links").Success().
				Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
				AssertEq(false, "Properties", "WantsDIAS").
				AssertEq(false, "Properties", "OnProbation").
				AssertEq(false, "Properties", "ReadyToResolve")
		})

		t.Run("TestStagingGamePlayer0Gone", func(t *testing.T) {
			WaitForEmptyQueue("game-ejectProbationaries")
			WaitForEmptyQueue("game-ejectMember")
			startedGameEnvs[0].GetRoute(game.ListOpenGamesRoute).Success().
				AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		})

		t.Run("TestOldPhase-3", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved")
			pr := p.Follow("phase-result", "Links").Success().
				AssertNil("Properties", "ReadyUsers").
				AssertLen(1, "Properties", "NMRUsers").
				AssertLen(6, "Properties", "ActiveUsers")
			for i, env := range startedGameEnvs {
				if i == 0 {
					pr.Find(env.GetUID(), []string{"Properties", "NMRUsers"}, nil)
				} else {
					pr.Find(env.GetUID(), []string{"Properties", "ActiveUsers"}, nil)
				}
			}
		})

		t.Run("TimeoutResolve-3", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "6").Success()
		})

		t.Run("TestGameFinished", func(t *testing.T) {
			startedGames[0].Follow("self", "Links").Success().
				Find(7, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"}).
				AssertEq(true, "Resolved")

			startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("started-games", "Links").Success().
				AssertNotFind(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			g := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
				Follow("finished-games", "Links").Success().
				Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			res := g.Follow("game-result", "Links").Success().
				AssertNil("Properties", "DIASMembers").
				AssertNil("Properties", "DIASUsers").
				AssertLen(7, "Properties", "NMRMembers").
				AssertLen(7, "Properties", "NMRUsers").
				AssertNil("Properties", "EliminatedMembers").
				AssertNil("Properties", "EliminatedUsers").
				AssertEq("", "Properties", "SoloWinnerMember").
				AssertEq("", "Properties", "SoloWinnerUser")
			for i := range startedGameEnvs {
				res.Find(startedGameNats[i], []string{"Properties", "NMRMembers"}, nil)
				res.Find(startedGameEnvs[i].GetUID(), []string{"Properties", "NMRUsers"}, nil)
			}
		})

		t.Run("TestOldPhase-4", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(6, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved")
			pr := p.Follow("phase-result", "Links").Success().
				AssertNil("Properties", "ReadyUsers").
				AssertLen(7, "Properties", "NMRUsers").
				AssertNil("Properties", "ActiveUsers")
			for _, env := range startedGameEnvs {
				pr.Find(env.GetUID(), []string{"Properties", "NMRUsers"}, nil)
			}
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
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
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
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
				Follow("phase-states", "Links").Success().
				Find(startedGameNats[i], []string{"Properties"}, []string{"Properties", "Nation"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      wantsDIAS,
			}).Success()
		}
	})

	WaitForEmptyQueue("game-asyncResolvePhase")

	t.Run("TestOldPhase", func(t *testing.T) {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find(1, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
			AssertEq(true, "Properties", "Resolved")
		p.Follow("orders", "Links").Success().
			AssertLen(7, "Properties").
			AssertNotFind("delete", []string{"Properties"}, []string{"Links", "Rel"}).
			AssertNotFind("update", []string{"Properties"}, []string{"Links", "Rel"})
		p.Follow("phase-states", "Links").Success().
			AssertLen(7, "Properties").
			AssertNotFind("update", []string{"Properties"}, []string{"Links", "Rel"})
		pr := p.Follow("phase-result", "Links").Success().
			AssertLen(7, "Properties", "ReadyUsers").
			AssertNil("Properties", "NMRUsers").
			AssertNil("Properties", "ActiveUsers")
		for _, env := range startedGameEnvs {
			pr.Find(env.GetUID(), []string{"Properties", "ReadyUsers"}, nil)
		}
	})
	t.Run("TestSkippedPhase", func(t *testing.T) {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find(2, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
			AssertEq(true, "Properties", "Resolved")

		p.Find("rum", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("nth", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("bur", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("hol", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("ion", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("bul", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("bot", []string{"Properties", "Units"}, []string{"Province"})

		p.Follow("phase-states", "Links").Success().
			Find(startedGameNats[0], []string{"Properties"}, []string{"Properties", "Nation"}).
			AssertEq(true, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(true, "Properties", "ReadyToResolve")
		p.Follow("phase-states", "Links").Success().
			Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
			AssertEq(false, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(true, "Properties", "ReadyToResolve")
		pr := p.Follow("phase-result", "Links").Success().
			AssertLen(7, "Properties", "ReadyUsers").
			AssertNil("Properties", "NMRUsers").
			AssertNil("Properties", "ActiveUsers")
		for _, env := range startedGameEnvs {
			pr.Find(env.GetUID(), []string{"Properties", "ReadyUsers"}, nil)
		}
	})
	t.Run("TestNextPhase", func(t *testing.T) {
		p := startedGames[0].Follow("phases", "Links").Success().
			Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
			AssertEq(false, "Properties", "Resolved")

		p.Find("rum", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("nth", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("bur", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("hol", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("ion", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("bul", []string{"Properties", "Units"}, []string{"Province"})
		p.Find("bot", []string{"Properties", "Units"}, []string{"Province"})

		p.Follow("phase-states", "Links").Success().
			Find(startedGameNats[0], []string{"Properties"}, []string{"Properties", "Nation"}).
			AssertEq(true, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(false, "Properties", "ReadyToResolve")

		startedGames[1].Follow("phases", "Links").Success().
			Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
			Follow("phase-states", "Links").Success().
			Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
			AssertEq(false, "Properties", "WantsDIAS").
			AssertEq(false, "Properties", "OnProbation").
			AssertEq(false, "Properties", "ReadyToResolve")
	})
	t.Run("TestGameNotFinished", func(t *testing.T) {
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("started-games", "Links").Success().
			Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("finished-games", "Links").Success().
			AssertNotFind(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

}

func TestCorroborate(t *testing.T) {
	withStartedGame(func() {
		russia := startedGameEnvs[startedGameIdxByNat["Russia"]]
		russiaPhase := russia.GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})
		mosIncon := russiaPhase.
			Follow("corroborate", "Links").Success().
			Find("mos", []string{"Properties", "Inconsistencies"}, []string{"Province"}).GetValue("Inconsistencies").([]interface{})
		if mosIncon[0].(string) != "InconsistencyMissingOrder" {
			t.Errorf("Got %v, wanted %v", mosIncon[0], "InconsistencyMissingOrder")
		}
		england := startedGameEnvs[startedGameIdxByNat["England"]]
		englandPhase := england.GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})
		englandPhase.
			Follow("corroborate", "Links").Success().
			AssertNotFind("mos", []string{"Properties", "Inconsistencies"}, []string{"Province"})
		russiaPhase.Follow("create-order", "Links").Body(map[string]interface{}{
			"Parts": []string{"mos", "Move", "lvn"},
		}).Success()
		russiaCorr := russiaPhase.
			Follow("corroborate", "Links").Success()
		russiaCorr.
			AssertNotFind("mos", []string{"Properties", "Inconsistencies"}, []string{"Province"})
		russiaCorr.
			Find("Russia", []string{"Properties", "Orders"}, []string{"Nation"})
		englandCorr := englandPhase.
			Follow("corroborate", "Links").Success()
		englandCorr.
			AssertNotFind("Russia", []string{"Properties", "Orders"}, []string{"Nation"})
		observer := NewEnv().SetUID(String("fake"))
		observer.GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
			Follow("corroborate", "Links").Success().
			AssertNotFind("Russia", []string{"Properties", "Orders"}, []string{"Nation"})
		for _, env := range startedGameEnvs {
			env.GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
				Follow("phases", "Links").Success().
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
				Follow("phase-states", "Links").Success().
				Find(false, []string{"Properties"}, []string{"Properties", "WantsDIAS"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"WantsDIAS":      true,
				"ReadyToResolve": true,
			}).Success()
		}
		WaitForEmptyQueue("game-asyncResolvePhase")
		observer.GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
			Follow("corroborate", "Links").Success().
			Find("Russia", []string{"Properties", "Orders"}, []string{"Nation"})
	})
}

func TestEliminatedNMRPlayer(t *testing.T) {
	withStartedGameOpts(func(opts map[string]interface{}) {
		opts["Variant"] = "Pure"
	}, func() {
		gameDesc := String("game-desc")
		t.Run("CreateStagingGamePlayer0", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.IndexRoute).
				Success().Follow("create-game", "Links").
				Body(map[string]interface{}{
					"Variant":            "Pure",
					"NoMerge":            true,
					"Desc":               gameDesc,
					"PhaseLengthMinutes": 60 * 24,
				}).Success()
		})
		t.Run("PreparePhaseStatesWithOneNMR", func(t *testing.T) {
			for i, nat := range startedGameNats {
				var order []string
				switch nat {
				case "Austria":
					// Austria is NMR.
					continue
				case "England":
					order = []string{"lon", "Move", "vie"}
				case "France":
					order = []string{"par", "Support", "lon", "vie"}
				case "Germany":
					order = []string{"ber", "Hold"}
				case "Italy":
					order = []string{"rom", "Hold"}
				case "Turkey":
					order = []string{"con", "Hold"}
				case "Russia":
					order = []string{"mos", "Hold"}
				}

				p := startedGames[i].Follow("phases", "Links").Success().
					Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

				p.Follow("create-order", "Links").Body(map[string]interface{}{
					"Parts": order,
				}).Success()

				p.Follow("phase-states", "Links").Success().
					Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": true,
					"WantsDIAS":      false,
				}).Success()
			}
		})
		t.Run("TimeoutResolvePhase1", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "1").Success()
		})
		t.Run("TestOldPhase1", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(1, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved").
				AssertLen(7, "Properties", "Resolutions")

			pr := p.Follow("phase-result", "Links").Success().
				AssertLen(6, "Properties", "ReadyUsers").
				AssertLen(1, "Properties", "NMRUsers").
				AssertNil("Properties", "ActiveUsers")
			for i, env := range startedGameEnvs {
				if startedGameNats[i] == "Austria" {
					pr.Find(env.GetUID(), []string{"Properties", "NMRUsers"}, nil)
				} else {
					pr.Find(env.GetUID(), []string{"Properties", "ReadyUsers"}, nil)
				}
			}
		})
		t.Run("PreparePhase3WithNoMoreNMRs", func(t *testing.T) {
			for i, nat := range startedGameNats {
				var order []string
				switch nat {
				case "Austria":
					// Austria is eliminated.
					continue
				case "England":
					order = []string{"vie", "Hold"}
				case "France":
					order = []string{"par", "Hold"}
				case "Germany":
					order = []string{"ber", "Hold"}
				case "Italy":
					order = []string{"rom", "Hold"}
				case "Turkey":
					order = []string{"con", "Hold"}
				case "Russia":
					order = []string{"mos", "Hold"}
				}

				p := startedGames[i].Follow("phases", "Links").Success().
					Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"})

				p.Follow("create-order", "Links").Body(map[string]interface{}{
					"Parts": order,
				}).Success()
			}
		})
		t.Run("TimeoutResolvePhase3", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "3").Success()
		})
		t.Run("TestOldPhase3", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(3, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved").
				AssertLen(6, "Properties", "Resolutions")

			pr := p.Follow("phase-result", "Links").Success().
				AssertNil("Properties", "ReadyUsers").
				// Austria is still NMR even though they have no options, because they still have an SC.
				// This is maybe slightly harsh, but then again they shouldn't NMR.
				AssertLen(1, "Properties", "NMRUsers").
				AssertLen(6, "Properties", "ActiveUsers")
			for i, env := range startedGameEnvs {
				if startedGameNats[i] == "Austria" {
					pr.Find(env.GetUID(), []string{"Properties", "NMRUsers"}, nil)
				} else {
					pr.Find(env.GetUID(), []string{"Properties", "ActiveUsers"}, nil)
				}
			}
		})
		t.Run("PreparePhase5WithNoMoreNMRs", func(t *testing.T) {
			for i, nat := range startedGameNats {
				// Only England gets a build.
				if nat != "England" {
					continue
				}
				order := []string{"lon", "Build", "Army"}
				p := startedGames[i].Follow("phases", "Links").Success().
					Find(5, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"})

				p.Follow("create-order", "Links").Body(map[string]interface{}{
					"Parts": order,
				}).Success()
			}
		})
		t.Run("TimeoutResolvePhase5", func(t *testing.T) {
			startedGameEnvs[0].GetRoute(game.DevResolvePhaseTimeoutRoute).
				RouteParams("game_id", fmt.Sprint(startedGameID), "phase_ordinal", "5").Success()
		})
		t.Run("TestOldPhase5", func(t *testing.T) {
			p := startedGames[0].Follow("phases", "Links").Success().
				Find(5, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
				AssertEq(true, "Properties", "Resolved").
				AssertLen(1, "Properties", "Resolutions")

			pr := p.Follow("phase-result", "Links").Success().
				// Austria is now Ready rather than NMR because they are eliminated.
				AssertLen(6, "Properties", "ReadyUsers").
				AssertNil("Properties", "NMRUsers").
				AssertLen(1, "Properties", "ActiveUsers")
			for i, env := range startedGameEnvs {
				if startedGameNats[i] == "England" {
					pr.Find(env.GetUID(), []string{"Properties", "ActiveUsers"}, nil)
				} else {
					pr.Find(env.GetUID(), []string{"Properties", "ReadyUsers"}, nil)
				}
			}
		})
	})
}
