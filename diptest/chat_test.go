package diptest

import (
	"sort"
	"strings"
	"testing"

	"github.com/zond/diplicity/game"
)

func testChat(t *testing.T) {
	t.Run("TestChatIsolationBetweenMembers", func(t *testing.T) {
		startedGames[0].Follow("channels", "Links").Success().AssertEmpty("Properties")
		startedGames[1].Follow("channels", "Links").Success().AssertEmpty("Properties")
		startedGames[2].Follow("channels", "Links").Success().AssertEmpty("Properties")

		msg1 := String("message")

		members := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
		sort.Sort(members)
		chanName := strings.Join(members, ",")

		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": members,
		}).Success()

		startedGames[0].Follow("channels", "Links").Success().
			Find([]string{"Properties"}, []string{"Name"}, chanName)
		startedGames[1].Follow("channels", "Links").Success().
			Find([]string{"Properties"}, []string{"Name"}, chanName)
		startedGames[2].Follow("channels", "Links").Success().AssertEmpty("Properties")

		startedGames[0].Follow("channels", "Links").Success().
			Find([]string{"Properties"}, []string{"Name"}, chanName).
			Follow("messages", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Body"}, msg1)

		startedGames[1].Follow("channels", "Links").Success().
			Find([]string{"Properties"}, []string{"Name"}, chanName).
			Follow("messages", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Body"}, msg1)
	})

	t.Run("TestNonMemberSeeingPublicChannelMessages", func(t *testing.T) {
		outsiderGame := NewEnv().SetUID(String("fake")).GetRoute(game.IndexRoute).Success().
			Follow("started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)

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

		for i := range startedGameEnvs {
			startedGames[i].Follow("channels", "Links").Success().
				Find([]string{"Properties"}, []string{"Name"}, strings.Join(sortedNations, ",")).
				Follow("messages", "Links").Success().
				Find([]string{"Properties"}, []string{"Properties", "Body"}, msg2)
		}

		outsiderGame.Follow("channels", "Links").Success().
			Find([]string{"Properties"}, []string{"Name"}, strings.Join(sortedNations, ",")).
			Follow("messages", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Body"}, msg2)
	})
}
