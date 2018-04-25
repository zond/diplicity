package game

import (
	"fmt"
	"time"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

type Histogram struct {
	Data        map[string]int
	Description string
}

type GlobalStats struct {
	ActiveGameHistograms                map[string]Histogram
	ActiveGameMemberUserStatsHistograms map[string]Histogram
	ActiveMemberUserStatsHistograms     map[string]Histogram
}

func bumpNamedHistogram(name string, value interface{}, m map[string]Histogram) {
	m[name].Data[fmt.Sprint(value)] += 1
}

func bumpUserStatsHistograms(userStats UserStats, m map[string]Histogram) {
	bumpNamedHistogram("StartedGames", userStats.StartedGames, m)
	bumpNamedHistogram("FinishedGames", userStats.FinishedGames, m)
	bumpNamedHistogram("SoloGames", userStats.SoloGames, m)
	bumpNamedHistogram("DIASGames", userStats.DIASGames, m)
	bumpNamedHistogram("EliminatedGames", userStats.EliminatedGames, m)
	bumpNamedHistogram("DroppedGames", userStats.DroppedGames, m)
	bumpNamedHistogram("NMRPhases", userStats.NMRPhases, m)
	bumpNamedHistogram("ActivePhases", userStats.ActivePhases, m)
	bumpNamedHistogram("ReadyPhases", userStats.ReadyPhases, m)
	bumpNamedHistogram("Reliability", int(userStats.Reliability), m)
	bumpNamedHistogram("Quickness", int(userStats.Quickness), m)
	bumpNamedHistogram("OwnedBans", userStats.OwnedBans, m)
	bumpNamedHistogram("SharedBans", userStats.SharedBans, m)
	bumpNamedHistogram("Hated", int(userStats.Hated), m)
	bumpNamedHistogram("Hater", int(userStats.Hater), m)
	bumpNamedHistogram("Rating", int(userStats.Glicko.PracticalRating), m)
}

func newHist(desc string) Histogram {
	return Histogram{
		Data:        map[string]int{},
		Description: desc,
	}
}

func newUserStatsHistograms(userDesc string) map[string]Histogram {
	return map[string]Histogram{
		"StartedGames":    newHist(fmt.Sprintf("Number of started games %s are members of", userDesc)),
		"FinishedGames":   newHist(fmt.Sprintf("Number of finished games %s are members of", userDesc)),
		"SoloGames":       newHist(fmt.Sprintf("Number of solo victories won by %s", userDesc)),
		"DIASGames":       newHist(fmt.Sprintf("Number of shared draws by %s", userDesc)),
		"EliminatedGames": newHist(fmt.Sprintf("Number of games %s have been eliminated from", userDesc)),
		"DroppedGames":    newHist(fmt.Sprintf("Number of games %s have been inactive at the end of", userDesc)),
		"NMRPhases":       newHist(fmt.Sprintf("Number of phases (in all games) %s have been inactive", userDesc)),
		"ActivePhases":    newHist(fmt.Sprintf("Number of phases (in all games) %s have issued orders (but not marked RDY)", userDesc)),
		"ReadyPhases":     newHist(fmt.Sprintf("Number of phases (in all games) %s have marked RDY", userDesc)),
		"Reliability":     newHist(fmt.Sprintf("Reliability [(ReadyPhases + ActivePhases) / (NMRPhases + 1)] attribute of %s", userDesc)),
		"Quickness":       newHist(fmt.Sprintf("Quickness [ReadyPhases / (NMRPhases + ActivePhases + 1)] attribute of %s", userDesc)),
		"OwnedBans":       newHist(fmt.Sprintf("Number of bans created by %s", userDesc)),
		"SharedBans":      newHist(fmt.Sprintf("Number of bans involving (created by or just mentioning) %s", userDesc)),
		"Hater":           newHist(fmt.Sprintf("Hater [OwnedBans / (StartedGames + 1)] attribute of %s", userDesc)),
		"Hated":           newHist(fmt.Sprintf("Hated [(SharedBans - OwnedBans) / (StartedGames + 1)] attribute of %s", userDesc)),
		"Rating":          newHist(fmt.Sprintf("Rating [an ELO variant called Glicko] of %s", userDesc)),
	}
}

func handleGlobalStats(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	globalStats := GlobalStats{
		ActiveGameHistograms: map[string]Histogram{
			"PhaseLengthMinutes": newHist("Phase length of active games, in minutes"),
			"MaxHated":           newHist("Max hated attribute for players to have joined active games"),
			"MaxHater":           newHist("Max hater attribute for players to have joined active games"),
			"MinRating":          newHist("Min rating attribute for players to have joined active games"),
			"MaxRating":          newHist("Max rating attribute for players to have joined active games"),
			"MinReliability":     newHist("Min reliability attribute for players to have joined active games"),
			"MinQuickness":       newHist("Min quickness attribute for players to have joined active games"),
			"NMembers":           newHist("Number of players in active games"),
			"Variant":            newHist("Variant of active games"),
			"CreatedAtDaysAgo":   newHist("Days ago active games were created"),
			"StartedAtDaysAgo":   newHist("Days ago active games were started"),
			"Private":            newHist("Distribution of private vs public games"),
			"ConferenceChat":     newHist("Distribution of games with vs without conference chat"),
			"GroupChat":          newHist("Distribution of games with vs without group chat"),
			"PrivateChat":        newHist("Distribution of games with vs without private chat"),
			"NationAllocation":   newHist("Distribution of games with different methods of nation allocation"),
		},
		ActiveGameMemberUserStatsHistograms: newUserStatsHistograms("members of active games"),
		ActiveMemberUserStatsHistograms:     newUserStatsHistograms("active members of active games"),
	}

	activeGames := Games{}
	_, err := datastore.NewQuery(gameKind).Filter("Finished=", false).Filter("Started=", true).GetAll(ctx, &activeGames)
	if err != nil {
		return err
	}

	activeGameMemberUserIds := map[string]struct{}{}
	activeMemberUserIds := map[string]struct{}{}

	for _, game := range activeGames {
		bumpNamedHistogram("PhaseLengthMinutes", int(game.PhaseLengthMinutes), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("MaxHated", int(game.MaxHated), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("MaxHater", int(game.MaxHater), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("MinRating", int(game.MinRating), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("MaxRating", int(game.MaxRating), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("MinReliability", int(game.MinReliability), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("MinQuickness", int(game.MinQuickness), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("NMembers", game.NMembers, globalStats.ActiveGameHistograms)
		bumpNamedHistogram("Variant", game.Variant, globalStats.ActiveGameHistograms)
		bumpNamedHistogram("CreatedAtDaysAgo", int(time.Now().Sub(game.CreatedAt)/(time.Hour*24)), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("StartedAtDaysAgo", int(time.Now().Sub(game.StartedAt)/(time.Hour*24)), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("Private", fmt.Sprint(game.Private), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("ConferenceChat", fmt.Sprint(!game.DisableConferenceChat), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("GroupChat", fmt.Sprint(!game.DisableGroupChat), globalStats.ActiveGameHistograms)
		bumpNamedHistogram("PrivateChat", fmt.Sprint(!game.DisablePrivateChat), globalStats.ActiveGameHistograms)
		for _, member := range game.Members {
			activeGameMemberUserIds[member.User.Id] = struct{}{}
			if !member.NewestPhaseState.OnProbation && !member.NewestPhaseState.Eliminated {
				activeMemberUserIds[member.User.Id] = struct{}{}
			}
		}
		allocation := "Random"
		if game.NationAllocation == 1 {
			allocation = "Preferences"
		}
		bumpNamedHistogram("NationAllocation", allocation, globalStats.ActiveGameHistograms)
	}

	activeUserStatsIDs := make([]*datastore.Key, 0, len(activeGameMemberUserIds))
	for userId := range activeGameMemberUserIds {
		activeUserStatsIDs = append(activeUserStatsIDs, UserStatsID(ctx, userId))
	}

	userStatsSlice := make(UserStatsSlice, len(activeUserStatsIDs))
	err = datastore.GetMulti(ctx, activeUserStatsIDs, userStatsSlice)
	if err != nil {
		return err
	}

	for _, userStats := range userStatsSlice {
		bumpUserStatsHistograms(userStats, globalStats.ActiveGameMemberUserStatsHistograms)
		if _, found := activeMemberUserIds[userStats.UserId]; found {
			bumpUserStatsHistograms(userStats, globalStats.ActiveMemberUserStatsHistograms)
		}
	}

	w.SetContent(NewItem(globalStats).SetName("global-stats").SetDesc([][]string{
		[]string{
			"Global stats",
			"Histograms with global statistics for diplicity.",
		},
		[]string{
			"ActiveGameHistograms",
			"Contains histograms for currently started but not yet finished games.",
		},
		[]string{
			"ActiveGameMemberUserStatsHistograms",
			"Contains histograms for all members of currently started but not yet finished games.",
		},
		[]string{
			"ActiveMemberUserStatsHistograms",
			"Contains histograms for all non-NMR and non-Eliminated members of currently started but not yet finished games.",
		},
	}).AddLink(r.NewLink(Link{
		Rel: "visualizations",
		URL: "/html/global-stats.html",
	})))
	return nil
}
