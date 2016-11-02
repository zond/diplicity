package diptest

import "github.com/zond/diplicity/game"

func testOrders(gameDesc string, envs []*Env) {
	g := envs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)

	parts := []string{"", "Hold"}

	nation := g.
		Find([]string{"Properties", "Members"}, []string{"User", "Id"}, envs[0].GetUID()).GetValue("Nation")

	switch nation {
	case "Austria":
		parts[0] = "vie"
	case "Germany":
		parts[0] = "ber"
	case "Turkey":
		parts[0] = "ank"
	case "Italy":
		parts[0] = "rom"
	case "France":
		parts[0] = "bre"
	case "Russia":
		parts[0] = "mos"
	case "England":
		parts[0] = "lon"
	}

	phase := g.
		Follow("phases", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

	phase.Follow("orders", "Links").Success().
		AssertEmpty("Properties")

	otherPlayerPhase := envs[1].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc).
		Follow("phases", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

	otherPlayerPhase.Follow("orders", "Links").Success().
		AssertEmpty("Properties")

	phase.Follow("create-order", "Links").Body(map[string]interface{}{
		"Parts": parts,
	}).Success()

	phase.Follow("orders", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Nation"}, nation)

	otherPlayerPhase.Follow("orders", "Links").Success().
		AssertEmpty("Properties")

}
