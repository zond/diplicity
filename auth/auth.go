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
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
	oauth2service "google.golang.org/api/oauth2/v2"
)

var (
	TestMode     = false
	HTMLAPILevel = 1
)

const (
	LoginRoute            = "Login"
	LogoutRoute           = "Logout"
	RedirectRoute         = "Redirect"
	OAuth2CallbackRoute   = "OAuth2Callback"
	UnsubscribeRoute      = "Unsubscribe"
	ApproveRedirectRoute  = "ApproveRedirect"
	ListRedirectURLsRoute = "ListRedirectURLs"
	ReplaceFCMRoute       = "ReplaceFCM"
	TestUpdateUserRoute   = "TestUpdateUser"
)

const (
	UserKind        = "User"
	naClKind        = "NaCl"
	oAuthKind       = "OAuth"
	redirectURLKind = "RedirectURL"
	superusersKind  = "Superusers"
	prodKey         = "prod"
)

var (
	prodOAuth          *OAuth
	prodOAuthLock      = sync.RWMutex{}
	prodNaCl           *naCl
	prodNaClLock       = sync.RWMutex{}
	prodSuperusers     *Superusers
	prodSuperusersLock = sync.RWMutex{}
	router             *mux.Router

	RedirectURLResource *Resource
)

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

func handleLogin(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf, err := getOAuth2Config(ctx, r.Req())
	if err != nil {
		return err
	}

	loginURL := conf.AuthCodeURL(r.Req().URL.Query().Get("redirect-to"))

	http.Redirect(w, r.Req(), loginURL, http.StatusTemporaryRedirect)
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

func encodeOAuthToken(ctx context.Context, token *oauth2.Token) (string, error) {
	b, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func decodeOAuthToken(ctx context.Context, b64 string) (*oauth2.Token, error) {
	b, err := base64.URLEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	token := &oauth2.Token{}
	if err := json.Unmarshal(b, token); err != nil {
		return nil, err
	}
	return token, nil
}

func encodeUserToToken(ctx context.Context, user *User) (string, error) {
	plain, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	return EncodeString(ctx, string(plain))
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

	user, err := getUserFromToken(ctx, token)
	if err != nil {
		log.Errorf(ctx, "Unable to produce user from token %#v: %v", token, err)
		HTTPError(w, r, err)
		return
	}

	state := r.URL.Query().Get("state")
	redirectURL, err := url.Parse(state)
	if err != nil {
		log.Warningf(ctx, "Unable to parse state parameter %#v to URL: %v", state, err)
		HTTPError(w, r, err)
		return
	}

	// Clients that are able to call this endpoint independently (without being
	// redirected from Google OAuth2 service) - which is what is necessary to
	// set the 'approve-redirect' query parameter - can work around the approved redirect
	// security measure anyway, and don't really need them anyway (since they are
	// mostly there to prevent evil blogs using your pre-recorded approval log in
	// as you to diplicity.
	if r.URL.Query().Get("approve-redirect") != "true" {
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

			b64Token, err := encodeOAuthToken(ctx, token)
			if err != nil {
				log.Errorf(ctx, "Unable to encode OAuth2 token %+v to base64: %v", token, err)
				HTTPError(w, r, err)
				return
			}

			clear := fmt.Sprintf("%s,%s,%s", redirectURL.String(), user.Id, b64Token)
			cipher, err := EncodeString(ctx, clear)
			if err != nil {
				log.Errorf(ctx, "Unable to encrypt  %#v: %v", clear, err)
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
        <form method="GET" action="%s">
	   	  <input type="hidden" name="state" value="%s">
		  <input class="pure-material-button-text" style="align-self:flex-start" type="submit" value="Yes, I want to play"/>
		</form>
		 <form method="GET" action="%s">
		  <input class="pure-material-button-text" style="align-self:flex-start" type="submit" value="Cancel"/>
		</form>
      </div>
`, strippedRedirectURL.String(), approveURL.String(), cipher, redirectURL.String()))
			return
		} else if err != nil {
			log.Errorf(ctx, "Unable to load approved redirect URL for %+v: %v", approvedURL, err)
			HTTPError(w, r, err)
			return
		}
	}

	finishLogin(ctx, w, r, user, redirectURL)
}

func getUserFromToken(ctx context.Context, token *oauth2.Token) (*User, error) {
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
	user.ValidUntil = time.Now().Add(time.Hour * 24)
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

	http.Redirect(w, r, redirectURL.String(), http.StatusTemporaryRedirect)
}

func handleLogout(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Req().URL.Query().Get("redirect-to"), http.StatusTemporaryRedirect)
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
				ValidUntil:    time.Now().Add(time.Hour * 24),
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
		if authHeader := r.Req().Header.Get("Authorization"); authHeader != "" {
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
		queryParams.Set("redirect-to", redirectURL.String())
		loginURL.RawQuery = queryParams.Encode()

		http.Redirect(w, r.Req(), loginURL.String(), http.StatusTemporaryRedirect)
		return false, nil
	}

	return true, errI
}

func handleApproveRedirect(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	state := r.Req().URL.Query().Get("state")
	plain, err := DecodeString(ctx, state)
	if err != nil {
		log.Warningf(ctx, "Unable to decode state %#v: %v", state, err)
		return err
	}

	parts := strings.Split(plain, ",")
	if len(parts) != 3 {
		log.Warningf(ctx, "Plain text token %+v is not three strings joined by ','.", plain)
		return fmt.Errorf("plain text token is not three strings joined by ','")
	}

	toApproveURL, err := url.Parse(parts[0])
	if err != nil {
		log.Warningf(ctx, "Unable to parse part of plain text %#v to URL: %v", parts[0], err)
		return err
	}

	strippedToApproveURL := *toApproveURL
	strippedToApproveURL.RawQuery = ""
	strippedToApproveURL.Path = ""

	userId := parts[1]

	approvedURL := &RedirectURL{
		UserId:      userId,
		RedirectURL: strippedToApproveURL.String(),
	}

	if _, err := datastore.Put(ctx, approvedURL.ID(ctx), approvedURL); err != nil {
		log.Errorf(ctx, "Unable to save approved url %+v: %v", approvedURL, err)
		return err
	}

	b64Token := parts[2]
	token, err := decodeOAuthToken(ctx, b64Token)
	if err != nil {
		log.Warningf(ctx, "Unable to decode base64 encoded OAuth2 token %#v: %v", b64Token, err)
		return err
	}

	user, err := getUserFromToken(ctx, token)
	if err != nil {
		log.Warningf(ctx, "Unable to fetch user from token %#v: %v", token, err)
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
		http.Redirect(w, r.Req(), redirURL, http.StatusTemporaryRedirect)
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
	Handle(router, "/Auth/Logout", []string{"GET"}, LogoutRoute, handleLogout)
	// Don't use `Handle` here, because we don't want CORS support for this particular route.
	router.Path("/Auth/OAuth2Callback").Methods("GET").Name(OAuth2CallbackRoute).HandlerFunc(handleOAuth2Callback)
	Handle(router, "/Auth/ApproveRedirect", []string{"GET"}, ApproveRedirectRoute, handleApproveRedirect)
	Handle(router, "/User/{user_id}/Unsubscribe", []string{"GET"}, UnsubscribeRoute, unsubscribe)
	Handle(router, "/User/{user_id}/FCMToken/{replace_token}/Replace", []string{"PUT"}, ReplaceFCMRoute, replaceFCM)
	AddFilter(decorateAPILevel)
	AddFilter(tokenFilter)
	AddFilter(logHeaders)
	AddPostProc(loginRedirect)
}
