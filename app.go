package main

import (
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/mux"
	"github.com/zond/diplicity/discord/api"
	"github.com/zond/diplicity/discord/handlers"
	"github.com/zond/diplicity/routes"
	"google.golang.org/appengine/v2"

	. "github.com/zond/goaeoas"
)

func main() {

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

	apiImpl := api.CreateApi()
	session, err := discordgo.New("Bot " + os.Getenv("DISCORD_BOT_TOKEN"))
	if err != nil {
		log.Fatalf("Cannot create Discord session: %v", err)
	}
	handlers.RegisterHandlers(session, apiImpl)
	log.Println("Discord initialization complete! Starting session...")

	err = session.Open()
	if err != nil {
		log.Fatal(err)
	}

	defer session.Close()

	appengine.Main()
}
