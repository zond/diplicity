package variants

import (
	"fmt"
	"net/http"

	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

type Phase struct {
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

func (p *Phase) Item(r Request) *Item {
	return NewItem(p).SetName(fmt.Sprintf("%s %d, %s", p.Season, p.Year, p.Type))
}

func NewPhase(state *state.State) *Phase {
	currentPhase := state.Phase()
	p := &Phase{
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

func (self *Phase) State(variant variants.Variant) (*state.State, error) {
	parsedOrders, err := variant.ParseOrders(self.Orders)
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
		http.Error(w, fmt.Sprintf("Variant %q not found", variantName), 404)
		return nil
	}
	p := &Phase{}
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
	w.SetContent(NewPhase(state).Item(r))
	return nil
}

func startVariant(w ResponseWriter, r Request) error {
	variantName := r.Vars()["name"]
	variant, found := variants.Variants[variantName]
	if !found {
		http.Error(w, fmt.Sprintf("Variant %q not found", variantName), 404)
		return nil
	}
	state, err := variant.Start()
	if err != nil {
		return err
	}
	w.SetContent(NewPhase(state).Item(r))
	return nil
}
