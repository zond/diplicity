package routes

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/game"
	"github.com/zond/diplicity/variants"

	. "github.com/zond/goaeoas"
)

func Setup(r *mux.Router) {
	r.Methods("OPTIONS").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		CORSHeaders(w)
	})
	auth.SetupRouter(r)
	game.SetupRouter(r)
	variants.SetupRouter(r)
}
