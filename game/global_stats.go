package game

import (
	"fmt"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

type GlobalStats struct {
	ActiveGameHistograms                map[string]map[string]int
	ActiveGameMemberUserStatsHistograms map[string]map[string]int
	ActiveMemberUserStatsHistograms     map[string]map[string]int
}

func bumpNamedHistogram(name string, value int, m map[string]map[string]int) {
	hist, found := m[name]
	if !found {
		hist = map[string]int{}
		m[name] = hist
	}
	hist[fmt.Sprint(value)] += 1
}

func bumpUserStatsHistograms(userStats UserStats, m map[string]map[string]int) {
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

func handleGlobalStats(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	globalStats := GlobalStats{
		ActiveGameHistograms:                map[string]map[string]int{},
		ActiveGameMemberUserStatsHistograms: map[string]map[string]int{},
		ActiveMemberUserStatsHistograms:     map[string]map[string]int{},
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
		for _, member := range game.Members {
			activeGameMemberUserIds[member.User.Id] = struct{}{}
			if !member.NewestPhaseState.OnProbation && !member.NewestPhaseState.Eliminated {
				activeMemberUserIds[member.User.Id] = struct{}{}
			}
		}
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
	}))
	return nil
}
