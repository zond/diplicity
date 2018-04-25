package auth

import (
	"net/http"

	"github.com/aymerick/raymond"
	"github.com/zond/go-fcm"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"gopkg.in/sendgrid/sendgrid-go.v2"

	. "github.com/zond/goaeoas"
)

const (
	userConfigKind = "UserConfig"
)

func init() {
	raymond.RegisterHelper("encodeKey", func(key datastore.Key) string {
		return key.Encode()
	})
}

type FCMNotificationConfig struct {
	ClickActionTemplate string `methods:"PUT" datastore:",noindex"`
	TitleTemplate       string `methods:"PUT" datastore:",noindex"`
	BodyTemplate        string `methods:"PUT" datastore:",noindex"`
}

func (f *FCMNotificationConfig) Customize(ctx context.Context, notif *fcm.NotificationPayload, data interface{}) {
	if f.TitleTemplate != "" {
		if customTitle, err := raymond.Render(f.TitleTemplate, data); err == nil {
			notif.Title = customTitle
		} else {
			log.Infof(ctx, "Broken TitleTemplate %q: %v", f.TitleTemplate, err)
		}
	}
	if f.BodyTemplate != "" {
		if customBody, err := raymond.Render(f.BodyTemplate, data); err == nil {
			notif.Body = customBody
		} else {
			log.Infof(ctx, "Broken BodyTemplate %q: %v", f.BodyTemplate, err)
		}
	}
	if f.ClickActionTemplate != "" {
		if customClickAction, err := raymond.Render(f.ClickActionTemplate, data); err == nil {
			notif.ClickAction = customClickAction
		} else {
			log.Infof(ctx, "Broken ClickActionTemplate %q: %v", f.ClickActionTemplate, err)
		}
	}
}

func (f *FCMNotificationConfig) Validate() error {
	if f.ClickActionTemplate != "" {
		if _, err := raymond.Parse(f.ClickActionTemplate); err != nil {
			return err
		}
	}
	if f.TitleTemplate != "" {
		if _, err := raymond.Parse(f.TitleTemplate); err != nil {
			return err
		}
	}
	if f.BodyTemplate != "" {
		if _, err := raymond.Parse(f.BodyTemplate); err != nil {
			return err
		}
	}
	return nil
}

type MailNotificationConfig struct {
	SubjectTemplate  string `methods:"PUT" datastore:",noindex"`
	TextBodyTemplate string `methods:"PUT" datastore:",noindex"`
	HTMLBodyTemplate string `methods:"PUT" datastore:",noindex"`
}

func (m *MailNotificationConfig) Validate() error {
	if m.SubjectTemplate != "" {
		if _, err := raymond.Parse(m.SubjectTemplate); err != nil {
			return err
		}
	}
	if m.TextBodyTemplate != "" {
		if _, err := raymond.Parse(m.TextBodyTemplate); err != nil {
			return err
		}
	}
	if m.HTMLBodyTemplate != "" {
		if _, err := raymond.Parse(m.HTMLBodyTemplate); err != nil {
			return err
		}
	}
	return nil
}

func (m *MailNotificationConfig) Customize(ctx context.Context, msg *sendgrid.SGMail, data interface{}) {
	if m.SubjectTemplate != "" {
		if customSubject, err := raymond.Render(m.SubjectTemplate, data); err == nil {
			msg.Subject = customSubject
		} else {
			log.Infof(ctx, "Broken SubjectTemplate %q: %v", m.SubjectTemplate, err)
		}
	}
	if m.TextBodyTemplate != "" {
		if customTextBody, err := raymond.Render(m.TextBodyTemplate, data); err == nil {
			msg.SetText(customTextBody)
		} else {
			log.Infof(ctx, "Broken TextBodyTemplate %q: %v", m.TextBodyTemplate, err)
		}
	}
	if m.HTMLBodyTemplate != "" {
		if customHTMLBody, err := raymond.Render(m.HTMLBodyTemplate, data); err == nil {
			msg.SetHTML(customHTMLBody)
		} else {
			log.Infof(ctx, "Broken HTMLBodyTemplate %q: %v", m.HTMLBodyTemplate, err)
		}
	}
}

type FCMToken struct {
	Value         string                `methods:"PUT"`
	Disabled      bool                  `methods:"PUT"`
	Note          string                `methods:"PUT" datastore:",noindex"`
	App           string                `methods:"PUT"`
	MessageConfig FCMNotificationConfig `methods:"PUT"`
	PhaseConfig   FCMNotificationConfig `methods:"PUT"`
	ReplaceToken  string                `methods:"PUT"`
}

type UnsubscribeConfig struct {
	RedirectTemplate string `methods:"PUT"`
	HTMLTemplate     string `methods:"PUT"`
}

func (u *UnsubscribeConfig) Validate() error {
	if u.RedirectTemplate != "" {
		if _, err := raymond.Parse(u.RedirectTemplate); err != nil {
			return err
		}
	}
	if u.HTMLTemplate != "" {
		if _, err := raymond.Parse(u.HTMLTemplate); err != nil {
			return err
		}
	}
	return nil
}

type MailConfig struct {
	Enabled           bool                   `methods:"PUT"`
	UnsubscribeConfig UnsubscribeConfig      `methods:"PUT"`
	MessageConfig     MailNotificationConfig `methods:"PUT"`
	PhaseConfig       MailNotificationConfig `methods:"PUT"`
}

func (m *MailConfig) Validate() error {
	if err := m.MessageConfig.Validate(); err != nil {
		return err
	}
	if err := m.PhaseConfig.Validate(); err != nil {
		return err
	}
	if err := m.UnsubscribeConfig.Validate(); err != nil {
		return err
	}
	return nil
}

type UserConfig struct {
	UserId     string
	FCMTokens  []FCMToken `methods:"PUT"`
	MailConfig MailConfig `methods:"PUT"`
	Colors     []string   `methods:"PUT"`
}

var UserConfigResource = &Resource{
	Load:     loadUserConfig,
	Update:   updateUserConfig,
	FullPath: "/User/{user_id}/UserConfig",
}

func UserConfigID(ctx context.Context, userID *datastore.Key) *datastore.Key {
	return datastore.NewKey(ctx, userConfigKind, "config", 0, userID)
}

func (u *UserConfig) ID(ctx context.Context) *datastore.Key {
	return UserConfigID(ctx, UserID(ctx, u.UserId))
}

func (u *UserConfig) Item(r Request) *Item {
	return NewItem(u).SetName("user-config").
		AddLink(r.NewLink(UserConfigResource.Link("self", Load, []string{"user_id", u.UserId}))).
		AddLink(r.NewLink(UserConfigResource.Link("update", Update, []string{"user_id", u.UserId}))).
		SetDesc([][]string{
			[]string{
				"User configuration",
				"Each diplicity user has exactly one user configuration. User configurations defined user selected configuration for all of diplicty, such as which FCM tokens should be notified of new press or new phases.",
			},
			[]string{
				"FCM tokens",
				"Each FCM token has several fields.",
				"A value, which is the registration ID received when registering with FCM.",
				"A disabled flag which will turn notification to that token off, and which the server toggles if FCM returns errors when notifications are sent to that token.",
				"A note field, which the server will populate with the reason the token was disabled.",
				"An app field, which the app populating the token can use to identify tokens belonging to it to avoid removing/updating tokens belonging to other apps.",
				"Two template fields, one for phase and one for message notifications.",
				"Each token also has a `ReplaceToken` defined by the client. Defining a `ReplaceToken` other than the empty strings allows the client to replace the `Value` in the token without requiring a regular authentication token.",
			},
			[]string{
				"ReplaceToken",
				"To use the `ReplaceToken` to replace the `Value` of your FCM token, `PUT` a JSON body containing `{ 'Value': 'new token value' }` to `/User/{user_id}/FCMToken/{replace_token}/Replace`.",
			},
			[]string{
				"FCM templates",
				"The FCM templates define the title, body and click action of the FCM notifications sent out.",
				"They are parsed by a Handlebars parser (https://github.com/aymerick/raymond).",
			},
			[]string{
				"New phase FCM notifications",
				"FCM notifications for new phases will have the payload `{ DiplicityJSON: DATA }` where DATA is `{ phaseMeta: [phase JSON], gameID: [game ID], type: 'phase' }` compressed with libz. The on click action will open an HTML page displaying the map of the new phase.",
			},
			[]string{
				"New message FCM notifications",
				"FCM notifications for new messages will have the payload `{ DiplicityJSON: DATA }` where DATA is `{ message: [message JSON], type: 'message' }` compressed with libz.",
			},
			[]string{
				"Email config",
				"A user has an email config, defining if and how this user should receive email about new phases and messages.",
				"The email config contains several fields.",
				"An enabled flag which turns email notifications on.",
				"Information about whether the unsubscribe link in the email should render some HTML or redirect to another host, defined by two Handlebars templates, one for the redirect link and one for the HTML to display.",
				"Two template fields, one for phase and one for message notifications.",
				"All templates will be parsed by the same parser as the FCM templates.",
			},
		})
}

func loadUserConfig(w ResponseWriter, r Request) (*UserConfig, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	config := &UserConfig{}
	if err := datastore.Get(ctx, UserConfigID(ctx, user.ID(ctx)), config); err == datastore.ErrNoSuchEntity {
		config.UserId = user.Id
		err = nil
	} else if err != nil {
		return nil, err
	}

	return config, nil
}

func updateUserConfig(w ResponseWriter, r Request) (*UserConfig, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*User)
	if !ok {
		return nil, HTTPErr{"unauthenticated", http.StatusUnauthorized}
	}

	config := &UserConfig{}
	if err := Copy(config, r, "PUT"); err != nil {
		return nil, err
	}
	config.UserId = user.Id

	for _, token := range config.FCMTokens {
		if err := token.MessageConfig.Validate(); err != nil {
			return nil, err
		}
		if err := token.PhaseConfig.Validate(); err != nil {
			return nil, err
		}
	}

	if err := config.MailConfig.Validate(); err != nil {
		return nil, err
	}

	if _, err := datastore.Put(ctx, config.ID(ctx), config); err != nil {
		return nil, err
	}

	return config, nil
}
