package diptest

import "github.com/zond/diplicity/game"

func testOptions(gameDesc string, envs []*Env) {
	g := envs[0].GetRoute(game.IndexRoute).Success().
		Follow("my-started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)

	nation := g.
		Find([]string{"Properties", "Members"}, []string{"User", "Id"}, envs[0].GetUID()).GetValue("Nation")

	var prov string

	switch nation {
	case "Austria":
		prov = "vie"
	case "Germany":
		prov = "ber"
	case "Turkey":
		prov = "ank"
	case "Italy":
		prov = "rom"
	case "France":
		prov = "bre"
	case "Russia":
		prov = "mos"
	case "England":
		prov = "lon"
	}

	phase := g.
		Follow("phases", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Season"}, "Spring")

	phase.Follow("options", "Links").Success().
		AssertStringEq("SrcProvince", "Properties", prov, "Next", "Hold", "Next", prov, "Type")

}
