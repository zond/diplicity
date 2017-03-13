package game

import (
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	phaseStateKind = "PhaseState"
)

var PhaseStateResource *Resource

func init() {
	PhaseStateResource = &Resource{
		Update:   updatePhaseState,
		FullPath: "/Game/{game_id}/Phase/{phase_ordinal}/PhaseState/{nation}",
		Listers: []Lister{
			{
				Path:    "/Game/{game_id}/Phase/{phase_ordinal}/PhaseStates",
				Route:   ListPhaseStatesRoute,
				Handler: listPhaseStates,
			},
		},
	}
}

type PhaseStates []PhaseState

func (p PhaseStates) Item(r Request, phase *Phase) *Item {
	if !phase.Resolved {
		r.Values()["is-unresolved"] = true
	}
	phaseStateItems := make(List, len(p))
	for i := range p {
		phaseStateItems[i] = p[i].Item(r)
	}
	phaseStatesItem := NewItem(phaseStateItems).SetName("phase-states").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListPhaseStatesRoute,
		RouteParams: []string{"game_id", phase.GameID.Encode(), "phase_ordinal", fmt.Sprint(phase.PhaseOrdinal)},
	})).SetDesc([][]string{
		[]string{
			"Phase states",
			"Each member has exactly one phase state per phase. The phase state defines phase scoped configuration for the member, such as whether the member is ready for the phase to resolve, if the member wants a draw and if the member is currently on probation.",
		},
		[]string{
			"Ready to resolve",
			"If all members of a game are ready for the phase to resolve, the phase will resolve immediately without waiting for the deadline.",
		},
		[]string{
			"Draws",
			"If all members of a game want a draw, the game will end early. The scoring system will reflect this by distributing points to all remaining players.",
		},
		[]string{
			"Probation",
			"Members on probation will get future phase states automatically marked as 'ready to resolve' and 'wanting draw'. To return from probation, simply update the phase state of the member on probation.",
		},
	})
	return phaseStatesItem
}

type PhaseState struct {
	GameID         *datastore.Key
	PhaseOrdinal   int64
	Nation         dip.Nation
	ReadyToResolve bool `methods:"PUT"`
	WantsDIAS      bool `methods:"PUT"`
	OnProbation    bool
	NoOrders       bool
	Eliminated     bool
	Note           string `datastore:",noindex"`
}

func PhaseStateID(ctx context.Context, phaseID *datastore.Key, nation dip.Nation) (*datastore.Key, error) {
	if phaseID == nil || nation == "" {
		return nil, fmt.Errorf("phase states must have phases and nations")
	}
	return datastore.NewKey(ctx, phaseStateKind, string(nation), 0, phaseID), nil
}

func (p *PhaseState) ID(ctx context.Context) (*datastore.Key, error) {
	phaseID, err := PhaseID(ctx, p.GameID, p.PhaseOrdinal)
	if err != nil {
		return nil, err
	}
	return PhaseStateID(ctx, phaseID, p.Nation)
}

func (p *PhaseState) Save(ctx context.Context) error {
	key, err := p.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, key, p)
	return err
}

func (p *PhaseState) Item(r Request) *Item {
	phaseStateItem := NewItem(p).SetName(string(p.Nation))
	if _, isUnresolved := r.Values()["is-unresolved"]; isUnresolved {
		phaseStateItem.AddLink(r.NewLink(PhaseStateResource.Link("update", Update, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal), "nation", string(p.Nation)})))
	}
	return phaseStateItem
}

func updatePhaseState(w ResponseWriter, r Request) (*PhaseState, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	nation := dip.Nation(r.Vars()["nation"])

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := ioutil.ReadAll(r.Req().Body)
	if err != nil {
		return nil, err
	}
	phaseState := &PhaseState{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		phase := &Phase{}
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
			return err
		}
		game.ID = gameID
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return HTTPErr{"can only update phase state of member games", 404}
		}

		if member.Nation != nation {
			return HTTPErr{"can only update own phase state", 403}
		}

		if phase.Resolved {
			return HTTPErr{"can only update phase states of unresolved phases", 412}
		}

		phaseStateID, err := PhaseStateID(ctx, phaseID, member.Nation)
		if err != nil {
			return err
		}
		if err := datastore.Get(ctx, phaseStateID, phaseState); err != nil && err != datastore.ErrNoSuchEntity {
			return err
		}

		err = CopyBytes(phaseState, r, bodyBytes, "PUT")
		if err != nil {
			return err
		}
		if phaseState.NoOrders {
			phaseState.ReadyToResolve = true
		}
		phaseState.GameID = gameID
		phaseState.PhaseOrdinal = phaseOrdinal
		phaseState.Nation = member.Nation
		phaseState.OnProbation = false
		member.NewestPhaseState = *phaseState

		if err := phaseState.Save(ctx); err != nil {
			return err
		}
		if err := game.Save(ctx); err != nil {
			return err
		}

		if phaseState.ReadyToResolve {
			allStates := []PhaseState{}
			if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &allStates); err != nil {
				return err
			}

			phaseStates := map[dip.Nation]*PhaseState{}
			readyNations := map[dip.Nation]struct{}{}
			for i := range allStates {
				phaseStates[allStates[i].Nation] = &allStates[i]
				if allStates[i].ReadyToResolve {
					readyNations[allStates[i].Nation] = struct{}{}
				}
			}

			// Overwrite what we found with what we know, since the query will have fetched what was visible before
			// the transaction.
			phaseStates[phaseState.Nation] = phaseState
			readyNations[phaseState.Nation] = struct{}{}

			allStates = make([]PhaseState, 0, len(phaseStates))
			for _, phaseState := range phaseStates {
				allStates = append(allStates, *phaseState)
			}

			if len(readyNations) == len(variants.Variants[game.Variant].Nations) {
				if err := (&PhaseResolver{
					Context:       ctx,
					Game:          game,
					Phase:         phase,
					PhaseStates:   allStates,
					TaskTriggered: false,
				}).Act(); err != nil {
					return err
				}
			}
		}
		return nil
	}, &datastore.TransactionOptions{XG: true}); err != nil {
		return nil, err
	}

	return phaseState, nil
}

func listPhaseStates(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
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

	phaseStates := PhaseStates{}

	if phase.Resolved {
		if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &phaseStates); err != nil {
			return err
		}
		for _, nat := range variants.Variants[game.Variant].Nations {
			found := false
			for _, phaseState := range phaseStates {
				if phaseState.Nation == nat {
					found = true
					break
				}
			}
			if !found {
				phaseStates = append(phaseStates, PhaseState{
					GameID:       gameID,
					PhaseOrdinal: phaseOrdinal,
					Nation:       nat,
				})
			}
		}
	} else {
		member, isMember := game.GetMember(user.Id)
		if isMember {
			phaseStateID, err := PhaseStateID(ctx, phaseID, member.Nation)
			if err != nil {
				return err
			}
			phaseState := &PhaseState{}
			if err := datastore.Get(ctx, phaseStateID, phaseState); err == datastore.ErrNoSuchEntity {
				phaseState.GameID = gameID
				phaseState.PhaseOrdinal = phaseOrdinal
				phaseState.Nation = member.Nation
			} else if err != nil {
				return err
			}
			phaseStates = append(phaseStates, *phaseState)
		}
	}

	w.SetContent(phaseStates.Item(r, phase))
	return nil
}
