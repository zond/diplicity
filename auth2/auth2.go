package auth2

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/dgrijalva/jwt-go"
	"google.golang.org/api/oauth2/v2"
	"google.golang.org/appengine/log"
)

var data map[string]interface{}

func LoginHandler(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	log.Infof(ctx, "LoginHandler invoked")

	log.Infof(ctx, "Reading request body")
	requestBody, err := ioutil.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when reading request body: %v", err.Error()), http.StatusBadRequest)
		return
	}

	log.Infof(ctx, "Unmarshalling request body")
	err = json.Unmarshal(requestBody, &data)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when unmarshalling request body: %v", err.Error()), http.StatusBadRequest)
		return
	}

	log.Infof(ctx, "Validating request body")
	idToken, ok := data["idToken"].(string)
	if !ok {
		http.Error(writer, "idToken is required", http.StatusBadRequest)
		return
	}

	log.Infof(ctx, "Creating oauth service")
	oauthService, err := oauth2.New(http.DefaultClient)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when creating oauth service: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	log.Infof(ctx, "Validating idToken")
	if _, err := oauthService.Tokeninfo().IdToken(string(requestBody)).Do(); err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when validating idToken: %v", err.Error()), http.StatusUnauthorized)
		return
	}

	log.Infof(ctx, "Parsing idToken")
	token, _, err := new(jwt.Parser).ParseUnverified(idToken, &TokenInfo{})
	if err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when parsing idToken: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	log.Infof(ctx, "Create TokenInfo instance from token")
	tokenInfo, ok := token.Claims.(*TokenInfo)
	if !ok {
		http.Error(writer, fmt.Sprintf("Error occurred when creating TokenInfo instance from token: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	log.Infof(ctx, "Create User instance from TokenInfo")
	user := (&UserFactory{}).CreateUserFromTokenInfo(*tokenInfo)

	log.Infof(ctx, "Saving user")
	if err := user.Save(ctx); err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when saving user: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	log.Infof(ctx, "Encoding user to base64")
	userToken, err := encodeToBase64(user)
	if err != nil {
		http.Error(writer, fmt.Sprintf("Error occurred when encoding user to base64: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(userToken))
}
