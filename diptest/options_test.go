package diptest

import "testing"

func testOptions(t *testing.T) {
	g := startedGames[0]

	nation := startedGameNats[0]

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
		AssertEq("SrcProvince", "Properties", prov, "Next", "Hold", "Next", prov, "Type")

}
