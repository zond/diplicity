package game

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/go-fcm"
	"github.com/zond/godip/state"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

var (
	timeoutResolvePhaseFunc *DelayFunc
)

func init() {
	timeoutResolvePhaseFunc = NewDelayFunc("game-timeoutResolvePhase", timeoutResolvePhase)
}

func timeoutResolvePhase(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) error {
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
		game.ID = gameID

		phaseStates := PhaseStates{}

		if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &phaseStates); err != nil {
			log.Errorf(ctx, "Unable to query phase states for %v/%v: %v; hope datastore will get fixed", gameID, phaseID, err)
			return err
		}

		return (&PhaseResolver{
			Context:       ctx,
			Game:          game,
			Phase:         phase,
			PhaseStates:   phaseStates,
			TaskTriggered: true,
		}).Act()
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		log.Errorf(ctx, "Unable to commit resolve tx: %v", err)
		return err
	}

	log.Infof(ctx, "timeoutResolvePhase(..., %v, %v): *** SUCCESS ***", gameID, phaseOrdinal)

	return nil
}

type PhaseResolver struct {
	Context       context.Context
	Game          *Game
	Phase         *Phase
	PhaseStates   PhaseStates
	TaskTriggered bool
}

func (p *PhaseResolver) SCCounts(s *state.State) map[dip.Nation]int {
	res := map[dip.Nation]int{}
	for _, nat := range s.SupplyCenters() {
		if nat != "" {
			res[nat] = res[nat] + 1
		}
	}
	return res
}

type nationState int

const (
	unknownState nationState = iota
	diasState
	eliminatedState
)

func (p *PhaseResolver) Act() error {
	log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act()", p.Phase.GameID, p.Phase.PhaseOrdinal)

	// Sanity check time and resolution status of the phase.

	if p.TaskTriggered && p.Phase.DeadlineAt.After(time.Now()) {
		log.Infof(p.Context, "Resolution postponed to %v by %v; rescheduling task", p.Phase.DeadlineAt, PP(p.Phase))
		return p.Phase.ScheduleResolution(p.Context)
	}

	if p.Phase.Resolved {
		log.Infof(p.Context, "Already resolved; %v; skipping resolution", PP(p.Phase))
		return nil
	}

	// Finish and save old phase.

	p.Phase.Resolved = true
	if err := p.Phase.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save old phase %v: %v; hope datastore gets fixed", PP(p.Phase), err)
		return err
	}

	// Roll forward the game state.

	log.Infof(p.Context, "PhaseStates at resolve time: %v", PP(p.PhaseStates))

	orderMap, err := p.Phase.Orders(p.Context)
	if err != nil {
		log.Errorf(p.Context, "Unable to load orders for %v: %v; fix phase.Orders or hope datastore will get fixed", PP(p.Phase), err)
		return err
	}
	log.Infof(p.Context, "Orders at resolve time: %v", PP(orderMap))

	variant := variants.Variants[p.Game.Variant]
	s, err := p.Phase.State(p.Context, variant, orderMap)
	if err != nil {
		log.Errorf(p.Context, "Unable to create godip State for %v: %v; fix godip!", PP(p.Phase), err)
		return err
	}
	if err := s.Next(); err != nil {
		log.Errorf(p.Context, "Unable to roll State forward for %v: %v; fix godip!", PP(p.Phase), err)
		return err
	}
	scCounts := p.SCCounts(s)

	// Create the new phase.

	newPhase := NewPhase(s, p.Phase.GameID, p.Phase.PhaseOrdinal+1)
	newPhase.DeadlineAt = time.Now().Add(time.Minute * p.Game.PhaseLengthMinutes)

	// Check if we can roll forward again, and potentially create new phase states.

	// Prepare some data to collect.
	allReady := true                            // All nations are ready to resolve the new phase as well.
	var soloNation dip.Nation                   // The nation, if any, reaching solo victory.
	quitNations := map[dip.Nation]nationState{} // One state per nation that wants to quit, with either dias or eliminated.
	newPhaseStates := PhaseStates{}             // The new phase states to save if we want to prepare resolution of a new phase.

	for _, nat := range variant.Nations {
		// Collect data on each nation.
		_, hadOrders := orderMap[nat]
		wasReady := false
		wantedDIAS := false
		wasOnProbation := false
		for _, phaseState := range p.PhaseStates {
			if phaseState.Nation == nat {
				wasReady = phaseState.ReadyToResolve
				wantedDIAS = phaseState.WantsDIAS
				if phaseState.WantsDIAS {
					quitNations[nat] = diasState
				}
				wasOnProbation = phaseState.OnProbation
				break
			}
		}
		newOptions := len(s.Phase().Options(s, nat))
		if scCounts[nat] == 0 {
			quitNations[nat] = eliminatedState
		} else if scCounts[nat] >= variant.SoloSupplyCenters {
			if soloNation != "" {
				msg := fmt.Sprintf("Found that %q has >= variant.SoloSupplyCenters (%d) SCs, but %q was already marked as solo winner? WTF?; fix godip?", nat, variant.SoloSupplyCenters, soloNation)
				log.Errorf(p.Context, msg)
				return fmt.Errorf(msg)
			}
			log.Infof(p.Context, "Found that %q has >= variant.SoloSupplyCenters (%d) SCs, marking %q as solo winner", nat, variant.SoloSupplyCenters, nat)
			soloNation = nat
		}

		// Log what we're doing.
		stateString := fmt.Sprintf("wasReady = %v, wantedDIAS = %v, onProbation = %v, hadOrders = %v, newOptions = %v", wasReady, wantedDIAS, wasOnProbation, hadOrders, newOptions)
		log.Infof(p.Context, "%v at phase change: %s", nat, stateString)

		// Calculate states for next phase.
		autoProbation := wasOnProbation || (!hadOrders && !wasReady)
		autoReady := newOptions == 0 || autoProbation
		autoDIAS := wantedDIAS || autoProbation
		allReady = allReady && autoReady

		// If the next phase state is non-default, we must save and append it.
		if autoReady || autoDIAS {
			newPhaseState := &PhaseState{
				GameID:         p.Phase.GameID,
				PhaseOrdinal:   newPhase.PhaseOrdinal,
				Nation:         nat,
				ReadyToResolve: autoReady,
				WantsDIAS:      autoDIAS,
				OnProbation:    autoProbation,
				Note:           fmt.Sprintf("Auto generated due to phase change at %v/%v: %s", p.Phase.GameID, p.Phase.PhaseOrdinal, stateString),
			}
			newPhaseStates = append(newPhaseStates, *newPhaseState)
		}
	}

	log.Infof(p.Context, "Calculated key metrics: allReady: %v, soloNation: %q, quitNations: %+v", allReady, soloNation, quitNations)

	// Check if the game should end.

	if soloNation != "" || len(quitNations) == len(variant.Nations) {
		log.Infof(p.Context, "soloNation: %q, quitNations: %+v => game needs to end", soloNation, quitNations)
		// Just to ensure we don't try to resolve it again, even by mistake.
		newPhase.Resolved = true
	}

	// Notify about the new phase.

	if err := newPhase.NotifyMembers(p.Context, p.Game); err != nil {
		log.Errorf(p.Context, "Unable to enqueue notification to game members: %v; hope datastore will get fixed", err)
		return err
	}

	// Save the new phase.

	if err := newPhase.Save(p.Context); err != nil {
		log.Errorf(p.Context, "Unable to save new Phase %v: %v; hope datastore will get fixed", PP(newPhase), err)
		return err
	}

	// Exit early if the new phase is already resolved.

	if newPhase.Resolved {
		log.Infof(p.Context, "New phase is already resolved, marking game as finished and stopping early")
		p.Game.Finished = true
		p.Game.Closed = true
		if err := p.Game.Save(p.Context); err != nil {
			log.Errorf(p.Context, "Unable to save game as finished: %v; hope datastore will get fixed", err)
			return err
		}

		log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESS ***", p.Phase.GameID, p.Phase.PhaseOrdinal)

		return nil
	}

	// Save the new phase states.

	if len(newPhaseStates) > 0 {
		ids := make([]*datastore.Key, len(newPhaseStates))
		for i := range newPhaseStates {
			id, err := newPhaseStates[i].ID(p.Context)
			if err != nil {
				log.Errorf(p.Context, "Unable to create new phase state ID for %v: %v; fix PhaseState.ID or hope datastore gets fixed", PP(newPhaseStates[i]), err)
				return err
			}
			ids[i] = id
		}
		if _, err := datastore.PutMulti(p.Context, ids, newPhaseStates); err != nil {
			log.Errorf(p.Context, "Unable to save new PhaseStates %v: %v; hope datastore will get fixed", PP(newPhaseStates), err)
			return err
		}
		log.Infof(p.Context, "Saved %v to get things moving", PP(newPhaseStates))
	}

	// If we can't roll forward again, schedule new resolution.

	if !allReady {
		if p.Game.PhaseLengthMinutes > 0 {
			if err := newPhase.ScheduleResolution(p.Context); err != nil {
				log.Errorf(p.Context, "Unable to schedule resolution for %v: %v; fix ScheduleResolution or hope datastore gets fixed", PP(newPhase), err)
				return err
			}
			log.Infof(p.Context, "%v has phase length of %v minutes, scheduled new resolve", PP(p.Game), p.Game.PhaseLengthMinutes)
		} else {
			log.Infof(p.Context, "%v has a zero phase length, skipping resolve scheduling", PP(p.Game))
		}

		log.Infof(p.Context, "PhaseResolver{GameID: %v, PhaseOrdinal: %v}.Act() *** SUCCESS ***", p.Phase.GameID, p.Phase.PhaseOrdinal)

		return nil
	}

	// Roll forward again.

	log.Infof(p.Context, "Since all players are ready to resolve RIGHT NOW, rolling forward again")

	newPhase.DeadlineAt = time.Now()
	p.Phase = newPhase
	p.PhaseStates = newPhaseStates
	if err := p.Act(); err != nil {
		log.Errorf(p.Context, "Unable to continue rolling forward: %v; fix the resolver!", err)
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

func devResolvePhaseTimeout(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		return fmt.Errorf("only accessible in local dev mode")
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

	phase := &Phase{}
	if err := datastore.Get(ctx, phaseID, phase); err != nil {
		return err
	}

	phase.DeadlineAt = time.Now()
	if _, err := datastore.Put(ctx, phaseID, phase); err != nil {
		return err
	}

	return timeoutResolvePhase(ctx, gameID, phaseOrdinal)
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
	return timeoutResolvePhaseFunc.EnqueueAt(ctx, p.DeadlineAt, p.GameID, p.PhaseOrdinal)
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

func (p *Phase) NotifyMembers(ctx context.Context, game *Game) error {
	memberIds := make([]string, len(game.Members))
	for i, member := range game.Members {
		memberIds[i] = member.User.Id
	}
	data, err := NewFCMData(map[string]interface{}{
		"diplicityPhase": p,
		"diplicityGame":  game,
	})
	if err != nil {
		return err
	}
	return FCMSendToUsersFunc.EnqueueIn(
		ctx,
		0,
		&fcm.NotificationPayload{
			Title:       fmt.Sprintf("%s %d, %s", p.Season, p.Year, p.Type),
			Body:        fmt.Sprintf("%s has a new phase.", game.Desc),
			Tag:         "diplicity-engine-new-phase",
			ClickAction: fmt.Sprintf("https://diplicity-engine.appspot.com/Game/%s/Phase/%d", game.ID.Encode(), p.PhaseOrdinal),
		},
		data,
		memberIds,
	)
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
