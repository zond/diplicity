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
	"gopkg.in/sendgrid/sendgrid-go.v2"

	"github.com/zond/go-fcm"
	. "github.com/zond/goaeoas"
	oauth2service "google.golang.org/api/oauth2/v2"
)

var TestMode = false

const (
	LoginRoute          = "Login"
	LogoutRoute         = "Logout"
	RedirectRoute       = "Redirect"
	OAuth2CallbackRoute = "OAuth2Callback"
	UnsubscribeRoute    = "Unsubscribe"
)

const (
	userKind       = "User"
	naClKind       = "NaCl"
	oAuthKind      = "OAuth"
	userConfigKind = "UserConfig"
	prodKey        = "prod"
)

var (
	prodOAuth     *OAuth
	prodOAuthLock = sync.RWMutex{}
	prodNaCl      *naCl
	prodNaClLock  = sync.RWMutex{}
	router        *mux.Router
)

func PP(i interface{}) string {
	b, err := json.MarshalIndent(i, "  ", "  ")
	if err != nil {
		panic(fmt.Errorf("trying to marshal %+v: %v", i, err))
	}
	return string(b)
}

type FCMNotificationConfig struct {
	ClickActionTemplate string `methods:"PUT" datastore:",noindex"`
	TitleTemplate       string `methods:"PUT" datastore:",noindex"`
	BodyTemplate        string `methods:"PUT" datastore:",noindex"`
}

func (f *FCMNotificationConfig) Customize(ctx context.Context, notif *fcm.NotificationPayload, data interface{}) {
	if f.TitleTemplate != "" {
		if customTitle, err := raymond.Render(f.TitleTemplate, data); err == nil {
			notif.Title = customTitle
		} else {
			log.Infof(ctx, "Broken TitleTemplate %q: %v", f.TitleTemplate, err)
		}
	}
	if f.BodyTemplate != "" {
		if customBody, err := raymond.Render(f.BodyTemplate, data); err == nil {
			notif.Body = customBody
		} else {
			log.Infof(ctx, "Broken BodyTemplate %q: %v", f.BodyTemplate, err)
		}
	}
	if f.ClickActionTemplate != "" {
		if customClickAction, err := raymond.Render(f.ClickActionTemplate, data); err == nil {
			notif.ClickAction = customClickAction
		} else {
			log.Infof(ctx, "Broken ClickActionTemplate %q: %v", f.ClickActionTemplate, err)
		}
	}
}

func (f *FCMNotificationConfig) Validate() error {
	if f.ClickActionTemplate != "" {
		if _, err := raymond.Parse(f.ClickActionTemplate); err != nil {
			return err
		}
	}
	if f.TitleTemplate != "" {
		if _, err := raymond.Parse(f.TitleTemplate); err != nil {
			return err
		}
	}
	if f.BodyTemplate != "" {
		if _, err := raymond.Parse(f.BodyTemplate); err != nil {
			return err
		}
	}
	return nil
}

type MailNotificationConfig struct {
	SubjectTemplate  string `methods:"PUT" datastore:",noindex"`
	TextBodyTemplate string `methods:"PUT" datastore:",noindex"`
	HTMLBodyTemplate string `methods:"PUT" datastore:",noindex"`
}

func (m *MailNotificationConfig) Validate() error {
	if m.SubjectTemplate != "" {
		if _, err := raymond.Parse(m.SubjectTemplate); err != nil {
			return err
		}
	}
	if m.TextBodyTemplate != "" {
		if _, err := raymond.Parse(m.TextBodyTemplate); err != nil {
			return err
		}
	}
	if m.HTMLBodyTemplate != "" {
		if _, err := raymond.Parse(m.HTMLBodyTemplate); err != nil {
			return err
		}
	}
	return nil
}

func (m *MailNotificationConfig) Customize(ctx context.Context, msg *sendgrid.SGMail, data interface{}) {
	if m.SubjectTemplate != "" {
		if customSubject, err := raymond.Render(m.SubjectTemplate, data); err == nil {
			msg.Subject = customSubject
		} else {
			log.Infof(ctx, "Broken SubjectTemplate %q: %v", m.SubjectTemplate, err)
		}
	}
	if m.TextBodyTemplate != "" {
		if customTextBody, err := raymond.Render(m.TextBodyTemplate, data); err == nil {
			msg.SetText(customTextBody)
		} else {
			log.Infof(ctx, "Broken TextBodyTemplate %q: %v", m.TextBodyTemplate, err)
		}
	}
	if m.HTMLBodyTemplate != "" {
		if customHTMLBody, err := raymond.Render(m.HTMLBodyTemplate, data); err == nil {
			msg.SetHTML(customHTMLBody)
		} else {
			log.Infof(ctx, "Broken HTMLBodyTemplate %q: %v", m.HTMLBodyTemplate, err)
		}
	}
}

type FCMToken struct {
	Value         string                `methods:"PUT"`
	Disabled      bool                  `methods:"PUT"`
	Note          string                `methods:"PUT" datastore:",noindex"`
	App           string                `methods:"PUT"`
	MessageConfig FCMNotificationConfig `methods:"PUT"`
	PhaseConfig   FCMNotificationConfig `methods:"PUT"`
}

type UnsubscribeConfig struct {
	RedirectTemplate string `methods:"PUT"`
	HTMLTemplate     string `methods:"PUT"`
}

type MailConfig struct {
	Enabled           bool                   `methods:"PUT"`
	UnsubscribeConfig UnsubscribeConfig      `methods:"PUT"`
	MessageConfig     MailNotificationConfig `methods:"PUT"`
	PhaseConfig       MailNotificationConfig `methods:"PUT"`
}

type UserConfig struct {
	UserId     string
	FCMTokens  []FCMToken `methods:"PUT"`
	MailConfig MailConfig `methods:"PUT"`
}

var UserConfigResource = &Resource{
	Load:     loadUserConfig,
	Update:   updateUserConfig,
	FullPath: "/User/{user_id}/UserConfig",
}

func UserConfigID(ctx context.Context, userID *datastore.Key) *datastore.Key {
	return datastore.NewKey(ctx, userConfigKind, "config", 0, userID)
}

func (u *UserConfig) ID(ctx context.Context) *datastore.Key {
	return UserConfigID(ctx, UserID(ctx, u.UserId))
}

func (u *UserConfig) Item(r Request) *Item {
	return NewItem(u).SetName("user-config").
		AddLink(r.NewLink(UserConfigResource.Link("self", Load, []string{"user_id", u.UserId}))).
		AddLink(r.NewLink(UserConfigResource.Link("update", Update, []string{"user_id", u.UserId}))).
		SetDesc([][]string{
		[]string{
			"User configuration",
			"Each diplicity user has exactly one user configuration. User configurations defined user selected configuration for all of diplicty, such as which FCM tokens should be notified of new press or new phases.",
		},
		[]string{
			"FCM tokens",
			"Each FCM token has several fields.",
			"A value, which is the registration ID received when registering with FCM.",
			"A disabled flag which will turn notification to that token off, and which the server toggles if FCM returns errors when notifications are sent to that token.",
			"A note field, which the server will populate with the reason the token was disabled.",
			"An app field, which the app populating the token can use to identify tokens belonging to it to avoid removing/updating tokens belonging to other apps.",
			"Two template fields, one for phase and one for message notifications.",
		},
		[]string{
			"FCM templates",
			"The FCM templates define the title, body and click action of the FCM notifications sent out.",
			"They are parsed by a Handlebars parser (https://github.com/aymerick/raymond), using the context objects containing the same data as the data payload of the FCM notifications.",
		},
		[]string{
			"New phase FCM notifications",
			"New phase notifications will have the `[phase season] [phase year], [phase type]` as title, and `[game desc] has a new phase` as body.",
			"The payload will be `{ DiplicityJSON: DATA }` where DATA is `{ diplicityPhase: [phase JSON], diplicityGame: [game JSON], diplicityUser: [user JSON] }` compressed with libz.",
		},
		[]string{
			"New message FCM notifications",
			"New message notifications will have `[channel members]: Message from [sender]` as title, and `[message body]` as body.",
			"The payload will be `{ DiplicityJSON: DATA }` where DATA is `{ diplicityMessage: [message JSON], diplicityChannel: [channel JSON], diplicityGame: [game JSON], diplicityUser: [user JSON] }` compressed with libz.",
		},
		[]string{
			"Email config",
			"A user has an email config, defining if and how this user should receive email about new phases and messages.",
			"The email config contains several fields.",
			"An enabled flag which turns email notifications on.",
			"Information about whether the unsubscribe link in the email should render some HTML or redirect to another host, defined by two Handlebars templates, one for the redirect link and one for the HTML to display.",
			"Two template fields, one for phase and one for message notifications.",
			"All templates will be parsed by the same parser as the FCM templates, using context objects containing the same data as the data payload of the FCM notifications + ` unsubscribeURL: [URL to unsubscribe] `.",
		},
	})
}

func loadUserConfig(w ResponseWriter, r Request) (*UserConfig, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	config := &UserConfig{}
	if err := datastore.Get(ctx, UserConfigID(ctx, user.ID(ctx)), config); err == datastore.ErrNoSuchEntity {
		config.UserId = user.Id
		err = nil
	} else if err != nil {
		return nil, err
	}

	return config, nil
}

func updateUserConfig(w ResponseWriter, r Request) (*UserConfig, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return nil, HTTPErr{"unauthorized", 401}
	}

	config := &UserConfig{}
	if err := Copy(config, r, "PUT"); err != nil {
		return nil, err
	}
	config.UserId = user.Id

	for _, token := range config.FCMTokens {
		if err := token.MessageConfig.Validate(); err != nil {
			return nil, err
		}
		if err := token.PhaseConfig.Validate(); err != nil {
			return nil, err
		}
	}

	if _, err := datastore.Put(ctx, config.ID(ctx), config); err != nil {
		return nil, err
	}

	return config, nil
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

func getOAuth2Config(ctx context.Context, r Request) (*oauth2.Config, error) {
	scheme := "http"
	if r.Req().TLS != nil {
		scheme = "https"
	}
	redirectURL, err := router.Get(OAuth2CallbackRoute).URL()
	if err != nil {
		return nil, err
	}
	redirectURL.Scheme = scheme
	redirectURL.Host = r.Req().Host

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

	conf, err := getOAuth2Config(ctx, r)
	if err != nil {
		return err
	}

	loginURL := conf.AuthCodeURL(r.Req().URL.Query().Get("redirect-to"))

	http.Redirect(w, r.Req(), loginURL, 303)
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
		return nil, HTTPErr{"badly encrypted token", 403}
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

func handleOAuth2Callback(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf, err := getOAuth2Config(ctx, r)
	if err != nil {
		return err
	}

	token, err := conf.Exchange(ctx, r.Req().URL.Query().Get("code"))
	if err != nil {
		return err
	}

	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	service, err := oauth2service.New(client)
	if err != nil {
		return err
	}
	userInfo, err := oauth2service.NewUserinfoService(service).Get().Context(ctx).Do()
	if err != nil {
		return err
	}
	user := infoToUser(userInfo)
	user.ValidUntil = time.Now().Add(time.Hour * 24)
	if _, err := datastore.Put(ctx, UserID(ctx, user.Id), user); err != nil {
		return err
	}

	userToken, err := EncodeToken(ctx, user)
	if err != nil {
		return err
	}

	redirectURL, err := url.Parse(r.Req().URL.Query().Get("state"))
	if err != nil {
		return err
	}

	query := url.Values{}
	query.Set("token", userToken)
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r.Req(), redirectURL.String(), 303)
	return nil
}

func handleLogout(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Req().URL.Query().Get("redirect-to"), 303)
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

	}
	return true, nil
}

func loginRedirect(w ResponseWriter, r Request, errI error) (bool, error) {
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

func unsubscribe(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	decodedUserId, err := DecodeString(ctx, r.Req().URL.Query().Get("t"))
	if err != nil {
		return err
	}

	if decodedUserId != r.Vars()["user_id"] {
		return HTTPErr{"can only unsubscribe yourself", 403}
	}

	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		userConfigID := UserConfigID(ctx, UserID(ctx, r.Vars()["user_id"]))
		userConfig := &UserConfig{}
		if err := datastore.Get(ctx, userConfigID, userConfig); err != nil {
			return err
		}
		if !userConfig.MailConfig.Enabled {
			log.Infof(ctx, "%v is turned off mail, exiting", PP(userConfig))
			return nil
		}
		userConfig.MailConfig.Enabled = false
		_, err := datastore.Put(ctx, userConfigID, userConfig)
		return err
	}, &datastore.TransactionOptions{XG: false})
}

func SetupRouter(r *mux.Router) {
	router = r
	HandleResource(router, UserConfigResource)
	Handle(router, "/Auth/Login", []string{"GET"}, LoginRoute, handleLogin)
	Handle(router, "/Auth/Logout", []string{"GET"}, LogoutRoute, handleLogout)
	Handle(router, "/Auth/OAuth2Callback", []string{"GET"}, OAuth2CallbackRoute, handleOAuth2Callback)
	Handle(router, "/User/{user_id}/Unsubscribe", []string{"GET"}, UnsubscribeRoute, unsubscribe)
	AddFilter(tokenFilter)
	AddPostProc(loginRedirect)
}
