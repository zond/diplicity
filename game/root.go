package game

import (
	"net/url"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

type Diplicity struct {
	User *auth.User
}

type renderPhase struct {
	Year   int
	Season dip.Season
	Type   dip.PhaseType
	SCs    map[dip.Province]dip.Nation
	Units  map[dip.Province]dip.Unit
}

type renderVariant struct {
	variants.Variant
	Start renderPhase
}

func listVariants(w ResponseWriter, r Request) error {
	renderVariants := map[string]renderVariant{}
	for k, v := range variants.Variants {
		s, err := v.Start()
		if err != nil {
			return err
		}
		p := s.Phase()
		renderVariants[k] = renderVariant{
			Variant: v,
			Start: renderPhase{
				Year:   p.Year(),
				Season: p.Season(),
				Type:   p.Type(),
				SCs:    s.SupplyCenters(),
				Units:  s.Units(),
			},
		}
	}
	w.SetContent(NewItem(renderVariants).SetName("variants").AddLink(r.NewLink(Link{
		Rel:   "self",
		Route: ListVariantsRoute,
	})).SetDesc([][]string{
		[]string{
			"Variants",
			"This lists the supported variants on the server. Graph logically represents the map, while the rest of the fields should be fairly self explanatory.",
		},
	}))
	return nil
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
		Route: ListVariantsRoute,
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
			Rel:   "my-staging-games",
			Route: MyStagingGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-started-games",
			Route: MyStartedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "my-finished-games",
			Route: MyFinishedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "open-games",
			Route: OpenGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "started-games",
			Route: StartedGamesRoute,
		})).AddLink(r.NewLink(Link{
			Rel:   "finished-games",
			Route: FinishedGamesRoute,
		})).AddLink(r.NewLink(GameResource.Link("create-game", Create, nil)))
	}
	w.SetContent(index)
	return nil
}
