package variants

import (
	"github.com/gorilla/mux"
	"github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	ListVariantsRoute = "ListVariants"
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
	})
	return rvItem
}

type RenderVariant struct {
	variants.Variant
	Start RenderPhase
}

func (rv *RenderVariant) Item(r Request) *Item {
	return NewItem(rv).SetName(rv.Name)
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
}
