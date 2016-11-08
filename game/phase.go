package game

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/delay"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

var (
	timeoutResolvePhase *delay.Function
)

func init() {
	timeoutResolvePhase = delay.Func("game-timeoutResolvePhase", func(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) error {
		log.Infof(ctx, "timeoutResolvePhase(..., %v, %v)", gameID, phaseOrdinal)

		phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
		if err != nil {
			log.Errorf(ctx, "PhaseID(..., %v, %v): %v, %v; fix the PhaseID func", gameID, phaseOrdinal, phaseID, err)
			return err
		}

		if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
			game := &Game{}
			phase := &Phase{}
			keys := []*datastore.Key{gameID, phaseID}
			values := []interface{}{game, phase}
			if err := datastore.GetMulti(ctx, keys, values); err != nil {
				log.Errorf(ctx, "datastore.GetMulti(..., %v, %v): %v; hope datastore will get fixed", keys, values, err)
				return err
			}

			return (&PhaseResolver{
				Context:       ctx,
				Game:          game,
				Phase:         phase,
				PhaseState:    nil,
				TaskTriggered: true,
			}).Act()
		}, &datastore.TransactionOptions{XG: true}); err != nil {
			log.Errorf(ctx, "Unable to commit resolve tx: %v; retrying later", err)
			return err
		}

		log.Infof(ctx, "timeoutResolvePhase(..., %v, %v): *** SUCCESS ***", gameID, phaseOrdinal, err)

		return nil
	})
}

type PhaseResolver struct {
	Context       context.Context
	Game          *Game
	Phase         *Phase
	PhaseState    *PhaseState
	TaskTriggered bool
}

func (p *PhaseResolver) Act() error {
	log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act()", p.Phase.GameID, p.Phase.PhaseOrdinal)

	phaseID, err := PhaseID(p.Context, p.Phase.GameID, p.Phase.PhaseOrdinal)
	if err != nil {
		return err
	}

	if p.TaskTriggered && p.Phase.DeadlineAt.After(time.Now()) {
		log.Infof(p.Context, "Resolution postponed to %v by %v; rescheduling task", p.Phase.DeadlineAt, spew.Sdump(p.Phase))
		return p.Phase.ScheduleResolution(p.Context)
	}

	if p.Phase.Resolved {
		log.Infof(p.Context, "Already resolved; %v; skipping resolution", spew.Sdump(p.Phase))
		return nil
	}

	phaseStates := []PhaseState{}
	if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(p.Context, &phaseStates); err != nil {
		log.Errorf(p.Context, "Unable to load phase states for all members of %v: %v; hope datastore will get fixed", spew.Sdump(p.Phase), err)
		return err
	}
	// This is necessary because datastore transactions read the state from before they started,
	// which would make the query for phase states after the last player flags 'ready' read the
	// last player as 'not ready'. Let's just override that here.
	if p.PhaseState != nil {
		foundPhaseStateOverride := false
		for i := range phaseStates {
			if phaseStates[i].Nation == p.PhaseState.Nation {
				phaseStates[i] = *p.PhaseState
				foundPhaseStateOverride = true
				break
			}
		}
		if !foundPhaseStateOverride {
			phaseStates = append(phaseStates, *p.PhaseState)
		}
	}
	log.Infof(p.Context, "PhaseStates at resolve time: %v", spew.Sdump(phaseStates))

	orderMap, err := p.Phase.Orders(p.Context)
	if err != nil {
		log.Errorf(p.Context, "Unable to load orders for %v: %v; fix phase.Orders or hope datastore will get fixed", spew.Sdump(p.Phase), err)
		return err
	}
	log.Infof(p.Context, "Orders at resolve time: %v", spew.Sdump(orderMap))

	s, err := p.Phase.State(p.Context, variants.Variants[p.Game.Variant], orderMap)
	if err != nil {
		log.Errorf(p.Context, "Unable to create godip State for %v: %v; fix godip!", spew.Sdump(p.Phase), err)
		return err
	}
	if err := s.Next(); err != nil {
		log.Errorf(p.Context, "Unable to roll State forward for %v: %v; fix godip!", spew.Sdump(p.Phase), err)
		return err
	}

	newPhase := NewPhase(s, p.Phase.GameID, p.Phase.PhaseOrdinal+1)
	newPhase.DeadlineAt = time.Now().Add(time.Minute * p.Game.PhaseLengthMinutes)
	if err := newPhase.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save new Phase %v: %v; hope datastore will get fixed", spew.Sdump(newPhase), err)
		return err
	}

	for _, nat := range variants.Variants[p.Game.Variant].Nations {
		_, hadOrders := orderMap[nat]
		wasReady := false
		for _, phaseState := range phaseStates {
			if phaseState.Nation == nat && phaseState.ReadyToResolve {
				wasReady = true
				break
			}
		}
		log.Infof(p.Context, "%v NMR state: hadOrders = %v, wasReady = %v", nat, hadOrders, wasReady)
		if !hadOrders && !wasReady {
			newPhaseState := &PhaseState{
				GameID:         p.Phase.GameID,
				PhaseOrdinal:   newPhase.PhaseOrdinal,
				Nation:         nat,
				ReadyToResolve: true,
				WantsDIAS:      true,
				Note:           fmt.Sprintf("Auto generated due to NMR in %v/%v; hadOrders = %v, wasReady = %v", p.Phase.GameID, p.Phase.PhaseOrdinal, hadOrders, wasReady),
			}
			if err := newPhaseState.Save(p.Context); err != nil {
				log.Errorf(p.Context, "Unable to save new PhaseState %v: %v; hope datastore will get fixed", spew.Sdump(newPhaseState), err)
				return err
			}
			log.Infof(p.Context, "Saved %v to avoid delays due to tardy players", spew.Sdump(newPhaseState))
		}
	}

	if p.Game.PhaseLengthMinutes > 0 {
		if err := newPhase.ScheduleResolution(p.Context); err != nil {
			log.Errorf(p.Context, "Unable to schedule resolution for %v: %v; fix ScheduleResolution or hope datastore gets fixed", spew.Sdump(newPhase), err)
			return err
		}
	} else {
		log.Infof(p.Context, "%v has a zero phase length, skipping resolve scheduling", spew.Sdump(p.Game))
	}

	p.Phase.Resolved = true
	if err := p.Phase.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save old phase %v: %v; hope datastore gets fixed", spew.Sdump(p.Phase), err)
		return err
	}

	log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESS ***", p.Phase.GameID, p.Phase.PhaseOrdinal)

	return nil
}

const (
	phaseKind        = "Phase"
	memberNationFlag = "member-nation"
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
	GameID       *datastore.Key
	PhaseOrdinal int64
	Season       dip.Season
	Year         int
	Type         dip.PhaseType
	Units        []Unit
	SCs          []SC
	Dislodgeds   []Dislodged
	Dislodgers   []Dislodger
	Bounces      []Bounce
	Resolutions  []Resolution
	Resolved     bool
	DeadlineAt   time.Time
}

var PhaseResource = &Resource{
	Load:     loadPhase,
	FullPath: "/Game/{game_id}/Phase/{phase_ordinal}",
}

func loadPhase(w ResponseWriter, r Request) (*Phase, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
		return nil, err
	}
	game.ID = gameID

	member, isMember := game.GetMember(user.Id)
	if isMember {
		r.Values()[memberNationFlag] = member.Nation
	}

	return phase, nil
}

func (p *Phase) Item(r Request) *Item {
	phaseItem := NewItem(p).SetName(fmt.Sprintf("%s %d, %s", p.Season, p.Year, p.Type))
	phaseItem.AddLink(r.NewLink(PhaseResource.Link("self", Load, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)})))
	_, isMember := r.Values()[memberNationFlag]
	if isMember || p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "orders",
			Route:       ListOrdersRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
	}
	if isMember && !p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "options",
			Route:       ListOptionsRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
		phaseItem.AddLink(r.NewLink(OrderResource.Link("create-order", Create, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)})))
	}
	if isMember || p.Resolved {
		phaseItem.AddLink(r.NewLink(Link{
			Rel:         "phase-states",
			Route:       ListPhaseStatesRoute,
			RouteParams: []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal)},
		}))
	}
	return phaseItem
}

func (p *Phase) ScheduleResolution(ctx context.Context) error {
	task, err := timeoutResolvePhase.Task(p.GameID, p.PhaseOrdinal)
	if err != nil {
		return err
	}
	task.ETA = p.DeadlineAt
	_, err = taskqueue.Add(ctx, task, "game-timeoutResolvePhase")
	return err
}

func PhaseID(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) (*datastore.Key, error) {
	if gameID == nil || phaseOrdinal < 0 {
		return nil, fmt.Errorf("phases must have games and ordinals > 0")
	}
	return datastore.NewKey(ctx, phaseKind, "", phaseOrdinal, gameID), nil
}

func (p *Phase) ID(ctx context.Context) (*datastore.Key, error) {
	return PhaseID(ctx, p.GameID, p.PhaseOrdinal)
}

func (p *Phase) Save(ctx context.Context) error {
	key, err := p.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, key, p)
	return err
}

func NewPhase(s *state.State, gameID *datastore.Key, phaseOrdinal int64) *Phase {
	current := s.Phase()
	p := &Phase{
		GameID:       gameID,
		PhaseOrdinal: phaseOrdinal,
		Season:       current.Season(),
		Year:         current.Year(),
		Type:         current.Type(),
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

func listOptions(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return err
	}

	game := &Game{}
	phase := &Phase{}
	if err = datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
		return err
	}
	game.ID = gameID

	member, isMember := game.GetMember(user.Id)
	if !isMember {
		return fmt.Errorf("can only load options for member games")
	}

	state, err := phase.State(ctx, variants.Variants[game.Variant], nil)
	if err != nil {
		return err
	}

	w.SetContent(NewItem(state.Phase().Options(state, member.Nation)).SetName("options").SetDesc([][]string{
		[]string{
			"Options explained",
			"The options consist of a decision tree where each node represents a decision a player has to make when defining an order.",
			"Each child set contains one or more alternatives of the same decision type, viz. `Province`, `OrderType`, `UnitType` or `SrcProvince`.",
			"To guide the player towards defining an order, present the alternatives for each node, then the sub tree pointed to by `Next`, etc. until a leaf node is reached.",
			"When a leaf is reached, the value nodes between root and leaf contain the list of strings defining an order the server will understand.",
		},
		[]string{
			"Province",
			"`Province` decisions represent picking a province from the game map.",
			"The children of the root of the options tree indicate that the user needs to select which province to define an order for.",
		},
		[]string{
			"OrderType",
			"`OrderType` decisions represent picking a type of order for a province.",
		},
		[]string{
			"UnitType",
			"`UnitType` decisions represent picking a type of unit for an order.",
		},
		[]string{
			"SrcProvince",
			"`SrcProvince` is unique for `Hold` orders, and indicates that the value should be prepended to the order string list without presenting the player with a choice - i.e. a `Hold` order always only affects the source province of the order.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListOptionsRoute,
		RouteParams: []string{"game_id", gameID.Encode(), "phase_ordinal", fmt.Sprint(phaseOrdinal)},
	})))

	return nil
}

func (p *Phase) Orders(ctx context.Context) (map[dip.Nation]map[dip.Province][]string, error) {
	phaseID, err := PhaseID(ctx, p.GameID, p.PhaseOrdinal)
	if err != nil {
		return nil, err
	}

	orders := []Order{}
	if _, err := datastore.NewQuery(orderKind).Ancestor(phaseID).GetAll(ctx, &orders); err != nil {
		return nil, err
	}

	orderMap := map[dip.Nation]map[dip.Province][]string{}
	for _, order := range orders {
		nationMap, found := orderMap[order.Nation]
		if !found {
			nationMap = map[dip.Province][]string{}
			orderMap[order.Nation] = nationMap
		}
		nationMap[dip.Province(order.Parts[0])] = order.Parts[1:]
	}

	return orderMap, nil
}

func (p *Phase) State(ctx context.Context, variant variants.Variant, orderMap map[dip.Nation]map[dip.Province][]string) (*state.State, error) {
	parsedOrders, err := variant.ParseOrders(orderMap)
	if err != nil {
		return nil, err
	}

	units := map[dip.Province]dip.Unit{}
	for _, unit := range p.Units {
		units[unit.Province] = unit.Unit
	}

	supplyCenters := map[dip.Province]dip.Nation{}
	for _, sc := range p.SCs {
		supplyCenters[sc.Province] = sc.Owner
	}

	dislodgeds := map[dip.Province]dip.Unit{}
	for _, dislodged := range p.Dislodgeds {
		dislodgeds[dislodged.Province] = dislodged.Dislodged
	}

	dislodgers := map[dip.Province]dip.Province{}
	for _, dislodger := range p.Dislodgers {
		dislodgers[dislodger.Province] = dislodger.Dislodger
	}

	bounces := map[dip.Province]map[dip.Province]bool{}
	for _, bounce := range p.Bounces {
		bounceMap := map[dip.Province]bool{}
		for _, prov := range strings.Split(bounce.BounceList, ",") {
			bounceMap[dip.Province(prov)] = true
		}
		bounces[bounce.Province] = bounceMap
	}

	return variant.Blank(variant.Phase(p.Year, p.Season, p.Type)).Load(units, supplyCenters, dislodgeds, dislodgers, bounces, parsedOrders), nil
}

func listPhases(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	game := &Game{}
	if err := datastore.Get(ctx, gameID, game); err != nil {
		return err
	}
	member, isMember := game.GetMember(user.Id)
	if isMember {
		r.Values()[memberNationFlag] = member.Nation
	}

	phases := Phases{}
	_, err = datastore.NewQuery(phaseKind).Ancestor(gameID).GetAll(ctx, &phases)
	if err != nil {
		return err
	}

	w.SetContent(phases.Item(r, gameID))
	return nil
}
