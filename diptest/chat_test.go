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
}
