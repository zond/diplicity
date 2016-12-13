package diptest

import (
	"sort"
	"strings"
	"testing"

	"github.com/zond/diplicity/game"
)

func testChat(t *testing.T) {
	msg1 := String("message")

	members := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
	sort.Sort(members)
	chanName := strings.Join(members, ",")

	t.Run("TestChatIsolationBetweenMembers", func(t *testing.T) {
		startedGames[0].Follow("channels", "Links").Success().AssertEmpty("Properties")
		startedGames[1].Follow("channels", "Links").Success().AssertEmpty("Properties")
		startedGames[2].Follow("channels", "Links").Success().AssertEmpty("Properties")

		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": members,
		}).Success()

		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"})
		startedGames[1].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"})
		startedGames[2].Follow("channels", "Links").Success().AssertEmpty("Properties")

		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			Follow("messages", "Links").Success().
			Find(msg1, []string{"Properties"}, []string{"Properties", "Body"})

		startedGames[1].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			Follow("messages", "Links").Success().
			Find(msg1, []string{"Properties"}, []string{"Properties", "Body"})
	})

	t.Run("TestMuting", func(t *testing.T) {
		startedGames[1].Follow("game-states", "Links").Success().
			Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
			Follow("update", "Links").Body(map[string]interface{}{
			"Muted": []string{startedGameNats[0]},
		}).Success()
		startedGames[1].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			Follow("messages", "Links").Success().
			AssertNotFind(msg1, []string{"Properties"}, []string{"Properties", "Body"})
		startedGames[1].Follow("game-states", "Links").Success().
			Find(startedGameNats[1], []string{"Properties"}, []string{"Properties", "Nation"}).
			Follow("update", "Links").Body(map[string]interface{}{
			"Muted": []string{},
		}).Success()
		startedGames[1].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			Follow("messages", "Links").Success().
			Find(msg1, []string{"Properties"}, []string{"Properties", "Body"})
	})

	t.Run("TestNonMemberSeeingPublicChannelMessages", func(t *testing.T) {
		outsiderGame := NewEnv().SetUID(String("fake")).GetRoute(game.IndexRoute).Success().
			Follow("started-games", "Links").Success().
			Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		outsiderGame.Follow("channels", "Links").Success().
			AssertEmpty("Properties")

		msg2 := String("message")

		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg2,
			"ChannelMembers": startedGameNats,
		}).Success()

		sortedNations := make([]string, len(startedGameNats))
		copy(sortedNations, startedGameNats)
		sort.Sort(sort.StringSlice(sortedNations))
		chanName := strings.Join(sortedNations, ",")

		for i := range startedGameEnvs {
			startedGames[i].Follow("channels", "Links").Success().
				Find(chanName, []string{"Properties"}, []string{"Name"}).
				Follow("messages", "Links").Success().
				Find(msg2, []string{"Properties"}, []string{"Properties", "Body"})
		}

		outsiderGame.Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			Follow("messages", "Links").Success().
			Find(msg2, []string{"Properties"}, []string{"Properties", "Body"})
	})

	t.Run("TestNMessages", func(t *testing.T) {
		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			AssertEq(1.0, "Properties", "NMessages").
			AssertEq(0.0, "Properties", "NMessagesSince", "NMessages")
		bdy := String("body")
		startedGames[1].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           bdy,
			"ChannelMembers": members,
		}).Success()
		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			AssertEq(2.0, "Properties", "NMessages").
			AssertEq(1.0, "Properties", "NMessagesSince", "NMessages").
			Follow("messages", "Links").Success()
		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			AssertEq(2.0, "Properties", "NMessages").
			AssertEq(0.0, "Properties", "NMessagesSince", "NMessages")
	})
}

func TestNonMemberSeeingAllMessagesInFinishedGames(t *testing.T) {
	withStartedGame(func() {
		msg := String("message")
		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg,
			"ChannelMembers": []string{startedGameNats[0], startedGameNats[1]},
		}).Success()

		newEnv := NewEnv().SetUID(String("fake"))

		extGame := newEnv.GetRoute(game.ListStartedGamesRoute).Success().
			Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		extGame.Follow("channels", "Links").Success().
			AssertNotRel("message", "Links").
			AssertLen(0, "Properties")

		for _, game := range startedGames {
			p := game.Follow("phases", "Links").Success().
				Find("Spring", []string{"Properties"}, []string{"Properties", "Season"})

			p.Follow("phase-states", "Links").Success().
				Find("", []string{"Properties"}, []string{"Properties", "Note"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      true,
			}).Success()
		}

		extGame.Follow("channels", "Links").Success().
			AssertNotRel("message", "Links")
		extGame.Follow("channels", "Links").Success().
			AssertLen(1, "Properties").
			Find(1, []string{"Properties"}, []string{"Properties", "NMessages"}).
			Follow("messages", "Links").Success().
			Find(msg, []string{"Properties"}, []string{"Properties", "Body"})

	})
}
