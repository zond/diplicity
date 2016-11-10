package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func TestUserConfig(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	tokens := []interface{}{
		map[string]interface{}{
			"Value":    String("token"),
			"Disabled": false,
			"Note":     "",
			"App":      String("app"),
		},
		map[string]interface{}{
			"Value":    String("token"),
			"Disabled": false,
			"Note":     "",
			"App":      String("app"),
		},
	}
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
