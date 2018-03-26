package variants

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
	vrt "github.com/zond/godip/variants/common"
)

type Phase struct {
	Variant       string                                   `methods:"POST"`
	Season        dip.Season                               `methods:"POST"`
	Year          int                                      `methods:"POST"`
	Type          dip.PhaseType                            `methods:"POST"`
	Units         map[dip.Province]dip.Unit                `methods:"POST"`
	Orders        map[dip.Nation]map[dip.Province][]string `methods:"POST"`
	SupplyCenters map[dip.Province]dip.Nation              `methods:"POST"`
	Dislodgeds    map[dip.Province]dip.Unit                `methods:"POST"`
	Dislodgers    map[dip.Province]dip.Province            `methods:"POST"`
	Bounces       map[dip.Province]map[dip.Province]bool   `methods:"POST"`
	Resolutions   map[dip.Province]string                  `methods:"POST"`
}

func (p *Phase) FromQuery(q url.Values) error {
	p.Season = ""
	p.Year = 1901
	p.Type = ""
	p.Units = map[dip.Province]dip.Unit{}
	p.Orders = map[dip.Nation]map[dip.Province][]string{}
	p.SupplyCenters = map[dip.Province]dip.Nation{}
	p.Dislodgeds = map[dip.Province]dip.Unit{}
	p.Dislodgers = map[dip.Province]dip.Province{}
	p.Bounces = map[dip.Province]map[dip.Province]bool{}
	p.Resolutions = map[dip.Province]string{}

	for key, vals := range q {
		for _, val := range vals {
			switch key {
			case "s":
				p.Season = dip.Season(val)
			case "y":
				y, err := strconv.ParseInt(val, 10, 64)
				if err != nil {
					return err
				}
				p.Year = int(y)
			case "t":
				p.Type = dip.PhaseType(q.Get("t"))
			default:
				if strings.HasSuffix(key, "_SC") {
					parts := strings.Split(key, "_")
					provs := strings.Split(val, "_")
					for _, prov := range provs {
						p.SupplyCenters[dip.Province(prov)] = dip.Nation(parts[0])
					}
				} else if strings.Contains(key, "_") {
					parts := strings.Split(key, "_")
					provs := strings.Split(val, "_")
					for _, prov := range provs {
						p.Units[dip.Province(prov)] = dip.Unit{
							Type:   dip.UnitType(parts[1]),
							Nation: dip.Nation(parts[0]),
						}
					}
				} else if strings.Contains(key, "-") {
					parts := strings.Split(key, "-")
					orderParts := strings.Split(val, "_")
					nationMap, found := p.Orders[dip.Nation(parts[0])]
					if !found {
						nationMap = map[dip.Province][]string{}
					}
					nationMap[dip.Province(parts[1])] = orderParts
					p.Orders[dip.Nation(parts[0])] = nationMap
				}
			}
		}
	}

	return nil
}

func (p *Phase) ToQuery() url.Values {
	q := url.Values{}
	q.Set("s", string(p.Season))
	q.Set("y", fmt.Sprint(p.Year))
	q.Set("t", string(p.Type))

	scs := map[dip.Nation][]string{}
	for prov, nat := range p.SupplyCenters {
		scs[nat] = append(scs[nat], string(prov))
	}
	for nat, provs := range scs {
		q.Set(fmt.Sprintf("%s_SC", nat), strings.Join(provs, "_"))
	}

	units := map[dip.Nation]map[dip.UnitType][]string{}
	for prov, unit := range p.Units {
		natUnits, found := units[unit.Nation]
		if !found {
			natUnits = map[dip.UnitType][]string{}
		}
		natUnits[unit.Type] = append(natUnits[unit.Type], string(prov))
		units[unit.Nation] = natUnits
	}
	for nat, types := range units {
		for typ, provs := range types {
			q.Set(fmt.Sprintf("%s_%s", nat, typ), strings.Join(provs, "_"))
		}
	}

	for nat, provs := range p.Orders {
		for prov, parts := range provs {
			q.Set(fmt.Sprintf("%s-%s", nat, prov), strings.Join(parts, "_"))
		}
	}

	return q
}

func (p *Phase) Item(r Request) *Item {
	return NewItem(p).SetName(fmt.Sprintf("%s %d, %s", p.Season, p.Year, p.Type)).
		AddLink(r.NewLink(Link{
			Rel:         "map",
			Route:       RenderMapRoute,
			RouteParams: []string{"name", p.Variant},
			QueryParams: p.ToQuery(),
		}))
}

func NewPhase(state *state.State, variantName string) *Phase {
	currentPhase := state.Phase()
	p := &Phase{
		Variant:     variantName,
		Orders:      map[dip.Nation]map[dip.Province][]string{},
		Resolutions: map[dip.Province]string{},
		Season:      currentPhase.Season(),
		Year:        currentPhase.Year(),
		Type:        currentPhase.Type(),
	}
	var resolutions map[dip.Province]error
	p.Units, p.SupplyCenters, p.Dislodgeds, p.Dislodgers, p.Bounces, resolutions = state.Dump()
	for prov, err := range resolutions {
		if err == nil {
			p.Resolutions[prov] = "OK"
		} else {
			p.Resolutions[prov] = err.Error()
		}
	}
	return p
}

func (self *Phase) State(variant vrt.Variant) (*state.State, error) {
	parsedOrders, err := variant.Parser.ParseAll(self.Orders)
	if err != nil {
		return nil, err
	}
	return variant.Blank(variant.Phase(
		self.Year,
		self.Season,
		self.Type,
	)).Load(
		self.Units,
		self.SupplyCenters,
		self.Dislodgeds,
		self.Dislodgers,
		self.Bounces,
		parsedOrders,
	), nil
}

func resolveVariant(w ResponseWriter, r Request) error {
	variantName := r.Vars()["name"]
	variant, found := variants.Variants[variantName]
	if !found {
		return HTTPErr{fmt.Sprintf("Variant %q not found", variantName), 404}
	}
	p := &Phase{
		Variant: variantName,
	}
	if err := Copy(p, r, "POST"); err != nil {
		return err
	}
	state, err := p.State(variant)
	if err != nil {
		return err
	}
	if err = state.Next(); err != nil {
		return err
	}
	w.SetContent(NewPhase(state, p.Variant).Item(r))
	return nil
}

func startVariant(w ResponseWriter, r Request) error {
	variantName := r.Vars()["name"]
	variant, found := variants.Variants[variantName]
	if !found {
		return HTTPErr{fmt.Sprintf("Variant %q not found", variantName), 404}
	}
	state, err := variant.Start()
	if err != nil {
		return err
	}
	w.SetContent(NewPhase(state, variantName).Item(r))
	return nil
}
