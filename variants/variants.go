package variants

import (
	"reflect"

	"github.com/gorilla/mux"
	"github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	ListVariantsRoute   = "ListVariants"
	VariantStartRoute   = "StartVariant"
	VariantResolveRoute = "ResolveVariant"
)

type RenderPhase struct {
	Year   int
	Season dip.Season
	Type   dip.PhaseType
	SCs    map[dip.Province]dip.Nation
	Units  map[dip.Province]dip.Unit
}

type RenderVariants map[string]RenderVariant

func (rv RenderVariants) Item(r Request) *Item {
	vItems := make(List, 0, len(rv))
	for _, v := range rv {
		cpy := v
		vItems = append(vItems, cpy.Item(r))
	}
	rvItem := NewItem(vItems).SetName("variants").AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: ListVariantsRoute,
	})).SetDesc([][]string{
		[]string{
			"Variants",
			"This lists the supported variants on the server. Graph logically represents the map, while the rest of the fields should be fairly self explanatory.",
		},
		[]string{
			"Variant services",
			"Variants can provide clients with a start state as a JSON blob via the `start-state` link.",
			"Note: The start state is contained in the `Properties` field of the object presented at `start-state`.",
			"To get the resolved result of a state plus some orders, the client `POST`s the same state plus the orders as a map `{ NATION: { PROVINCE: []WORD } }`, e.g. `{ 'England': { 'lon': ['Move', 'nth'] } }`.",
			"Unfortunately the auto generated HTML interface isn't powerful enough to create an easy to use form for this, so interested parties might have to use `curl` or similar tools to experiment.",
		},
		[]string{
			"Phase types",
			"Note that the phase types used for the variant service (`/Variants` and `/Variant/...`) is not the same as the phase type presented in the regular game service (`/Games/...` and `/Game/...`).",
			"The variant service targets independent dippy service developers, not players or front end developers, and does not provide anything other than simple start-state and resolve-state functionality.",
		},
	})
	return rvItem
}

type RenderVariant struct {
	variants.Variant
	Start RenderPhase
}

func (rv *RenderVariant) Item(r Request) *Item {
	return NewItem(rv).SetName(rv.Name).AddLink(r.NewLink(Link{
		Rel:         "start-state",
		Route:       VariantStartRoute,
		RouteParams: []string{"name", rv.Name},
	})).AddLink(r.NewLink(Link{
		Rel:         "resolve-state",
		Method:      "POST",
		Route:       VariantResolveRoute,
		RouteParams: []string{"name", rv.Name},
		Type:        reflect.TypeOf(Phase{}),
	}))
}

func listVariants(w ResponseWriter, r Request) error {
	renderVariants := RenderVariants{}
	for k, v := range variants.Variants {
		s, err := v.Start()
		if err != nil {
			return err
		}
		p := s.Phase()
		renderVariants[k] = RenderVariant{
			Variant: v,
			Start: RenderPhase{
				Year:   p.Year(),
				Season: p.Season(),
				Type:   p.Type(),
				SCs:    s.SupplyCenters(),
				Units:  s.Units(),
			},
		}
	}
	w.SetContent(renderVariants.Item(r))
	return nil
}

func SetupRouter(r *mux.Router) {
	Handle(r, "/Variants", []string{"GET"}, ListVariantsRoute, listVariants)
	Handle(r, "/Variant/{name}/Start", []string{"GET"}, VariantStartRoute, startVariant)
	Handle(r, "/Variant/{name}/Resolve", []string{"POST"}, VariantResolveRoute, resolveVariant)
}
