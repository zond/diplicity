package diptest

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/zond/diplicity/game"
)

func TestBans(t *testing.T) {
	env1 := NewEnv().SetUID(String("fake"))
	env2 := NewEnv().SetUID(String("fake"))
	env3 := NewEnv().SetUID(String("fake"))

	env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env3.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env1.GetRoute("Ban.Load").
		RouteParams("user_id", env1.GetUID(), "banned_id", env2.GetUID()).
		Failure()

	b1 := env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success()

	b1.Follow("create", "Links").Body(map[string]interface{}{
		"UserIds": []string{env2.GetUID(), env3.GetUID()},
	}).Failure()
	b1.Follow("create", "Links").Body(map[string]interface{}{
		"UserIds": []string{env1.GetUID(), env2.GetUID(), env3.GetUID()},
	}).Failure()
	b1.Follow("create", "Links").Body(map[string]interface{}{
		"UserIds": []string{env1.GetUID()},
	}).Failure()
	b1.Follow("create", "Links").Body(map[string]interface{}{
		"UserIds": []string{env1.GetUID(), env2.GetUID()},
	}).Success()

	env1.GetRoute("Ban.Load").
		RouteParams("user_id", env1.GetUID(), "banned_id", env2.GetUID()).
		Success().
		AssertEq([]interface{}{env1.GetUID(), env2.GetUID()}, "Properties", "UserIds")

	bansRes := env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success()
	bansRes.Find(env1.GetUID(), []string{"Properties"}, []string{"Properties", "Users"}, []string{"Id"})
	bansRes.Find(env2.GetUID(), []string{"Properties"}, []string{"Properties", "Users"}, []string{"Id"})
	bans := bansRes.
		GetValue("Properties").([]interface{})
	ban := bans[0].(map[string]interface{})["Properties"].(map[string]interface{})
	owners := ban["OwnerIds"].([]interface{})
	if len(owners) != 1 || owners[0] != env1.GetUID() {
		panic(fmt.Errorf("%+v doesn't have exactly %q as owner", ban, env1.GetUID()))
	}
	users := ban["UserIds"].([]interface{})
	var has1, has2 bool
	for _, user := range users {
		if user == env1.GetUID() {
			has1 = true
		}
		if user == env2.GetUID() {
			has2 = true
		}
	}
	if !has1 || !has2 {
		panic(fmt.Errorf("%+v doesn't have both %q and %q as users", ban, env1.GetUID(), env2.GetUID()))
	}

	env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(1, "Properties").
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"})

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(1, "Properties").
		AssertNotFind("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"})

	env3.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		Follow("create", "Links").Body(map[string]interface{}{
		"UserIds": []string{env2.GetUID(), env1.GetUID()},
	}).Success()

	bans = env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().GetValue("Properties").([]interface{})
	ban = bans[0].(map[string]interface{})["Properties"].(map[string]interface{})
	owners = ban["OwnerIds"].([]interface{})
	if len(owners) != 2 {
		panic(fmt.Errorf("%+v doesn't have exactly two owners", ban))
	}
	has1 = false
	has2 = false
	for _, owner := range owners {
		if owner == env1.GetUID() {
			has1 = true
		}
		if owner == env2.GetUID() {
			has2 = true
		}
	}
	if !has1 || !has2 {
		panic(fmt.Errorf("%+v doesn't have both %q and %q as owners", ban, env1.GetUID(), env2.GetUID()))
	}
	has1 = false
	has2 = false
	users = ban["UserIds"].([]interface{})
	for _, user := range users {
		if user == env1.GetUID() {
			has1 = true
		}
		if user == env2.GetUID() {
			has2 = true
		}
	}
	if !has1 || !has2 {
		panic(fmt.Errorf("%+v doesn't have both %q and %q as users", ban, env1.GetUID(), env2.GetUID()))
	}

	env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(1, "Properties").
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"})

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(1, "Properties").
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"})

	env3.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"}).
		FollowLink().Success()

	env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(1, "Properties").
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"})

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(1, "Properties").
		AssertNotFind("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"})

	env3.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"}).
		FollowLink().Success()

	env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env2.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

	env3.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		AssertLen(0, "Properties")

}

func testBanEfficacy(t *testing.T) {
	newEnv := NewEnv().SetUID(String("fake"))

	gameURLString := startedGames[0].Find("self", []string{"Links"}, []string{"Rel"}).GetValue("URL").(string)
	gameURL, err := url.Parse(gameURLString)
	if err != nil {
		panic(err)
	}
	gameURL.RawQuery = ""

	newEnv.GetRoute(game.IndexRoute).Success().
		Follow("started-games", "Links").Success().
		Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	newEnv.GetURL(gameURL.String()).Success().
		AssertNil("Properties", "ActiveBans")

	startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		Follow("create", "Links").Body(map[string]interface{}{
		"UserIds": []string{startedGameEnvs[0].GetUID(), newEnv.GetUID()},
	}).Success()

	newEnv.GetRoute(game.IndexRoute).Success().
		Follow("started-games", "Links").Success().
		AssertNotFind(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	newEnv.GetURL(gameURL.String()).Success().
		Find(newEnv.GetUID(), []string{"Properties", "ActiveBans"}, []string{"UserIds"}, nil)

	newGameDesc := String("game")
	newEnv.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Variant":            "Classical",
			"Desc":               newGameDesc,
			"PhaseLengthMinutes": time.Duration(60),
		}).Success().
		AssertEq(newGameDesc, "Properties", "Desc")

	newGameURLString := newEnv.GetRoute(game.IndexRoute).Success().
		Follow("my-staging-games", "Links").Success().
		Find(newGameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
		Find("self", []string{"Links"}, []string{"Rel"}).GetValue("URL").(string)
	newGameURL, err := url.Parse(newGameURLString)
	if err != nil {
		panic(err)
	}

	startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("open-games", "Links").Success().
		AssertNotFind(newGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	banView := startedGameEnvs[0].GetURL(newGameURL.String()).Success()
	banView.Find(startedGameEnvs[0].GetUID(), []string{"Properties", "ActiveBans"}, []string{"UserIds"}, nil)
	banView.AssertNotFind("join", []string{"Links"}, []string{"Rel"})

	startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().
		Find("unsign", []string{"Properties"}, []string{"Links"}, []string{"Rel"}).
		FollowLink().Success()

	newEnv.GetRoute(game.IndexRoute).Success().
		Follow("started-games", "Links").Success().
		Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("open-games", "Links").Success().
		Find(newGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	unbanView := startedGameEnvs[0].GetURL(newGameURL.String()).Success()
	unbanView.AssertNil("Properties", "ActiveBans")
	unbanView.Find("join", []string{"Links"}, []string{"Rel"})
}
