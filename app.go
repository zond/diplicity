package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"
	"google.golang.org/appengine/v2"

	. "github.com/zond/goaeoas"
)

func main() {

	discord, err := discordgo.New("Bot " + os.Getenv("DISCORD_BOT_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}
		if m.Content == "ping" {
			s.ChannelMessageSend(m.ChannelID, "pong")
		}
	})

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
