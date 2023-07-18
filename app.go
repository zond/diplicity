package main

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"
	"google.golang.org/appengine/v2"

	. "github.com/zond/goaeoas"
)

func main() {
	jsonFormURL, err := url.Parse("/js/jsonform.js")
	if err != nil {
		panic(err)
	}
	SetJSONFormURL(jsonFormURL)
	jsvURL, err := url.Parse("/js/jsv.js")
	if err != nil {
		panic(err)
	}
	SetJSVURL(jsvURL)
	if appengine.IsDevAppServer() {
		DefaultScheme = "http"
	} else {
		DefaultScheme = "https"
	}
	router := mux.NewRouter()
	routes.Setup(router)
	http.Handle("/", router)
	appengine.Main()
}
