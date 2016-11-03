# diplicity

A dippy API service based on [App Engine](https://cloud.google.com/appengine) and [godip](https://github.com/zond/godip).

## Public host

A regularly updated service running this code is available at [https://diplicity-engine.appspot.com/](https://diplicity-engine.appspot.com/).

## Architecture

The API uses a slightly tweaked [HATEOAS](https://en.wikipedia.org/wiki/HATEOAS) style, but is basically JSON/REST.

## Auto generated HTML UI

To enable exploration of the API for debugging, research by UI engineers or even playing (not for the faint of heart) the API delivers a primitive HTML UI when queried with `Accept: text/html`.

## Forcing JSON output

To enable debugging the JSON output in a browser, adding the query parameter `accept=application/json` will make the server output JSON even to a browser that claims to prefer `text/html`.

## Running locally

To run it locally, install the [App Engine SDK for Go](https://cloud.google.com/appengine/docs/go/download) and run `goapp serve` in the `app` directory.

## Running the tests

To run the tests, run `goapp test` in the `diptest` directory.
