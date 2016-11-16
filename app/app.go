package app

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"

	. "github.com/zond/goaeoas"
)

func init() {
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
	router := mux.NewRouter()
	routes.Setup(router)
	http.Handle("/", router)
}
