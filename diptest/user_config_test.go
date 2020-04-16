package diptest

import (
	"testing"

	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/game"
)

func TestUserConfig(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	replaceToken := String("replace-token")
	tokens := []interface{}{
		map[string]interface{}{
			"Value":        String("token"),
			"Disabled":     false,
			"Note":         "",
			"App":          String("app"),
			"ReplaceToken": replaceToken,
			"PhaseConfig": map[string]interface{}{
				"DontSendData":        false,
				"ClickActionTemplate": "",
				"TitleTemplate":       "",
				"BodyTemplate":        "",
			},
			"MessageConfig": map[string]interface{}{
				"DontSendData":        false,
				"ClickActionTemplate": "",
				"TitleTemplate":       "",
				"BodyTemplate":        "",
			},
		},
		map[string]interface{}{
			"Value":        String("token"),
			"Disabled":     false,
			"Note":         "",
			"App":          String("app"),
			"ReplaceToken": "",
			"PhaseConfig": map[string]interface{}{
				"DontSendData":        false,
				"ClickActionTemplate": "",
				"TitleTemplate":       "",
				"BodyTemplate":        "",
			},
			"MessageConfig": map[string]interface{}{
				"DontSendData":        false,
				"ClickActionTemplate": "",
				"TitleTemplate":       "",
				"BodyTemplate":        "",
			},
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

	env2 := NewEnv().SetUID(String("fake"))
	newValue := String("new-token")
	env2.PutRoute(auth.ReplaceFCMRoute).
		RouteParams("user_id", env.GetUID(), "replace_token", replaceToken).
		Body(map[string]interface{}{
			"Value": newValue,
		}).Success()

	tokens[0].(map[string]interface{})["Value"] = newValue
	env.GetRoute(game.IndexRoute).Success().
		Follow("user-config", "Links").Success().
		AssertEq(tokens, "Properties", "FCMTokens")

}
