package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestRootNotLoggedIn(t *testing.T) {
	NewEnv().GetRoute(game.IndexRoute).Success().
		AssertStringEq("diplicity", "Name").
		AssertStringEq("Diplicity", "Type").
		AssertNil("Properties", "User").
		AssertRel("login", "Links").
		AssertRel("self", "Links")
}

func TestRootLoggedIn(t *testing.T) {
	uid := String("fake")
	NewEnv().SetUID(uid).GetRoute(game.IndexRoute).Success().
		AssertStringEq("diplicity", "Name").
		AssertStringEq("Diplicity", "Type").
		AssertStringEq(uid, "Properties", "User", "Id").
		AssertRel("logout", "Links").
		AssertRel("my-staging-games", "Links").
		AssertRel("my-started-games", "Links").
		AssertRel("my-finished-games", "Links").
		AssertRel("open-games", "Links").
		AssertRel("started-games", "Links").
		AssertRel("finished-games", "Links").
		AssertRel("create-game", "Links").
		AssertRel("self", "Links")
}
