package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func testPhaseState(t *testing.T) {
	phases := make([]*Result, len(startedGameEnvs))

	for i, env := range startedGameEnvs {
		phases[i] = env.GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc).
			Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")
		phases[i].Follow("phase-state", "Links").Success().
			AssertBoolEq(false, "Properties", "ReadyToResolve").
			AssertBoolEq(false, "Properties", "WantsDIAS")
	}

	phases[0].Follow("phase-state", "Links").Success().
		Follow("update", "Links").Body(map[string]interface{}{
		"ReadyToResolve": true,
		"WantsDIAS":      true,
	}).Success().
		AssertBoolEq(true, "Properties", "ReadyToResolve").
		AssertBoolEq(true, "Properties", "WantsDIAS")

	phases[1].Follow("phase-state", "Links").Success().
		AssertBoolEq(false, "Properties", "ReadyToResolve").
		AssertBoolEq(false, "Properties", "WantsDIAS")

}
