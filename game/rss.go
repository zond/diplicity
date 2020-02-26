package game

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zond/godip"

	"github.com/gorilla/feeds"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/memcache"

	. "github.com/zond/goaeoas"
)

type event struct {
	title       string
	description string
	link        string
}

func makeURL(route string, host string, urlParams ...string) (*url.URL, error) {
	phaseURL, err := router.Get(route).URL(urlParams...)
	if err != nil {
		return nil, err
	}
	phaseURL.Host = host
	phaseURL.Scheme = DefaultScheme
	return phaseURL, nil
}

// The RFC2616 date format.
const httpDateFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

// A key prefix for the date the RSS cache was last checked.
const rssCheckedMemcacheKey = "rssChecked:"

// A key prefix for the date the RSS cache was last known to be modified.
const rssModifiedMemcacheKey = "rssModified:"

// The maximum number of games to include in the feed.
const maxGames = 64

// The maximum number of items to include in the feed.
const maxItems = 256

// A fast (mostly collision resistant) hash function.
// Note that this isn't cryptographically secure.
// Returns a 32 character string.
func hashStr(inputStr string) string {
	hashArray := md5.Sum([]byte(inputStr))
	return hex.EncodeToString(hashArray[:])
}

// Write the RSS feed.
//   w The writer to use.
//   rss The body of the RSS feed.
//   etag A value to indicate the version of the feed.
//   cacheControl How long the page should be cached for.
func writeRss(w ResponseWriter, rss string, etag string, lastModified string, cacheControl string) {
	w.Header().Set("Etag", etag)
	w.Header().Set("Last-Modified", lastModified)
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", "application/rss+xml")
	w.Write([]byte(rss))
}

// If the request header includes If-Modified-Since then we should do an update if
// it was modified, or if an hour has elapsed since the db was last checked. The
// feed is generated dynamically, so it's expensive to check whether there's
// actually an update available or not.
func updateNeeded(r Request, checked string, modified string) bool {
	ifModifiedSince := r.Req().Header.Get("If-Modified-Since")
	ifDate, err := time.Parse(httpDateFormat, ifModifiedSince)
	if err != nil {
		// The If-Modified-Since in the request wasn't understood.
		return true
	}

	modifiedDate, err := time.Parse(httpDateFormat, modified)
	if err != nil {
		fmt.Printf("Modified date cache contains unparsable date string: %s", modified)
		return true
	}
	if modifiedDate.After(ifDate) {
		// There's definitely an update available
		return true
	}

	checkedDate, err := time.Parse(httpDateFormat, checked)
	if err != nil {
		fmt.Printf("Checked date cache contains unparsable date string: %s", checked)
		return true
	}
	// There might be an update available, so check for it if an hour has passed since the last check.
	diff := checkedDate.Sub(ifDate)
	return diff.Hours() > 1
}

func makeSummaryHtml(phase Phase, nationsList []godip.Nation, scCount map[godip.Nation]int,
	unitCount map[godip.Nation]int, dislodgedCount map[godip.Nation]int) string {
	summary := "<table>"
	if len(phase.Dislodgeds) > 0 {
		summary += "<th><td>SC Count</td><td>Units</td><td>Dislodged</td><td>Delta</td></th>"
	} else {
		summary += "<th><td>SC Count</td><td>Units</td><td>Delta</td></th>"
	}
	for _, nation := range nationsList {
		delta := scCount[nation] - unitCount[nation] - dislodgedCount[nation]
		summary += "<tr>"
		summary += "<td>" + nation.String() + "</td>"
		if len(phase.Dislodgeds) > 0 {
			summary += fmt.Sprintf("<td>%d</td><td>%d</td><td>%d</td><td>%+d</td>", scCount[nation], unitCount[nation], dislodgedCount[nation], delta)
		} else {
			summary += fmt.Sprintf("<td>%d</td><td>%d</td><td>%+d</td>", scCount[nation], unitCount[nation], delta)
		}
		summary += "</tr>"
	}
	summary += "</table>"
	return summary
}

func makeSummaryMarkdown(phase Phase, nationsList []godip.Nation, scCount map[godip.Nation]int,
	unitCount map[godip.Nation]int, dislodgedCount map[godip.Nation]int) string {
	summary := ""
	if len(phase.Dislodgeds) > 0 {
		summary += "| SC Count | Units | Dislodged | Delta |\n"
		summary += "| --- | --- | --- | --- |"
	} else {
		summary += "| SC Count | Units | Delta |\n"
		summary += "| --- | --- | --- |"
	}
	for _, nation := range nationsList {
		delta := scCount[nation] - unitCount[nation] - dislodgedCount[nation]
		summary += "\n| " + nation.String() + " |"
		if len(phase.Dislodgeds) > 0 {
			summary += fmt.Sprintf(" %d | %d | %d | %+d |", scCount[nation], unitCount[nation], dislodgedCount[nation], delta)
		} else {
			summary += fmt.Sprintf(" %d | %d | %+d |", scCount[nation], unitCount[nation], delta)
		}
	}
	return summary
}

func makeSummary(phase Phase, format string) string {
	// A set of all nations still in the game.
	nations := map[godip.Nation]bool{}
	// SC Count
	scCount := map[godip.Nation]int{}
	for _, sc := range phase.SCs {
		nation := sc.Owner
		scCount[nation]++
		nations[nation] = true
	}
	// Units
	unitCount := map[godip.Nation]int{}
	for _, unitWrapper := range phase.Units {
		nation := unitWrapper.Unit.Nation
		unitCount[nation]++
		nations[nation] = true
	}
	// Dislodged
	dislodgedCount := map[godip.Nation]int{}
	for _, dislodged := range phase.Dislodgeds {
		nation := dislodged.Dislodged.Nation
		dislodgedCount[nation]++
		nations[nation] = true
	}
	// Make ordered set of nations.
	var nationsList []godip.Nation
	for nation, _ := range nations {
		nationsList = append(nationsList, nation)
	}
	sort.Slice(nationsList, func(i, j int) bool {
		return nationsList[i].String() < nationsList[j].String()
	})

	if format == "markdown" {
		return makeSummaryMarkdown(phase, nationsList, scCount, unitCount, dislodgedCount)
	}
	return makeSummaryHtml(phase, nationsList, scCount, unitCount, dislodgedCount)
}

// Supported query parameters:
//   gameID: Limit the feed to a single game.
//   variant: Limit the feed to a single variant.
//   phaseType: Limit the feed to a single phase type.
//   gameLimit: The maximum number of games to return in the results.
//   phaseLimit: The maximum number of phases from each game to return.
//   format: The format of the description (e.g. "html" or "markdown").
func handleRss(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	// Use memcache to handle If-Modified-Since requests.
	urlHash := hashStr(r.Req().URL.String())
	checkedKey := rssCheckedMemcacheKey + urlHash
	modifiedKey := rssModifiedMemcacheKey + urlHash
	itemMap, err := memcache.GetMulti(ctx, []string{checkedKey, modifiedKey})
	if err != nil && err != memcache.ErrCacheMiss {
		return err
	}
	if err == nil {
		checked := itemMap[checkedKey]
		modified := itemMap[modifiedKey]
		if checked != nil && checked.Value != nil && modified != nil && modified.Value != nil {
			checkedStr := string(checked.Value[:])
			modifiedStr := string(modified.Value[:])
			if !updateNeeded(r, checkedStr, modifiedStr) {
				w.WriteHeader(http.StatusNotModified)
				return nil
			}
		}
	}

	// Process the request.
	uq := r.Req().URL.Query()

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
		limit, err := strconv.ParseInt(uq.Get("gameLimit"), 10, 64)
		if err != nil || limit > maxGames {
			limit = maxGames
			err = nil
		}

		q := datastore.NewQuery(gameKind).Filter("Started=", true).Order("-CreatedAt").Limit(int(limit))

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
		limit, err := strconv.ParseInt(uq.Get("phaseLimit"), 10, 64)
		if err != nil {
			// Default to the last four phases per game.
			limit = 4
			err = nil
		}
		if limit > int64(maxItems/len(games)) {
			limit = int64(maxItems / len(games))
		}

		phases := []Phase{}
		q := datastore.NewQuery(phaseKind).Ancestor(game.ID).Filter("Resolved=", true).Order("-ResolvedAt").Limit(int(limit))

		if phaseTypeFilter := uq.Get("phaseType"); phaseTypeFilter != "" {
			q = q.Filter("Type=", phaseTypeFilter)
		}

		if _, err := q.GetAll(ctx, &phases); err != nil {
			return err
		}
		for _, phase := range phases {
			// This will double encode the title.
			// See http://www.%E8%A9%B9%E5%A7%86%E6%96%AF.com/blog/2006/06/encoding-rss-titles
			// for more details about the complexity of RSS reader compatibility.
			title := fmt.Sprintf("%s %d %s %s (%s)", game.Desc, phase.Year, phase.Season, phase.Type, game.Variant)
			title = strings.Replace(title, "&", "&amp;", -1)
			title = strings.Replace(title, "<", "&lt;", -1)
			if err != nil {
				return err
			}
			format := uq.Get("format")
			description := makeSummary(phase, format)
			phaseURL, err := makeURL(RenderPhaseMapRoute, r.Req().Host, "game_id", game.ID.Encode(), "phase_ordinal", fmt.Sprint(phase.PhaseOrdinal))
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
	appURL, err := makeURL(IndexRoute, r.Req().Host)
	if err != nil {
		return err
	}
	lastModified := time.Now()
	if len(eventTimes) > 0 {
		lastModified = eventTimes[0]
	}
	feed := &feeds.Feed{
		Title:       "Diplicity RSS",
		Link:        &feeds.Link{Href: appURL.String()},
		Description: "Feed of phases from Diplicity games.",
		Author:      &feeds.Author{Name: "Diplicity", Email: "diplicity-talk@googlegroups.com"},
		Created:     lastModified,
	}

	feed.Items = []*feeds.Item{}
	for _, t := range eventTimes {
		for _, event := range events[t] {
			feed.Items = append(feed.Items, &feeds.Item{
				Title:       event.title,
				Link:        &feeds.Link{Href: event.link},
				Description: event.description,
				// Only the name field of the author is output.
				Author:  &feeds.Author{Name: "diplicity-talk@googlegroups.com (Diplicity)", Email: "unused@example.com"},
				Created: t,
				Id:      event.link,
			})
		}
	}

	rss, err := feed.ToRss()
	if err != nil {
		return err
	}

	cacheControl := "max-age=86400" // 1 day
	if permanentCache {
		cacheControl = "max-age=31536000" // 1 year
	}
	modifiedStr := lastModified.Format(httpDateFormat)
	writeRss(w, rss, lastModified.String(), modifiedStr, cacheControl)

	// Populate memcache.
	// Use an expiry of 1 hour, since requests after that will hit the db anyway.
	checkedStr := time.Now().Format(httpDateFormat)
	checkedItem := &memcache.Item{
		Key:        checkedKey,
		Value:      []byte(checkedStr),
		Expiration: time.Hour,
	}
	modifiedItem := &memcache.Item{
		Key:        modifiedKey,
		Value:      []byte(modifiedStr),
		Expiration: time.Hour,
	}
	items := []*memcache.Item{checkedItem, modifiedItem}
	memcache.SetMulti(ctx, items)

	return nil
}
