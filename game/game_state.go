package game

import (
	"fmt"
	"net/http"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	gameStateKind = "GameState"
)

var GameStateResource = &Resource{
	Update:   updateGameState,
	FullPath: "/Game/{game_id}/GameState/{nation}",
}

type GameStates []GameState

func (g GameStates) Item(r Request, gameID *datastore.Key) *Item {
	gameStateItems := make(List, len(g))
	for i := range g {
		gameStateItems[i] = g[i].Item(r)
	}
	gameStatesItem := NewItem(gameStateItems).SetName("phase-states").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListGameStatesRoute,
		RouteParams: []string{"game_id", gameID.Encode()},
	}))
	return gameStatesItem
}

type GameState struct {
	GameID *datastore.Key
	Nation dip.Nation
	Muted  []dip.Nation `methods:"PUT"`
}

func GameStateID(ctx context.Context, gameID *datastore.Key, nation dip.Nation) (*datastore.Key, error) {
	if gameID == nil || nation == "" {
		return nil, fmt.Errorf("game states must have games and nations")
	}
	return datastore.NewKey(ctx, gameStateKind, string(nation), 0, gameID), nil
}

func (g *GameState) ID(ctx context.Context) (*datastore.Key, error) {
	return GameStateID(ctx, g.GameID, g.Nation)
}

func (g *GameState) Save(ctx context.Context) error {
	key, err := g.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, key, g)
	return err
}

func (p *GameState) Item(r Request) *Item {
	gameStateItem := NewItem(p).SetName(string(p.Nation))
	memberNation, isMember := r.Values()[memberNationFlag]
	if isMember && memberNation == p.Nation {
		gameStateItem.AddLink(r.NewLink(GameStateResource.Link("update", Update, []string{"game_id", p.GameID.Encode(), "nation", fmt.Sprint(memberNation)})))
	}
	return gameStateItem
}

func updateGameState(w ResponseWriter, r Request) (*GameState, error) {
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

	nation := dip.Nation(r.Vars()["nation"])

	game := &Game{}
	gameState := &GameState{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, gameID, game); err != nil {
			return err
		}
		game.ID = gameID
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return fmt.Errorf("can only update phase state of member games")
		}

		if member.Nation != nation {
			return fmt.Errorf("can only update own game state")
		}

		err = Copy(gameState, r, "PUT")
		if err != nil {
			return err
		}

		gameState.GameID = gameID
		gameState.Nation = member.Nation

		return gameState.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return gameState, nil
}

func listGameStates(w ResponseWriter, r Request) error {
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
	if err = datastore.Get(ctx, gameID, game); err != nil {
		return err
	}

	member, isMember := game.GetMember(user.Id)
	if isMember {
		r.Values()[memberNationFlag] = member.Nation
	}

	gameStates := GameStates{}

	if _, err := datastore.NewQuery(gameStateKind).Ancestor(gameID).GetAll(ctx, &gameStates); err != nil {
		return err
	}
	for _, nat := range variants.Variants[game.Variant].Nations {
		found := false
		for _, gameState := range gameStates {
			if gameState.Nation == nat {
				found = true
				break
			}
		}
		if !found {
			gameStates = append(gameStates, GameState{
				GameID: gameID,
				Nation: nat,
			})
		}
	}

	w.SetContent(gameStates.Item(r, gameID))
	return nil
}
