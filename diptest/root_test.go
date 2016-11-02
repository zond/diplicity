package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestRootNotLoggedIn(t *testing.T) {
	NewEnv().Get(game.IndexRoute).Do().
		AssertOK().
		AssertStringEq("diplicity", "Name").
		AssertStringEq("Diplicity", "Type").
		AssertNil("Properties", "User").
		AssertRel("login", "Links").
		AssertRel("self", "Links")
}

func TestRootLoggedIn(t *testing.T) {
	NewEnv().UID("fake1").Get(game.IndexRoute).Do().
		AssertOK().
		AssertStringEq("diplicity", "Name").
		AssertStringEq("Diplicity", "Type").
		AssertStringEq("fake1", "Properties", "User", "Id").
		AssertRel("logout", "Links").
		AssertRel("self", "Links")
}
