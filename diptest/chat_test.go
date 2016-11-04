package diptest

import (
	"sort"
	"strings"
	"testing"

	"github.com/zond/diplicity/game"
)

func testChat(t *testing.T) {
	games := make([]*Result, len(startedGameEnvs))
	nations := make([]string, len(startedGameEnvs))

	for i, env := range startedGameEnvs {
		games[i] = env.GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)

		nations[i] = games[i].
			Find([]string{"Properties", "Members"}, []string{"User", "Id"}, startedGameEnvs[i].GetUID()).GetValue("Nation").(string)
	}

	games[0].Follow("channels", "Links").Success().AssertEmpty("Properties")
	games[1].Follow("channels", "Links").Success().AssertEmpty("Properties")
	games[2].Follow("channels", "Links").Success().AssertEmpty("Properties")

	msg1 := String("message")

	members := sort.StringSlice{nations[0], nations[1]}
	sort.Sort(members)
	chanName := strings.Join(members, ",")

	games[0].Follow("channels", "Links").Success().
		Follow("message", "Links").Body(map[string]interface{}{
		"Body":           msg1,
		"ChannelMembers": members,
	}).Success()

	games[0].Follow("channels", "Links").Success().
		Find([]string{"Properties"}, []string{"Name"}, chanName)
	games[1].Follow("channels", "Links").Success().
		Find([]string{"Properties"}, []string{"Name"}, chanName)
	games[2].Follow("channels", "Links").Success().AssertEmpty("Properties")

	games[0].Follow("channels", "Links").Success().
		Find([]string{"Properties"}, []string{"Name"}, chanName).
		Follow("messages", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Body"}, msg1)

	games[1].Follow("channels", "Links").Success().
		Find([]string{"Properties"}, []string{"Name"}, chanName).
		Follow("messages", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Body"}, msg1)

	outsiderGame := NewEnv().SetUID(String("fake")).GetRoute(game.IndexRoute).Success().
		Follow("started-games", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Desc"}, startedGameDesc)

	outsiderGame.Follow("channels", "Links").Success().
		AssertEmpty("Properties")

	msg2 := String("message")

	games[0].Follow("channels", "Links").Success().
		Follow("message", "Links").Body(map[string]interface{}{
		"Body":           msg2,
		"ChannelMembers": nations,
	}).Success()

	sortedNations := make([]string, len(nations))
	copy(sortedNations, nations)
	sort.Sort(sort.StringSlice(sortedNations))

	for i := range startedGameEnvs {
		games[i].Follow("channels", "Links").Success().
			Find([]string{"Properties"}, []string{"Name"}, strings.Join(sortedNations, ",")).
			Follow("messages", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Body"}, msg2)
	}

	outsiderGame.Follow("channels", "Links").Success().
		Find([]string{"Properties"}, []string{"Name"}, strings.Join(sortedNations, ",")).
		Follow("messages", "Links").Success().
		Find([]string{"Properties"}, []string{"Properties", "Body"}, msg2)
}
