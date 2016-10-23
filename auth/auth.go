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

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	"github.com/gorilla/mux"
	. "github.com/zond/goaeoas"
	oauth2service "google.golang.org/api/oauth2/v1"
)

const (
	AuthConfigureRoute  = "AuthConfigure"
	LoginRoute          = "Login"
	LogoutRoute         = "Logout"
	RedirectRoute       = "Redirect"
	OAuth2CallbackRoute = "OAuth2Callback"
)

const (
	naClKind  = "NaCl"
	oAuthKind = "OAuth"
	prodKey   = "prod"
)

var (
	prodOAuth     *oAuth
	prodOAuthLock = sync.RWMutex{}
	prodNaCl      *naCl
	prodNaClLock  = sync.RWMutex{}
	router        *mux.Router
)

type configuration struct {
	OAuth oAuth
}

type User struct {
	UserInfo   *oauth2service.Userinfoplus
	ValidUntil time.Time
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
	prodNaCl = &naCl{}
	if err := datastore.Get(ctx, getNaClKey(ctx), prodNaCl); err == nil {
		return prodNaCl, nil
	} else if err != datastore.ErrNoSuchEntity {
		return nil, err
	}
	// nope, create new key
	prodNaCl.Secret = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, prodNaCl.Secret); err != nil {
		return nil, err
	}
	// write it transactionally into datastore
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.Get(ctx, getNaClKey(ctx), prodNaCl); err == nil {
			return nil
		} else if err != datastore.ErrNoSuchEntity {
			return err
		}
		if _, err := datastore.Put(ctx, getNaClKey(ctx), prodNaCl); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}
	return prodNaCl, nil
}

type oAuth struct {
	ClientID string
	Secret   string
}

func getOAuthKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, oAuthKind, prodKey, 0, nil)
}

func getOAuth(ctx context.Context) (*oAuth, error) {
	prodOAuthLock.RLock()
	if prodOAuth != nil {
		defer prodOAuthLock.RUnlock()
		return prodOAuth, nil
	}
	prodOAuthLock.RUnlock()
	prodOAuthLock.Lock()
	defer prodOAuthLock.Unlock()
	prodOAuth = &oAuth{}
	if err := datastore.Get(ctx, getOAuthKey(ctx), prodOAuth); err != nil {
		return nil, err
	}
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

func encodeToken(ctx context.Context, userInfo *oauth2service.Userinfoplus) (string, error) {
	nacl, err := getNaCl(ctx)
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(User{
		UserInfo:   userInfo,
		ValidUntil: time.Now().Add(time.Hour * 24),
	})
	if err != nil {
		return "", err
	}
	var nonceAry [24]byte
	if _, err := io.ReadFull(rand.Reader, nonceAry[:]); err != nil {
		return "", err
	}
	var secretAry [32]byte
	copy(secretAry[:], nacl.Secret)
	cipher := secretbox.Seal(nonceAry[:], plain, &nonceAry, &secretAry)
	return base64.URLEncoding.EncodeToString(cipher), nil
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

	userToken, err := encodeToken(ctx, userInfo)
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

func handleConfigure(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf := &configuration{}
	if err := json.NewDecoder(r.Req().Body).Decode(conf); err != nil {
		return err
	}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		current := &oAuth{}
		if err := datastore.Get(ctx, getOAuthKey(ctx), current); err == nil {
			return fmt.Errorf("OAuth already configured")
		}
		if _, err := datastore.Put(ctx, getOAuthKey(ctx), &conf.OAuth); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return err
	}
	return nil
}

func tokenFilter(w ResponseWriter, r Request) error {
	token := r.Req().URL.Query().Get("token")
	if token == "" {
		if authHeader := r.Req().Header.Get("Authorization"); authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 {
				return fmt.Errorf("Authorization header not two parts joined by space")
			}
			if strings.ToLower(parts[0]) != "bearer" {
				return fmt.Errorf("Authorization header part 1 not 'bearer'")
			}
			token = parts[1]
		}
	}

	if token != "" {
		ctx := appengine.NewContext(r.Req())

		b, err := base64.URLEncoding.DecodeString(token)
		if err != nil {
			return err
		}

		var nonceAry [24]byte
		copy(nonceAry[:], b)
		nacl, err := getNaCl(ctx)
		if err != nil {
			return err
		}
		var secretAry [32]byte
		copy(secretAry[:], nacl.Secret)

		plain, ok := secretbox.Open([]byte{}, b[24:], &nonceAry, &secretAry)
		if !ok {
			http.Error(w, "badly decrypted token", 403)
			return nil
		}

		user := &User{}
		if err := json.Unmarshal(plain, user); err != nil {
			return err
		}
		log.Infof(ctx, "user: %+v", user)
		if user.ValidUntil.After(time.Now()) {
			r.Values()["user"] = user.UserInfo
			if r.Media() == "text/html" {
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
	}
	return nil
}

func SetupRouter(r *mux.Router) {
	router = r
	Handle(router, "/auth/_configure", []string{"POST"}, AuthConfigureRoute, handleConfigure)
	Handle(router, "/auth/login", []string{"GET"}, LoginRoute, handleLogin)
	Handle(router, "/auth/logout", []string{"GET"}, LogoutRoute, handleLogout)
	Handle(router, "/auth/oauth2callback", []string{"GET"}, OAuth2CallbackRoute, handleOAuth2Callback)
	AddFilter(tokenFilter)
}
