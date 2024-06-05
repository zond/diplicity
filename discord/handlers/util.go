package handlers

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/zond/diplicity/game"
)

// Discord handler related utilities

// Session interface is used to mock discordgo.Session in tests
type Session interface {
	InteractionRespond(*discordgo.Interaction, *discordgo.InteractionResponse, ...discordgo.RequestOption) error
	ChannelMessageDelete(channelID string, messageID string, options ...discordgo.RequestOption) (err error)
	ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

func GetUserAndChannelId(i *discordgo.InteractionCreate) (string, string) {
	log.Printf("Getting user ID and channel ID from interaction\n")
	userId := i.Member.User.ID
	channelId := i.ChannelID
	log.Printf("User ID: %s, Channel ID: %s\n", userId, channelId)
	return userId, channelId
}

func RespondWithError(message string, s Session, i *discordgo.InteractionCreate, err error) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("%s: %s", message, err.Error()),
		},
	})
}

// Useful when interaction message value is a JSON string (used in multi-step interactions)
func UnmarshalMessageComponentData(i *discordgo.InteractionCreate, v any) error {
	return json.Unmarshal([]byte(i.MessageComponentData().Values[0]), v)
}

// Useful when rendering a disabled select menu where available options are not known
var DummySelectMenuOptions = []discordgo.SelectMenuOption{
	{
		Label: "Dummy",
		Value: "dummy",
	},
}

func GetPhaseLengthDisplay(g *game.Game) string {
	fmt.Printf("Getting phase length display for game: %+v\n", g)
	phaseLengthHours := g.PhaseLengthMinutes / 60
	phaseLengthMinutes := g.PhaseLengthMinutes % 60
	phaseLengthDisplay := ""
	if phaseLengthHours > 0 {
		phaseLengthDisplay += fmt.Sprintf("%d hours", phaseLengthHours)
	}
	if phaseLengthMinutes > 0 {
		phaseLengthDisplay += fmt.Sprintf(" %d minutes", phaseLengthMinutes)
	}
	return phaseLengthDisplay
}
