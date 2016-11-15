package diptest

import (
	"fmt"
	"testing"

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

	bans := env1.GetRoute(game.IndexRoute).Success().
		Follow("bans", "Links").Success().GetValue("Properties").([]interface{})
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
