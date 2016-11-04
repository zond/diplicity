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
)

const (
	phaseStateKind = "PhaseState"
)

var PhaseStateResource = &Resource{
	Update:   updatePhaseState,
	Load:     loadPhaseState,
	FullPath: "/Game/{game_id}/Phase/{phase_ordinal}/State/{nation}",
}

type PhaseState struct {
	GameID         *datastore.Key
	PhaseOrdinal   int64
	Nation         dip.Nation
	ReadyToResolve bool `methods:"PUT"`
	WantsDIAS      bool `methods:"PUT"`
}

func PhaseStateID(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64, nation dip.Nation) (*datastore.Key, error) {
	if gameID == nil || phaseOrdinal < 0 || nation == "" {
		return nil, fmt.Errorf("phase states must have games, ordinals > 0 and nations")
	}
	if gameID.IntID() == 0 {
		return nil, fmt.Errorf("gameIDs must have int IDs")
	}
	return datastore.NewKey(ctx, phaseStateKind, fmt.Sprintf("%d:%d:%s", gameID.IntID(), phaseOrdinal, nation), 0, nil), nil
}

func (p *PhaseState) ID(ctx context.Context) (*datastore.Key, error) {
	return PhaseStateID(ctx, p.GameID, p.PhaseOrdinal, p.Nation)
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
	phaseStateItem.AddLink(r.NewLink(PhaseStateResource.Link("update", Update, []string{"game_id", p.GameID.Encode(), "phase_ordinal", fmt.Sprint(p.PhaseOrdinal), "nation", string(p.Nation)})))
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

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	member := &Member{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, memberID}, []interface{}{game, phase, member}); err != nil {
		return nil, err
	}

	if member.Nation != nation {
		return nil, fmt.Errorf("can only update own phase state")
	}

	if phase.Resolved {
		return nil, fmt.Errorf("can only update phase states of unresolved phases")
	}

	phaseState := &PhaseState{}
	err = Copy(phaseState, r, "PUT")
	if err != nil {
		return nil, err
	}

	phaseState.GameID = gameID
	phaseState.PhaseOrdinal = phaseOrdinal
	phaseState.Nation = member.Nation

	if err := phaseState.Save(ctx); err != nil {
		return nil, err
	}

	return phaseState, nil
}

func loadPhaseState(w ResponseWriter, r Request) (*PhaseState, error) {
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

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	phaseStateID, err := PhaseStateID(ctx, gameID, phaseOrdinal, nation)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	member := &Member{}
	phaseState := &PhaseState{}
	err = datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, memberID, phaseStateID}, []interface{}{game, phase, member, phaseState})
	if err != nil {
		if merr, ok := err.(appengine.MultiError); ok {
			if merr[0] != nil || merr[1] != nil || merr[2] != nil {
				return nil, merr
			} else if merr[3] != datastore.ErrNoSuchEntity {
				return nil, merr[3]
			} else {
				phaseState.Nation = nation
				phaseState.GameID = gameID
				phaseState.PhaseOrdinal = phaseOrdinal
			}
		} else {
			return nil, err
		}
	}

	if !phase.Resolved && nation != member.Nation {
		return nil, fmt.Errorf("can only load own phase states before phase resolution")
	}

	return phaseState, nil
}
