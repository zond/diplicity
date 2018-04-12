package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/zond/diplicity/game"

	. "github.com/zond/diplicity/diptest"
)

func main() {
	gameDesc := flag.String("gameDesc", "", "Game desc for game actions.")
	uids := flag.String("uids", "", "',' separated list of user Ids to handle for user actions.")
	emails := flag.String("emails", "", "',' separated list of emails to use when faking the provided uids.")

	cmdFuncs := map[string]func(){
		"resolvePhase": func() {
			fmt.Printf("Resolving %q...", *gameDesc)
			game := NewEnv().SetUID(String("fake")).GetRoute(game.ListStartedGamesRoute).Success().
				Find(*gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			gameURLString := game.Find("self", []string{"Links"}, []string{"Rel"}).GetValue("URL").(string)
			phaseOrdinal := int(game.GetValue("Properties", "NewestPhaseMeta").([]interface{})[0].(map[string]interface{})["PhaseOrdinal"].(float64))
			members := game.GetValue("Properties", "Members").([]interface{})
			for _, member := range members {
				NewEnv().SetUID(member.(map[string]interface{})["User"].(map[string]interface{})["Id"].(string)).GetURL(gameURLString).Success().
					Follow("phases", "Links").Success().
					Find(phaseOrdinal, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
					Follow("phase-states", "Links").Success().
					Find(phaseOrdinal, []string{"Properties"}, []string{"Properties", "PhaseOrdinal"}).
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": true,
				}).Success()
			}
			fmt.Println("Success")
		},
		"startGame": func() {
			fmt.Printf("Joining %q with enough users to start it...", *gameDesc)
			started := false
			var missing *int
			for !started {
				env := NewEnv().SetUID(String("fake"))
				game := env.GetRoute(game.ListOpenGamesRoute).Success().
					Find(*gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
				if missing == nil {
					realMissing := 7 - int(game.GetValue("Properties", "NMembers").(float64))
					missing = &realMissing
				}
				started = game.GetValue("Properties", "Started").(bool)
				if !started {
					game.Follow("join", "Links").Success()
					(*missing)--
				}
				if *missing == 0 {
					break
				}
			}
			fmt.Println("Success")
		},
		"addToGame": func() {
			fmt.Printf("Adding %+v to %q...", *uids, *gameDesc)
			emailSlice := strings.Split(*emails, ",")
			for i, uid := range strings.Split(*uids, ",") {
				NewEnv().SetUID(uid).SetEmail(emailSlice[i]).GetRoute(game.ListOpenGamesRoute).Success().
					Find(*gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
					Follow("join", "Links").Success()
			}
			fmt.Println("Success")
		},
		"createGame": func() {
			fmt.Printf("Creating game %q...", *gameDesc)
			env := NewEnv().SetUID(String("fake"))
			env.GetURL("/").Success().
				Follow("create-game", "Links").Body(map[string]interface{}{
				"Desc":    *gameDesc,
				"Variant": "Classical",
				"NoMerge": true,
			}).Success()
			fmt.Println("Success")
		},
	}
	cmdNames := []string{}
	for k := range cmdFuncs {
		cmdNames = append(cmdNames, k)
	}

	cmds := flag.String("cmds", "", fmt.Sprintf("What to do, a ',' separated list of %+v.", cmdNames))

	flag.Parse()

	for _, cmd := range strings.Split(*cmds, ",") {
		cmdFuncs[cmd]()
	}
}
