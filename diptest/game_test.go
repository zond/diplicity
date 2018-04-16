package diptest

import (
	"net/http"
	"net/url"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zond/diplicity/game"
)

func TestGameSorting(t *testing.T) {
	g := game.Games{
		{
			Desc:      "a",
			NMembers:  4,
			CreatedAt: time.Now().Add(-time.Minute),
		},
		{
			Desc:      "b",
			NMembers:  4,
			CreatedAt: time.Now().Add(-time.Hour),
		},
		{
			Desc:      "c",
			NMembers:  5,
			CreatedAt: time.Now(),
		},
	}
	sort.Sort(g)
	if g[0].Desc != "c" {
		t.Errorf("got %q, wanted 'c'", g[0].Desc)
	}
	if g[1].Desc != "b" {
		t.Errorf("got %q, wanted 'b'", g[1].Desc)
	}
	if g[2].Desc != "a" {
		t.Errorf("got %q, wanted 'a'", g[2].Desc)
	}
}

var (
	uniqueMaxHated uint64 = uint64(time.Now().UnixNano() / 1000000000)
)

func TestGameMerging(t *testing.T) {
	maxHated := atomic.AddUint64(&uniqueMaxHated, 1)
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	t.Run("CreateGame", func(t *testing.T) {
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc,
				"MaxHated":           float64(maxHated),
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc, "Properties", "Desc")

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifySelfGameNoMerge", func(t *testing.T) {
		gameDesc2 := String("test-game")
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"MaxHated":           float64(maxHated),
				"Desc":               gameDesc2,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc2, "Properties", "Desc")
		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc2, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyNoMergeGameNoMerge", func(t *testing.T) {
		env2 := NewEnv().SetUID(String("fake"))
		gameDesc3 := String("test-game")
		env2.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc3,
				"MaxHated":           float64(maxHated),
				"NoMerge":            true,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc3, "Properties", "Desc")
		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc3, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyDifferentVariantGameNoMerge", func(t *testing.T) {
		env3 := NewEnv().SetUID(String("fake"))
		gameDesc4 := String("test-game")
		env3.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Fleet Rome",
				"MaxHated":           float64(maxHated),
				"Desc":               gameDesc4,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc4, "Properties", "Desc")
		env3.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc4, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyEqualGameMerge", func(t *testing.T) {
		env4 := NewEnv().SetUID(String("fake"))
		gameDesc5 := String("test-game")
		env4.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc5,
				"MaxHated":           float64(maxHated),
				"PhaseLengthMinutes": time.Duration(60),
			}).Status(http.StatusTeapot)
		env4.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyBannedGameNoMerge", func(t *testing.T) {
		env5 := NewEnv().SetUID(String("fake"))
		env5.GetRoute(game.IndexRoute).Success().
			Follow("bans", "Links").Success().
			Follow("create", "Links").Body(map[string]interface{}{
			"UserIds": []string{env5.GetUID(), env.GetUID()},
		}).Success()

		gameDesc6 := String("test-game")
		env5.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc6,
				"MaxHated":           float64(maxHated),
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc6, "Properties", "Desc")
		env5.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc6, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}

func TestCreateLeaveGame(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	t.Run("TestCreateGame", func(t *testing.T) {
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"NoMerge":            true,
				"Desc":               gameDesc,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc, "Properties", "Desc")

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			AssertNil("Properties", "NewestPhaseMeta")
	})

	t.Run("TestLeaveAndDestroyGame", func(t *testing.T) {
		env.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("leave", "Links").Success()

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}

func TestGameListFilters(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":            "Classical",
		"NoMerge":            true,
		"Desc":               gameDesc,
		"MaxHated":           10,
		"MaxHater":           10,
		"MinReliability":     10,
		"MinQuickness":       10,
		"MinRating":          10,
		"MaxRating":          100,
		"PhaseLengthMinutes": 60,
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
		"Variant":            "Classical",
		"NoMerge":            true,
		"Desc":               gameDesc,
		"MaxHated":           10,
		"MaxHater":           10,
		"MinReliability":     10,
		"MinQuickness":       10,
		"MinRating":          10,
		"MaxRating":          100,
		"PhaseLengthMinutes": time.Duration(60),
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

	for _, f := range []filter{
		{
			"variant",
			"Classical",
			true,
		},
		{
			"variant",
			"blapp",
			false,
		},
		{
			"min-reliability",
			"5:15",
			true,
		},
		{
			"min-reliability",
			"0:5",
			false,
		},
		{
			"min-reliability",
			"15:20",
			false,
		},
		{
			"min-quickness",
			"5:15",
			true,
		},
		{
			"min-quickness",
			"0:5",
			false,
		},
		{
			"min-quickness",
			"15:20",
			false,
		},
		{
			"min-rating",
			"5:15",
			true,
		},
		{
			"min-rating",
			"0:5",
			false,
		},
		{
			"min-rating",
			"15:20",
			false,
		},
		{
			"max-rating",
			"95:115",
			true,
		},
		{
			"max-rating",
			"10:15",
			false,
		},
		{
			"max-rating",
			"125:130",
			false,
		},
		{
			"max-hater",
			"5:15",
			true,
		},
		{
			"max-hater",
			"0:5",
			false,
		},
		{
			"max-hater",
			"15:25",
			false,
		},
		{
			"max-hated",
			"5:15",
			true,
		},
		{
			"max-hated",
			"0:5",
			false,
		},
		{
			"max-hated",
			"15:25",
			false,
		},
	} {
		res := env2.GetRoute(game.ListOpenGamesRoute).QueryParams(url.Values{
			f.name: []string{f.value},
		}).Success()
		if f.wantFind {
			res.Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		} else {
			res.AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		}
	}
}

type filter struct {
	name     string
	value    string
	wantFind bool
}

func TestGameLists(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.ListMyStagingGamesRoute).Success()
	env.GetRoute(game.ListMyStartedGamesRoute).Success()
	env.GetRoute(game.ListMyFinishedGamesRoute).Success()
	env.GetRoute(game.ListOpenGamesRoute).Success()
	env.GetRoute(game.ListStartedGamesRoute).Success()
	env.GetRoute(game.ListFinishedGamesRoute).Success()
}
