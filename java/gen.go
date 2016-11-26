package main

import (
	"flag"
	"os"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"
	"github.com/zond/goaeoas"
)

func main() {
	dir := flag.String("dir", "", "Where to put the generated files.")
	flag.Parse()

	if *dir == "" {
		flag.Usage()
		os.Exit(1)
	}

	router := mux.NewRouter()
	routes.Setup(router)
	os.MkdirAll(*dir, 0755)
	if err := goaeoas.GenerateJava(*dir); err != nil {
		panic(err)
	}
}
