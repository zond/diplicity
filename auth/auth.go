package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

var TestMode = false

const (
	LoginRoute            = "Login"
	LogoutRoute           = "Logout"
	RedirectRoute         = "Redirect"
	OAuth2CallbackRoute   = "OAuth2Callback"
	UnsubscribeRoute      = "Unsubscribe"
	ApproveRedirectRoute  = "ApproveRedirect"
	ListRedirectURLsRoute = "ListRedirectURLs"
	ReplaceFCMRoute       = "ReplaceFCM"
)

const (
	userKind        = "User"
	naClKind        = "NaCl"
	oAuthKind       = "OAuth"
	redirectURLKind = "RedirectURL"
	prodKey         = "prod"
)

var (
	prodOAuth     *OAuth
	prodOAuthLock = sync.RWMutex{}
	prodNaCl      *naCl
	prodNaClLock  = sync.RWMutex{}
	router        *mux.Router

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
}

func PP(i interface{}) string {
	b, err := json.MarshalIndent(i, "  ", "  ")
	if err != nil {
		panic(fmt.Errorf("trying to marshal %+v: %v", i, err))
	}
	return string(b)
}

func GetUnsubscribeURL(ctx context.Context, r *mux.Router, host, scheme string, userId string) (*url.URL, error) {
	unsubscribeURL, err := r.Get(UnsubscribeRoute).URL("user_id", userId)
	if err != nil {
		return nil, err
	}

	unsubscribeURL.Host = host
	unsubscribeURL.Scheme = scheme

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
		return nil, HTTPErr{"unauthorized", 401}
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
			return HTTPErr{"can only delete your own redirect URLs", 403}
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
		return HTTPErr{"unauthorized", 401}
	}

	if user.Id != r.Vars()["user_id"] {
		return HTTPErr{"can only list your own redirect URLs", 403}
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
	return datastore.NewKey(ctx, userKind, userID, 0, nil)
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
			return HTTPErr{"OAuth already configured", 400}
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
	if r.TLS == nil {
		redirectURL.Scheme = "http"
	} else {
		redirectURL.Scheme = "https"
	}
	redirectURL.Host = r.Host

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

	http.Redirect(w, r.Req(), loginURL, 307)
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
		return nil, HTTPErr{"badly encrypted token", 401}
	}
	return plain, nil
}

func EncodeToken(ctx context.Context, user *User) (string, error) {
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
		HTTPError(w, r, err)
		return
	}

	token, err := conf.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		HTTPError(w, r, err)
		return
	}

	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	service, err := oauth2service.New(client)
	if err != nil {
		HTTPError(w, r, err)
		return
	}
	userInfo, err := oauth2service.NewUserinfoService(service).Get().Context(ctx).Do()
	if err != nil {
		HTTPError(w, r, err)
		return
	}
	user := infoToUser(userInfo)
	user.ValidUntil = time.Now().Add(time.Hour * 24)
	if _, err := datastore.Put(ctx, UserID(ctx, user.Id), user); err != nil {
		HTTPError(w, r, err)
		return
	}

	redirectURL, err := url.Parse(r.URL.Query().Get("state"))
	if err != nil {
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
		if r.TLS == nil {
			requestedURL.Scheme = "http"
		} else {
			requestedURL.Scheme = "https"
		}
		requestedURL.RawQuery = ""
		requestedURL.Path = ""

		cipher, err := EncodeString(ctx, fmt.Sprintf("%s,%s", redirectURL.String(), user.Id))
		if err != nil {
			HTTPError(w, r, err)
			return
		}
		approveURL, err := router.Get(ApproveRedirectRoute).URL()
		if err != nil {
			HTTPError(w, r, err)
			return
		}

		renderMessage(w, "Approval requested", fmt.Sprintf(`%s wants to act on your behalf on %s. Is this OK? Your decision will be remembered.</br>
<form method="GET" action="%s"><input type="hidden" name="state" value="%s"><input type="submit" value="Yes"/></form>
<form method="GET" action="%s"><input type="submit" value="No"/></form>`, strippedRedirectURL.String(), requestedURL.String(), approveURL.String(), cipher, redirectURL.String()))
		return
	} else if err != nil {
		HTTPError(w, r, err)
		return
	}

	userToken, err := EncodeToken(ctx, user)
	if err != nil {
		HTTPError(w, r, err)
		return
	}

	query := url.Values{}
	query.Set("token", userToken)
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURL.String(), 307)
}

func handleLogout(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Req().URL.Query().Get("redirect-to"), 307)
	return nil
}

func tokenFilter(w ResponseWriter, r Request) (bool, error) {
	ctx := appengine.NewContext(r.Req())

	if fakeID := r.Req().URL.Query().Get("fake-id"); (TestMode || appengine.IsDevAppServer()) && fakeID != "" {
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
				return false, HTTPErr{"Authorization header not two parts joined by space", 400}
			}
			if strings.ToLower(parts[0]) != "bearer" {
				return false, HTTPErr{"Authorization header part 1 not 'bearer'", 400}
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
			return false, HTTPErr{"token timed out", 401}
		}

		log.Infof(ctx, "Request by %+v", user)

		r.Values()["user"] = user

		if queryToken {
			r.DecorateLinks(func(l *Link, u *url.URL) error {
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

func loginRedirect(w ResponseWriter, r Request, errI error) (bool, error) {
	ctx := appengine.NewContext(r.Req())
	log.Infof(ctx, "loginRedirect called with %+v", errI)

	if r.Media() != "text/html" {
		return true, errI
	}

	if herr, ok := errI.(HTTPErr); ok && herr.Status == 401 {
		redirectURL := r.Req().URL
		if r.Req().TLS == nil {
			redirectURL.Scheme = "http"
		} else {
			redirectURL.Scheme = "https"
		}
		redirectURL.Host = r.Req().Host

		loginURL, err := router.Get(LoginRoute).URL()
		if err != nil {
			return false, err
		}
		queryParams := loginURL.Query()
		queryParams.Set("redirect-to", redirectURL.String())
		loginURL.RawQuery = queryParams.Encode()

		http.Redirect(w, r.Req(), loginURL.String(), 307)
		return false, nil
	}

	return true, errI
}

func handleApproveRedirect(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	plain, err := DecodeString(ctx, r.Req().URL.Query().Get("state"))
	if err != nil {
		return err
	}

	parts := strings.Split(plain, ",")
	if len(parts) != 2 {
		return fmt.Errorf("plain text token is not two strings joined by ','")
	}

	toApproveURL, err := url.Parse(parts[0])
	if err != nil {
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
		return err
	}

	loginURL, err := router.Get(LoginRoute).URL()
	q := loginURL.Query()
	q.Set("redirect-to", toApproveURL.String())
	loginURL.RawQuery = q.Encode()

	http.Redirect(w, r.Req(), loginURL.String(), 307)

	return nil
}

func renderMessage(w http.ResponseWriter, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	fmt.Fprintf(w, `
<html>
<head>
<title>%s</title>
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1">
<style>
form {
	display: inline;
	margin: 0px;
	padding: 0px;
}
.bubble {
	position: relative;
	padding: 15px;
	margin: 1em 0 3em;
	color: #000;
	background: #f3961c;
	background: -webkit-gradient(linear, 0 0, 0 100%%, from(#f9d835), to(#f3961c));
	background: -moz-linear-gradient(#f9d835, #f3961c);
	background: -o-linear-gradient(#f9d835, #f3961c);
	background: linear-gradient(#f9d835, #f3961c);
	-webkit-border-radius: 10px;
	-moz-border-radius: 10px;
	border-radius: 10px;
	margin-left: 50px;
	background: #f3961c;
	bottom: -30px;
}
.bubble:after {
	content: "";
	position: absolute;
	bottom: -15px;
	left: -48px;
	border-width: 15px 15px 0;
	border-style: solid;
	border-color: #f3961c transparent;
	display: block;
	width: 0;
	top: auto;
	bottom: 12px;
	border-width: 10px 50px 10px 0;
	border-color: transparent #f3961c;
}
</style>
</head>
<body>
<table><tr>
<td>
<img width="100%%" src="/img/otto.png">
</td>
<td valign="bottom">
<div class="bubble">%s</div>
</td>
</tr></table>
</body>
</html>
`, title, msg)
}

func unsubscribe(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	decodedUserId, err := DecodeString(ctx, r.Req().URL.Query().Get("t"))
	if err != nil {
		return err
	}

	if decodedUserId != r.Vars()["user_id"] {
		return HTTPErr{"can only unsubscribe yourself", 403}
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
		http.Redirect(w, r.Req(), redirURL, 307)
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

	renderMessage(w, "Unsubscribed", fmt.Sprintf("%v has been unsubscribed from diplicity mail.", user.Name))

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
		return HTTPErr{"no such FCM token found", 404}
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
		return HTTPErr{"no such FCM token found", 404}
	}
	if len(userConfigs) > 1 {
		return HTTPErr{"too many FCM tokens found?", 500}
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

func SetupRouter(r *mux.Router) {
	router = r
	HandleResource(router, UserConfigResource)
	HandleResource(router, RedirectURLResource)
	Handle(router, "/Auth/Login", []string{"GET"}, LoginRoute, handleLogin)
	Handle(router, "/Auth/Logout", []string{"GET"}, LogoutRoute, handleLogout)
	// Don't use `Handle` here, because we don't want CORS support for this particular route.
	router.Path("/Auth/OAuth2Callback").Methods("GET").Name(OAuth2CallbackRoute).HandlerFunc(handleOAuth2Callback)
	Handle(router, "/Auth/ApproveRedirect", []string{"GET"}, ApproveRedirectRoute, handleApproveRedirect)
	Handle(router, "/User/{user_id}/Unsubscribe", []string{"GET"}, UnsubscribeRoute, unsubscribe)
	Handle(router, "/User/{user_id}/FCMToken/{replace_token}/Replace", []string{"PUT"}, ReplaceFCMRoute, replaceFCM)
	AddFilter(tokenFilter)
	AddPostProc(loginRedirect)
}
