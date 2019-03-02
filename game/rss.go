package game

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/feeds"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

type event struct {
	title       string
	description string
	link        string
}

func makeURL(route string, urlParams ...string) (*url.URL, error) {
	phaseURL, err := router.Get(route).URL(urlParams...)
	if err != nil {
		return nil, err
	}
	phaseURL.Host = "diplicity-engine.appspot.com"
	phaseURL.Scheme = "https"
	return phaseURL, nil
}

// The RFC2616 date format.
var httpDateFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

// Supported query parameters:
//   gameID: Limit the feed to a single game.
//   variant: Limit the feed to a single variant.
//   phaseType: Limit the feed to a single phase type.
func handleRss(w ResponseWriter, r Request) error {
	// If the request header includes If-Modified-Since then just check if an hour
	// has elapsed. The feed is generated dynamically, so it's expensive to check
	// whether there's actually an update available or not.
	ifModifiedSince := r.Req().Header.Get("If-Modified-Since")
	ifDate, err := time.Parse(httpDateFormat, ifModifiedSince)
	if err == nil {
		now := time.Now()
		diff := now.Sub(ifDate)
		if diff.Hours() < 1 {
			w.WriteHeader(http.StatusNotModified)
			return nil
		}
	} else {
		// Ignore as the header probably wasn't present (or wasn't understood).
		err = nil
	}

	// Process the request.
	ctx := appengine.NewContext(r.Req())
	uq := r.Req().URL.Query()

	limit, err := strconv.ParseInt(uq.Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}

	// If the RSS feed will never change then cache it for a long time.
	permanentCache := false

	games := Games{}
	// Treat the case where a game ID has been specified differently.
	if gameIDFilter := uq.Get("gameID"); gameIDFilter != "" {
		gameID, err := datastore.DecodeKey(gameIDFilter)
		if err != nil {
			return err
		}
		game := Game{}
		err = datastore.Get(ctx, gameID, &game)
		game.ID = gameID
		if game.Finished {
			permanentCache = true
		}
		games = append(games, game)
	} else {
		q := datastore.NewQuery(gameKind).Filter("Started=", true)

		if variantFilter := uq.Get("variant"); variantFilter != "" {
			q = q.Filter("Variant=", variantFilter)
		}

		gameIDs, err := q.GetAll(ctx, &games)
		if err != nil {
			return err
		}
		for idx, id := range gameIDs {
			games[idx].ID = id
		}
	}

	// Map to a list of strings, just in case it's possible that two events have the same timestamp.
	events := map[time.Time][]event{}
	eventTimes := []time.Time{}
	for _, game := range games {
		phases := []Phase{}
		q := datastore.NewQuery(phaseKind).Ancestor(game.ID)

		if phaseTypeFilter := uq.Get("phaseType"); phaseTypeFilter != "" {
			q = q.Filter("Type=", phaseTypeFilter)
		}

		if _, err := q.GetAll(ctx, &phases); err != nil {
			return err
		}
		for _, phase := range phases {
			title := fmt.Sprintf("%s %d %s %s (%s)", game.Desc, phase.Year, phase.Season, phase.Type, game.Variant)
			description := "Map should go here"
			phaseURL, err := makeURL(RenderPhaseMapRoute, "game_id", game.ID.Encode(), "phase_ordinal", fmt.Sprint(phase.PhaseOrdinal))
			if err != nil {
				return err
			}
			link := fmt.Sprint(phaseURL)
			event := event{title, description, link}
			if _, ok := events[phase.CreatedAt]; !ok {
				eventTimes = append(eventTimes, phase.CreatedAt)
			}
			events[phase.CreatedAt] = append(events[phase.CreatedAt], event)
		}
	}

	sort.Slice(eventTimes, func(i, j int) bool { return eventTimes[i].After(eventTimes[j]) })

	// Convert this into an RSS feed.
	author := feeds.Author{Name: "Diplicity", Email: "diplicity-talk@googlegroups.com"}
	appURL, err := makeURL(IndexRoute)
	if err != nil {
		return err
	}
	feed := &feeds.Feed{
		Title:       "Diplicity RSS",
		Link:        &feeds.Link{Href: appURL.String()},
		Description: "Feed of phases from Diplicity games.",
		Author:      &author,
		Created:     eventTimes[0],
	}

	feed.Items = []*feeds.Item{}
	for _, t := range eventTimes {
		for _, event := range events[t] {
			feed.Items = append(feed.Items, &feeds.Item{
				Title:       event.title,
				Link:        &feeds.Link{Href: event.link},
				Description: event.description,
				Author:      &author,
				Created:     t,
			})
		}
	}

	rss, err := feed.ToRss()
	if err != nil {
		return err
	}

	// Cache settings.
	w.Header().Set("Etag", eventTimes[0].String())
	w.Header().Set("Last-Modified", eventTimes[0].Format(httpDateFormat))
	cacheControl := "max-age=86400" // 1 day
	if permanentCache {
		cacheControl = "max-age=31536000" // 1 year
	}
	w.Header().Set("Cache-Control", cacheControl)

	w.Write([]byte(rss))

	return nil
}
