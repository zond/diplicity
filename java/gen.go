package main

import (
	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"
	"github.com/zond/goaeoas"
)

func main() {
	router := mux.NewRouter()
	routes.Setup(router)
	if err := goaeoas.GenerateJava("classes"); err != nil {
		panic(err)
	}
}
