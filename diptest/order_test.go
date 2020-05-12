package diptest

import (
	"testing"

	"github.com/zond/diplicity/game"
)

func testOrders(t *testing.T) {
	g := startedGames[0]

	okParts := []string{"", "Hold"}
	badParts := []string{"", "Hold"}

	nation := startedGameNats[0]

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
		Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

	t.Run("TestOrdersIsolated", func(t *testing.T) {
		phase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")

		otherPlayerPhase := startedGameEnvs[1].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("phases", "Links").Success().
			Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

		otherPlayerPhase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")

		phase.Follow("create-order", "Links").Body(map[string]interface{}{
			"Parts": okParts,
		}).Success()

		phase.Follow("orders", "Links").Success().
			Find(nation, []string{"Properties"}, []string{"Properties", "Nation"})

		otherPlayerPhase.Follow("orders", "Links").Success().
			AssertEmpty("Properties")
	})

	t.Run("TestDeleteOrder", func(t *testing.T) {

		phase.Follow("orders", "Links").Success().
			Find(nation, []string{"Properties"}, []string{"Properties", "Nation"}).
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
