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
			[]string{
				"Creating games",
				"Most fields when creating games are self explanatory, but some of them require a bit of extra help.",
				"FirstMember.GameAlias is the alias that will be saved for the user that created the game. This is the same GameAlias as when updating a game membership.",
				"FirstMember.NationPreferences is the nations the game creator wants to play, in order of preference. This is the same NationPreferences as when updating a game membership.",
				"NoMerge should be set to true if the game should _not_ be merged with another open public game with the same settings.",
				"Private should be set to true if the game should _not_ show up in any game lists other than 'My ...'.",
			},
		}).AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: IndexRoute,
	})).AddLink(r.NewLink(Link{
		Rel:   "variants",
		Route: variants.ListVariantsRoute,
	})).AddLink(r.NewLink(Link{
		Rel:   "ratings-histogram",
		Route: GetUserRatingHistogramRoute,
	})).AddLink(r.NewLink(Link{
		Rel:   "global-stats",
		Route: GlobalStatsRoute,
	})).AddLink(r.NewLink(Link{
		Rel:   "rss",
		Route: RssRoute,
	})).AddLink(r.NewLink(AllocationResource.Link("test-allocation", Create, nil)))

	if user == nil {
		redirectURL := r.Req().URL
		redirectURL.Host = r.Req().Host
		redirectURL.Scheme = DefaultScheme
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
		}))
		addGamesHandlerLink(r, index, masteredStagingGamesHandler)
		addGamesHandlerLink(r, index, masteredStartedGamesHandler)
		addGamesHandlerLink(r, index, masteredFinishedGamesHandler)
		addGamesHandlerLink(r, index, myStagingGamesHandler)
		addGamesHandlerLink(r, index, myStartedGamesHandler)
		addGamesHandlerLink(r, index, myFinishedGamesHandler)
		addGamesHandlerLink(r, index, openGamesHandler)
		addGamesHandlerLink(r, index, startedGamesHandler)
		addGamesHandlerLink(r, index, finishedGamesHandler)
		index.AddLink(r.NewLink(Link{
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
