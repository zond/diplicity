package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	oauth2service "google.golang.org/api/oauth2/v1"
)

var (
	router = mux.NewRouter()
)

type configuration struct {
	OAuth OAuth
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
			"Use the `login` link to log in to the system.",
			"CORS requests are allowed.",
		},
		[]string{
			"Authentication",
			"The `login` link redirects to the Google OAuth2 login flow, and then back the `redirect-to` query param used when `GET`ing the `login` link.",
			"In the final redirect, the query parameter `token` will be your OAuth2 token.",
			"Use this `token` parameter when loading requests, or base64 decode it and use the `access_token` field inside as `Authorization: Bearer ...` header to authenticate requests.",
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

func init() {
	router.Methods("OPTIONS").HandlerFunc(preflight)
	Handle(router, "/", []string{"GET"}, "index", handleIndex)
	Handle(router, "/_configure", []string{"POST"}, "_configure", handleConfigure)
	Handle(router, "/login", []string{"GET"}, "login", handleLogin)
	Handle(router, "/logout", []string{"GET"}, "logout", handleLogout)
	Handle(router, "/redirect", []string{"GET"}, "redirect", handleRedirect)
	Handle(router, "/oauth2callback", []string{"GET"}, "oauth2callback", handleOAuth2Callback)
	AddFilter(tokenFilter)
	http.Handle("/", router)
}
