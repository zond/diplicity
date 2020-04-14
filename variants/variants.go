package variants

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip"
	"github.com/zond/godip/variants"
	"github.com/zond/godip/variants/chaos"
	"github.com/zond/godip/variants/empiresandcoalitions"
	"github.com/zond/godip/variants/europe1939"
	"github.com/zond/godip/variants/hundred"
	"github.com/zond/godip/variants/northseawars"
	"github.com/zond/godip/variants/twentytwenty"
	"github.com/zond/godip/variants/westernworld901"
	"github.com/zond/godip/variants/youngstownredux"

	vrt "github.com/zond/godip/variants/common"

	. "github.com/zond/goaeoas"
)

var (
	router *mux.Router
	// Maps variant key to Diplicity API level.
	// Since all clients (including clients created before the API level concept existed)
	// has at least level 1, the Youngstown Redux entry is just an example.
	// (And used when testing, by artificially forcing API level 0.)
	LaunchSchedule = map[string]int{
		empiresandcoalitions.EmpiresAndCoalitionsVariant.Name: 8,
		europe1939.Europe1939Variant.Name:                     7,
		northseawars.NorthSeaWarsVariant.Name:                 6,
		twentytwenty.TwentyTwentyVariant.Name:                 5,
		westernworld901.WesternWorld901Variant.Name:           4,
		chaos.ChaosVariant.Name:                               3,
		hundred.HundredVariant.Name:                           2,
		youngstownredux.YoungstownReduxVariant.Name:           1,
	}

	VariantResource *Resource
)

const (
	ListVariantsRoute   = "ListVariants"
	VariantStartRoute   = "StartVariant"
	VariantResolveRoute = "ResolveVariant"
	VariantUnitsRoute   = "VariantUnits"
	VariantFlagsRoute   = "VariantFlags"
	VariantMapRoute     = "VariantMap"
	RenderMapRoute      = "RenderMap"
)

func init() {
	for _, launchLevel := range LaunchSchedule {
		if launchLevel > auth.HTMLAPILevel {
			auth.HTMLAPILevel = launchLevel
		}
	}

	VariantResource = &Resource{
		Load:     loadVariant,
		FullPath: "/Variant/{variant_name}",
		Listers: []Lister{
			{
				Path:    "/Variants",
				Route:   ListVariantsRoute,
				Handler: listVariants,
			},
		},
	}
}

type RenderPhase struct {
	Year   int
	Season godip.Season
	Type   godip.PhaseType
	SCs    map[godip.Province]godip.Nation
	Units  map[godip.Province]godip.Unit
	Map    string
}

type Variants map[string]Variant

func (rv Variants) Item(r Request) *Item {
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

type Variant struct {
	vrt.Variant
	// OrderTypes are the types of orders this variant has.
	OrderTypes []godip.OrderType
	Start      RenderPhase
	Graph      godip.Graph `skip:"yes"`
}

func (rv *Variant) Item(r Request) *Item {
	item := NewItem(rv).SetName(rv.Name).AddLink(r.NewLink(VariantResource.Link("self", Load, []string{"variant_name", rv.Name}))).AddLink(r.NewLink(Link{
		Rel:         "start-state",
		Route:       VariantStartRoute,
		RouteParams: []string{"name", rv.Name},
	})).AddLink(r.NewLink(Link{
		Rel:         "resolve-state",
		Method:      "POST",
		Route:       VariantResolveRoute,
		RouteParams: []string{"name", rv.Name},
		Type:        reflect.TypeOf(Phase{}),
	})).AddLink(r.NewLink(Link{
		Rel:         "map",
		Route:       VariantMapRoute,
		RouteParams: []string{"name", rv.Variant.Name},
	}))
	for unitName := range rv.SVGUnits {
		item.AddLink(r.NewLink(Link{
			Rel:         fmt.Sprintf("unit-%v", unitName),
			Route:       VariantUnitsRoute,
			RouteParams: []string{"variant_name", rv.Name, "unit_name", string(unitName)},
		}))
	}
	for nationName := range rv.SVGFlags {
		item.AddLink(r.NewLink(Link{
			Rel:         fmt.Sprintf("flag-%v", nationName),
			Route:       VariantFlagsRoute,
			RouteParams: []string{"variant_name", rv.Name, "nation_name", string(nationName)},
		}))
	}
	return item
}

func loadVariant(w ResponseWriter, r Request) (*Variant, error) {
	if v, found := variants.Variants[r.Vars()["variant_name"]]; found {
		s, err := v.Start()
		if err != nil {
			return nil, err
		}
		p := s.Phase()
		return &Variant{
			Variant:    v,
			OrderTypes: v.Parser.OrderTypes(),
			Start: RenderPhase{
				Year:   p.Year(),
				Season: p.Season(),
				Type:   p.Type(),
				SCs:    s.SupplyCenters(),
				Units:  s.Units(),
			},
			Graph: v.Graph(),
		}, nil
	}
	return nil, HTTPErr{fmt.Sprintf("Variant %#v not found", r.Values()["variant_name"]), http.StatusNotFound}
}

func listVariants(w ResponseWriter, r Request) error {
	apiLevel := auth.APILevel(r)
	renderVariants := Variants{}
	for k, v := range variants.Variants {
		// If the scheduled launch level for this variant is less than the API level of the client,
		// just don't list it.
		if launchLevel, found := LaunchSchedule[k]; found {
			if launchLevel > apiLevel {
				continue
			}
		}
		s, err := v.Start()
		if err != nil {
			return err
		}
		p := s.Phase()
		renderVariants[k] = Variant{
			Variant:    v,
			OrderTypes: v.Parser.OrderTypes(),
			Start: RenderPhase{
				Year:   p.Year(),
				Season: p.Season(),
				Type:   p.Type(),
				SCs:    s.SupplyCenters(),
				Units:  s.Units(),
			},
			Graph: v.Graph(),
		}
	}
	w.SetContent(renderVariants.Item(r))
	return nil
}

func variantFlags(w ResponseWriter, r Request) error {
	variantName := r.Vars()["variant_name"]
	variant, found := variants.Variants[variantName]
	if !found {
		return HTTPErr{fmt.Sprintf("Variant %q not found", variantName), http.StatusNotFound}
	}

	nationName := r.Vars()["nation_name"]
	nationFunc, found := variant.SVGFlags[godip.Nation(nationName)]
	if !found {
		return HTTPErr{fmt.Sprintf("Flag for nation %q not found in variant %q", nationName, variantName), http.StatusNotFound}
	}

	b, err := nationFunc()
	if err != nil {
		return err
	}

	etag := variant.SVGVersion
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Etag", etag)
	w.Header().Set("Cache-Control", "max-age=3600") // 1 hour
	if match := r.Req().Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	_, err = w.Write(b)
	return err
}

func variantUnits(w ResponseWriter, r Request) error {
	variantName := r.Vars()["variant_name"]
	variant, found := variants.Variants[variantName]
	if !found {
		return HTTPErr{fmt.Sprintf("Variant %q not found", variantName), http.StatusNotFound}
	}

	unitName := r.Vars()["unit_name"]
	unitFunc, found := variant.SVGUnits[godip.UnitType(unitName)]
	if !found {
		return HTTPErr{fmt.Sprintf("Unit %q not found in variant %q", unitName, variantName), http.StatusNotFound}
	}

	b, err := unitFunc()
	if err != nil {
		return err
	}

	etag := variant.SVGVersion
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Etag", etag)
	w.Header().Set("Cache-Control", "max-age=3600") // 1 hour
	if match := r.Req().Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	_, err = w.Write(b)
	return err
}

func variantMap(w ResponseWriter, r Request) error {
	variantName := r.Vars()["name"]
	variant, found := variants.Variants[variantName]
	if !found {
		return HTTPErr{fmt.Sprintf("Variant %q not found", variantName), http.StatusNotFound}
	}

	b, err := variant.SVGMap()
	if err != nil {
		return err
	}

	etag := variant.SVGVersion
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Etag", etag)
	w.Header().Set("Cache-Control", "max-age=3600") // 1 hour
	if match := r.Req().Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	_, err = w.Write(b)
	return err
}

func SetupRouter(r *mux.Router) {
	router = r
	HandleResource(r, VariantResource)
	Handle(r, "/Variant/{name}/Start", []string{"GET"}, VariantStartRoute, startVariant)
	Handle(r, "/Variant/{name}/Resolve", []string{"POST"}, VariantResolveRoute, resolveVariant)
	Handle(r, "/Variant/{name}/Map.svg", []string{"GET"}, VariantMapRoute, variantMap)
	Handle(r, "/Variant/{variant_name}/Units/{unit_name}.svg", []string{"GET"}, VariantUnitsRoute, variantUnits)
	Handle(r, "/Variant/{variant_name}/Flags/{nation_name}.svg", []string{"GET"}, VariantFlagsRoute, variantFlags)
	Handle(r, "/Variant/{name}/Render", []string{"GET"}, RenderMapRoute, handleRenderMap)
}
