package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	oauth2service "google.golang.org/api/oauth2/v1"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"

	. "github.com/zond/goaeoas"
)

var (
	prod     *OAuth
	prodLock = sync.RWMutex{}
	router   = mux.NewRouter()
)

type configuration struct {
	OAuth OAuth
}

type OAuth struct {
	ClientID string
	Secret   string
}

func getOAuthKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, "OAuth", "prod", 0, nil)
}

func getOAuth(ctx context.Context) (*OAuth, error) {
	prodLock.RLock()
	if prod != nil {
		prodLock.RUnlock()
		return prod, nil
	}
	prodLock.RUnlock()
	prodLock.Lock()
	defer prodLock.Unlock()
	prod = &OAuth{}
	if err := datastore.Get(ctx, getOAuthKey(ctx), prod); err != nil {
		return nil, err
	}
	return prod, nil
}

func getOAuth2Config(ctx context.Context, r Request) (*oauth2.Config, error) {
	scheme := "http"
	if r.Req().TLS != nil {
		scheme = "https"
	}
	redirectURL, err := url.Parse(fmt.Sprintf("%s://%s/oauth2callback", scheme, r.Req().Host))
	if err != nil {
		return nil, err
	}

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

func preflight(w http.ResponseWriter, r *http.Request) {
	CORSHeaders(w)
}

type Diplicity struct {
	User *oauth2service.Userinfoplus
}

func handleIndex(w ResponseWriter, r Request) error {
	user, _ := r.Values()["user"].(*oauth2service.Userinfoplus)

	index := NewItem(Diplicity{
		User: user,
	}).
		SetName("diplicity").
		SetDesc([][]string{
		[]string{
			"Usage",
			"Use the `Accept` header or `accept` query parameter to choose `text/html` or `application/json` as output.",
			"CORS requests are allowed.",
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: "index",
	}))
	if user == nil {
		index.AddLink(r.NewLink(Link{
			Rel:   "login",
			Route: "login",
			QueryParams: url.Values{
				"redirect-to": []string{"/"},
			},
		}))
	} else {
		index.AddLink(r.NewLink(Link{
			Rel:   "logout",
			Route: "logout",
			QueryParams: url.Values{
				"redirect-to": []string{"/"},
			},
		}))
	}
	w.SetContent(index)
	return nil
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

	b, err := json.Marshal(token)
	if err != nil {
		return err
	}
	b64 := base64.URLEncoding.EncodeToString(b)
	log.Infof(ctx, "b64: %s", b64)

	redirectURL, err := url.Parse(r.Req().URL.Query().Get("state"))
	if err != nil {
		return err
	}

	query := url.Values{}
	query.Set("token", b64)
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r.Req(), redirectURL.String(), 303)
	return nil
}

func handleLogout(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Req().URL.Query().Get("redirect-to"), 303)
	return nil
}

func handleRedirect(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Vars()["redirect-to"], 303)
	return nil
}

func handleConfigure(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf := &configuration{}
	if err := json.NewDecoder(r.Req().Body).Decode(conf); err != nil {
		return err
	}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		current := &OAuth{}
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

func oauth2Filter(w ResponseWriter, r Request) error {
	var token *oauth2.Token

	tokenParam := r.Req().URL.Query().Get("token")
	if tokenParam != "" {
		b, err := base64.URLEncoding.DecodeString(tokenParam)
		if err != nil {
			return err
		}
		token = &oauth2.Token{}
		if err := json.Unmarshal(b, token); err != nil {
			return err
		}
	} else {
		if authHeader := r.Req().Header.Get("Authorization"); authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 {
				return fmt.Errorf("Authorization header not two parts joined by space")
			}
			if strings.ToLower(parts[0]) != "bearer" {
				return fmt.Errorf("Authorization header part 1 not 'bearer'")
			}
			token = &oauth2.Token{
				AccessToken: parts[1],
			}
		}
	}

	if token != nil {
		ctx := appengine.NewContext(r.Req())
		client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
		service, err := oauth2service.New(client)
		if err != nil {
			return err
		}
		userInfo, err := oauth2service.NewUserinfoService(service).Get().Context(ctx).Do()
		if err != nil {
			return err
		}
		r.Values()["user"] = userInfo
	}
	return nil
}

func init() {
	router.Methods("OPTIONS").HandlerFunc(preflight)
	Handle(router, "/", []string{"GET"}, "index", handleIndex)
	Handle(router, "/_configure", []string{"POST"}, "_configure", handleConfigure)
	Handle(router, "/login", []string{"GET"}, "login", handleLogin)
	Handle(router, "/logout", []string{"GET"}, "logout", handleLogout)
	Handle(router, "/redirect", []string{"GET"}, "redirect", handleRedirect)
	Handle(router, "/oauth2callback", []string{"GET"}, "oauth2callback", handleOAuth2Callback)
	AddFilter(oauth2Filter)
	http.Handle("/", router)
}
