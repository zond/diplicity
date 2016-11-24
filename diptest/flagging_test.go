package diptest

import (
	"sort"
	"strings"
	"testing"

	"github.com/zond/diplicity/game"
)

func testMessageFlagging(t *testing.T) {
	msg1 := String("message")

	members := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
	sort.Sort(members)
	chanName := strings.Join(members, ",")

	messageCreatedAt := startedGames[0].Follow("channels", "Links").Success().
		Follow("message", "Links").Body(map[string]interface{}{
		"Body":           msg1,
		"ChannelMembers": members,
	}).Success().GetValue("Properties", "CreatedAt").(string)

	messages := startedGames[0].Follow("channels", "Links").Success().
		Find(chanName, []string{"Properties"}, []string{"Name"}).
		Follow("messages", "Links").Success()

	messages.Find(msg1, []string{"Properties"}, []string{"Properties", "Body"})

	messages.Follow("flag-messages", "Links").Body(map[string]interface{}{
		"From": messageCreatedAt,
		"To":   messageCreatedAt,
	}).Success()

	messages.Follow("flag-messages", "Links").Body(map[string]interface{}{
		"From": messageCreatedAt,
		"To":   messageCreatedAt,
	}).Failure()

	flagged := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
		Follow("flagged-messages", "Links").Success()

	flagged.Find(startedGameEnvs[0].GetUID(), []string{"Properties"}, []string{"Properties", "UserId"})

	flagged.AssertRel("create-ban", "Links")

}
