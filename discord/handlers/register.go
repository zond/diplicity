package handlers

import (
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/zond/diplicity/discord/api"
)

var applicationId = "1246942452791644281"

var commands = []discordgo.ApplicationCommand{
	CreateOrderCommand,
	CreateGameCommand,
}

func RegisterHandlers(session *discordgo.Session, apiImpl *api.Api) {
	log.Printf("Initializing Discord handlers\n")

	commandHandlers := map[string]func(s Session, i *discordgo.InteractionCreate){
		CreateOrderCommand.Name: CreateOrderCommandHandlerFactory(apiImpl),
		CreateGameCommand.Name:  CreateGameCommandHandlerFactory(apiImpl),
	}

	log.Printf("Registering debug handler\n")
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type == discordgo.InteractionApplicationCommand {
			log.Printf("Received command %q\n", i.ApplicationCommandData().Name)
		}
		if i.Type == discordgo.InteractionMessageComponent {
			log.Printf("Received message component %q\n", i.MessageComponentData().CustomID)
		}
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
				h(s, i)
			}
		}
	})

	for _, cmd := range commands {
		log.Printf("Creating slash command %q\n", cmd.Name)
		_, err := session.ApplicationCommandCreate(applicationId, "", &cmd)
		if err != nil {
			log.Fatalf("Cannot create slash command %q: %v", cmd.Name, err)
		}
	}

	log.Printf("Registering interaction handlers\n")
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type == discordgo.InteractionMessageComponent {
			if strings.HasPrefix(i.MessageComponentData().CustomID, CreateOrderInteractionIdPrefix) {
				CreateOrderCommandHandlerFactory(apiImpl)(s, i)
			}
			if strings.HasPrefix(i.MessageComponentData().CustomID, CreateOrderSubmitIdPrefix) {
				SubmitOrderInteractionHandlerFactory(apiImpl)(s, i)
			}
		}
	})
	log.Printf("Discord handlers initialized\n")

	log.Printf("Setting intents\n")
	session.Identify.Intents = discordgo.IntentsAllWithoutPrivileged
}
