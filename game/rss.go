package game

import (
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

type Rss struct {
	Games []string
}

func handleRss(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	rss := Rss{}

	activeGames := Games{}
	_, err := datastore.NewQuery(gameKind).Filter("Finished=", false).Filter("Started=", true).GetAll(ctx, &activeGames)
	if err != nil {
		return err
	}

	for _, game := range activeGames {
		rss.Games = append(rss.Games, game.Desc)
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
