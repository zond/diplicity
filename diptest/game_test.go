package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestCreateGame(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().UID("fake1")
	env.GetRoute(game.IndexRoute).Do().
		FollowPOST(map[string]string{
		"Variant": "Classical",
		"Desc":    gameDesc,
	}, "create-game", "Links").
		AssertOK().
		AssertStringEq(gameDesc, "Properties", "Desc")
	env.GetRoute(game.MyStagingGamesRoute).Do().
		AssertSliceStringEq(gameDesc, []string{"Properties", "Desc"}, "Properties")
	env.GetRoute(game.OpenGamesRoute).Do().
		AssertSliceStringEq(gameDesc, []string{"Properties", "Desc"}, "Properties")
}
