package game

import (
	"fmt"

	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
)

const (
	phaseResultKind = "PhaseResult"
)

type PhaseResult struct {
	GameID       *datastore.Key
	PhaseOrdinal int64
	NMRUsers     []string
	NonNMRUsers  []string
}

func (p *PhaseResult) ID(ctx context.Context) (*datastore.Key, error) {
	if p.GameID == nil || p.PhaseOrdinal < 0 {
		return nil, fmt.Errorf("phase results must have games and ordinals > 0")
	}
	return datastore.NewKey(ctx, phaseResultKind, "", p.PhaseOrdinal, p.GameID), nil
}

func (p *PhaseResult) Save(ctx context.Context) error {
	id, err := p.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, id, p)
	return err
}
