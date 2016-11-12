package game

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
)

func preflight(w http.ResponseWriter, r *http.Request) {
	CORSHeaders(w)
}

var (
	router = mux.NewRouter()
)

const (
	maxLimit = 64
)

const (
	GetManifestJSRoute          = "GetManifestJS"
	GetSWJSRoute                = "GetSWJS"
	GetMainJSRoute              = "GetMainJS"
	ConfigureRoute              = "AuthConfigure"
	IndexRoute                  = "Index"
	OpenGamesRoute              = "OpenGames"
	StartedGamesRoute           = "StartedGames"
	FinishedGamesRoute          = "FinishedGames"
	MyStagingGamesRoute         = "MyStagingGames"
	MyStartedGamesRoute         = "MyStartedGames"
	MyFinishedGamesRoute        = "MyFinishedGames"
	ListOrdersRoute             = "ListOrders"
	ListPhasesRoute             = "ListPhases"
	ListPhaseStatesRoute        = "ListPhaseStates"
	ListGameStatesRoute         = "ListGameStates"
	ListOptionsRoute            = "ListOptions"
	ListChannelsRoute           = "ListChannels"
	ListMessagesRoute           = "ListMessages"
	DevResolvePhaseTimeoutRoute = "DevResolvePhaseTimeout"
)

type gamesHandler struct {
	query   *datastore.Query
	name    string
	desc    []string
	route   string
	private bool
}

type gamesReq struct {
	ctx   context.Context
	w     ResponseWriter
	r     Request
	user  *auth.User
	iter  *datastore.Iterator
	limit int
	h     *gamesHandler
}

func (r *gamesReq) cursor(err error) (*datastore.Cursor, error) {
	if err == nil {
		curs, err := r.iter.Cursor()
		if err != nil {
			return nil, err
		}
		return &curs, nil
	}
	if err == datastore.Done {
		return nil, nil
	}
	return nil, err
}

func (h *gamesHandler) prepare(w ResponseWriter, r Request, private bool) (*gamesReq, error) {
	req := &gamesReq{
		ctx: appengine.NewContext(r.Req()),
		w:   w,
		r:   r,
		h:   h,
	}

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}
	req.user = user

	limit, err := strconv.ParseInt(r.Req().URL.Query().Get("limit"), 10, 64)
	if err != nil || limit > maxLimit {
		limit = maxLimit
		err = nil
	}
	req.limit = int(limit)

	q := h.query
	if private {
		q = q.Filter("Members.User.Id=", user.Id)
	}

	if variantFilter := r.Req().URL.Query().Get("variant"); variantFilter != "" {
		q = q.Filter("Variant=", variantFilter)
	}

	cursor := r.Req().URL.Query().Get("cursor")
	if cursor == "" {
		req.iter = q.Run(req.ctx)
		return req, nil
	}

	decoded, err := datastore.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	req.iter = q.Start(decoded).Run(req.ctx)
	return req, nil
}

func (req *gamesReq) handle() error {
	var err error
	games := Games{}
	for err == nil && len(games) < req.limit {
		game := Game{}
		game.ID, err = req.iter.Next(&game)
		if err == nil {
			games = append(games, game)
		}
	}

	curs, err := req.cursor(err)
	if err != nil {
		return err
	}

	req.w.SetContent(games.Item(req.r, req.user, curs, req.limit, req.h.name, req.h.desc, req.h.route))
	return nil
}

func (h *gamesHandler) handlePublic(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r, false)
	if err != nil {
		return err
	}

	return req.handle()
}

func (h gamesHandler) handlePrivate(w ResponseWriter, r Request) error {
	req, err := h.prepare(w, r, true)
	if err != nil {
		return err
	}

	return req.handle()
}

var (
	finishedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Finished=", true).Order("-CreatedAt"),
		name:  "finished-games",
		desc:  []string{"Finished games", "Finished games, sorted with newest first."},
		route: FinishedGamesRoute,
	}
	startedGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", true).Order("CreatedAt"),
		name:  "started-games",
		desc:  []string{"Started games", "Started games, sorted with oldest first."},
		route: StartedGamesRoute,
	}
	openGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Closed=", false).Order("-NMembers").Order("CreatedAt"),
		name:  "open-games",
		desc:  []string{"Open games", "Open games, sorted with fullest and oldest first."},
		route: OpenGamesRoute,
	}
	stagingGamesHandler = gamesHandler{
		query: datastore.NewQuery(gameKind).Filter("Started=", false).Order("-NMembers").Order("CreatedAt"),
		name:  "my-staging-games",
		desc:  []string{"My staging games", "Unstarted games I'm a member of, sorted with fullest and oldest first."},
		route: MyStagingGamesRoute,
	}
)

type configuration struct {
	OAuth   *auth.OAuth
	FCMConf *FCMConf
}

func handleConfigure(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	conf := &configuration{}
	if err := json.NewDecoder(r.Req().Body).Decode(conf); err != nil {
		return err
	}
	if conf.OAuth != nil {
		if err := auth.SetOAuth(ctx, conf.OAuth); err != nil {
			return err
		}
	}
	if conf.FCMConf != nil {
		if err := SetFCMConf(ctx, conf.FCMConf); err != nil {
			return err
		}
	}
	return nil
}

func handleMainJS(w ResponseWriter, r Request) error {
	w.Header().Set("Content-Type", "text/javascript; charset=UTF-8")
	_, err := io.WriteString(w, `
const messaging = firebase.messaging();
messaging.requestPermission().then(function() {
	console.log('Notification permission granted.');
	// Get Instance ID token. Initially this makes a network call, once retrieved
	// subsequent calls to getToken will return from cache.
	messaging.getToken()
	.then(function(currentToken) {
		if (currentToken) {
			if ($('#fcm-token').length == 0) {
				$('body').prepend('<div id="fcm-token" style="font-size: xx-small; font-weight: light;">Your FCM token: ' + currentToken + '</div>');
			} else {
				$('#fcm-token').text('Your FCM token: ' + currentToken);
			}
		} else {
			$('#fcm-token').remove();
		}
	})
	.catch(function(err) {
		console.log('An error occurred while retrieving token. ', err);
	});
	// Callback fired if Instance ID token is updated.
	messaging.onTokenRefresh(function() {
		messaging.getToken()
		.then(function(refreshedToken) {
			console.log('Token refreshed.');
		})
		.catch(function(err) {
			console.log('Unable to retrieve refreshed token ', err);
		});
	});
	// Handle incoming messages. Called when:
	// - a message is received while the app has focus
	// - the user clicks on an app notification created by a sevice worker
	//   'messaging.setBackgroundMessageHandler' handler.
	messaging.onMessage(function(payload) {
		console.log("Message received. ", payload);
		alert(payload.notification.title + '\n' + payload.notification.body);
		// ...
	});
}).catch(function(err) {
	console.log('Unable to get permission to notify.', err);
});
`)
	return err
}

func handleSWJS(w ResponseWriter, r Request) error {
	w.Header().Set("Content-Type", "text/javascript; charset=UTF-8")
	_, err := io.WriteString(w, `
	// Give the service worker access to Firebase Messaging.
	// Note that you can only use Firebase Messaging here, other Firebase libraries
	// are not available in the service worker.
	importScripts('https://www.gstatic.com/firebasejs/3.5.2/firebase-app.js');
	importScripts('https://www.gstatic.com/firebasejs/3.5.2/firebase-messaging.js');

	// Initialize the Firebase app in the service worker by passing in the
	// messagingSenderId.
	firebase.initializeApp({
		'messagingSenderId': 'YOUR-SENDER-ID'
	});

	// Retrieve an instance of Firebase Messaging so that it can handle background
	// messages.
	const messaging = firebase.messaging();
`)
	return err
}

func handleManifestJS(w ResponseWriter, r Request) error {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	return json.NewEncoder(w).Encode(map[string]string{
		"name":          "diplicity-engine",
		"gcm_sender_id": "103953800507",
	})
}

func SetupRouter(r *mux.Router) {
	Handle(r, "/_configure", []string{"POST"}, ConfigureRoute, handleConfigure)
	Handle(r, "/", []string{"GET"}, IndexRoute, handleIndex)
	Handle(r, "/Game/{game_id}/Channel/{channel_members}/Messages", []string{"GET"}, ListMessagesRoute, listMessages)
	Handle(r, "/Game/{game_id}/Channels", []string{"GET"}, ListChannelsRoute, listChannels)
	Handle(r, "/Game/{game_id}/GameStates", []string{"GET"}, ListGameStatesRoute, listGameStates)
	Handle(r, "/Game/{game_id}/Phases", []string{"GET"}, ListPhasesRoute, listPhases)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/_dev_resolve_timeout", []string{"GET"}, DevResolvePhaseTimeoutRoute, devResolvePhaseTimeout)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/PhaseStates", []string{"GET"}, ListPhaseStatesRoute, listPhaseStates)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Orders", []string{"GET"}, ListOrdersRoute, listOrders)
	Handle(r, "/Game/{game_id}/Phase/{phase_ordinal}/Options", []string{"GET"}, ListOptionsRoute, listOptions)
	Handle(r, "/Games/Open", []string{"GET"}, OpenGamesRoute, openGamesHandler.handlePublic)
	Handle(r, "/Games/Started", []string{"GET"}, StartedGamesRoute, startedGamesHandler.handlePublic)
	Handle(r, "/Games/Finished", []string{"GET"}, FinishedGamesRoute, finishedGamesHandler.handlePublic)
	Handle(r, "/Games/My/Staging", []string{"GET"}, MyStagingGamesRoute, stagingGamesHandler.handlePrivate)
	Handle(r, "/Games/My/Started", []string{"GET"}, MyStartedGamesRoute, startedGamesHandler.handlePrivate)
	Handle(r, "/Games/My/Finished", []string{"GET"}, MyFinishedGamesRoute, finishedGamesHandler.handlePrivate)
	Handle(r, "/js/main.js", []string{"GET"}, GetMainJSRoute, handleMainJS)
	Handle(r, "/firebase-messaging-sw.js", []string{"GET"}, GetSWJSRoute, handleSWJS)
	Handle(r, "/js/manifest.json", []string{"GET"}, GetManifestJSRoute, handleManifestJS)
	HandleResource(r, GameResource)
	HandleResource(r, MemberResource)
	HandleResource(r, PhaseResource)
	HandleResource(r, OrderResource)
	HandleResource(r, MessageResource)
	HandleResource(r, PhaseStateResource)
	HandleResource(r, GameStateResource)
	HeadCallback(func(head *Node) error {
		head.AddEl("script", "src", "https://ajax.googleapis.com/ajax/libs/jquery/3.1.1/jquery.min.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/3.6.0/firebase.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/3.5.2/firebase-app.js")
		head.AddEl("script", "src", "https://www.gstatic.com/firebasejs/3.5.2/firebase-messaging.js")
		head.AddEl("script").AddText(`
  // Initialize Firebase
  var config = {
    apiKey: "AIzaSyB0rX7dts3Rk0UnDRR9A4vghO01mwCvLxY",
    authDomain: "diplicity-engine.firebaseapp.com",
    databaseURL: "https://diplicity-engine.firebaseio.com",
    storageBucket: "diplicity-engine.appspot.com",
    messagingSenderId: "635122585664"
  };
  firebase.initializeApp(config);
`)
		mainJSURL, err := r.Get(GetMainJSRoute).URL()
		if err != nil {
			return err
		}
		head.AddEl("script", "src", mainJSURL.String())
		manifestURL, err := r.Get(GetManifestJSRoute).URL()
		if err != nil {
			return err
		}
		head.AddEl("link", "rel", "manifest", "href", manifestURL.String())
		return nil
	})
}
