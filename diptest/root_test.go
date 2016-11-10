package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

var loggedInRels = []string{
	"logout",
	"my-staging-games",
	"my-started-games",
	"my-finished-games",
	"started-games",
	"finished-games",
	"open-games",
	"create-game",
}

var loggedOutRels = []string{
	"login",
}

var bothRels = []string{
	"self",
	"variants",
}

func TestRootNotLoggedIn(t *testing.T) {
	r := NewEnv().GetRoute(game.IndexRoute).Success().
		AssertEq("diplicity", "Name").
		AssertEq("Diplicity", "Type").
		AssertNil("Properties", "User")
	for _, rel := range loggedInRels {
		r.AssertNotRel(rel, "Links")
	}
	for _, rel := range loggedOutRels {
		r.AssertRel(rel, "Links")
	}
	for _, rel := range bothRels {
		r.AssertRel(rel, "Links")
	}
}

func TestRootLoggedIn(t *testing.T) {
	uid := String("fake")
	r := NewEnv().SetUID(uid).GetRoute(game.IndexRoute).Success().
		AssertEq("diplicity", "Name").
		AssertEq("Diplicity", "Type").
		AssertEq(uid, "Properties", "User", "Id")
	for _, rel := range loggedInRels {
		r.AssertRel(rel, "Links")
	}
	for _, rel := range loggedOutRels {
		r.AssertNotRel(rel, "Links")
	}
	for _, rel := range bothRels {
		r.AssertRel(rel, "Links")
	}
}
