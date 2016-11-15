package diptest

import "testing"

func testPhaseState(t *testing.T) {
	phases := make([]*Result, len(startedGameEnvs))

	for i, g := range startedGames {
		phases[i] = g.Follow("phases", "Links").Success().
			Find("Spring", []string{"Properties"}, []string{"Properties", "Season"})
		phases[i].Follow("phase-states", "Links").Success().
			Find("", []string{"Properties"}, []string{"Properties", "Note"}).
			AssertBoolEq(false, "Properties", "ReadyToResolve").
			AssertBoolEq(false, "Properties", "WantsDIAS")
	}

	phases[0].Follow("phase-states", "Links").Success().
		Find("", []string{"Properties"}, []string{"Properties", "Note"}).
		Follow("update", "Links").Body(map[string]interface{}{
		"ReadyToResolve": true,
		"WantsDIAS":      true,
	}).Success().
		AssertBoolEq(true, "Properties", "ReadyToResolve").
		AssertBoolEq(true, "Properties", "WantsDIAS")

	phases[1].Follow("phase-states", "Links").Success().
		Find("", []string{"Properties"}, []string{"Properties", "Note"}).
		AssertBoolEq(false, "Properties", "ReadyToResolve").
		AssertBoolEq(false, "Properties", "WantsDIAS")

}
