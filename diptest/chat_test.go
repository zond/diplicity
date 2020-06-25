package diptest

import (
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/zond/diplicity/game"
	"github.com/zond/godip/variants/classical"
)

func testChat(t *testing.T) {
	msg1 := String("message")

	members := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
	sort.Sort(members)
	chanName := strings.Join(members, ",")

	WaitForEmptyQueue("game-asyncSendMsg")
	WaitForEmptyQueue("game-sendMsgNotificationsToUsers")

	t.Run("TestChatIsolationBetweenMembers", func(t *testing.T) {
		startedGames[0].Follow("channels", "Links").Success().AssertLen(1, "Properties")
		startedGames[1].Follow("channels", "Links").Success().AssertLen(1, "Properties")
		startedGames[2].Follow("channels", "Links").Success().AssertLen(1, "Properties")

		startedGameEnvs[1].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameID, []string{"Properties"}, []string{"Properties", "ID"}).
			Find(startedGameNats[1], []string{"Properties", "Members"}, []string{"Nation"}).
			AssertEq(3.0, "UnreadMessages")

		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameID, []string{"Properties"}, []string{"Properties", "ID"}).
			Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
			AssertEq(3.0, "UnreadMessages")

		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": members,
		}).Success()

		WaitForEmptyQueue("game-sendMsgNotificationsToUsers")

		startedGameEnvs[1].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameID, []string{"Properties"}, []string{"Properties", "ID"}).
			Find(startedGameNats[1], []string{"Properties", "Members"}, []string{"Nation"}).
			AssertEq(5.0, "UnreadMessages")

		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameID, []string{"Properties"}, []string{"Properties", "ID"}).
			Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
			AssertEq(3.0, "UnreadMessages")

		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"})
		startedGames[1].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"})
		startedGames[2].Follow("channels", "Links").Success().AssertLen(1, "Properties")

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
			AssertLen(1, "Properties")

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
		oldLatestMessage := startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			AssertEq(2.0, "Properties", "NMessages").
			AssertEq(0.0, "Properties", "NMessagesSince", "NMessages").
			GetValue("Properties", "LatestMessage").(map[string]interface{})
		bdy := String("body")
		newMess := startedGames[1].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           bdy,
			"ChannelMembers": members,
		}).Success().GetValue("Properties").(map[string]interface{})
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameID, []string{"Properties"}, []string{"Properties", "ID"}).
			Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
			AssertEq(float64(0), "UnreadMessages")
		latestMessage := startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			GetValue("Properties", "LatestMessage").(map[string]interface{})
		if latestMessage["Body"].(string) == oldLatestMessage["Body"].(string) {
			t.Errorf("Got LatestMessage %+v, wanted something different from %+v", latestMessage, oldLatestMessage)
		}
		if latestMessage["Body"].(string) != newMess["Body"].(string) {
			t.Errorf("Got LatestMessage %+v, wanted %v", latestMessage, newMess)
		}
		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			AssertEq(3.0, "Properties", "NMessages").
			AssertEq(1.0, "Properties", "NMessagesSince", "NMessages").
			Follow("messages", "Links").Success()
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(startedGameID, []string{"Properties"}, []string{"Properties", "ID"}).
			Find(startedGameNats[0], []string{"Properties", "Members"}, []string{"Nation"}).
			AssertEq(float64(0), "UnreadMessages")
		startedGames[0].Follow("channels", "Links").Success().
			Find(chanName, []string{"Properties"}, []string{"Name"}).
			AssertEq(3.0, "Properties", "NMessages").
			AssertEq(0.0, "Properties", "NMessagesSince", "NMessages")
	})

	t.Run("TestNMessagesGroupChat", func(t *testing.T) {
		members := sort.StringSlice{startedGameNats[0], startedGameNats[1], startedGameNats[2]}
		sort.Sort(members)
		chanName := strings.Join(members, ",")

		for i := 0; i < 3; i++ {
			startedGames[i].Follow("channels", "Links").Success().
				AssertNotFind(chanName, []string{"Properties"}, []string{"Name"})
		}

		bdy := String("body")

		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           bdy,
			"ChannelMembers": members,
		}).Success()

		WaitForEmptyQueue("game-sendMsgNotificationsToUsers")

		for i := 0; i < 3; i++ {
			startedGames[i].Follow("channels", "Links").Success().
				Find(chanName, []string{"Properties"}, []string{"Name"}).
				AssertEq(2.0, "Properties", "NMessages").
				AssertEq(2.0, "Properties", "NMessagesSince", "NMessages").
				Follow("messages", "Links").Success()
			startedGames[i].Follow("channels", "Links").Success().
				Find(chanName, []string{"Properties"}, []string{"Name"}).
				AssertEq(2.0, "Properties", "NMessages").
				AssertEq(0.0, "Properties", "NMessagesSince", "NMessages")
		}
	})
}

func TestDisabledChats(t *testing.T) {
	t.Run("AllDisabled", func(t *testing.T) {
		withStartedGameOpts(func(opts map[string]interface{}) {
			opts["DisableConferenceChat"] = true
			opts["DisableGroupChat"] = true
			opts["DisablePrivateChat"] = true
		}, func() {
			channels := startedGames[1].Follow("channels", "Links").Success()
			channels.AssertNotFind("message", []string{"Links"}, []string{"Rel"})
			startedGameEnvs[1].GetRoute("Message.Create").RouteParams("game_id", startedGameID).Body(map[string]interface{}{
				"Body":           String("body"),
				"ChannelMembers": classical.Nations,
			}).Failure()
			for _, game := range startedGames {
				game.Follow("phases", "Links").Success().
					Find("Movement", []string{"Properties"}, []string{"Properties", "Type"}).
					Follow("phase-states", "Links").Success().
					Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
					Follow("update", "Links").Body(map[string]interface{}{
					"ReadyToResolve": true,
					"WantsDIAS":      true,
				}).Success()
			}
		})
		WaitForEmptyQueue("game-asyncResolvePhase")
		startedGames[1].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           String("body"),
			"ChannelMembers": classical.Nations,
		}).Success()
	})
	t.Run("ConferenceChat", func(t *testing.T) {
		t.Run("Enabled", func(t *testing.T) {
			withStartedGame(func() {
				startedGames[1].Follow("channels", "Links").Success().
					Follow("message", "Links").Body(map[string]interface{}{
					"Body":           String("body"),
					"ChannelMembers": classical.Nations,
				}).Success()
			})
		})
		t.Run("Disabled", func(t *testing.T) {
			withStartedGameOpts(func(opts map[string]interface{}) {
				opts["DisableConferenceChat"] = true
			}, func() {
				startedGames[1].Follow("channels", "Links").Success().
					Follow("message", "Links").Body(map[string]interface{}{
					"Body":           String("body"),
					"ChannelMembers": classical.Nations,
				}).Failure()
			})
		})
	})
	t.Run("GroupChat", func(t *testing.T) {
		t.Run("Enabled", func(t *testing.T) {
			withStartedGame(func() {
				members := sort.StringSlice{startedGameNats[0], startedGameNats[1], startedGameNats[2]}
				sort.Sort(members)
				startedGames[1].Follow("channels", "Links").Success().
					Follow("message", "Links").Body(map[string]interface{}{
					"Body":           String("body"),
					"ChannelMembers": members,
				}).Success()
			})
		})
		t.Run("Disabled", func(t *testing.T) {
			withStartedGameOpts(func(opts map[string]interface{}) {
				opts["DisableGroupChat"] = true
			}, func() {
				members := sort.StringSlice{startedGameNats[0], startedGameNats[1], startedGameNats[2]}
				sort.Sort(members)
				startedGames[1].Follow("channels", "Links").Success().
					Follow("message", "Links").Body(map[string]interface{}{
					"Body":           String("body"),
					"ChannelMembers": members,
				}).Failure()
			})
		})
	})
	t.Run("PrivateChat", func(t *testing.T) {
		t.Run("Enabled", func(t *testing.T) {
			withStartedGame(func() {
				members := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
				sort.Sort(members)
				startedGames[1].Follow("channels", "Links").Success().
					Follow("message", "Links").Body(map[string]interface{}{
					"Body":           String("body"),
					"ChannelMembers": members,
				}).Success()
			})
		})
		t.Run("Disabled", func(t *testing.T) {
			withStartedGameOpts(func(opts map[string]interface{}) {
				opts["DisablePrivateChat"] = true
			}, func() {
				members := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
				sort.Sort(members)
				startedGames[1].Follow("channels", "Links").Success().
					Follow("message", "Links").Body(map[string]interface{}{
					"Body":           String("body"),
					"ChannelMembers": members,
				}).Failure()
			})
		})
	})
}

func TestNonMemberSeeingAllMessagesInFinishedGames(t *testing.T) {
	withStartedGame(func() {
		msg := String("message")
		chanMembers := sort.StringSlice{startedGameNats[0], startedGameNats[1]}
		sort.Sort(chanMembers)
		startedGames[0].Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg,
			"ChannelMembers": chanMembers,
		}).Success()

		startedGames[0].Follow("channels", "Links").Success().
			Find(strings.Join(chanMembers, ","), []string{"Properties"}, []string{"Name"}).
			Find(2.0, []string{"Properties", "NMessages"}).
			Follow("messages", "Links").Success().
			Find(game.DiplicitySender, []string{"Properties"}, []string{"Properties", "Sender"}).
			Find("Remember that all messages become public once the game finishes.", []string{"Properties", "Body"})

		newEnv := NewEnv().SetUID(String("fake"))

		extGame := newEnv.GetRoute(game.ListStartedGamesRoute).Success().
			Find(startedGameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		extGame.Follow("channels", "Links").Success().
			AssertNotRel("message", "Links").
			AssertLen(1, "Properties")

		for idx, game := range startedGames {
			p := game.Follow("phases", "Links").Success().
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

			p.Follow("phase-states", "Links").Success().
				Find(startedGameNats[idx], []string{"Properties"}, []string{"Properties", "Nation"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      true,
			}).Success()
		}

		WaitForEmptyQueue("game-asyncResolvePhase")

		extGame.Follow("channels", "Links").Success().
			AssertNotRel("message", "Links")
		extGame.Follow("channels", "Links").Success().
			AssertLen(2, "Properties").
			Find(startedGameNats[0], []string{"Properties"}, []string{"Properties", "LatestMessage", "Sender"}).
			Follow("messages", "Links").Success().
			Find(msg, []string{"Properties"}, []string{"Properties", "Body"})

		WaitForEmptyQueue("game-reRateTrueSkills")
		WaitForEmptyQueue("game-updateUserStats")
		WaitForEmptyQueue("game-updateUserStat")
		for idx, env := range startedGameEnvs {
			wantedScore := 14.0
			wantedRating := 10.0
			if startedGameNats[idx] == "Russia" {
				wantedScore = 16.0
				wantedRating = 12.0
			}
			if foundScore := env.GetRoute("GameResult.Load").RouteParams("game_id", startedGameID).Success().
				Find(env.GetUID(), []string{"Properties", "Scores"}, []string{"UserId"}).GetValue("Score").(float64); math.Round(foundScore) != wantedScore {
				t.Errorf("Got score %v for %v, wanted %v", foundScore, startedGameNats[idx], wantedScore)
			}
			if foundRating := env.GetRoute("UserStats.Load").RouteParams("user_id", env.GetUID()).Success().
				GetValue("Properties", "TrueSkill", "Rating").(float64); math.Round(foundRating) != wantedRating {
				t.Errorf("Got rating %v for %v, wanted %v", foundRating, startedGameNats[idx], wantedRating)
			}
		}

	})
}
