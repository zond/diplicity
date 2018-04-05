package variants

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/zond/godip"
	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"

	vrt "github.com/zond/godip/variants/common"

	. "github.com/zond/goaeoas"
)

type Phase struct {
	Variant       string                                       `methods:"POST"`
	Season        godip.Season                                 `methods:"POST"`
	Year          int                                          `methods:"POST"`
	Type          godip.PhaseType                              `methods:"POST"`
	Units         map[godip.Province]godip.Unit                `methods:"POST"`
	Orders        map[godip.Nation]map[godip.Province][]string `methods:"POST"`
	SupplyCenters map[godip.Province]godip.Nation              `methods:"POST"`
	Dislodgeds    map[godip.Province]godip.Unit                `methods:"POST"`
	Dislodgers    map[godip.Province]godip.Province            `methods:"POST"`
	Bounces       map[godip.Province]map[godip.Province]bool   `methods:"POST"`
	Resolutions   map[godip.Province]string                    `methods:"POST"`
}

func (p *Phase) FromQuery(q url.Values) error {
	p.Season = ""
	p.Year = 1901
	p.Type = ""
	p.Units = map[godip.Province]godip.Unit{}
	p.Orders = map[godip.Nation]map[godip.Province][]string{}
	p.SupplyCenters = map[godip.Province]godip.Nation{}
	p.Dislodgeds = map[godip.Province]godip.Unit{}
	p.Dislodgers = map[godip.Province]godip.Province{}
	p.Bounces = map[godip.Province]map[godip.Province]bool{}
	p.Resolutions = map[godip.Province]string{}

	for key, vals := range q {
		for _, val := range vals {
			switch key {
			case "s":
				p.Season = godip.Season(val)
			case "y":
				y, err := strconv.ParseInt(val, 10, 64)
				if err != nil {
					return err
				}
				p.Year = int(y)
			case "t":
				p.Type = godip.PhaseType(q.Get("t"))
			default:
				if strings.HasSuffix(key, "_SC") {
					parts := strings.Split(key, "_")
					provs := strings.Split(val, "_")
					for _, prov := range provs {
						p.SupplyCenters[godip.Province(prov)] = godip.Nation(parts[0])
					}
				} else if strings.Contains(key, "_") {
					parts := strings.Split(key, "_")
					provs := strings.Split(val, "_")
					for _, prov := range provs {
						p.Units[godip.Province(prov)] = godip.Unit{
							Type:   godip.UnitType(parts[1]),
							Nation: godip.Nation(parts[0]),
						}
					}
				} else if strings.Contains(key, "-") {
					parts := strings.Split(key, "-")
					orderParts := strings.Split(val, "_")
					nationMap, found := p.Orders[godip.Nation(parts[0])]
					if !found {
						nationMap = map[godip.Province][]string{}
					}
					nationMap[godip.Province(parts[1])] = orderParts
					p.Orders[godip.Nation(parts[0])] = nationMap
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

	scs := map[godip.Nation][]string{}
	for prov, nat := range p.SupplyCenters {
		scs[nat] = append(scs[nat], string(prov))
	}
	for nat, provs := range scs {
		q.Set(fmt.Sprintf("%s_SC", nat), strings.Join(provs, "_"))
	}

	units := map[godip.Nation]map[godip.UnitType][]string{}
	for prov, unit := range p.Units {
		natUnits, found := units[unit.Nation]
		if !found {
			natUnits = map[godip.UnitType][]string{}
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
		Orders:      map[godip.Nation]map[godip.Province][]string{},
		Resolutions: map[godip.Province]string{},
		Season:      currentPhase.Season(),
		Year:        currentPhase.Year(),
		Type:        currentPhase.Type(),
	}
	var resolutions map[godip.Province]error
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
