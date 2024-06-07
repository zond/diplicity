package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aymerick/raymond"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/appengine/v2"
	"google.golang.org/appengine/v2/datastore"
	"google.golang.org/appengine/v2/log"

	. "github.com/zond/goaeoas"
	oauth2service "google.golang.org/api/oauth2/v2"
)

var (
	TestMode     = false
	HTMLAPILevel = 1
)

const (
	LoginRoute               = "Login"
	LogoutRoute              = "Logout"
	TokenForDiscordUserRoute = "TokenForDiscordUser"
	DiscordBotLoginRoute     = "DiscordBotLogin"
	RedirectRoute            = "Redirect"
	OAuth2CallbackRoute      = "OAuth2Callback"
	UnsubscribeRoute         = "Unsubscribe"
	ApproveRedirectRoute     = "ApproveRedirect"
	ListRedirectURLsRoute    = "ListRedirectURLs"
	ReplaceFCMRoute          = "ReplaceFCM"
	TestUpdateUserRoute      = "TestUpdateUser"
)

const (
	redirectToKey    = "redirect-to"
	tokenDurationKey = "token-duration"
	stateKey         = "state"
)

const (
	UserKind                  = "User"
	naClKind                  = "NaCl"
	oAuthKind                 = "OAuth"
	redirectURLKind           = "RedirectURL"
	superusersKind            = "Superusers"
	discordBotCredentialsKind = "DiscordBotCredentials"
	prodKey                   = "prod"
)

const (
	defaultTokenDuration = time.Hour * 20
)

var (
	prodOAuth                     *OAuth
	prodOAuthLock                 = sync.RWMutex{}
	prodNaCl                      *naCl
	prodNaClLock                  = sync.RWMutex{}
	prodSuperusers                *Superusers
	prodSuperusersLock            = sync.RWMutex{}
	prodDiscordBotCredentials     *DiscordBotCredentials
	prodDiscordBotCredentialsLock = sync.RWMutex{}
	router                        *mux.Router

	RedirectURLResource *Resource
)

type DiscordBotCredentials struct {
	Username string
	Password string
}

func getDiscordBotCredentialsKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, discordBotCredentialsKind, prodKey, 0, nil)
}

func SetDiscordBotCredentials(ctx context.Context, discordBotCredentials *DiscordBotCredentials) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentDiscordBotCredentials := &DiscordBotCredentials{}
		if err := datastore.Get(ctx, getDiscordBotCredentialsKey(ctx), currentDiscordBotCredentials); err == nil {
			return HTTPErr{"DiscordBotCredentials already configured", http.StatusBadRequest}
		}
		if _, err := datastore.Put(ctx, getDiscordBotCredentialsKey(ctx), discordBotCredentials); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

func getDiscordBotCredentials(ctx context.Context) (*DiscordBotCredentials, error) {
	prodDiscordBotCredentialsLock.RLock()
	if prodDiscordBotCredentials != nil {
		defer prodDiscordBotCredentialsLock.RUnlock()
		return prodDiscordBotCredentials, nil
	}
	prodDiscordBotCredentialsLock.RUnlock()
	prodDiscordBotCredentialsLock.Lock()
	defer prodDiscordBotCredentialsLock.Unlock()
	foundDiscordBotCredentials := &DiscordBotCredentials{}
	if err := datastore.Get(ctx, getDiscordBotCredentialsKey(ctx), foundDiscordBotCredentials); err != nil {
		return nil, err
	}
	prodDiscordBotCredentials = foundDiscordBotCredentials
	return prodDiscordBotCredentials, nil
}

func init() {
	RedirectURLResource = &Resource{
		Delete: deleteRedirectURL,
		Listers: []Lister{
			{
				Path:    "/User/{user_id}/RedirectURLs",
				Route:   ListRedirectURLsRoute,
				Handler: listRedirectURLs,
			},
		},
	}
	CORSAllowHeaders = append(CORSAllowHeaders, "X-Diplicity-API-Level", "X-Diplicity-Client-Name")
}

func APILevel(r Request) int {
	if levelHeader := r.Req().Header.Get("X-Diplicity-API-Level"); levelHeader != "" {
		if level, err := strconv.Atoi(levelHeader); err == nil {
			return level
		}
	}
	if levelParam := r.Req().URL.Query().Get("api-level"); levelParam != "" {
		if level, err := strconv.Atoi(levelParam); err == nil {
			return level
		}
	}
	return 1
}

func PP(i interface{}) string {
	b, err := json.MarshalIndent(i, "  ", "  ")
	if err != nil {
		panic(fmt.Errorf("trying to marshal %+v: %v", i, err))
	}
	return string(b)
}

func GetUnsubscribeURL(ctx context.Context, r *mux.Router, host string, userId string) (*url.URL, error) {
	unsubscribeURL, err := r.Get(UnsubscribeRoute).URL("user_id", userId)
	if err != nil {
		return nil, err
	}
	unsubscribeURL.Host = host
	unsubscribeURL.Scheme = DefaultScheme

	unsubToken, err := EncodeString(ctx, userId)
	if err != nil {
		return nil, err
	}

	qp := unsubscribeURL.Query()
	qp.Set("t", unsubToken)
	unsubscribeURL.RawQuery = qp.Encode()

	return unsubscribeURL, nil
}

type RedirectURLs []RedirectURL

func (u RedirectURLs) Item(r Request, userId string) *Item {
	urlItems := make(List, len(u))
	for i := range u {
		urlItems[i] = u[i].Item(r)
	}
	urlsItem := NewItem(urlItems).SetName("approved-frontends").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListRedirectURLsRoute,
		RouteParams: []string{"user_id", userId},
	}))
	return urlsItem
}

func deleteRedirectURL(w ResponseWriter, r Request) (*RedirectURL, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	redirectURLID, err := datastore.DecodeKey(r.Vars()["id"])
	if err != nil {
		return nil, err
	}

	redirectURL := &RedirectURL{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, redirectURLID, redirectURL); err != nil {
			return err
		}
		if redirectURL.UserId != user.Id {
			return HTTPErr{"can only delete your own redirect URLs", http.StatusForbidden}
		}

		return datastore.Delete(ctx, redirectURLID)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return redirectURL, nil
}

type RedirectURL struct {
	UserId      string
	RedirectURL string
}

func (u *RedirectURL) Item(r Request) *Item {
	ctx := appengine.NewContext(r.Req())
	return NewItem(u).SetName("approved-frontend").AddLink(r.NewLink(RedirectURLResource.Link("delete", Delete, []string{"id", u.ID(ctx).Encode()})))
}

func (r *RedirectURL) ID(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, redirectURLKind, fmt.Sprintf("%s,%s", r.UserId, r.RedirectURL), 0, nil)
}

func listRedirectURLs(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	if user.Id != r.Vars()["user_id"] {
		return HTTPErr{"can only list your own redirect URLs", http.StatusForbidden}
	}

	redirectURLs := RedirectURLs{}
	if _, err := datastore.NewQuery(redirectURLKind).Filter("UserId=", user.Id).GetAll(ctx, &redirectURLs); err != nil {
		return err
	}

	w.SetContent(redirectURLs.Item(r, user.Id))

	return nil
}

type User struct {
	Email         string
	FamilyName    string
	Gender        string
	GivenName     string
	Hd            string
	Id            string
	Link          string
	Locale        string
	Name          string
	Picture       string
	VerifiedEmail bool
	ValidUntil    time.Time
}

func UserID(ctx context.Context, userID string) *datastore.Key {
	return datastore.NewKey(ctx, UserKind, userID, 0, nil)
}

func (u *User) ID(ctx context.Context) *datastore.Key {
	return UserID(ctx, u.Id)
}

func infoToUser(ui *oauth2service.Userinfoplus) *User {
	u := &User{
		Email:      ui.Email,
		FamilyName: ui.FamilyName,
		Gender:     ui.Gender,
		GivenName:  ui.GivenName,
		Hd:         ui.Hd,
		Id:         ui.Id,
		Link:       ui.Link,
		Locale:     ui.Locale,
		Name:       ui.Name,
		Picture:    ui.Picture,
	}
	if ui.VerifiedEmail != nil {
		u.VerifiedEmail = *ui.VerifiedEmail
	}
	return u
}

type Superusers struct {
	UserIds string
}

func (s *Superusers) Includes(userId string) bool {
	superusers := strings.Split(s.UserIds, ",")
	for i := range superusers {
		if superusers[i] == userId {
			return true
		}
	}
	return false
}

func getSuperusersKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, superusersKind, prodKey, 0, nil)
}

func SetSuperusers(ctx context.Context, superusers *Superusers) error {
	log.Infof(ctx, "Setting superusers to %+v", superusers)
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentSuperusers := &Superusers{}
		if err := datastore.Get(ctx, getSuperusersKey(ctx), currentSuperusers); err == nil {
			return HTTPErr{"Superusers already configured", http.StatusBadRequest}
		}
		if _, err := datastore.Put(ctx, getSuperusersKey(ctx), superusers); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

func GetSuperusers(ctx context.Context) (*Superusers, error) {
	prodSuperusersLock.RLock()
	if prodSuperusers != nil {
		defer prodSuperusersLock.RUnlock()
		return prodSuperusers, nil
	}
	prodSuperusersLock.RUnlock()
	prodSuperusersLock.Lock()
	defer prodSuperusersLock.Unlock()
	foundSuperusers := &Superusers{}
	if err := datastore.Get(ctx, getSuperusersKey(ctx), foundSuperusers); err != nil {
		return nil, err
	}
	prodSuperusers = foundSuperusers
	return prodSuperusers, nil
}

type naCl struct {
	Secret []byte
}

func getNaClKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, naClKind, prodKey, 0, nil)
}

func getNaCl(ctx context.Context) (*naCl, error) {
	// check if in memory
	prodNaClLock.RLock()
	if prodNaCl != nil {
		defer prodNaClLock.RUnlock()
		return prodNaCl, nil
	}
	prodNaClLock.RUnlock()
	// nope, check if in datastore
	prodNaClLock.Lock()
	defer prodNaClLock.Unlock()
	foundNaCl := &naCl{}
	if err := datastore.Get(ctx, getNaClKey(ctx), foundNaCl); err == nil {
		prodNaCl = foundNaCl
		return foundNaCl, nil
	} else if err != datastore.ErrNoSuchEntity {
		return nil, err
	}
	// nope, create new key
	foundNaCl.Secret = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, foundNaCl.Secret); err != nil {
		return nil, err
	}
	// write it transactionally into datastore
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, getNaClKey(ctx), foundNaCl); err == nil {
			return nil
		} else if err != datastore.ErrNoSuchEntity {
			return err
		}
		if _, err := datastore.Put(ctx, getNaClKey(ctx), foundNaCl); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}
	prodNaCl = foundNaCl
	return prodNaCl, nil
}

type OAuth struct {
	ClientID string
	Secret   string
}

func getOAuthKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, oAuthKind, prodKey, 0, nil)
}

func SetOAuth(ctx context.Context, oAuth *OAuth) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentOAuth := &OAuth{}
		if err := datastore.Get(ctx, getOAuthKey(ctx), currentOAuth); err == nil {
			return HTTPErr{"OAuth already configured", http.StatusBadRequest}
		}
		if _, err := datastore.Put(ctx, getOAuthKey(ctx), oAuth); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

func getOAuth(ctx context.Context) (*OAuth, error) {
	prodOAuthLock.RLock()
	if prodOAuth != nil {
		defer prodOAuthLock.RUnlock()
		return prodOAuth, nil
	}
	prodOAuthLock.RUnlock()
	prodOAuthLock.Lock()
	defer prodOAuthLock.Unlock()
	foundOAuth := &OAuth{}
	if err := datastore.Get(ctx, getOAuthKey(ctx), foundOAuth); err != nil {
		return nil, err
	}
	prodOAuth = foundOAuth
	return prodOAuth, nil
}

func getOAuth2Config(ctx context.Context, r *http.Request) (*oauth2.Config, error) {
	redirectURL, err := router.Get(OAuth2CallbackRoute).URL()
	if err != nil {
		return nil, err
	}
	redirectURL.Host = r.Host
	redirectURL.Scheme = DefaultScheme

	oauth, err := getOAuth(ctx)
	if err != nil {
		return nil, err
	}

	return &oauth2.Config{
		ClientID:     oauth.ClientID,
		ClientSecret: oauth.Secret,
		RedirectURL:  redirectURL.String(),
		Scopes: []string{
			"openid",
			"profile",
			"email",
		},
		Endpoint: google.Endpoint,
	}, nil
}

func handleGetTokenForDiscordUser(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return HTTPErr{
			Body:   "Unauthenticated",
			Status: http.StatusUnauthorized,
		}
	}

	if !appengine.IsDevAppServer() {

		superusers, err := GetSuperusers(ctx)
		if err != nil {
			return HTTPErr{
				Body:   "Unable to load superusers",
				Status: http.StatusInternalServerError,
			}
		}

		if !superusers.Includes(user.Id) {
			return HTTPErr{
				Body:   "Unauthorized",
				Status: http.StatusForbidden,
			}
		}
	}

	discordUserId := r.Vars()["user_id"]
	if discordUserId == "" {
		return HTTPErr{
			Body:   "Must provide discord user id",
			Status: http.StatusBadRequest,
		}
	}

	discordUser := createUserFromDiscordUserId(discordUserId)

	if _, err := datastore.Put(ctx, UserID(ctx, discordUser.Id), discordUser); err != nil {
		return HTTPErr{
			Body:   "Unable to store user",
			Status: http.StatusInternalServerError,
		}
	}

	token, err := encodeUserToToken(ctx, discordUser)
	if err != nil {
		return HTTPErr{
			Body:   "Unable to encode user to token",
			Status: http.StatusInternalServerError,
		}
	}

	w.SetContent(NewItem(token).SetName("token"))
	return nil
}

func createUserFromDiscordUserId(discordUserId string) *User {
	return &User{
		Email:         "discord-user@discord-user.fake",
		FamilyName:    "Discord User",
		GivenName:     "Discord User",
		Id:            discordUserId,
		Name:          "Discord User",
		VerifiedEmail: true,
		ValidUntil:    time.Now().Add(time.Hour * 24 * 365 * 10),
	}
}

func createDiscordBotUser() *User {
	return &User{
		Email:         "discord-bot@discord-bot.fake",
		FamilyName:    "Discord Bot",
		GivenName:     "Discord Bot",
		Id:            "discord-bot-user-id",
		Name:          "Discord Bot",
		VerifiedEmail: true,
		ValidUntil:    time.Now().Add(time.Hour * 24 * 365 * 10),
	}
}

func handleLogin(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf, err := getOAuth2Config(ctx, r.Req())
	if err != nil {
		return err
	}

	tokenDuration := defaultTokenDuration
	if tokenDurationString := r.Req().URL.Query().Get(tokenDurationKey); tokenDurationString != "" {
		tokenDurationLong, err := strconv.ParseInt(tokenDurationString, 10, 64)
		if err != nil {
			return err
		}
		tokenDuration = time.Second * time.Duration(tokenDurationLong)
	}

	redirectURL, err := url.Parse(r.Req().URL.Query().Get(redirectToKey))
	if err != nil {
		return err
	}

	stateString, err := encodeStateString(ctx, redirectURL, tokenDuration)
	if err != nil {
		return err
	}

	http.Redirect(w, r.Req(), conf.AuthCodeURL(stateString), http.StatusSeeOther)
	return nil
}

func handleDiscordBotLogin(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	discordBotCredentials, err := getDiscordBotCredentials(ctx)
	if err != nil {
		return HTTPErr{"Unable to load discord bot credentials", http.StatusInternalServerError}
	}

	authHeader := r.Req().Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Basic ") {
		return HTTPErr{"Authorization header must be Basic", http.StatusBadRequest}
	}

	decoded, err := base64.StdEncoding.DecodeString(authHeader[6:])
	if err != nil {
		return HTTPErr{"Unable to decode authorization header", http.StatusBadRequest}
	}

	parts := strings.Split(string(decoded), ":")
	if len(parts) != 2 {
		return HTTPErr{"Authorization header format not username:password", http.StatusBadRequest}
	}

	if parts[0] != discordBotCredentials.Username || parts[1] != discordBotCredentials.Password {
		return HTTPErr{"Unauthorized", http.StatusUnauthorized}
	}

	discordBotUser := createDiscordBotUser()

	if _, err := datastore.Put(ctx, UserID(ctx, discordBotUser.Id), discordBotUser); err != nil {
		return HTTPErr{"Unable to store user", http.StatusInternalServerError}
	}

	token, err := encodeUserToToken(ctx, discordBotUser)
	if err != nil {
		return HTTPErr{"Unable to encode user to token", http.StatusInternalServerError}
	}

	w.SetContent(NewItem(token).SetName("token"))
	return nil
}

func EncodeString(ctx context.Context, s string) (string, error) {
	b, err := EncodeBytes(ctx, []byte(s))
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func EncodeBytes(ctx context.Context, b []byte) ([]byte, error) {
	nacl, err := getNaCl(ctx)
	if err != nil {
		return nil, err
	}
	var nonceAry [24]byte
	if _, err := io.ReadFull(rand.Reader, nonceAry[:]); err != nil {
		return nil, err
	}
	var secretAry [32]byte
	copy(secretAry[:], nacl.Secret)
	cipher := secretbox.Seal(nonceAry[:], b, &nonceAry, &secretAry)
	return cipher, nil
}

func DecodeString(ctx context.Context, s string) (string, error) {
	sb, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	b, err := DecodeBytes(ctx, sb)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func DecodeBytes(ctx context.Context, b []byte) ([]byte, error) {
	var nonceAry [24]byte
	copy(nonceAry[:], b)
	nacl, err := getNaCl(ctx)
	if err != nil {
		return nil, err
	}
	var secretAry [32]byte
	copy(secretAry[:], nacl.Secret)

	plain, ok := secretbox.Open([]byte{}, b[24:], &nonceAry, &secretAry)
	if !ok {
		return nil, HTTPErr{"badly encrypted token", http.StatusUnauthorized}
	}
	return plain, nil
}

func encodeUserToToken(ctx context.Context, user *User) (string, error) {
	plain, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	return EncodeString(ctx, string(plain))
}

func decodeStateString(ctx context.Context, encryptedState string) (redirectURL *url.URL, tokenDuration time.Duration, err error) {
	decryptedState, err := DecodeString(ctx, encryptedState)
	if err != nil {
		return nil, 0, err
	}
	stateQuery, err := url.ParseQuery(decryptedState)
	if err != nil {
		return nil, 0, err
	}
	log.Infof(ctx, "decoded state query %+v", stateQuery)

	redirectURL, err = url.Parse(stateQuery.Get(redirectToKey))
	if err != nil {
		return nil, 0, err
	}
	tokenDurationLong, err := strconv.ParseInt(stateQuery.Get(tokenDurationKey), 10, 64)
	if err != nil {
		return nil, 0, err
	}
	log.Infof(ctx, "returning %v, %v, %v", redirectURL, time.Second*time.Duration(tokenDurationLong), nil)
	return redirectURL, time.Second * time.Duration(tokenDurationLong), nil
}

func encodeStateString(ctx context.Context, redirectURL *url.URL, tokenDuration time.Duration) (string, error) {
	stateQuery := url.Values{}
	stateQuery.Set(redirectToKey, redirectURL.String())
	stateQuery.Set(tokenDurationKey, fmt.Sprint(int64(tokenDuration/time.Second)))
	log.Infof(ctx, "encoded state query %+v", stateQuery)

	return EncodeString(ctx, stateQuery.Encode())
}

type approveState struct {
	User        User
	RedirectURL string
}

func encodeApproveState(ctx context.Context, redirectURL *url.URL, user *User) (string, error) {
	b, err := json.Marshal(approveState{User: *user, RedirectURL: redirectURL.String()})
	if err != nil {
		return "", err
	}
	if b, err = EncodeBytes(ctx, b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func decodeApproveState(ctx context.Context, b64EncryptedState string) (redirectURL *url.URL, user *User, err error) {
	encryptedBytes, err := base64.URLEncoding.DecodeString(b64EncryptedState)
	if err != nil {
		return nil, nil, err
	}
	decryptedBytes, err := DecodeBytes(ctx, encryptedBytes)
	if err != nil {
		return nil, nil, err
	}
	state := approveState{}
	if err := json.Unmarshal(decryptedBytes, &state); err != nil {
		return nil, nil, err
	}
	if redirectURL, err = url.Parse(state.RedirectURL); err != nil {
		return nil, nil, err
	}
	return redirectURL, &state.User, nil
}

func handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	conf, err := getOAuth2Config(ctx, r)
	if err != nil {
		log.Errorf(ctx, "Unable to load OAuth2Config: %v", err)
		HTTPError(w, r, err)
		return
	}

	code := r.URL.Query().Get("code")
	token, err := conf.Exchange(ctx, code)
	if err != nil {
		log.Warningf(ctx, "Unable to exchange code for token %#v: %v", code, err)
		HTTPError(w, r, err)
		return
	}

	state := r.URL.Query().Get(stateKey)

	// Clients that are able to call this endpoint independently (without being
	// redirected from Google OAuth2 service) - which is what is necessary to
	// set the 'approve-redirect' query parameter - can work around the approved redirect
	// security measure anyway, and don't really need them anyway (since they are
	// mostly there to prevent evil blogs using your pre-recorded approval log in
	// as you to diplicity.
	approveRedirect := r.URL.Query().Get("approve-redirect")
	if approveRedirect == "true" {
		redirectURL, err := url.Parse(state)
		if err != nil {
			log.Warningf(ctx, "Unable to parse state parameter %#v to URL: %v", state, err)
			HTTPError(w, r, err)
			return
		}

		user, err := getUserFromToken(ctx, token, defaultTokenDuration)
		if err != nil {
			log.Errorf(ctx, "Unable to produce user from token %#v: %v", token, err)
			HTTPError(w, r, err)
			return
		}

		finishLogin(ctx, w, r, user, redirectURL)
		return
	}

	redirectURL, tokenDuration, err := decodeStateString(ctx, state)
	if err != nil {
		log.Errorf(ctx, "Unable to decode state string from %#v: %v", state, err)
		HTTPError(w, r, err)
		return
	}

	user, err := getUserFromToken(ctx, token, tokenDuration)
	if err != nil {
		log.Errorf(ctx, "Unable to produce user from token %#v: %v", token, err)
		HTTPError(w, r, err)
		return
	}

	strippedRedirectURL := *redirectURL
	strippedRedirectURL.RawQuery = ""
	strippedRedirectURL.Path = ""

	approvedURL := &RedirectURL{
		UserId:      user.Id,
		RedirectURL: strippedRedirectURL.String(),
	}
	if err := datastore.Get(ctx, approvedURL.ID(ctx), approvedURL); err == datastore.ErrNoSuchEntity {
		requestedURL := r.URL
		requestedURL.Host = r.Host
		requestedURL.Scheme = DefaultScheme
		requestedURL.RawQuery = ""
		requestedURL.Path = ""

		approveState, err := encodeApproveState(ctx, redirectURL, user)
		if err != nil {
			log.Errorf(ctx, "Unable to encode approve state %v, %+v, %q: %v", redirectURL, token, user.Id, err)
			HTTPError(w, r, err)
			return
		}

		approveURL, err := router.Get(ApproveRedirectRoute).URL()
		if err != nil {
			log.Errorf(ctx, "Unable to get ApproveRedirectRoute %#v: %v", ApproveRedirectRoute, err)
			HTTPError(w, r, err)
			return
		}

		renderMessage(w, "Approval requested", fmt.Sprintf(`
      <span class="title">Play Diplomacy on the Diplicity server?</span>
      <span class="messagetext">You just logged in to a Diplomacy game for the first time. Welcome!
        <br /><br />
        The game uses the Diplicity server, and to prevent cheating it needs your
        permission to play for you. Are you okay to use the web site
        <u>%s</u> to play diplomacy?
	  </span>

      <div class="buttonlayout">
        <form method="POST" action="%s">
	   	  <input type="hidden" name="state" value="%s">
		  <input class="pure-material-button-text" style="align-self:flex-start" type="submit" value="Yes, I want to play"/>
		</form>
		 <form method="GET" action="%s">
		  <input class="pure-material-button-text" style="align-self:flex-start" type="submit" value="Cancel"/>
		</form>
      </div>
`, strippedRedirectURL.String(), approveURL.String(), approveState, redirectURL.String()))
		return
	} else if err != nil {
		log.Errorf(ctx, "Unable to load approved redirect URL for %+v: %v", approvedURL, err)
		HTTPError(w, r, err)
		return
	}

	finishLogin(ctx, w, r, user, redirectURL)
}

func getUserFromToken(ctx context.Context, token *oauth2.Token, duration time.Duration) (*User, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	service, err := oauth2service.New(client)
	if err != nil {
		log.Warningf(ctx, "Unable to create OAuth2 client from token %#v: %v", token, err)
		return nil, err
	}
	userInfo, err := oauth2service.NewUserinfoService(service).Get().Context(ctx).Do()
	if err != nil {
		log.Warningf(ctx, "Unable to fetch user info: %v", err)
		return nil, err
	}
	user := infoToUser(userInfo)
	user.ValidUntil = time.Now().Add(duration)
	if _, err := datastore.Put(ctx, UserID(ctx, user.Id), user); err != nil {
		log.Warningf(ctx, "Unable to store user info %+v: %v", user, err)
		return nil, err
	}
	return user, nil
}

func finishLogin(ctx context.Context, w http.ResponseWriter, r *http.Request, user *User, redirectURL *url.URL) {
	userToken, err := encodeUserToToken(ctx, user)
	if err != nil {
		log.Errorf(ctx, "Unable to encrypt token for %+v: %v", user, err)
		HTTPError(w, r, err)
		return
	}

	query := url.Values{}
	query.Set("token", userToken)
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
}

func handleLogout(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Req().URL.Query().Get(redirectToKey), http.StatusSeeOther)
	return nil
}

func tokenFilter(w ResponseWriter, r Request) (bool, error) {
	ctx := appengine.NewContext(r.Req())

	if fakeID := r.Req().URL.Query().Get("fake-id"); appengine.IsDevAppServer() && fakeID != "" {
		fakeEmail := "fake@fake.fake"
		if providedFake := r.Req().URL.Query().Get("fake-email"); providedFake != "" {
			fakeEmail = providedFake
			r.DecorateLinks(func(l *Link, u *url.URL) error {
				if l.Rel != "logout" {
					q := u.Query()
					q.Set("fake-email", fakeEmail)
					u.RawQuery = q.Encode()
				}
				return nil
			})
		}
		user := &User{
			Id: fakeID,
		}
		if err := datastore.Get(ctx, UserID(ctx, user.Id), user); err == datastore.ErrNoSuchEntity {
			user = &User{
				Email:         fakeEmail,
				FamilyName:    "Fakeson",
				GivenName:     "Fakey",
				Id:            fakeID,
				Name:          "Fakey Fakeson",
				VerifiedEmail: true,
				ValidUntil:    time.Now().Add(defaultTokenDuration),
			}
			if _, err := datastore.Put(ctx, UserID(ctx, user.Id), user); err != nil {
				return false, err
			}
		} else if err != nil {
			return false, err
		}

		r.Values()["user"] = user

		r.DecorateLinks(func(l *Link, u *url.URL) error {
			if l.Rel != "logout" {
				q := u.Query()
				q.Set("fake-id", fakeID)
				u.RawQuery = q.Encode()
			}
			return nil
		})

		log.Infof(ctx, "Request by fake %+v", user)

		return true, nil
	}

	queryToken := true
	token := r.Req().URL.Query().Get("token")
	if token == "" {
		queryToken = false
		if authHeader := r.Req().Header.Get("Authorization"); authHeader != "" && !strings.HasPrefix(authHeader, "Basic") {
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 {
				return false, HTTPErr{"Authorization header not two parts joined by space", http.StatusBadRequest}
			}
			if strings.ToLower(parts[0]) != "bearer" {
				return false, HTTPErr{"Authorization header part 1 not 'bearer'", http.StatusBadRequest}
			}
			token = parts[1]
		}
	}

	if token != "" {
		plain, err := DecodeString(ctx, token)
		if err != nil {
			return false, err
		}

		user := &User{}
		if err := json.Unmarshal([]byte(plain), user); err != nil {
			return false, err
		}
		if user.ValidUntil.Before(time.Now()) {
			return false, HTTPErr{"token timed out", http.StatusUnauthorized}
		}

		log.Infof(ctx, "Request by %+v", user)

		if fakeID := r.Req().URL.Query().Get("fake-id"); fakeID != "" {
			superusers, err := GetSuperusers(ctx)
			if err != nil {
				return false, err
			}

			if !superusers.Includes(user.Id) {
				return false, HTTPErr{"unauthorized", http.StatusForbidden}
			}

			log.Infof(ctx, "Faking user Id %q", fakeID)
			user.Id = fakeID
		}

		r.Values()["user"] = user

		if queryToken {
			r.DecorateLinks(func(l *Link, u *url.URL) error {
				log.Infof(ctx, "going to decorate %+v, %+v with %q", l, u, token)
				if l.Rel != "logout" {
					q := u.Query()
					q.Set("token", token)
					u.RawQuery = q.Encode()
				}
				return nil
			})
		}

	} else {
		log.Infof(ctx, "Unauthenticated request")
	}

	return true, nil
}

func decorateAPILevel(w ResponseWriter, r Request) (bool, error) {
	media, _ := Media(r.Req(), "Accept")
	if media == "text/html" {
		r.DecorateLinks(func(l *Link, u *url.URL) error {
			q := u.Query()
			q.Set("api-level", fmt.Sprint(HTMLAPILevel))
			u.RawQuery = q.Encode()
			return nil
		})
	}

	return true, nil
}

func loginRedirect(w ResponseWriter, r Request, errI error) (bool, error) {
	ctx := appengine.NewContext(r.Req())
	log.Infof(ctx, "loginRedirect called with %+v", errI)

	if r.Media() != "text/html" {
		return true, errI
	}

	if herr, ok := errI.(HTTPErr); ok && herr.Status == http.StatusUnauthorized {
		redirectURL := r.Req().URL
		redirectURL.Scheme = DefaultScheme
		redirectURL.Host = r.Req().Host

		loginURL, err := router.Get(LoginRoute).URL()
		if err != nil {
			return false, err
		}
		queryParams := loginURL.Query()
		queryParams.Set(redirectToKey, redirectURL.String())
		loginURL.RawQuery = queryParams.Encode()

		http.Redirect(w, r.Req(), loginURL.String(), http.StatusSeeOther)
		return false, nil
	}

	return true, errI
}

func handleApproveRedirect(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if err := r.Req().ParseForm(); err != nil {
		return err
	}

	toApproveURL, user, err := decodeApproveState(ctx, r.Req().Form.Get(stateKey))
	if err != nil {
		log.Warningf(ctx, "Unable to decode approve state %#v: %v", r.Req().Form.Get(stateKey), err)
		return err
	}

	strippedToApproveURL := *toApproveURL
	strippedToApproveURL.RawQuery = ""
	strippedToApproveURL.Path = ""

	approvedURL := &RedirectURL{
		UserId:      user.Id,
		RedirectURL: strippedToApproveURL.String(),
	}

	if _, err := datastore.Put(ctx, approvedURL.ID(ctx), approvedURL); err != nil {
		log.Errorf(ctx, "Unable to save approved url %+v: %v", approvedURL, err)
		return err
	}

	finishLogin(ctx, w, r.Req(), user, toApproveURL)
	return nil
}

func renderMessage(w http.ResponseWriter, title, msg string) error {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	return messageTemplate.Execute(w, map[string]string{
		"Title":   title,
		"Message": msg,
	})
}

func unsubscribe(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	decodedUserId, err := DecodeString(ctx, r.Req().URL.Query().Get("t"))
	if err != nil {
		return err
	}

	if decodedUserId != r.Vars()["user_id"] {
		return HTTPErr{"can only unsubscribe yourself", http.StatusForbidden}
	}

	userID := UserID(ctx, r.Vars()["user_id"])

	userConfigID := UserConfigID(ctx, userID)

	user := &User{}
	userConfig := &UserConfig{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.GetMulti(ctx, []*datastore.Key{userID, userConfigID}, []interface{}{user, userConfig}); err != nil {
			return err
		}
		if !userConfig.MailConfig.Enabled {
			return nil
		}
		userConfig.MailConfig.Enabled = false
		_, err := datastore.Put(ctx, userConfigID, userConfig)
		return err
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return err
	}

	if redirTemplate := userConfig.MailConfig.UnsubscribeConfig.RedirectTemplate; redirTemplate != "" {
		redirURL, err := raymond.Render(redirTemplate, map[string]interface{}{
			"user":       user,
			"userConfig": userConfig,
		})
		if err != nil {
			return err
		}
		http.Redirect(w, r.Req(), redirURL, http.StatusSeeOther)
		return nil
	}

	if htmlTemplate := userConfig.MailConfig.UnsubscribeConfig.HTMLTemplate; htmlTemplate != "" {
		html, err := raymond.Render(htmlTemplate, map[string]interface{}{
			"user":       user,
			"userConfig": userConfig,
		})
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, err = io.WriteString(w, html)
		return err
	}

	renderMessage(w, "Unsubscribed", fmt.Sprintf("<span class='messagetext'>%v has been unsubscribed from diplicity mail.</span>", user.Name))

	return nil
}

type FCMValue struct {
	Value string
}

func replaceFCM(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	userId := r.Vars()["user_id"]

	replaceToken := r.Vars()["replace_token"]
	if replaceToken == "" {
		return HTTPErr{"no such FCM token found", http.StatusNotFound}
	}

	fcmValue := &FCMValue{}
	if err := json.NewDecoder(r.Req().Body).Decode(fcmValue); err != nil {
		return err
	}

	userConfigs := []UserConfig{}
	ids, err := datastore.NewQuery(userConfigKind).Filter("UserId=", userId).Filter("FCMTokens.ReplaceToken=", replaceToken).GetAll(ctx, &userConfigs)
	if err != nil {
		return err
	}
	if len(userConfigs) == 0 {
		return HTTPErr{"no such FCM token found", http.StatusNotFound}
	}
	if len(userConfigs) > 1 {
		return HTTPErr{"too many FCM tokens found?", http.StatusInternalServerError}
	}
	for i := range userConfigs[0].FCMTokens {
		if userConfigs[0].FCMTokens[i].ReplaceToken == replaceToken {
			userConfigs[0].FCMTokens[i].Value = fcmValue.Value
		}
	}

	if _, err = datastore.Put(ctx, ids[0], &userConfigs[0]); err != nil {
		return err
	}

	w.SetContent(NewItem(fcmValue).SetName("value"))
	return err
}

func logHeaders(w ResponseWriter, r Request) (bool, error) {
	ctx := appengine.NewContext(r.Req())

	log.Infof(ctx, "APILevel:%v", APILevel(r))

	version := 0
	if versionHeader := r.Req().Header.Get("X-Diplicity-Client-Version"); versionHeader != "" {
		headerValue, err := strconv.Atoi(versionHeader)
		if err != nil {
			version = -1
		} else {
			version = headerValue
		}
	}
	log.Infof(ctx, "ClientVersion:%v", version)

	name := "unknown"
	if nameHeader := r.Req().Header.Get("X-Diplicity-Client-Name"); nameHeader != "" {
		name = nameHeader
	}
	log.Infof(ctx, "ClientName:%v", name)

	return true, nil
}

func handleTestUpdateUser(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	if !appengine.IsDevAppServer() {
		return HTTPErr{"only permitted during tests", http.StatusUnauthorized}
	}

	user := &User{}
	if err := json.NewDecoder(r.Req().Body).Decode(user); err != nil {
		return err
	}

	if _, err := datastore.Put(ctx, UserID(ctx, user.Id), user); err != nil {
		return err
	}

	return nil
}

func SetupRouter(r *mux.Router) {
	router = r
	HandleResource(router, UserConfigResource)
	HandleResource(router, RedirectURLResource)
	Handle(router, "/_test_update_user", []string{"PUT"}, TestUpdateUserRoute, handleTestUpdateUser)
	Handle(router, "/Auth/Login", []string{"GET"}, LoginRoute, handleLogin)
	Handle(router, "/Auth/DiscordBotLogin", []string{"GET"}, DiscordBotLoginRoute, handleDiscordBotLogin)
	Handle(router, "/Auth/{user_id}/TokenForDiscordUser", []string{"GET"}, TokenForDiscordUserRoute, handleGetTokenForDiscordUser)
	Handle(router, "/Auth/Logout", []string{"GET"}, LogoutRoute, handleLogout)
	// Don't use `Handle` here, because we don't want CORS support for this particular route.
	router.Path("/Auth/OAuth2Callback").Methods("GET").Name(OAuth2CallbackRoute).HandlerFunc(handleOAuth2Callback)
	Handle(router, "/Auth/ApproveRedirect", []string{"POST"}, ApproveRedirectRoute, handleApproveRedirect)
	Handle(router, "/User/{user_id}/Unsubscribe", []string{"GET"}, UnsubscribeRoute, unsubscribe)
	Handle(router, "/User/{user_id}/FCMToken/{replace_token}/Replace", []string{"PUT"}, ReplaceFCMRoute, replaceFCM)
	AddFilter(decorateAPILevel)
	AddFilter(tokenFilter)
	AddFilter(logHeaders)
	AddPostProc(loginRedirect)
}
