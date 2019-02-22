package game

import (
	"fmt"
	"strconv"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

type Rss struct {
	Events []string
}

func handleRss(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())
	uq := r.Req().URL.Query()

	limit, err := strconv.ParseInt(uq.Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}

	q := datastore.NewQuery(gameKind).Filter("Started=", true)

	if variantFilter := uq.Get("variant"); variantFilter != "" {
		q = q.Filter("Variant=", variantFilter)
	}

	games := Games{}
	gameIDs, err := q.GetAll(ctx, &games)
	if err != nil {
		return err
	}
	for idx, id := range gameIDs {
		games[idx].ID = id
	}

	rss := Rss{}

	for _, game := range games {
		phases := []Phase{}
		if _, err := datastore.NewQuery(phaseKind).Ancestor(game.ID).GetAll(ctx, &phases); err != nil {
			return err
		}
		for _, phase := range phases {
			rss.Events = append(rss.Events, fmt.Sprintf("%s %s %d %s %s", phase.CreatedAt, game.Desc, phase.Year, phase.Season, phase.Type))
		}
	}

	w.SetContent(NewItem(rss).SetName("rss").SetDesc([][]string{
		[]string{
			"RSS Feeds",
			"Diplicity RSS feeds.",
		},
	}))

	return nil
}

func showGameRss(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	game := &Game{}
	err = datastore.Get(
		ctx,
		gameID,
		game,
	)

	// TODO Populate an RSS template here (and add some headers).
	w.Write([]byte("<b>" + game.Desc + "</b>"))

	return nil
}
