package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func testOrders(t *testing.T) {
	g := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)

	okParts := []string{"", "Hold"}
	badParts := []string{"", "Hold"}

	nation := g.
		Find([]string{"Properties", "Members"}, []string{"User", "Id"}, startedGameEnvs[0].GetUID()).GetValue("Nation")

	switch nation {
	case "Austria":
		okParts[0] = "vie"
		badParts[0] = "ber"
	case "Germany":
		okParts[0] = "ber"
		badParts[0] = "ank"
	case "Turkey":
		okParts[0] = "ank"
		badParts[0] = "rom"
	case "Italy":
		okParts[0] = "rom"
		badParts[0] = "bre"
	case "France":
		okParts[0] = "bre"
		badParts[0] = "mos"
	case "Russia":
		okParts[0] = "mos"
		badParts[0] = "lon"
	case "England":
		okParts[0] = "lon"
		badParts[0] = "vie"
	}

	phase := g.
		Follow("phases", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

	t.Run("TestOrdersIsolated", func(t *testing.T) {
		phase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")

		otherPlayerPhase := startedGameEnvs[1].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc).
			Follow("phases", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

		otherPlayerPhase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")

		phase.Follow("create-order", "Links").Body(map[string]interface{}{
			"Parts": okParts,
		}).Success()

		phase.Follow("orders", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Nation"}, nation)

		otherPlayerPhase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")
	})

	t.Run("TestDeleteOrder", func(t *testing.T) {

		phase.Follow("orders", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Nation"}, nation).
			Follow("delete", "Links").Success()

		phase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")
	})

	t.Run("TestBadOrderErrors", func(t *testing.T) {
		phase.Follow("create-order", "Links").Body(map[string]interface{}{
			"Parts": badParts,
		}).Failure()

		phase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")
	})
}
