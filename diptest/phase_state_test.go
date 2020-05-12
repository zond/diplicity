package diptest

import "testing"

func testPhaseState(t *testing.T) {
	phases := make([]*Result, len(startedGameEnvs))

	for i, g := range startedGames {
		startedGameEnvs[i].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
			Find(startedGameEnvs[i].GetUID(), []string{"Properties", "Members"}, []string{"User", "Id"}).
			Find(false, []string{"NewestPhaseState", "ReadyToResolve"})

		phases[i] = g.Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})
		phases[i].Follow("phase-states", "Links").Success().
			Find(startedGameNats[i], []string{"Properties"}, []string{"Properties", "Nation"}).
			AssertBoolEq(false, "Properties", "ReadyToResolve").
			AssertBoolEq(false, "Properties", "WantsDIAS")
	}

	phases[0].Follow("phase-states", "Links").Success().
		Find(startedGameNats[0], []string{"Properties"}, []string{"Properties", "Nation"}).
		Follow("update", "Links").Body(map[string]interface{}{
		"ReadyToResolve": true,
		"WantsDIAS":      true,
	}).Success().
		AssertBoolEq(true, "Properties", "ReadyToResolve").
		AssertBoolEq(true, "Properties", "WantsDIAS")

	startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", startedGameID).Success().
		Find(startedGameEnvs[0].GetUID(), []string{"Properties", "Members"}, []string{"User", "Id"}).
		Find(true, []string{"NewestPhaseState", "ReadyToResolve"})

	phases[1].Follow("phase-states", "Links").Success().
		Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
		AssertBoolEq(false, "Properties", "ReadyToResolve").
		AssertBoolEq(false, "Properties", "WantsDIAS")

}
