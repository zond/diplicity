[![Test diplicity](https://github.com/zond/diplicity/workflows/Test%20diplicity/badge.svg)](https://github.com/zond/diplicity/actions)
[![GoDoc](https://godoc.org/github.com/zond/diplicity?status.svg)](https://godoc.org/github.com/zond/diplicity)

# diplicity

A dippy API service based on [App Engine](https://cloud.google.com/appengine) and [godip](https://github.com/zond/godip).

## Forum

Discussions about this project happen at [https://groups.google.com/forum/#!forum/diplicity-dev](https://groups.google.com/forum/#!forum/diplicity-dev).

## Public host

A regularly updated service running this code is available at [https://diplicity-engine.appspot.com/](https://diplicity-engine.appspot.com/).

### Play the game

To play, either be brave and use the auto generated HTML UI at [https://diplicity-engine.appspot.com/](https://diplicity-engine.appspot.com/), or use one of the client projects:

- [https://github.com/spamguy/dipl.io](https://github.com/spamguy/dipl.io)
- [https://github.com/zond/android-diplicity](https://github.com/zond/android-diplicity)

## Architecture

The API uses a slightly tweaked [HATEOAS](https://en.wikipedia.org/wiki/HATEOAS) style, but is basically JSON/REST.

## Auto generated HTML UI

To enable exploration of the API for debugging, research by UI engineers or even playing (not for the faint of heart) the API delivers a primitive HTML UI when queried with `Accept: text/html`.

## Forcing JSON output

To enable debugging the JSON output in a browser, adding the query parameter `accept=application/json` will make the server output JSON even to a browser that claims to prefer `text/html`.

## Running locally using Docker (recommended)

- Download Docker
- Navigate to the root directory of this project
- Run `docker build --tag 'diplicity' .`
- Run `docker run -p 8080:8080 -p 8000:8000 diplicity`
- The API is now available on your machine at `localhost:8080`
- The Admin server is now available on your machine at `localhost:8000`

## Running locally

To run it locally

1. Clone this repo.
2. Install the [App Engine SDK for Go](https://cloud.google.com/appengine/docs/go/download).
3. Make sure your `$GOPATH` is set to something reasonable, like `$HOME/go`.
4. Run `dev_appserver.py .` in the checked out directory.
5. Run `curl -XPOST http://localhost:8080/_configure -d '{"FCMConf": {"ServerKey": SERVER_KEY_FROM_FCM}, "OAuth": {"ClientID": CLIENT_ID_FROM_GOOGLE_CLOUD_PROJECT, "Secret": SECRET_FROM_GOOGLE_CLOUD_PROJECT}, "SendGrid": {"APIKey": SEND_GRID_API_KEY}}'`.
   - This isn't necessary to run the server per se, but `FCMConf` is necessary for FCM message sending, `OAuth` is necessary for non `fake-id` login, and `SendGrid` is necessary for email sending.

### Faking user ID

When running the server locally, you can use the query parameter `fake-id` to set a fake user ID for your requests. This makes it possible and easy to test interaction between users without creating multiple Google accounts or even running multiple browsers.

#### Faking user email

When running the server locally, you can also use the query parameter `fake-email` to set the fake email of the fake user ID. This makes it possible and easy to test the email notification system.

## Running the tests

To run the tests

1. Start the local server with a `--clear_datastore` to avoid pre-test clutter and `--datastore_consistency_policy=consistent` to avoid eventual consistency. Since the tests don't wait around for consistency to be achieved, this simplifies writing the tests. Also, use `--require_indexes` so that you verify all the necessary indices are present in `app/index.yaml`. If you find indices for `Game` missing, update and run `go run tools/genindex.go`, for other entity types remove `app/index.yaml`, run `go run tools/genindex.go` and then run the tests without `--require_indexes` to let `dev_appserver.py` add missing indices as it comes across them. The reason `Game` indices are special is that they are built using composite indexes according to https://cloud.google.com/appengine/articles/indexselection.

`dev_appserver.py --require_indexes --skip_sdk_update_check=true --clear_datastore=true --datastore_consistency_policy=consistent .`

2. Run `go test -v` in the `diptest` directory.
