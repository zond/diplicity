package game

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/state"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	phaseKind = "Phase"
)

type Unit struct {
	Province dip.Province
	Unit     dip.Unit
}

type SC struct {
	Province dip.Province
	Owner    dip.Nation
}

type Dislodger struct {
	Province  dip.Province
	Dislodger dip.Province
}

type Dislodged struct {
	Province  dip.Province
	Dislodged dip.Unit
}

type Bounce struct {
	Province   dip.Province
	BounceList string
}

type Resolution struct {
	Province   dip.Province
	Resolution string
}

type Phases []Phase

func (p Phases) Item(r Request, gameID *datastore.Key) *Item {
	phaseItems := make(List, len(p))
	for i := range p {
		phaseItems[i] = p[i].Item(r)
	}
	phasesItem := NewItem(phaseItems).SetName("phases").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListPhasesRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	return phasesItem
}

type Phase struct {
	GameID      *datastore.Key
	Ordinal     int64
	Season      dip.Season
	Year        int
	Type        dip.PhaseType
	Units       []Unit
	SCs         []SC
	Dislodgeds  []Dislodged
	Dislodgers  []Dislodger
	Bounces     []Bounce
	Resolutions []Resolution
	Resolved    bool
}

var PhaseResource = &Resource{
	Load:     loadPhase,
	FullPath: "/Game/{game_id}/Phase/{ordinal}",
}

func loadPhase(w ResponseWriter, r Request) (*Phase, error) {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	ordinal, err := strconv.ParseInt(r.Vars()["ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	phase := &Phase{}
	phaseID, err := PhaseID(ctx, gameID, ordinal)
	if err != nil {
		return nil, err
	}
	if err := datastore.Get(ctx, phaseID, phase); err != nil {
		return nil, err
	}

	return phase, nil
}

func (p *Phase) Item(r Request) *Item {
	phaseItem := NewItem(p).SetName(fmt.Sprintf("%s %d, %s", p.Season, p.Year, p.Type))
	phaseItem.AddLink(r.NewLink(PhaseResource.Link("self", Load, []string{"game_id", p.GameID.Encode(), "ordinal", fmt.Sprint(p.Ordinal)})))
	phaseItem.AddLink(r.NewLink(Link{
		Rel:         "orders",
		Route:       ListOrdersRoute,
		RouteParams: []string{"game_id", p.GameID.Encode(), "ordinal", fmt.Sprint(p.Ordinal)},
	}))
	return phaseItem
}

func PhaseID(ctx context.Context, gameID *datastore.Key, ordinal int64) (*datastore.Key, error) {
	if gameID == nil || ordinal < 0 {
		return nil, fmt.Errorf("phases must have games and ordinals > 0")
	}
	return datastore.NewKey(ctx, phaseKind, "", ordinal, gameID), nil
}

func (p *Phase) ID(ctx context.Context) (*datastore.Key, error) {
	return PhaseID(ctx, p.GameID, p.Ordinal)
}

func (p *Phase) Save(ctx context.Context) error {
	key, err := p.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, key, p)
	return err
}

func NewPhase(s *state.State, gameID *datastore.Key, ordinal int64) *Phase {
	current := s.Phase()
	p := &Phase{
		GameID:  gameID,
		Ordinal: ordinal,
		Season:  current.Season(),
		Year:    current.Year(),
		Type:    current.Type(),
	}
	units, scs, dislodgeds, dislodgers, bounces, resolutions := s.Dump()
	for prov, unit := range units {
		p.Units = append(p.Units, Unit{prov, unit})
	}
	for prov, nation := range scs {
		p.SCs = append(p.SCs, SC{prov, nation})
	}
	for prov, unit := range dislodgeds {
		p.Dislodgeds = append(p.Dislodgeds, Dislodged{prov, unit})
	}
	for prov, dislodger := range dislodgers {
		p.Dislodgers = append(p.Dislodgers, Dislodger{prov, dislodger})
	}
	for prov, bounceMap := range bounces {
		bounceList := []string{}
		for prov := range bounceMap {
			bounceList = append(bounceList, string(prov))
		}
		p.Bounces = append(p.Bounces, Bounce{prov, strings.Join(bounceList, ",")})
	}
	for prov, err := range resolutions {
		if err == nil {
			p.Resolutions = append(p.Resolutions, Resolution{prov, "OK"})
		} else {
			p.Resolutions = append(p.Resolutions, Resolution{prov, err.Error()})
		}
	}
	return p
}

func listPhases(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	phases := Phases{}
	_, err = datastore.NewQuery(phaseKind).Ancestor(gameID).GetAll(ctx, &phases)
	if err != nil {
		return err
	}

	w.SetContent(phases.Item(r, gameID))
	return nil
}
