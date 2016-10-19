package app

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"google.golang.org/appengine"
	"google.golang.org/appengine/user"

	. "github.com/zond/goaeoas"
)

var router = mux.NewRouter()

func preflight(w http.ResponseWriter, r *http.Request) {
	CORSHeaders(w)
}

type Diplicity struct {
	User *user.User
}

func handleIndex(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	index := NewItem(Diplicity{
		User: user.Current(appengine.NewContext(r.Req())),
	}).SetDesc("Diplicity - use Accept: header to get text/html or application/json").AddLink(Link{
		Rel:   "self",
		Route: "index",
	})
	if user.Current(ctx) == nil {
		index.AddLink(Link{
			Rel:   "login",
			Route: "login",
		})
	} else {
		index.AddLink(Link{
			Rel:   "logout",
			Route: "logout",
		})
	}
	w.SetContent(index)
	return nil
}

func handleLogin(w ResponseWriter, r Request) error {
	redirectURL, err := router.Get("redirect").URL()
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Add("redirect-to", r.Vars()["redirect-to"])
	redirectURL.RawQuery = params.Encode()
	loginURL, err := user.LoginURL(appengine.NewContext(r.Req()), redirectURL.String())
	if err != nil {
		return err
	}
	http.Redirect(w, r.Req(), loginURL, 303)
	return nil
}

func handleLogout(w ResponseWriter, r Request) error {
	redirectURL, err := router.Get("redirect").URL()
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Add("redirect-to", r.Vars()["redirect-to"])
	redirectURL.RawQuery = params.Encode()
	logoutURL, err := user.LogoutURL(appengine.NewContext(r.Req()), redirectURL.String())
	if err != nil {
		return err
	}
	http.Redirect(w, r.Req(), logoutURL, 303)
	return nil
}

func handleRedirect(w ResponseWriter, r Request) error {
	http.Redirect(w, r.Req(), r.Vars()["redirect-to"], 303)
	return nil
}

func init() {
	router.Methods("OPTIONS").HandlerFunc(preflight)
	Handle(router, "/", []string{"GET"}, "index", handleIndex)
	Handle(router, "/login", []string{"GET"}, "login", handleLogin)
	Handle(router, "/logout", []string{"GET"}, "logout", handleLogout)
	Handle(router, "/redirect", []string{"GET"}, "redirect", handleRedirect)
	http.Handle("/", router)
}
