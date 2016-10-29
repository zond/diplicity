package game

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

const (
	OpenGamesRoute       = "OpenGames"
	StartedGamesRoute    = "StartedGames"
	FinishedGamesRoute   = "FinishedGames"
	MyStagingGamesRoute  = "MyStagingGames"
	MyStartedGamesRoute  = "MyStartedGames"
	MyFinishedGamesRoute = "MyFinishedGames"
)

const (
	gameKind   = "Game"
	memberKind = "Member"
)

var GameResource = &Resource{
	Load:   loadGame,
	Create: createGame,
}

type Games []Game

func (g Games) Item(r Request, cursor *datastore.Cursor, limit int, name string, desc []string, route string) *Item {
	gameItems := make(List, len(g))
	for index := range g {
		gameItems[index] = g[index].Item(r)
	}
	gamesItem := NewItem(gameItems).SetName(name).SetDesc([][]string{
		desc,
		[]string{
			"Cursor and limit",
			fmt.Sprintf("The list contains at most %d games.", maxLimit),
			"If there are additional matching games, a 'next' link will be available with a 'cursor' parameter.",
			"Use the 'next' link to list the next batch of matching games.",
			fmt.Sprintf("To list fewer games than %d, add an explicit 'limit' parameter.", maxLimit),
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: route,
	}))
	if cursor != nil {
		gamesItem.AddLink(r.NewLink(Link{
			Rel:   "next",
			Route: route,
			QueryParams: url.Values{
				"cursor": []string{cursor.String()},
				"limit":  []string{fmt.Sprint(limit)},
			},
		}))
	}
	return gamesItem
}

type GameData struct {
	ID             *datastore.Key
	Started        bool   // Game has started.
	Closed         bool   // Game is no longer joinable..
	Finished       bool   // Game has reached its end.
	Desc           string `methods:"POST"`
	Variant        string `methods:"POST"`
	NextDeadlineAt time.Time
	NMembers       int
	CreatedAt      time.Time
}

type Game struct {
	GameData `methods:"POST"`
	Members  []Member
}

func (g *Game) Item(r Request) *Item {
	gameItem := NewItem(g).SetName(g.Desc).AddLink(r.NewLink(GameResource.Link("self", Load, []string{"id", g.ID.Encode()})))
	if len(g.Members) < len(variants.Variants[g.Variant].Nations) {
		user, ok := r.Values()["user"].(*auth.User)
		if ok {
			already := false
			for _, member := range g.Members {
				if member.User.Id == user.Id {
					already = true
					break
				}
			}
			if !already {
				gameItem.AddLink(MemberResource.Link("join", Create, []string{"game_id", g.ID.Encode()}))
			}
		}
	}
	return gameItem
}

func (g *Game) Save(ctx context.Context) error {
	var err error
	if g.ID == nil {
		g.ID = datastore.NewKey(ctx, gameKind, "", 0, nil)
	}
	g.NMembers = len(g.Members)
	g.ID, err = datastore.Put(ctx, g.ID, g)
	return err
}

func createGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	game := &Game{}
	err := Copy(game, r, "POST")
	if err != nil {
		return nil, err
	}
	if _, found := variants.Variants[game.Variant]; !found {
		http.Error(w, "unknown variant", 400)
		return nil, nil
	}
	game.CreatedAt = time.Now()

	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := game.Save(ctx); err != nil {
			return err
		}
		member := &Member{
			User:     *user,
			GameData: game.GameData,
		}
		if err := member.Save(ctx); err != nil {
			return err
		}
		game.Members = []Member{*member}
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return game, nil
}

func loadGame(w ResponseWriter, r Request) (*Game, error) {
	id, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	ctx := appengine.NewContext(r.Req())

	game := &Game{}
	if err := datastore.Get(ctx, id, game); err != nil {
		if err == datastore.ErrNoSuchEntity {
			http.Error(w, "not found", 404)
			return nil, nil
		}
		return nil, err
	}

	game.ID = id
	return game, nil
}
