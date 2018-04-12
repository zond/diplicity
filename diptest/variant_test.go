package diptest

import (
	"net/url"
	"testing"
	"time"

	"github.com/zond/diplicity/game"
	"github.com/zond/diplicity/variants"
	"github.com/zond/godip/variants/youngstownredux"
)

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
