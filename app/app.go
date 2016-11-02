package app

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"
)

func init() {
	router := mux.NewRouter()
	routes.Setup(router)
	http.Handle("/", router)
}
