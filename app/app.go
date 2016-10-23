package app

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"

	"github.com/zond/diplicity/auth"
	. "github.com/zond/goaeoas"
	oauth2service "google.golang.org/api/oauth2/v1"
)

var (
	router = mux.NewRouter()
)

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
			Route: auth.LoginRoute,
			QueryParams: url.Values{
				"redirect-to": []string{"/"},
			},
		}))
	} else {
		index.AddLink(r.NewLink(Link{
			Rel:   "logout",
			Route: auth.LogoutRoute,
			QueryParams: url.Values{
				"redirect-to": []string{"/"},
			},
		}))
	}
	w.SetContent(index)
	return nil
}

func init() {
	router.Methods("OPTIONS").HandlerFunc(preflight)
	Handle(router, "/", []string{"GET"}, "index", handleIndex)
	auth.SetupRouter(router)
	http.Handle("/", router)
}
