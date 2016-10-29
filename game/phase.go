package game

import (
	"fmt"
	"strings"

	"github.com/zond/godip/state"
	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"

	dip "github.com/zond/godip/common"
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
