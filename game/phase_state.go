package game

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
	"github.com/zond/godip/variants"
)

const (
	phaseStateKind = "PhaseState"
)

var PhaseStateResource = &Resource{
	Update:   updatePhaseState,
	FullPath: "/Game/{game_id}/Phase/{phase_ordinal}/State/{nation}",
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
	}))
	return phaseStatesItem
}

type PhaseState struct {
	GameID         *datastore.Key
	PhaseOrdinal   int64
	Nation         dip.Nation
	ReadyToResolve bool `methods:"PUT"`
	WantsDIAS      bool `methods:"PUT"`
	Note           string
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

	nation := dip.Nation(r.Vars()["nation"])

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	phaseState := &PhaseState{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
			return err
		}
		game.ID = gameID
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return fmt.Errorf("can only update phase state of member games")
		}

		if member.Nation != nation {
			return fmt.Errorf("can only update own phase state")
		}

		if phase.Resolved {
			return fmt.Errorf("can only update phase states of unresolved phases")
		}

		err = Copy(phaseState, r, "PUT")
		if err != nil {
			return err
		}

		phaseState.GameID = gameID
		phaseState.PhaseOrdinal = phaseOrdinal
		phaseState.Nation = member.Nation

		if phaseState.ReadyToResolve {
			allStates := []PhaseState{}
			if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &allStates); err != nil {
				return err
			}
			readyNations := map[dip.Nation]struct{}{
				// Override the result from the query, since it will read the state from before the transaction
				// started.
				phaseState.Nation: struct{}{},
			}
			for _, otherState := range allStates {
				if otherState.ReadyToResolve {
					readyNations[otherState.Nation] = struct{}{}
				}
			}
			if len(readyNations) == len(variants.Variants[game.Variant].Nations) {
				if err := (&PhaseResolver{
					Context:       ctx,
					Game:          game,
					Phase:         phase,
					PhaseState:    phaseState,
					TaskTriggered: false,
				}).Act(); err != nil {
					return err
				}
			}
		}

		return phaseState.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return phaseState, nil
}

func listPhaseStates(w ResponseWriter, r Request) error {
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

	phaseStates := PhaseStates{}

	if phase.Resolved {
		if _, err := datastore.NewQuery(phaseStateKind).Ancestor(phaseID).GetAll(ctx, &phaseStates); err != nil {
			return err
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
