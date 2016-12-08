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
	pkg := flag.String("pkg", "", "The Java package to put the classes in.")
	flag.Parse()

	if *dir == "" || *pkg == "" {
		flag.Usage()
		os.Exit(1)
	}

	router := mux.NewRouter()
	routes.Setup(router)
	os.MkdirAll(*dir, 0755)
	classes, err := goaeoas.GenerateJava(*pkg)
	if err != nil {
		panic(err)
	}
	if err := goaeoas.DumpJava(*dir, classes); err != nil {
		panic(err)
	}
}
