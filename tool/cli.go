package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
)

func main() {
	token := flag.String("token", "", "Auth token to access the local server.")

	flag.Parse()

	if *token == "" {
		flag.Usage()
		return
	}

	for _, route := range []string{
		"/games/open",
		"/games/started",
		"/games/finished",
		"/games/my/staging",
		"/games/my/started",
		"/games/my/finished",
	} {
		url, err := url.Parse(fmt.Sprintf("http://localhost:8080%s?token=%s", route, *token))
		if err != nil {
			panic(err)
		}
		_, err = http.Get(url.String())
		if err != nil {
			panic(err)
		}
		_, err = http.Get(url.String() + "&variant=Classical")
		if err != nil {
			panic(err)
		}
	}
}
