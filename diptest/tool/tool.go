package main

import (
	"flag"
	"fmt"

	"github.com/zond/diplicity/game"

	. "github.com/zond/diplicity/diptest"
)

func main() {
	gameDesc := flag.String("gameDesc", "", "Game desc for game actions.")

	cmds := map[string]func(){
		"startGame": func() {
			started := false
			for !started {
				env := NewEnv().SetUID(String("fake"))
				game := env.GetRoute(game.ListOpenGamesRoute).Success().
					Find(*gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
				started = game.GetValue("Properties", "Started").(bool)
				if !started {
					game.Follow("join", "Links").Success()
				}
			}
		},
	}
	cmdNames := []string{}
	for k := range cmds {
		cmdNames = append(cmdNames, k)
	}

	cmd := flag.String("cmd", "", fmt.Sprintf("What to do, one of %+v.", cmdNames))

	flag.Parse()

	cmds[*cmd]()
}
