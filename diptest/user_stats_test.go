package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestUserStatsLists(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.ListTopRatedPlayersRoute).Success()
	env.GetRoute(game.ListTopReliablePlayersRoute).Success()
	env.GetRoute(game.ListTopHatedPlayersRoute).Success()
	env.GetRoute(game.ListTopHaterPlayersRoute).Success()
	env.GetRoute(game.ListTopQuickPlayersRoute).Success()
}
