package main

import (
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/zond/diplicity/routes"
	"github.com/zond/goaeoas"
)

func main() {
	router := mux.NewRouter()
	routes.Setup(router)
	dir := filepath.Join("classes", "diplicity")
	os.MkdirAll(dir, 0755)
	if err := goaeoas.GenerateJava(dir); err != nil {
		panic(err)
	}
}
