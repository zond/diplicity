package handlers

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/zond/diplicity/discord/api"
	"github.com/zond/diplicity/game"
)

var CreateGameCommand = discordgo.ApplicationCommand{
	Name:        "create-game",
	Description: "Create a new game",
}

func CreateGameCommandHandlerFactory(api *api.Api) func(Session, *discordgo.InteractionCreate) {
	return func(s Session, i *discordgo.InteractionCreate) {
		log.Printf("Handling create game command\n")

		userId, channelId := GetUserAndChannelId(i)

		game, err := api.CreateGame(userId, channelId)
		if err != nil {
			RespondWithError("Failed to create game", s, i, err)
			return
		}

		successMessage := createSuccessMessage(game)

		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: successMessage,
			},
		})
		if err != nil {
			panic(err)
		}
	}
}

func createSuccessMessage(game *game.Game) string {
	return fmt.Sprintf(`
## Game Created!

### Game Settings

- **Variant**: %s
- **Phase length**: %s

### Next steps

- **The game has not started yet**: You need to add players to the game
- Run the **/add-members** command to add players to the game
- **Note**: you can only add users which have joined the server to the game

### Additional information

- Only one game can be created per channel
`, game.Variant, GetPhaseLengthDisplay(game))
}
