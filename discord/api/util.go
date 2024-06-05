package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/game"
	"google.golang.org/appengine"
	"google.golang.org/appengine/v2/datastore"
)

func CreateAuthenticatedRequest(userId string, vars map[string]string, data any) (*GoaeoasRequest, error) {
	log.Printf("api.CreateAuthenticatedRequest invoked - userId: %s; var: %s; data: %s \n", userId, vars, data)

	user := createUserFromDiscordUserId(userId)
	log.Printf("User instance created from Discord user ID: %+v\n", user)

	bodyJson, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	log.Printf("Data marshalled to JSON: %s\n", bodyJson)

	// Note, url and method don't matter because we skip router
	httpRequest, err := http.NewRequest("GET", "", bytes.NewBuffer(bodyJson))
	if err != nil {
		return nil, err
	}
	log.Printf("HTTP request created: %+v\n", httpRequest)

	httpRequest.Header.Set("Content-Type", "application/json")

	goaeoasRequest := &GoaeoasRequest{
		req:  httpRequest,
		vars: vars,
		values: map[string]interface{}{
			"user": user,
		},
	}
	log.Printf("GoaeoasRequest created: %+v\n", goaeoasRequest)

	_, err = getOrCreateUser(goaeoasRequest, user)
	if err != nil {
		return nil, err
	}

	return goaeoasRequest, nil
}

var NewGameDefaultValues = &game.Game{
	Variant:                       "Classical",
	PhaseLengthMinutes:            60 * 24,
	NonMovementPhaseLengthMinutes: 60 * 24,
	MaxHated:                      0,
	MaxHater:                      0,
	MinRating:                     0,
	MaxRating:                     0,
	MinReliability:                0,
	MinQuickness:                  0,
	Private:                       true,
	NoMerge:                       false,
	DisableConferenceChat:         true,
	DisableGroupChat:              true,
	DisablePrivateChat:            true,
	NationAllocation:              0,
	Anonymous:                     false,
	LastYear:                      0,
	SkipMuster:                    false,
	ChatLanguageISO639_1:          "en",
	GameMasterEnabled:             false,
	RequireGameMasterInvitation:   false,
}

func createUserFromDiscordUserId(userId string) *auth.User {
	return &auth.User{
		Email:         "discord-user@discord-user.fake",
		FamilyName:    "Discord User",
		GivenName:     "Discord User",
		Id:            userId,
		Name:          "Discord User",
		VerifiedEmail: true,
		ValidUntil:    time.Now().Add(time.Hour * 24 * 365 * 10),
	}
}

// Get the user from the datastore or create it if it does not exist.
func getOrCreateUser(r *GoaeoasRequest, user *auth.User) (*auth.User, error) {
	ctx := appengine.NewContext(r.Req())
	log.Printf("Getting or creating user: %+v\n", user)
	if err := datastore.Get(ctx, auth.UserID(ctx, user.Id), user); err == datastore.ErrNoSuchEntity {
		log.Printf("User not found, creating it\n")
		if _, err := datastore.Put(ctx, auth.UserID(ctx, user.Id), user); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	log.Printf("User: %+v\n", user)
	return user, nil
}
