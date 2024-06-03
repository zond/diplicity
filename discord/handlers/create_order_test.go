package handlers

import (
	"errors"
	"testing"

	"github.com/bwmarrin/discordgo"
	gomock "go.uber.org/mock/gomock"
)

func expectPanic(t *testing.T) {
	if r := recover(); r == nil {
		t.Errorf("expected panic")
	}
}

var mockMember = &discordgo.Member{
	User: &discordgo.User{
		ID: "123",
	},
}

var mockUnitOptions = []discordgo.SelectMenuOption{
	{
		Label: "Army Berlin",
		Value: `{"source": "berlin"}`,
	},
}

var expectedResponse = &discordgo.InteractionResponse{
	Type: discordgo.InteractionResponseChannelMessageWithSource,
	Data: &discordgo.InteractionResponseData{
		Title: "Create Order",
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.SelectMenu{
						CustomID:    "source",
						Placeholder: "Select a unit to move",
						Options: []discordgo.SelectMenuOption{
							{
								Label: "Army Berlin",
								Value: `{"source": "berlin"}`,
							},
						},
					},
				},
			},
		},
	},
}

var mockInteraction = InteractionCreate{
	Member:      mockMember,
	Interaction: &discordgo.Interaction{},
}

func TestCreateOrderCommandHandler(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockApi := NewMockApi(mockCtrl)
	mockApi.EXPECT().ListAvailableSourceUnits(gomock.Eq(mockMember.User.ID), gomock.Eq("gameID"), gomock.Eq("phaseID")).Return(mockUnitOptions, nil).AnyTimes()

	mockSession := NewMockSession(mockCtrl)

	t.Run("Calls InteractionRespond with expected response", func(t *testing.T) {
		mockSession.EXPECT().InteractionRespond(gomock.Any(), gomock.Eq(expectedResponse)).Return(nil)
		NewCreateOrderCommandHandler(mockSession, mockInteraction, mockApi)
	})

	t.Run("Panics if InteractionRespond returns an error", func(t *testing.T) {
		mockSession.EXPECT().InteractionRespond(gomock.Any(), gomock.Any()).Return(errors.New("Custom test error"))

		defer expectPanic(t)

		NewCreateOrderCommandHandler(mockSession, mockInteraction, mockApi)
	})

	t.Run("Panics if ListAvailableSourceUnits returns an error", func(t *testing.T) {
		mockApi.EXPECT().ListAvailableSourceUnits(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Custom test error")).AnyTimes()
		mockSession.EXPECT().InteractionRespond(gomock.Any(), gomock.Any()).Return(errors.New("Custom test error"))

		defer expectPanic(t)

		NewCreateOrderCommandHandler(mockSession, mockInteraction, mockApi)
	})
}
