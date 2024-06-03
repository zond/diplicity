package handlers

// This file container the handler for the create-order command
// and subsequent interactions

import (
	"github.com/bwmarrin/discordgo"
)

// Declare a Discord application command
var CreateOrderCommand = discordgo.ApplicationCommand{
	Name:        "create-order",
	Description: "Create a new order",
}

type OrderData struct {
	Source      string
	OrderType   string
	AuxUnit     string
	Destination string
}

type Session interface {
	InteractionRespond(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error
}

type InteractionCreate struct {
	Interaction *discordgo.Interaction
	Member      *discordgo.Member
	Type        discordgo.InteractionType
}

type Api interface {
	ListAvailableSourceUnits(userID, gameID, phaseID string) ([]discordgo.SelectMenuOption, error)
}

func NewCreateOrderCommandHandler(s Session, i InteractionCreate, api Api) {
	options, err := api.ListAvailableSourceUnits(i.Member.User.ID, "gameID", "phaseID")

	if err != nil {
		panic(err)
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Title: "Create Order",
			Components: []discordgo.MessageComponent{
				&discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.SelectMenu{
							CustomID:    "source",
							Placeholder: "Select a unit to move",
							Options:     options,
						},
					},
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
}

// // Executed when the Discord command is invoked by a user
// func CreateOrderCommandHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
// 	// TODO add mechanism to ensure that command is being called from the
// 	// correct channel OR check if command can be added such that it is only
// 	// available in some channels

// 	// TODO add mechanism to ensure that command is being called by a user
// 	// who is in the game (role). If not, return an error message

// 	options := api.ListAvailableSourceUnits(i.Member.User.ID, "gameID", "phaseID")

// 	// TODO add mechanism to return error message if there are no units
// 	// to order

// 	orderData := OrderData{}

// 	// check that interaction is ApplicationCommand type
// 	if i.Type == discordgo.InteractionApplicationCommand {
// 		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 			Type: discordgo.InteractionResponseChannelMessageWithSource,
// 			Data: &discordgo.InteractionResponseData{
// 				Title: "Create Order",
// 				Components: []discordgo.MessageComponent{
// 					&discordgo.ActionsRow{
// 						Components: []discordgo.MessageComponent{
// 							&discordgo.SelectMenu{
// 								CustomID:    "source",
// 								Placeholder: "Select a unit to move",
// 								Options:     options,
// 							},
// 						},
// 					},
// 				},
// 			},
// 		})
// 		if err != nil {
// 			panic(err)
// 		}
// 	}

// 	// Add a handler for the InteractionCreate event
// 	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
// 		// Check that the interaction is not the ApplicationCommand type
// 		if i.Type != discordgo.InteractionApplicationCommand {
// 			// Check if the interaction is a select menu interaction
// 			if i.MessageComponentData().CustomID == "source" {
// 				// Get the selected option
// 				selectedOption := i.MessageComponentData().Values[0]
// 				orderData.Source = selectedOption

// 				// Get the available order types for the selected unit
// 				options := api.ListAvailableOrderTypes(i.Member.User.ID, "gameID", "phaseID", selectedOption)

// 				err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 					Type: discordgo.InteractionResponseChannelMessageWithSource,
// 					Data: &discordgo.InteractionResponseData{
// 						Title: "Create Order",
// 						Components: []discordgo.MessageComponent{
// 							&discordgo.ActionsRow{
// 								Components: []discordgo.MessageComponent{
// 									&discordgo.SelectMenu{
// 										CustomID:    "order-type",
// 										Placeholder: "Select an order type",
// 										Options:     options,
// 									},
// 								},
// 							},
// 						},
// 					},
// 				})
// 				if err != nil {
// 					panic(err)
// 				}
// 			} else if i.MessageComponentData().CustomID == "order-type" {
// 				// Get the selected option
// 				selectedOption := i.MessageComponentData().Values[0]
// 				orderData.OrderType = selectedOption

// 				// Check if an aux unit is required for the selected order type
// 				auxUnitRequired := api.AuxUnitRequired(orderData.OrderType)

// 				// Check if a destination is required for the selected order type
// 				destinationRequired := api.DestinationRequired(orderData.OrderType)

// 				if auxUnitRequired {
// 					options := api.ListAvailableAuxUnits(i.Member.User.ID, "gameID", "phaseID", orderData.Source, orderData.OrderType)
// 					err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 						Type: discordgo.InteractionResponseChannelMessageWithSource,
// 						Data: &discordgo.InteractionResponseData{
// 							Title: "Create Order",
// 							Components: []discordgo.MessageComponent{
// 								&discordgo.ActionsRow{
// 									Components: []discordgo.MessageComponent{
// 										&discordgo.SelectMenu{
// 											CustomID:    "aux-unit",
// 											Placeholder: "Select an aux unit",
// 											Options:     options,
// 										},
// 									},
// 								},
// 							},
// 						},
// 					})
// 					if err != nil {
// 						panic(err)
// 					}
// 				} else if destinationRequired {
// 					options := api.ListAvailableDestinations(i.Member.User.ID, "gameID", "phaseID", orderData.Source, orderData.OrderType)
// 					err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 						Type: discordgo.InteractionResponseChannelMessageWithSource,
// 						Data: &discordgo.InteractionResponseData{
// 							Title: "Create Order",
// 							Components: []discordgo.MessageComponent{
// 								&discordgo.ActionsRow{
// 									Components: []discordgo.MessageComponent{
// 										&discordgo.SelectMenu{
// 											CustomID:    "destination",
// 											Placeholder: "Select a destination",
// 											Options:     options,
// 										},
// 									},
// 								},
// 							},
// 						},
// 					})

// 					if err != nil {
// 						panic(err)
// 					}
// 				} else {
// 					// Create string representation of the order
// 					orderString := fmt.Sprintf("%s %s", orderData.Source, orderData.OrderType)
// 					// TODO add mechanism to create the order

// 					// Clear the order data
// 					orderData = OrderData{}
// 					err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 						Type: discordgo.InteractionResponseChannelMessageWithSource,
// 						Data: &discordgo.InteractionResponseData{
// 							Content: fmt.Sprintf("Order created: %s", orderString),
// 						},
// 					})
// 					if err != nil {
// 						panic(err)
// 					}
// 				}
// 			} else if i.MessageComponentData().CustomID == "aux-unit" {
// 				// Get the selected option
// 				selectedOption := i.MessageComponentData().Values[0]
// 				orderData.AuxUnit = selectedOption

// 				options := api.ListAvailableAuxDestinations(i.Member.User.ID, "gameID", "phaseID", orderData.Source, orderData.OrderType, orderData.AuxUnit)
// 				err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 					Type: discordgo.InteractionResponseChannelMessageWithSource,
// 					Data: &discordgo.InteractionResponseData{
// 						Title: "Create Order",
// 						Components: []discordgo.MessageComponent{
// 							&discordgo.ActionsRow{
// 								Components: []discordgo.MessageComponent{
// 									&discordgo.SelectMenu{
// 										CustomID:    "destination",
// 										Placeholder: "Select a destination",
// 										Options:     options,
// 									},
// 								},
// 							},
// 						},
// 					},
// 				})

// 				if err != nil {
// 					panic(err)
// 				}
// 			} else if i.MessageComponentData().CustomID == "destination" {
// 				// Get the selected option
// 				selectedOption := i.MessageComponentData().Values[0]
// 				orderData.Destination = selectedOption
// 				orderString := fmt.Sprintf("%s %s %s %s", orderData.Source, orderData.OrderType, orderData.AuxUnit, orderData.Destination)

// 				// TODO add mechanism to create the order
// 				// Clear the order data
// 				orderData = OrderData{}
// 				err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
// 					Type: discordgo.InteractionResponseChannelMessageWithSource,
// 					Data: &discordgo.InteractionResponseData{
// 						Content: fmt.Sprintf("Order created: %s", orderString),
// 					},
// 				})
// 				if err != nil {
// 					panic(err)
// 				}
// 			}
// 		}
// 	})
// }
