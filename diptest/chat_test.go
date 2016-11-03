package diptest

import (
	"sort"
	"strings"

	"github.com/zond/diplicity/game"
)

func testChat(gameDesc string, envs []*Env) {
	games := make([]*Result, len(envs))
	nations := make([]string, len(envs))

	for i, env := range envs {
		games[i] = env.GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find([]string{"Properties"}, []string{"Properties", "Desc"}, gameDesc)

		nations[i] = games[i].
			Find([]string{"Properties", "Members"}, []string{"User", "Id"}, envs[i].GetUID()).GetValue("Nation").(string)

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
}
