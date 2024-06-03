package api

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// This file is a facade between the discord package and the backend
// logic. It helps make the Discord bot easier to maintain and
// understand by separating the command logic from the Discord
// bot logic.

// TODO implement real facade

// listSourceUnits is a function that returns a list of units that
// the user can select as the source unit for the order.
var ListAvailableSourceUnits = func(userID, gameID, phaseID string) ([]discordgo.SelectMenuOption, error) {
	// TODO implement
	return []discordgo.SelectMenuOption{
		{
			Label: "Army Berlin",
			// TODO move this to Discord layer
			Value: fmt.Sprintf(`{"source": "berlin"}`),
		},
		{
			Label: "Fleet Kiel",
			// TODO move this to Discord layer
			Value: fmt.Sprintf(`{"source": "kiel"}`),
		},
		{
			Label: "Army Munich",
			// TODO move this to Discord layer
			Value: fmt.Sprintf(`{"source": "munich"}`),
		},
	}, nil
}

var ListAvailableOrderTypes = func(userID, gameID, phaseID string, source string) []discordgo.SelectMenuOption {
	return []discordgo.SelectMenuOption{
		{
			Label: "Move",
			// TODO move this to Discord layer
			Value: fmt.Sprintf(`{"type": "move", "source": "%s"}`, source),
		},
		{
			Label: "Support",
			// TODO move this to Discord layer
			Value: fmt.Sprintf(`{"type": "support", "source": "%s"}`, source),
		},
		{
			Label: "Hold",
			// TODO move this to Discord layer
			Value: fmt.Sprintf(`{"type": "hold", "source": "%s"}`, source),
		},
		{
			Label: "Convoy",
			Value: fmt.Sprintf(`{"type": "convoy", "source": "%s"}`, source),
		},
	}
}

var ListAvailableAuxUnits = func(userID, gameID, phaseID string, source string, orderType string) []discordgo.SelectMenuOption {
	return []discordgo.SelectMenuOption{
		{
			Label: "Army Berlin",
			Value: "berlin",
		},
		{
			Label: "Fleet Kiel",
			Value: "kiel",
		},
		{
			Label: "Army Munich",
			Value: "munich",
		},
	}
}

var ListAvailableDestinations = func(userID, gameID, phaseID string, source string, orderType string) []discordgo.SelectMenuOption {
	return []discordgo.SelectMenuOption{
		{
			Label: "Berlin",
			Value: "berlin",
		},
		{
			Label: "Kiel",
			Value: "kiel",
		},
		{
			Label: "Munich",
			Value: "munich",
		},
	}
}

var ListAvailableAuxDestinations = func(userID, gameID, phaseID string, source string, orderType string, auxUnit string) []discordgo.SelectMenuOption {
	return []discordgo.SelectMenuOption{
		{
			Label: "Berlin",
			Value: "berlin",
		},
		{
			Label: "Kiel",
			Value: "kiel",
		},
		{
			Label: "Munich",
			Value: "munich",
		},
	}
}

var AuxUnitRequired = func(orderType string) bool {
	// return true if orderType is support or convoy
	return orderType == "support" || orderType == "convoy"
}

var DestinationRequired = func(orderType string) bool {
	// return false if orderType is hold
	return orderType != "hold"
}
