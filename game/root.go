package game

import (
	"net/url"

	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/variants"

	. "github.com/zond/goaeoas"
)

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
			"The `login` link redirects to the Google OAuth2 login flow, and then back the `redirect-to` query param used when loading the `login` link.",
			"In the final redirect, the query parameter `token` will be your OAuth2 token.",
			"Use this token as the URL parameter `token`, or use it inside an `Authorization: Bearer ...` header to authenticate requests.",
		},
		[]string{
			"Source code",
			"The source code for this service can be found at https://github.com/zond/diplicity.",
			"Patches are welcome!",
		},
	}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: IndexRoute,
	})).AddLink(r.NewLink(Link{
		Rel:   "variants",
		Route: variants.ListVariantsRoute,
	}))

	if user == nil {
		redirectURL := r.Req().URL
		redirectURL.Host = r.Req().Host
		if r.Req().TLS == nil {
			redirectURL.Scheme = "http"
		} else {
			redirectURL.Scheme = "https"
		}
		index.AddLink(r.NewLink(Link{
			Rel:   "login",
			Route: auth.LoginRoute,
			QueryParams: url.Values{
				"redirect-to": []string{redirectURL.String()},
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
			Rel:   "top-rated-players",
			Route: ListTopRatedPlayersRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "top-reliable-players",
			Route: ListTopReliablePlayersRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "top-hated-players",
			Route: ListTopHatedPlayersRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "top-hater-players",
			Route: ListTopHaterPlayersRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "top-quick-players",
			Route: ListTopQuickPlayersRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-staging-games",
			Route: ListMyStagingGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-started-games",
			Route: ListMyStartedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-finished-games",
			Route: ListMyFinishedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "open-games",
			Route: ListOpenGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "started-games",
			Route: ListStartedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "finished-games",
			Route: ListFinishedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "flagged-messages",
			Route: ListFlaggedMessagesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:         "approved-frontends",
			Route:       auth.ListRedirectURLsRoute,
			RouteParams: []string{"user_id", user.Id},
		})).AddLink(r.NewLink(GameResource.Link("create-game", Create, nil))).
			AddLink(r.NewLink(auth.UserConfigResource.Link("user-config", Load, []string{"user_id", user.Id}))).
			AddLink(r.NewLink(Link{
			Rel:         "bans",
			Route:       ListBansRoute,
			RouteParams: []string{"user_id", user.Id},
		})).AddLink(r.NewLink(UserStatsResource.Link("user-stats", Load, []string{"user_id", user.Id})))
	}
	w.SetContent(index)
	return nil
}
