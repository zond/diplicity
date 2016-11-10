package diptest

import "testing"

func testGameState(t *testing.T) {
	g0 := startedGames[0]
	g1 := startedGames[1]
	nat0 := startedGameNats[0]
	nat1 := startedGameNats[1]

	g0.Follow("game-states", "Links").Success().AssertLen(7, "Properties").
		Find([]string{"Properties"}, []string{"Properties", "Nation"}, nat0).
		Follow("update", "Links").Body(map[string]interface{}{
		"Muted": []string{nat1},
	}).Success()

	g0.Follow("game-states", "Links").Success().AssertLen(7, "Properties").
		Find([]string{"Properties"}, []string{"Properties", "Nation"}, nat0).
		AssertEq([]interface{}{nat1}, "Properties", "Muted")

	g0.Follow("game-states", "Links").Success().AssertLen(7, "Properties").
		Find([]string{"Properties"}, []string{"Properties", "Nation"}, nat1).
		AssertNil("Properties", "Muted").AssertNil("Links")

	g1.Follow("game-states", "Links").Success().AssertLen(7, "Properties").
		Find([]string{"Properties"}, []string{"Properties", "Nation"}, nat0).
		AssertEq([]interface{}{nat1}, "Properties", "Muted")

	g1.Follow("game-states", "Links").Success().AssertLen(7, "Properties").
		Find([]string{"Properties"}, []string{"Properties", "Nation"}, nat1).
		AssertNil("Properties", "Muted")

}
