package handlers

// This file container the handler for the create-order command
// and subsequent interactions

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/zond/diplicity/discord/api"
)

var (
	CreateOrderInteractionIdPrefix = "create-order-interaction-"
	CreateOrderSubmitIdPrefix      = "create-order-submit-"
)

type OrderData struct {
	Source         string `json:"source,omitempty"`
	Type           string `json:"type,omitempty"`
	Destination    string `json:"destination,omitempty"`
	Aux            string `json:"aux,omitempty"`
	AuxDestination string `json:"auxDestination,omitempty"`
}

var CreateOrderCommand = discordgo.ApplicationCommand{
	Name:        "create-order",
	Description: "Create a new order",
}

func CreateOrderCommandHandlerFactory(api *api.Api) func(Session, *discordgo.InteractionCreate) {
	return func(s Session, i *discordgo.InteractionCreate) {
		log.Printf("Handling create order command\n")

		userId, channelId := GetUserAndChannelId(i)

		orderData := &OrderData{}

		if i.Type == discordgo.InteractionApplicationCommand {
			// TODO allow user to pass arguments with command
		} else {
			err := UnmarshalMessageComponentData(i, orderData)
			if err != nil {
				RespondWithError("Failed to unmarshal message component data", s, i, err)
				return
			}
		}

		sourceProvinces, err := api.SourceProvinces(userId, channelId)
		if err != nil {
			RespondWithError("Failed to unmarshal message component data", s, i, err)
			return
		}

		orderTypes, err := api.OrderTypes(userId, channelId, orderData.Source)
		if err != nil {
			RespondWithError("Failed to unmarshal message component data", s, i, err)
			return
		}

		destinationProvinces, err := api.DestinationProvinces(userId, channelId, orderData.Source, orderData.Type)
		if err != nil {
			RespondWithError("Failed to unmarshal message component data", s, i, err)
			return
		}

		auxProvinces, err := api.AuxProvinces(userId, channelId, orderData.Source, orderData.Type)
		if err != nil {
			RespondWithError("Failed to unmarshal message component data", s, i, err)
			return
		}

		auxDestinationProvinces, err := api.AuxDestinationProvinces(userId, channelId, orderData.Source, orderData.Type, orderData.Aux)
		if err != nil {
			RespondWithError("Failed to unmarshal message component data", s, i, err)
			return
		}

		orderDataString, err := json.Marshal(orderData)
		if err != nil {
			RespondWithError("Failed to unmarshal message component data", s, i, err)
			return
		}

		sourceProvinceOptions := createOptions(orderData, provincesToItemTypes(sourceProvinces), setSource, setDefaultSource)
		orderTypeOptions := createOptions(orderData, orderTypesToItemTypes(orderTypes), setOrderType, setDefaultOrderType)
		destinationProvinceOptions := createOptions(orderData, provincesToItemTypes(destinationProvinces), setDestination, setDefaultDestination)
		auxProvinceOptions := createOptions(orderData, provincesToItemTypes(auxProvinces), setAux, setDefaultAux)
		auxDestinationProvinceOptions := createOptions(orderData, provincesToItemTypes(auxDestinationProvinces), setAuxDestination, setDefaultAuxDestination)

		components := []discordgo.MessageComponent{}
		components = append(components, &discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				&discordgo.SelectMenu{
					CustomID:    fmt.Sprintf("%s%s", CreateOrderInteractionIdPrefix, "source"),
					Placeholder: "Select a unit to move",
					Options:     sourceProvinceOptions,
				},
			},
		})
		components = append(components, &discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				&discordgo.SelectMenu{
					CustomID:    fmt.Sprintf("%s%s", CreateOrderInteractionIdPrefix, "type"),
					Placeholder: "Select order type",
					Options:     orderTypeOptions,
					Disabled:    orderData.Source == "",
				},
			},
		})
		if orderData.Type == "move" {
			components = append(components, &discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.SelectMenu{
						CustomID:    fmt.Sprintf("%s%s", CreateOrderInteractionIdPrefix, "destination"),
						Placeholder: "Select destination",
						Options:     destinationProvinceOptions,
					},
				},
			})
		}
		if orderData.Type == "support" || orderData.Type == "convoy" {
			components = append(components, &discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.SelectMenu{
						CustomID:    fmt.Sprintf("%s%s", CreateOrderInteractionIdPrefix, "aux"),
						Placeholder: "Select auxiliary unit",
						Options:     auxProvinceOptions,
					},
				},
			})
			components = append(components, &discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.SelectMenu{
						CustomID:    fmt.Sprintf("%s%s", CreateOrderInteractionIdPrefix, "aux-destination"),
						Placeholder: "Select destination for auxiliary unit",
						Options:     auxDestinationProvinceOptions,
						Disabled:    orderData.Aux == "",
					},
				},
			})
		}
		components = append(components, &discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				&discordgo.Button{
					CustomID: "cancel",
					Label:    "Cancel",
					Style:    discordgo.SecondaryButton,
				},
				&discordgo.Button{
					CustomID: fmt.Sprintf("%s%s", CreateOrderSubmitIdPrefix, string(orderDataString)),
					Label:    "Submit",
					Style:    discordgo.SuccessButton,
					Disabled: !orderReadyToSubmit(orderData),
				},
			},
		})

		responseData := &discordgo.InteractionResponseData{
			Title:      "Create Order",
			Components: components,
		}

		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: responseData,
		})
		if err != nil {
			panic(err)
		}
	}
}

func SubmitOrderInteractionHandlerFactory(api *api.Api) func(Session, *discordgo.InteractionCreate) {
	return func(s Session, i *discordgo.InteractionCreate) {
		userId, channelId := GetUserAndChannelId(i)
		buttonId := i.MessageComponentData().CustomID
		orderDataString := buttonId[len(CreateOrderSubmitIdPrefix):]
		orderData := &OrderData{}
		err := json.Unmarshal([]byte(orderDataString), orderData)
		if err != nil {
			panic(err)
		}

		_, error := api.CreateOrder(userId, channelId)
		if error != nil {
			panic(error)
		}

		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				// TODO better success message, see create_game
				Content: "Order created!",
			},
		})
		if err != nil {
			panic(err)
		}
	}
}

func orderReadyToSubmit(orderData *OrderData) bool {
	if orderData.Type == "hold" {
		return orderData.Source != "" && orderData.Type != ""
	}
	if orderData.Type == "move" {
		return orderData.Source != "" && orderData.Type != "" && orderData.Destination != ""
	}
	if orderData.Type == "support" || orderData.Type == "convoy" {
		return orderData.Source != "" && orderData.Type != "" && orderData.Aux != "" && orderData.AuxDestination != ""
	}
	return false
}

// Note, this function is a bit complex. The create-order process is a multi-step process
// but we don't want our bot to be stateful. So instead, we pass the current state of the
// order as a JSON string in the value of the select menu options. This way, we can
// continuously reconstruct the order state from the interaction data.
func createOptions(orderData *OrderData, items []itemType, setValueFunc setValue, setDefaultFunc setDefault) []discordgo.SelectMenuOption {
	options := make([]discordgo.SelectMenuOption, len(items))
	if len(items) == 0 {
		return DummySelectMenuOptions
	}
	for i, item := range items {
		optionOrderData := &OrderData{
			Source:         orderData.Source,
			Type:           orderData.Type,
			Destination:    orderData.Destination,
			Aux:            orderData.Aux,
			AuxDestination: orderData.AuxDestination,
		}
		optionOrderData = setValueFunc(optionOrderData, item)
		value, error := json.Marshal(optionOrderData)
		if error != nil {
			panic(error)
		}
		options[i] = discordgo.SelectMenuOption{
			Label:   item.Name,
			Value:   string(value),
			Default: setDefaultFunc(orderData, item),
		}
	}
	return options
}

// NOTE the types and functions below are required to make the createOptions function reusable
// for each property of OrderData.
type itemType struct {
	Name string
	Key  string
}

type setValue func(*OrderData, itemType) *OrderData

type setDefault func(*OrderData, itemType) bool

func provincesToItemTypes(province []api.Province) []itemType {
	items := make([]itemType, len(province))
	for i, p := range province {
		items[i] = itemType{
			Name: p.Name,
			Key:  p.Key,
		}
	}
	return items
}

func orderTypesToItemTypes(orderTypes []api.OrderType) []itemType {
	items := make([]itemType, len(orderTypes))
	for i, ot := range orderTypes {
		items[i] = itemType{
			Name: ot.Name,
			Key:  ot.Key,
		}
	}
	return items
}

func setSource(orderData *OrderData, item itemType) *OrderData {
	orderData.Source = item.Key
	return orderData
}

func setDefaultSource(orderData *OrderData, item itemType) bool {
	return item.Key == orderData.Source
}

func setOrderType(orderData *OrderData, item itemType) *OrderData {
	orderData.Type = item.Key
	return orderData
}

func setDefaultOrderType(orderData *OrderData, item itemType) bool {
	return item.Key == orderData.Type
}

func setDestination(orderData *OrderData, item itemType) *OrderData {
	orderData.Destination = item.Key
	return orderData
}

func setDefaultDestination(orderData *OrderData, item itemType) bool {
	return item.Key == orderData.Destination
}

func setAux(orderData *OrderData, item itemType) *OrderData {
	orderData.Aux = item.Key
	return orderData
}

func setDefaultAux(orderData *OrderData, item itemType) bool {
	return item.Key == orderData.Aux
}

func setAuxDestination(orderData *OrderData, item itemType) *OrderData {
	orderData.AuxDestination = item.Key
	return orderData
}

func setDefaultAuxDestination(orderData *OrderData, item itemType) bool {
	return item.Key == orderData.AuxDestination
}
