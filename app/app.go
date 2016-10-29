package app

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/game"

	. "github.com/zond/goaeoas"
)

var (
	router = mux.NewRouter()
)

const (
	indexRoute = "Index"
)

func preflight(w http.ResponseWriter, r *http.Request) {
	CORSHeaders(w)
}

type Diplicity struct {
	User *auth.User
}

func handleIndex(w ResponseWriter, r Request) error {
	user, _ := r.Values()["user"].(*auth.User)

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
		Route: indexRoute,
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
		})).AddLink(r.NewLink(Link{
			Rel:   "my-open-games",
			Route: game.MyOpenGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-closed-games",
			Route: game.MyClosedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-finished-games",
			Route: game.MyFinishedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "open-games",
			Route: game.OpenGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "closed-games",
			Route: game.ClosedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "finished-games",
			Route: game.FinishedGamesRoute,
		})).AddLink(r.NewLink(game.GameResource.Link("create-game", Create, nil)))
	}
	w.SetContent(index)
	return nil
}

func init() {
	router.Methods("OPTIONS").HandlerFunc(preflight)
	Handle(router, "/", []string{"GET"}, indexRoute, handleIndex)
	auth.SetupRouter(router)
	game.SetupRouter(router)
	http.Handle("/", router)
}
