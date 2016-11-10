package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestUserConfig(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	tokens := []string{String("token"), String("token")}
	env.GetRoute(game.IndexRoute).Success().
		Follow("user-config", "Links").Success().
		AssertNil("Properties", "FCMTokens").
		Follow("update", "Links").Body(map[string]interface{}{
		"FCMTokens": tokens,
	}).Success().AssertEq(tokens, "Properties", "FCMTokens")
	env.GetRoute(game.IndexRoute).Success().
		Follow("user-config", "Links").Success().
		AssertEq(tokens, "Properties", "FCMTokens")
}
