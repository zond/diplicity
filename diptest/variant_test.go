package diptest

import (
	"net/url"
	"testing"
	"time"

	"github.com/kr/pretty"
	"github.com/zond/diplicity/game"
	"github.com/zond/diplicity/variants"
	"github.com/zond/godip"
	"github.com/zond/godip/variants/youngstownredux"

	gvars "github.com/zond/godip/variants"
)

func TestColorParsing(t *testing.T) {
	o, n, v := variants.ParseColors([]string{
		"ff00ff",
		"#f0f0f0",
		"#ff00ff00",
		"#fff",
		"#f0f0f",
		"#ff00ff0",
		"#ff00ff00f",
		"#ff00ff00ff",
		"Classical/France/#ff00ff",
		"Classical/France/#ff00fff",
		"Classical/France/#ff00f",
		"France/#ff0000",
		"France/#ff00f",
		"France/#ff00ff0",
	})
	if diff := pretty.Diff(o, []string{
		"#f0f0f0",
		"#ff00ff00",
	}); diff != nil {
		t.Errorf("Got wrong colors: %+v", diff)
	}
	if diff := pretty.Diff(v, map[string]map[godip.Nation]string{
		"Classical": map[godip.Nation]string{
			godip.France: "#ff00ff",
		},
	}); diff != nil {
		t.Errorf("Got wrong colors: %+v", diff)
	}
	if diff := pretty.Diff(n, map[godip.Nation]string{
		godip.France: "#ff0000",
	}); diff != nil {
		t.Errorf("Got wrong colors: %+v", diff)
	}
}

func TestVariantVisibility(t *testing.T) {
	uid := String("fake")
	e := NewEnv().SetUID(uid)

	e.GetRoute(variants.ListVariantsRoute).Success().
		Find(youngstownredux.YoungstownReduxVariant.Name, []string{"Properties"}, []string{"Properties", "Name"})

	e.GetRoute(variants.ListVariantsRoute).QueryParams(url.Values{
		"api-level": []string{"0"},
	}).Success().
		AssertNotFind(youngstownredux.YoungstownReduxVariant.Name, []string{"Properties"}, []string{"Properties", "Name"})

	gameDesc := String("test-game")
	e.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":            youngstownredux.YoungstownReduxVariant.Name,
		"NoMerge":            true,
		"Desc":               gameDesc,
		"MaxHated":           0,
		"MaxHater":           0,
		"MinReliability":     0,
		"MinQuickness":       0,
		"MinRating":          0,
		"MaxRating":          0,
		"PhaseLengthMinutes": time.Duration(60),
	}).Success()

	e.GetRoute(game.ListOpenGamesRoute).Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	e.GetRoute(game.ListOpenGamesRoute).QueryParams(url.Values{
		"api-level": []string{"0"},
	}).Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
}

func TestLoadSingleVariant(t *testing.T) {
	e := NewEnv()
	for n := range gvars.Variants {
		e.GetRoute("Variant.Load").RouteParams("variant_name", n).Success().
			Find(n, []string{"Properties", "Name"})
	}

}
