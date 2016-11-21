package game

import (
	"fmt"
	"strconv"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	"github.com/zond/diplicity/auth"
	. "github.com/zond/goaeoas"
)

const (
	phaseResultKind = "PhaseResult"
)

type PhaseResult struct {
	GameID       *datastore.Key
	PhaseOrdinal int64
	NMRUsers     []string
	ActiveUsers  []string
	ReadyUsers   []string
}

var PhaseResultResource = &Resource{
	Load:     loadPhaseResult,
	FullPath: "/Game/{game_id}/Phase/{phase_ordinal}/Result",
}

func loadPhaseResult(w ResponseWriter, r Request) (*PhaseResult, error) {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
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

	phaseResultID, err := PhaseResultID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	phaseResult := &PhaseResult{}
	if err := datastore.Get(ctx, phaseResultID, phaseResult); err != nil {
		return nil, err
	}

	return phaseResult, nil
}

func PhaseResultID(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64) (*datastore.Key, error) {
	if gameID == nil || phaseOrdinal < 0 {
		return nil, fmt.Errorf("phase results must have games and ordinals > 0")
	}
	return datastore.NewKey(ctx, phaseResultKind, "", phaseOrdinal, gameID), nil
}

func (p *PhaseResult) ID(ctx context.Context) (*datastore.Key, error) {
	return PhaseResultID(ctx, p.GameID, p.PhaseOrdinal)
}

func (p *PhaseResult) Item(r Request) *Item {
	return NewItem(p).SetName("phase-result")
}

func (p *PhaseResult) Save(ctx context.Context) error {
	id, err := p.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, id, p)
	return err
}
