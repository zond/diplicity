package game

import (
	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	gameResultKind = "GameResult"
)

type GameResult struct {
	GameID            *datastore.Key
	SoloWinner        dip.Nation
	DIASMembers       []dip.Nation
	NMRMembers        []dip.Nation
	EliminatedMembers []dip.Nation
}

func GameResultID(ctx context.Context, gameID *datastore.Key) *datastore.Key {
	return datastore.NewKey(ctx, gameResultKind, "result", 0, gameID)
}

func (g *GameResult) ID(ctx context.Context) *datastore.Key {
	return GameResultID(ctx, g.GameID)
}

func (g *GameResult) Save(ctx context.Context) error {
	_, err := datastore.Put(ctx, g.ID(ctx), g)
	return err
}

func loadGameResult(w ResponseWriter, r Request) (*GameResult, error) {
	ctx := appengine.NewContext(r.Req())

	_, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	gameResultID := GameResultID(ctx, gameID)

	gameResult := &GameResult{}
	if err := datastore.Get(ctx, gameResultID, gameResult); err != nil {
		return nil, err
	}

	return gameResult, nil
}

var GameResultResource = &Resource{
	Load:     loadGameResult,
	FullPath: "/Game/{game_id}/GameResult",
}

func (g *GameResult) Item(r Request) *Item {
	return NewItem(g).SetName("game-result").AddLink(r.NewLink(GameResultResource.Link("self", Load, []string{"game_id", g.GameID.Encode()})))
}
