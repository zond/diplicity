package diptest

import (
	"net/http"
	"testing"
	"time"

	"github.com/zond/diplicity/game"
)

func TestGameMasterPreallocationBothPref(t *testing.T) {
	masterEnv := NewEnv().SetUID(String("fake"))
	gameDesc := String("test-game")

	masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Desc":                        gameDesc,
			"Variant":                     "Cold War",
			"PhaseLengthMinutes":          time.Duration(60),
			"GameMasterEnabled":           true,
			"RequireGameMasterInvitation": true,
			"SkipMuster":                  true,
			"Private":                     true,
			"NationAllocation":            1,
		}).Success()
	gameID := masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("mastered-staging-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)

	player1Env := NewEnv().SetUID(String("player1")).SetEmail(String("email1"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "blaj",
	}).Failure()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "USSR",
	}).Success()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email": player1Env.email,
	}).Failure()
	player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(map[string]interface{}{
		"NationPreferences": "NATO,USSR",
	}).Success()

	player2Env := NewEnv().SetUID(String("player2")).SetEmail(String("email2"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player2Env.email,
		"Nation": "NATO",
	}).Success()
	player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(map[string]interface{}{
		"NationPreferences": "USSR,NATO",
	}).Success()

	WaitForEmptyQueue("game-asyncStartGame")

	player1Env.GetRoute("ListMyStartedGames").Success().
		Find("USSR", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player1Env.uid, []string{"User", "Id"})
	player2Env.GetRoute("ListMyStartedGames").Success().
		Find("NATO", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player2Env.uid, []string{"User", "Id"})
}

func TestGameMasterPreallocationOnePref(t *testing.T) {
	masterEnv := NewEnv().SetUID(String("fake"))
	gameDesc := String("test-game")

	masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Desc":                        gameDesc,
			"Variant":                     "Cold War",
			"PhaseLengthMinutes":          time.Duration(60),
			"GameMasterEnabled":           true,
			"RequireGameMasterInvitation": true,
			"SkipMuster":                  true,
			"Private":                     true,
			"NationAllocation":            1,
		}).Success()
	gameID := masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("mastered-staging-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)

	player1Env := NewEnv().SetUID(String("player1")).SetEmail(String("email1"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "blaj",
	}).Failure()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "USSR",
	}).Success()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email": player1Env.email,
	}).Failure()
	player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(map[string]interface{}{
		"NationPreferences": "NATO,USSR",
	}).Success()

	player2Env := NewEnv().SetUID(String("player2")).SetEmail(String("email2"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email": player2Env.email,
	}).Success()
	player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(map[string]interface{}{
		"NationPreferences": "USSR,NATO",
	}).Success()

	WaitForEmptyQueue("game-asyncStartGame")

	player1Env.GetRoute("ListMyStartedGames").Success().
		Find("USSR", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player1Env.uid, []string{"User", "Id"})
	player2Env.GetRoute("ListMyStartedGames").Success().
		Find("NATO", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player2Env.uid, []string{"User", "Id"})
}

func TestGameMasterPreallocationOneRandom(t *testing.T) {
	masterEnv := NewEnv().SetUID(String("fake"))
	gameDesc := String("test-game")

	masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Desc":                        gameDesc,
			"Variant":                     "Cold War",
			"PhaseLengthMinutes":          time.Duration(60),
			"GameMasterEnabled":           true,
			"RequireGameMasterInvitation": true,
			"SkipMuster":                  true,
			"Private":                     true,
		}).Success()
	gameID := masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("mastered-staging-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)

	player1Env := NewEnv().SetUID(String("player1")).SetEmail(String("email1"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "blaj",
	}).Failure()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "USSR",
	}).Success()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email": player1Env.email,
	}).Failure()
	player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(nil).Success()

	player2Env := NewEnv().SetUID(String("player2")).SetEmail(String("email2"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email": player2Env.email,
	}).Success()
	player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(nil).Success()

	WaitForEmptyQueue("game-asyncStartGame")

	player1Env.GetRoute("ListMyStartedGames").Success().
		Find("USSR", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player1Env.uid, []string{"User", "Id"})
	player2Env.GetRoute("ListMyStartedGames").Success().
		Find("NATO", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player2Env.uid, []string{"User", "Id"})
}

func TestGameMasterPreallocationBothRandom(t *testing.T) {
	masterEnv := NewEnv().SetUID(String("fake"))
	gameDesc := String("test-game")

	masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Desc":                        gameDesc,
			"Variant":                     "Cold War",
			"PhaseLengthMinutes":          time.Duration(60),
			"GameMasterEnabled":           true,
			"RequireGameMasterInvitation": true,
			"SkipMuster":                  true,
			"Private":                     true,
		}).Success()
	gameID := masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("mastered-staging-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)

	player1Env := NewEnv().SetUID(String("player1")).SetEmail(String("email1"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "blaj",
	}).Failure()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player1Env.email,
		"Nation": "USSR",
	}).Success()
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email": player1Env.email,
	}).Failure()
	player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(nil).Success()

	player2Env := NewEnv().SetUID(String("player2")).SetEmail(String("email2"))
	masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("invite-user", "Links").Body(map[string]interface{}{
		"Email":  player2Env.email,
		"Nation": "NATO",
	}).Success()
	player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(nil).Success()

	WaitForEmptyQueue("game-asyncStartGame")

	player1Env.GetRoute("ListMyStartedGames").Success().
		Find("USSR", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player1Env.uid, []string{"User", "Id"})
	player2Env.GetRoute("ListMyStartedGames").Success().
		Find("NATO", []string{"Properties"}, []string{"Properties", "Members"}, []string{"Nation"}).
		Find(player2Env.uid, []string{"User", "Id"})
}

func TestGameMasterFunctionality(t *testing.T) {
	masterEnv := NewEnv().SetUID(String("fake"))
	t.Run("MustBePrivate", func(t *testing.T) {
		masterEnv.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"PhaseLengthMinutes": time.Duration(60),
				"GameMasterEnabled":  true,
			}).Failure()
	})

	gameID := ""
	gameDesc := String("test-game")
	t.Run("CanCreateGMGame", func(t *testing.T) {

		masterEnv.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Desc":                        gameDesc,
				"Variant":                     "Cold War",
				"PhaseLengthMinutes":          time.Duration(60),
				"GameMasterEnabled":           true,
				"RequireGameMasterInvitation": true,
				"Private":                     true,
			}).Success()
		gameID = masterEnv.GetRoute(game.IndexRoute).Success().
			Follow("mastered-staging-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)

		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(true, "Properties", "GameMasterEnabled").
			AssertEq(masterEnv.uid, "Properties", "GameMaster", "Id").
			AssertRel("update-game", "Links").
			AssertRel("delete-game", "Links").
			AssertRel("invite-user", "Links")
	})

	player1Env := NewEnv().SetUID(String("player1")).SetEmail(String("email1"))
	t.Run("OthersCanNotGM", func(t *testing.T) {
		player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotRel("update-game", "Links").
			AssertNotRel("delete-game", "Links").
			AssertNotRel("invite-user", "Links")
		player1Env.DeleteRoute("Game.Delete").RouteParams("id", gameID).AuthFailure()
		player1Env.PutRoute("Game.Update").RouteParams("id", gameID).Body(map[string]interface{}{
			"RequireGameMasterInvitation": false,
		}).AuthFailure()
		player1Env.PostRoute("GameMasterInvitation.Create").RouteParams("game_id", gameID).Body(map[string]interface{}{
			"Email": "hehu",
		}).AuthFailure()
	})

	t.Run("CanNotJoinWithoutInvitation", func(t *testing.T) {
		player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotFind("join", []string{"Links"}, []string{"Rel"})
		player1Env.PostRoute("Member.Create").RouteParams("game_id", gameID).Body(nil).Status(http.StatusPreconditionFailed)
	})

	t.Run("GMCanInvite", func(t *testing.T) {
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("invite-user", "Links").Body(map[string]interface{}{
			"Email": player1Env.email,
		}).Success()
	})

	t.Run("CanJoinWithInvitation", func(t *testing.T) {
		player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(nil).Success()
		player1Env.GetRoute("ListMyStagingGames").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	player2Env := NewEnv().SetUID(String("player2")).SetEmail(String("email2"))
	t.Run("OthersCanNotKick", func(t *testing.T) {
		player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotRel("kick-"+player1Env.uid, "Links")
		player2Env.DeleteRoute("Member.Delete").RouteParams("game_id", gameID, "user_id", player1Env.uid).Status(http.StatusPreconditionFailed)
	})

	t.Run("GMCanKick", func(t *testing.T) {
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("kick-"+player1Env.uid, "Links").Success()
		player1Env.GetRoute("ListMyStagingGames").Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("CanReJoinWithInvitation", func(t *testing.T) {
		player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(nil).Success()
		player1Env.GetRoute("ListMyStagingGames").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("GMCanInviteAndUninvite", func(t *testing.T) {
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("invite-user", "Links").Body(map[string]interface{}{
			"Email": player2Env.email,
		}).Success()
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("uninvite-"+player2Env.email, "Links").Success()
	})

	t.Run("CanNotJoinUninvited", func(t *testing.T) {
		player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotFind("join", []string{"Links"}, []string{"Rel"})
		player2Env.PostRoute("Member.Create").RouteParams("game_id", gameID).Body(nil).Status(http.StatusPreconditionFailed)
	})

	t.Run("GMCanModify", func(t *testing.T) {
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("update-game", "Links").Body(map[string]interface{}{
			"RequireGameMasterInvitation": false,
		}).Success()
	})

	t.Run("CanJoinWithoutInvitation", func(t *testing.T) {
		gameResp := player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		gameResp.Find("join", []string{"Links"}, []string{"Rel"})
		gameResp.Follow("join", "Links").Body(nil).Success()
	})

	WaitForEmptyQueue("game-asyncStartGame")

	masterEnv.GetRoute(game.IndexRoute).Success().
		Follow("mastered-started-games", "Links").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	player1Env.GetRoute("ListMyStartedGames").Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	t.Run("ChangingDeadlines", func(t *testing.T) {
		player1Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotRel("edit-newest-phase-deadline-at", "Links")
		player1Env.PostRoute("GameEditNewestPhaseDeadlineAtMock.Create").RouteParams("game_id", gameID, "phase_ordinal", "1").Body(map[string]interface{}{
			"NextPhaseDeadlineInMinutes": 60,
		}).AuthFailure()
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("edit-newest-phase-deadline-at", "Links").Body(map[string]interface{}{
			"NextPhaseDeadlineInMinutes": 60,
		}).Success()
	})

	player3Env := NewEnv().SetUID(String("player3")).SetEmail(String("email3"))

	t.Run("CanNotJoinFullGame", func(t *testing.T) {
		player3Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotRel("join", "Links")
		player3Env.PostRoute("Member.Create").RouteParams("game_id", gameID).Body(nil).Status(http.StatusPreconditionFailed)
	})

	t.Run("KickingPlayers", func(t *testing.T) {
		player2Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertNotRel("kick-"+player1Env.uid, "Links")
		player2Env.DeleteRoute("Member.Delete").RouteParams("game_id", gameID, "user_id", player1Env.uid).Status(http.StatusPreconditionFailed)
		player1Env.GetRoute("ListMyStartedGames").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		masterEnv.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("kick-"+player1Env.uid, "Links").Success()
		player1Env.GetRoute("ListMyStartedGames").Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("CanReplace", func(t *testing.T) {
		player3Env.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(nil).Success()
		player3Env.GetRoute("ListMyStartedGames").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}
