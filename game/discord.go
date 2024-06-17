package game

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/zond/diplicity/auth"
	"google.golang.org/appengine/v2/log"
)

type DiscordWebhook struct {
	Id    string
	Token string
}

type DiscordWebhooks struct {
	GameStarted  DiscordWebhook
	PhaseStarted DiscordWebhook
}

func CreateDiscordSession(ctx context.Context) (*discordgo.Session, error) {
	log.Infof(ctx, "Creating Discord session")
	discordBotToken, err := auth.GetDiscordBotToken(ctx)
	if err != nil {
		log.Warningf(ctx, "Error getting Discord bot token", err)
		return nil, err
	} else {
		discordSession, err := discordgo.New("Bot " + discordBotToken.Token)
		if err != nil {
			log.Errorf(ctx, "Error creating Discord session", err)
			return nil, err
		}
		return discordSession, nil
	}
}
