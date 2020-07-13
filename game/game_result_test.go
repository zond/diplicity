package game

import (
	"fmt"
	"math"
	"testing"

	"github.com/zond/godip"
)

// Assert that the scores are the same as a provided set of values.
// gameScores: The actual scores
// expectedScores: A slice of the expected scores with each given to two decimal places.
func assertScoresTo2DP(t *testing.T, gameScores GameScores, expectedScores []float64) {
	if len(gameScores) != len(expectedScores) {
		panic(fmt.Errorf("Test error: gameScores has length %d but expectedScores has length %d.", len(gameScores), len(expectedScores)))
	}
	for i := range gameScores {
		roundedActual := math.Round(gameScores[i].Score*100) / 100
		roundedExpected := math.Round(expectedScores[i]*100) / 100
		if roundedActual != roundedExpected {
			t.Errorf("Expected %v to have %f points but had %f.", gameScores[i].Member, roundedExpected, roundedActual)
		}
	}
}

// Check that scores are in order of SC count with eliminated players getting 0 and survivors getting more.
// Note that this does not handle solos (and nor does the GameScores.Assign method)
func assertScoresMakeSense(t *testing.T, gameScores GameScores) {
	// Special case for all scores 0.
	total := 0
	for _, gameScore := range gameScores {
		total += gameScore.SCs
	}
	for i := range gameScores {
		gameScore0 := gameScores[i]
		if gameScore0.SCs == 0 && gameScore0.Score != 0 && total != 0 {
			t.Errorf("Expected eliminated nation %v to have no points but had %f.", gameScore0.Member, gameScore0.Score)
		}
		if gameScore0.SCs > 0 && gameScore0.Score == 0 {
			t.Errorf("Nation %v was not elimated (%d SCs) but scored 0: %v", gameScore0.Member, gameScore0.SCs, gameScores)
		}
		for j := range gameScores {
			if i >= j {
				continue
			}
			gameScore1 := gameScores[j]
			if gameScore0.SCs > gameScore1.SCs && gameScore0.Score <= gameScore1.Score {
				t.Errorf("Expected %v (%d SCs, %f points) to have more points than %v (%d SCs, %f points).", gameScore0.Member, gameScore0.SCs, gameScore0.Score, gameScore1.Member, gameScore1.SCs, gameScore1.Score)
			} else if gameScore0.SCs == gameScore1.SCs && gameScore0.Score != gameScore1.Score {
				t.Errorf("Expected %v (%d SCs, %f points) to have the same points as %v (%d SCs, %f points).", gameScore0.Member, gameScore0.SCs, gameScore0.Score, gameScore1.Member, gameScore1.SCs, gameScore1.Score)
			} else if gameScore0.SCs < gameScore1.SCs && gameScore0.Score >= gameScore1.Score {
				t.Errorf("Expected %v (%d SCs, %f points) to have fewer points than %v (%d SCs, %f points). %v", gameScore0.Member, gameScore0.SCs, gameScore0.Score, gameScore1.Member, gameScore1.SCs, gameScore1.Score, gameScores)
			}
		}
	}
}

// Check that solo handling causes the winning nation to receive all 100 points.
func TestAssignScores_SoloGetsAllPoints(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 18})
	scores = append(scores, GameScore{Member: "England", SCs: 13})
	scores = append(scores, GameScore{Member: "France", SCs: 3})
	scores = append(scores, GameScore{Member: "Germany", SCs: 0})
	scores = append(scores, GameScore{Member: "Italy", SCs: 0})
	scores = append(scores, GameScore{Member: "Russia", SCs: 0})
	scores = append(scores, GameScore{Member: "Turkey", SCs: 0})
	gameScores := GameScores(scores)
	gameResult := GameResult{Scores: gameScores, SoloWinnerMember: "Austria"}

	gameResult.AssignScores()

	assertScoresTo2DP(t, gameScores, []float64{100, 0, 0, 0, 0, 0, 0})
}

// Check that if only one nation remains then they get all 100 points.
func TestAssign_OneNationRemaining(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 34})
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresTo2DP(t, gameScores, []float64{100})
}

// Check that for a very large lead the tribute is capped by the survival bonus.
func TestAssign_LargeLead(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 17})
	scores = append(scores, GameScore{Member: "England", SCs: 4})
	scores = append(scores, GameScore{Member: "France", SCs: 4})
	scores = append(scores, GameScore{Member: "Germany", SCs: 3})
	scores = append(scores, GameScore{Member: "Italy", SCs: 3})
	scores = append(scores, GameScore{Member: "Russia", SCs: 2})
	scores = append(scores, GameScore{Member: "Turkey", SCs: 1})
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresTo2DP(t, gameScores, []float64{83, 4, 4, 3, 3, 2, 1})
}

// Check the SC counts mentioned in letter column in Diplomacy World #150.
func TestAssign_Monotonicity(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 16})
	scores = append(scores, GameScore{Member: "England", SCs: 13})
	scores = append(scores, GameScore{Member: "France", SCs: 4})
	scores = append(scores, GameScore{Member: "Germany", SCs: 1})
	oldGameScores := GameScores(scores)

	oldGameScores.Assign()

	assertScoresTo2DP(t, oldGameScores, []float64{50.5, 23.5, 14.5, 11.5})

	scores = []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 17})
	scores = append(scores, GameScore{Member: "England", SCs: 13})
	scores = append(scores, GameScore{Member: "France", SCs: 4})
	newGameScores := GameScores(scores)

	newGameScores.Assign()

	assertScoresTo2DP(t, newGameScores, []float64{47, 31, 22})

	// Note that Austria lost points gaining a center from Germany.
}

// Check that with SCs split almost evenly then the scores are distributed appropriately.
func TestAssign_SevenNationsRemaining(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 5})
	scores = append(scores, GameScore{Member: "England", SCs: 5})
	scores = append(scores, GameScore{Member: "France", SCs: 5})
	scores = append(scores, GameScore{Member: "Germany", SCs: 5})
	scores = append(scores, GameScore{Member: "Italy", SCs: 5})
	scores = append(scores, GameScore{Member: "Russia", SCs: 5})
	scores = append(scores, GameScore{Member: "Turkey", SCs: 4})
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresMakeSense(t, gameScores)
}

// Check that if not all SCs are assigned then the scoring still makes sense.
func TestAssign_ClassicalStartPosition(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 3})
	scores = append(scores, GameScore{Member: "England", SCs: 3})
	scores = append(scores, GameScore{Member: "France", SCs: 3})
	scores = append(scores, GameScore{Member: "Germany", SCs: 3})
	scores = append(scores, GameScore{Member: "Italy", SCs: 3})
	scores = append(scores, GameScore{Member: "Russia", SCs: 4})
	scores = append(scores, GameScore{Member: "Turkey", SCs: 3})
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresMakeSense(t, gameScores)
}

// Check that very small survivors are still ranked in order of SCs.
func TestAssign_SmallSurvivors(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Austria", SCs: 17})
	scores = append(scores, GameScore{Member: "England", SCs: 3})
	scores = append(scores, GameScore{Member: "France", SCs: 3})
	scores = append(scores, GameScore{Member: "Germany", SCs: 3})
	scores = append(scores, GameScore{Member: "Italy", SCs: 3})
	scores = append(scores, GameScore{Member: "Russia", SCs: 2})
	scores = append(scores, GameScore{Member: "Turkey", SCs: 1})
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresMakeSense(t, gameScores)
}

// Check that small survivors are still ranked in order of SCs with all SCs assigned in Europe 1939.
func TestAssign_SmallSurvivorsEurope1939(t *testing.T) {
	scores := []GameScore{}
	scores = append(scores, GameScore{Member: "Britain", SCs: 27})
	scores = append(scores, GameScore{Member: "France", SCs: 5})
	scores = append(scores, GameScore{Member: "Germany", SCs: 4})
	scores = append(scores, GameScore{Member: "Italy", SCs: 4})
	scores = append(scores, GameScore{Member: "Poland", SCs: 4})
	scores = append(scores, GameScore{Member: "Spain", SCs: 4})
	scores = append(scores, GameScore{Member: "Turkey", SCs: 4})
	scores = append(scores, GameScore{Member: "USSR", SCs: 3})
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresMakeSense(t, gameScores)
}

// Check that the algorithm can cope with lots of nations and lots of SCs.
func TestAssign_LotsOfNationsAndSCs(t *testing.T) {
	scores := []GameScore{}
	// Create two nations with each number of SCs from 0 to 99.
	for i := 0; i < 100; i++ {
		scores = append(scores, GameScore{Member: godip.Nation(fmt.Sprintf("Nation%da", i)), SCs: i})
		scores = append(scores, GameScore{Member: godip.Nation(fmt.Sprintf("Nation%db", i)), SCs: i})
	}
	gameScores := GameScores(scores)

	gameScores.Assign()

	assertScoresMakeSense(t, gameScores)
}

func assignSCsAndSanityTest(t *testing.T, nationId int, nations int, scs int, minSCs int, maxSCs int, scsAssigned []GameScore) []GameScores {
	// Terminate if no more nations left.
	if nations == 0 {
		gameScores := GameScores(scsAssigned)
		gameScores.Assign()
		assertScoresMakeSense(t, gameScores)
		return []GameScores{gameScores}
	}
	// Otherwise find all possible GameScores for this nation.
	results := []GameScores{}
	nation := godip.Nation(fmt.Sprintf("Nation%d", nationId))
	// Check if it's possible to assign the remaining SCs and satisfy the min/max requirements.
	if scs < minSCs || minSCs > maxSCs {
		return results
	}
	// Add the possible SC counts for this nation.
	for scCount := minSCs; scCount <= scs && scCount <= maxSCs; scCount++ {
		gameScore := GameScore{Member: godip.Nation(nation), SCs: scCount}
		gameScores := make([]GameScore, len(scsAssigned))
		copy(gameScores, scsAssigned)
		gameScores = append(gameScores, gameScore)
		results = append(results, assignSCsAndSanityTest(t, nationId+1, nations-1, scs-scCount, scCount, maxSCs, gameScores)...)
	}
	return results
}

// Run a sanity test for every possible (non-solo) position.
// This method is prohibitively slow for variants which aren't small.
func sanityTestAllPositions(t *testing.T, nations int, scs int, solo int) {
	assignSCsAndSanityTest(t, 0, nations, scs, 0, solo-1, []GameScore{})
}

func TestAssign_SanityTesting_ColdWar(t *testing.T) {
	sanityTestAllPositions(t, 2, 27, 17)
}

func TestAssign_SanityTesting_FranceVsAustria(t *testing.T) {
	sanityTestAllPositions(t, 2, 34, 18)
}

func TestAssign_SanityTesting_Hundred(t *testing.T) {
	sanityTestAllPositions(t, 3, 17, 9)
}

func TestAssign_SanityTesting_VietnamWar(t *testing.T) {
	sanityTestAllPositions(t, 5, 25, 15)
}

func TestAssign_SanityTesting_AncientMed(t *testing.T) {
	sanityTestAllPositions(t, 5, 34, 18)
}
