package game

import (
	"fmt"
	"math/rand"
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
	gameKind = "Game"
)

var GameResource = &Resource{
	Load:   loadGame,
	Create: createGame,
}

type Games []Game

func (g Games) Item(r Request, user *auth.User, cursor *datastore.Cursor, limit int, name string, desc []string, route string) *Item {
	gameItems := make(List, len(g))
	for i := range g {
		if !g[i].HasMember(user.Id) {
			g[i].Redact()
		}
		gameItems[i] = g[i].Item(r)
	}
	gamesItem := NewItem(gameItems).SetName(name).SetDesc([][]string{
		desc,
		[]string{
			"Cursor and limit",
			fmt.Sprintf("The list contains at most %d games.", maxLimit),
			"If there are additional matching games, a 'next' link will be available with a 'cursor' query parameter.",
			"Use the 'next' link to list the next batch of matching games.",
			fmt.Sprintf("To list fewer games than %d, add an explicit 'limit' query parameter.", maxLimit),
		},
		[]string{
			"Filters",
			"To show only games with a given variant, add a `variant` query parameter.",
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

func (g *Game) HasMember(userID string) bool {
	for _, member := range g.Members {
		if member.User.Id == userID {
			return true
		}
	}
	return false
}

func (g GameData) Leavable() bool {
	return !g.Started
}

func (g GameData) Joinable() bool {
	return !g.Closed && g.NMembers < len(variants.Variants[g.Variant].Nations)
}

func (g *Game) Item(r Request) *Item {
	gameItem := NewItem(g).SetName(g.Desc).AddLink(r.NewLink(GameResource.Link("self", Load, []string{"id", g.ID.Encode()})))
	user, ok := r.Values()["user"].(*auth.User)
	if ok {
		if g.HasMember(user.Id) {
			if g.Leavable() {
				gameItem.AddLink(r.NewLink(MemberResource.Link("leave", Delete, []string{"game_id", g.ID.Encode(), "user_id", user.Id})))
			}
		} else {
			if g.Joinable() {
				gameItem.AddLink(r.NewLink(MemberResource.Link("join", Create, []string{"game_id", g.ID.Encode()})))
			}
		}
	}
	if g.Started {
		gameItem.AddLink(r.NewLink(Link{
			Rel:         "phases",
			Route:       ListPhasesRoute,
			RouteParams: []string{"game_id", g.ID.Encode()},
		}))
	}
	return gameItem
}

func (g *Game) Save(ctx context.Context) error {
	g.NMembers = len(g.Members)

	var err error
	if g.ID == nil {
		g.ID, err = datastore.Put(ctx, datastore.NewIncompleteKey(ctx, gameKind, nil), g)
	} else {
		_, err = datastore.Put(ctx, g.ID, g)
	}
	if err != nil {
		return err
	}
	memberIDs := make([]*datastore.Key, len(g.Members))
	for index, member := range g.Members {
		g.Members[index].GameData = g.GameData
		memberIDs[index], err = member.ID(ctx)
		if err != nil {
			return err
		}
	}
	_, err = datastore.PutMulti(ctx, memberIDs, g.Members)
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
		member := Member{
			User:     *user,
			GameData: game.GameData,
		}
		game.Members = []Member{member}
		return game.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return game, nil
}

func (g *Game) Redact() {
	for index := range g.Members {
		g.Members[index].Redact()
	}
}

func (g *Game) Start(ctx context.Context) error {
	variant := variants.Variants[g.Variant]
	s, err := variant.Start()
	if err != nil {
		return err
	}
	phase := NewPhase(s, g.ID, 1)

	g.Started = true
	g.Closed = true

	for memberIndex, nationIndex := range rand.Perm(len(variants.Variants[g.Variant].Nations)) {
		g.Members[memberIndex].Nation = variants.Variants[g.Variant].Nations[nationIndex]
	}

	return phase.Save(ctx)
}

func loadGame(w ResponseWriter, r Request) (*Game, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	id, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	game := &Game{}
	if err := datastore.Get(ctx, id, game); err != nil {
		return nil, err
	}

	if !game.HasMember(user.Id) {
		game.Redact()
	}

	game.ID = id
	return game, nil
}
