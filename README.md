
# diplicity

A dippy API service based on [App Engine](https://cloud.google.com/appengine) and [godip](https://github.com/zond/godip).

## Forum

Discussions about this project happen at [https://groups.google.com/forum/#!forum/diplicity-dev](https://groups.google.com/forum/#!forum/diplicity-dev).

## Public host

A regularly updated service running this code is available at [https://diplicity-engine.appspot.com/](https://diplicity-engine.appspot.com/).

### Play the game

To play, either be brave and use the auto generated HTML UI at [https://diplicity-engine.appspot.com/](https://diplicity-engine.appspot.com/), or use one of the client projects:

* [https://github.com/spamguy/dipl.io](https://github.com/spamguy/dipl.io)
* [https://github.com/zond/android-diplicity](https://github.com/zond/android-diplicity)

## Architecture

The API uses a slightly tweaked [HATEOAS](https://en.wikipedia.org/wiki/HATEOAS) style, but is basically JSON/REST.

## Auto generated HTML UI

To enable exploration of the API for debugging, research by UI engineers or even playing (not for the faint of heart) the API delivers a primitive HTML UI when queried with `Accept: text/html`.

## Forcing JSON output

To enable debugging the JSON output in a browser, adding the query parameter `accept=application/json` will make the server output JSON even to a browser that claims to prefer `text/html`.

## Running locally

To run it locally

1. Clone this repo.
2. Install the [App Engine SDK for Go](https://cloud.google.com/appengine/docs/go/download).
4. Make sure your `$GOPATH` is set to something reasonable, like `$HOME/go`.
5. Run `go get -v ./...` in the root directory.
6. Run `go get -v -u github.com/gorilla/sessions`. I have no idea why, but for some reason `github.com/gorilla/context` won't get downloaded automatically by the previous command, while this one does download it...
7. Run `dev_appserver.py .` in the `app` directory.
8. Run `curl -XPOST http://localhost:8080/_configure -d '{"FCMConf": {"ServerKey": SERVER_KEY_FROM_FCM}, "OAuth": {"ClientID": CLIENT_ID_FROM_GOOGLE_CLOUD_PROJECT, "Secret": SECRET_FROM_GOOGLE_CLOUD_PROJECT}, "SendGrid": {"APIKey": SEND_GRID_API_KEY}}'`.
   - This isn't necessary to run the server per se, but `FCMConf` is necessary for FCM message sending, `OAuth` is necessary for non `fake-id` login, and `SendGrid` is necessary for email sending.

### Faking user ID

When running the server locally, you can use the query parameter `fake-id` to set a fake user ID for your requests. This makes it possible and easy to test interaction between users without creating multiple Google accounts or even running multiple browsers.

#### Faking user email

When running the server locally, you can also use the query parameter `fake-email` to set the fake email of the fake user ID. This makes it possible and easy to test the email notification system.

## Running the tests

To run the tests

1. Start the local server with a clean database and consistent datastore. Since the tests don't wait around for consistency to be achieved, this simplifies writing the tests. Run `dev_appserver.py --clear_datastore=yes --datastore_consistency_policy=consistent .` in the `app` directory.
2. Run `go test -v` in the `diptest` directory.
