package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/mux"
	"github.com/zond/diplicity/discord/handlers"
	"github.com/zond/diplicity/routes"
	"github.com/zond/godip/variants"
	"google.golang.org/appengine/v2"

	. "github.com/zond/goaeoas"
)

var (
	APPLICATION_ID = "1246942452791644281"
	commands       = []discordgo.ApplicationCommand{
		handlers.CreateOrderCommand,
	}
	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"create-order": handlers.CreateOrderCommandHandler,
	}
)

func main() {

	discord, err := discordgo.New("Bot " + os.Getenv("DISCORD_BOT_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	// Note these are dummy handlers for now. Will create a separate package
	// for discord bot handlers which will do actually useful stuff.
	discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}
		if m.Content == "ping" {
			s.ChannelMessageSend(m.ChannelID, "pong")
		}
		if m.Content == "list variants" {
			// iterate over variants and create a list of variant names
			variantNames := ""
			for _, variant := range variants.Variants {
				variantNames += variant.Name + "\n"
			}
			s.ChannelMessageSend(m.ChannelID, variantNames)
		}
	})

	discord.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
				h(s, i)
			}
		}
	})

	cmdIDs := make(map[string]string, len(commands))

	for _, cmd := range commands {
		rcmd, err := discord.ApplicationCommandCreate(APPLICATION_ID, "", &cmd)
		if err != nil {
			log.Fatalf("Cannot create slash command %q: %v", cmd.Name, err)
		}

		cmdIDs[rcmd.ID] = rcmd.Name
	}

	discord.Identify.Intents = discordgo.IntentsAllWithoutPrivileged

	err = discord.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer discord.Close()

	fmt.Println("Discord bot is now running!")

	jsonFormURL, err := url.Parse("/js/jsonform.js")
	if err != nil {
		panic(err)
	}
	SetJSONFormURL(jsonFormURL)
	jsvURL, err := url.Parse("/js/jsv.js")
	if err != nil {
		panic(err)
	}
	SetJSVURL(jsvURL)
	if appengine.IsDevAppServer() {
		DefaultScheme = "http"
	} else {
		DefaultScheme = "https"
	}
	router := mux.NewRouter()
	routes.Setup(router)
	http.Handle("/", router)
	appengine.Main()
}
