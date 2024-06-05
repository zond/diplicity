package api

// Api is a facade between the discord package and the backend

import (
	"log"

	"github.com/zond/diplicity/game"
)

type Province struct {
	Name     string
	Key      string
	UnitType string
}

type OrderType struct {
	Name string
	Key  string
}

type Api struct {
}

func (a *Api) SourceProvinces(userId, channelId string) ([]Province, error) {
	return []Province{
		{
			Name:     "Berlin",
			Key:      "berlin",
			UnitType: "Army",
		},
		{
			Name:     "Kiel",
			Key:      "kiel",
			UnitType: "Fleet",
		},
		{
			Name:     "Munich",
			Key:      "munich",
			UnitType: "Army",
		},
	}, nil
}

func (a *Api) OrderTypes(userId, channelId, source string) ([]OrderType, error) {
	if source == "" {
		return []OrderType{}, nil
	}
	return []OrderType{
		{
			Name: "Hold",
			Key:  "hold",
		},
		{
			Name: "Move",
			Key:  "move",
		},
		{
			Name: "Support",
			Key:  "support",
		},
		{
			Name: "Convoy",
			Key:  "convoy",
		},
	}, nil
}

func (a *Api) DestinationProvinces(userId, channelID, source, orderType string) ([]Province, error) {
	if source == "" || orderType == "" {
		return []Province{}, nil
	}
	return []Province{
		{
			Name: "Berlin",
			Key:  "berlin",
		},
		{
			Name: "Kiel",
			Key:  "kiel",
		},
		{
			Name: "Munich",
			Key:  "munich",
		},
	}, nil
}

func (a *Api) AuxProvinces(userId, channelId, source, orderType string) ([]Province, error) {
	if source == "" || orderType == "" {
		return []Province{}, nil
	}
	return []Province{
		{
			Name: "Berlin",
			Key:  "berlin",
		},
		{
			Name: "Kiel",
			Key:  "kiel",
		},
		{
			Name: "Munich",
			Key:  "munich",
		},
	}, nil
}

func (a *Api) AuxDestinationProvinces(userId, channelId, source, orderType, auxUnit string) ([]Province, error) {
	if source == "" || orderType == "" || auxUnit == "" {
		return []Province{}, nil
	}
	return []Province{
		{
			Name: "Berlin",
			Key:  "berlin",
		},
		{
			Name: "Kiel",
			Key:  "kiel",
		},
		{
			Name: "Munich",
			Key:  "munich",
		},
	}, nil
}

func (a *Api) CreateOrder(userId, channelId string) (*game.Order, error) {
	gameId := "gameId"             // TODO get game from channelId and get gameId from game
	phaseOrdinal := "phaseOrdinal" // TODO get game from channelId and get phaseOrdinal from game
	vars := map[string]string{
		"game_id":       gameId,
		"phase_ordinal": phaseOrdinal,
	}
	request, err := CreateAuthenticatedRequest(userId, vars, "")
	if err != nil {
		return nil, err
	}

	return game.CreateOrder(nil, request)
}

func (a *Api) CreateGame(userId, channelId string) (*game.Game, error) {
	log.Printf("api.CreateGame invoked\n")

	newGame := NewGameDefaultValues
	newGame.Desc = channelId // All new games are created with the channel ID as the name

	log.Printf("Creating game with values: %+v\n", newGame)

	vars := map[string]string{}

	request, err := CreateAuthenticatedRequest(userId, vars, NewGameDefaultValues)
	if err != nil {
		return nil, err
	}

	log.Printf("Calling game.CreateGame with request: %+v\n", request)
	return game.CreateGame(nil, request)
}

func CreateApi() *Api {
	return &Api{}
}
