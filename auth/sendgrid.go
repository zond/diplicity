package auth

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"golang.org/x/net/context"
	"google.golang.org/appengine/v2/datastore"
	"google.golang.org/appengine/v2/log"
	"google.golang.org/appengine/v2/urlfetch"

	. "github.com/zond/goaeoas"
)

var (
	sendLock         = sync.Mutex{}
	prodSendGrid     *SendGrid
	prodSendGridLock = sync.RWMutex{}
)

const (
	sendGridKind = "SendGrid"
)

type SendGrid struct {
	APIKey string
}

func getSendGridKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, sendGridKind, prodKey, 0, nil)
}

func SetSendGrid(ctx context.Context, sendGrid *SendGrid) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		currentSendGrid := &SendGrid{}
		if err := datastore.Get(ctx, getSendGridKey(ctx), currentSendGrid); err == nil {
			return HTTPErr{"SendGrid already configured", http.StatusBadRequest}
		}
		if _, err := datastore.Put(ctx, getSendGridKey(ctx), sendGrid); err != nil {
			return err
		}
		return nil
	}, &datastore.TransactionOptions{XG: false})
}

func GetSendGrid(ctx context.Context) (*SendGrid, error) {
	prodSendGridLock.RLock()
	if prodSendGrid != nil {
		defer prodSendGridLock.RUnlock()
		return prodSendGrid, nil
	}
	prodSendGridLock.RUnlock()
	prodSendGridLock.Lock()
	defer prodSendGridLock.Unlock()
	foundSendGrid := &SendGrid{}
	if err := datastore.Get(ctx, getSendGridKey(ctx), foundSendGrid); err != nil {
		return nil, err
	}
	prodSendGrid = foundSendGrid
	return prodSendGrid, nil
}

type EMail struct {
	FromAddr       string
	FromName       string
	ToAddr         string
	ToName         string
	Subject        string
	TextBody       string
	HTMLBody       string
	UnsubscribeURL string
	MessageID      string
	Reference      string
}

func (e *EMail) Send(ctx context.Context) error {
	if e.UnsubscribeURL == "" {
		return fmt.Errorf("invalid EMail %+v", e)
	}
	return e.SendWithoutUnsubscribeHeader(ctx)
}

func (e *EMail) SendWithoutUnsubscribeHeader(ctx context.Context) error {
	if e.FromAddr == "" || e.ToAddr == "" || e.Subject == "" || (e.TextBody == "" && e.HTMLBody == "") {
		return fmt.Errorf("invalid EMail %+v", e)
	}

	sendGridConf, err := GetSendGrid(ctx)
	if err != nil {
		return err
	}

	msg := mail.NewV3Mail()
	if e.TextBody != "" {
		msg.AddContent(mail.NewContent(
			"text/plain",
			e.TextBody,
		))
	}
	if e.HTMLBody != "" {
		msg.AddContent(mail.NewContent(
			"text/html",
			e.HTMLBody,
		))
	}
	msg.Subject = e.Subject
	p := mail.NewPersonalization()
	p.AddTos(mail.NewEmail(e.ToName, e.ToAddr))
	msg.AddPersonalizations(p)
	msg.SetFrom(mail.NewEmail(e.FromName, e.FromAddr))
	if e.UnsubscribeURL != "" {
		msg.SetHeader("List-Unsubscribe", fmt.Sprintf("<%s>", e.UnsubscribeURL))
	}
	idGen := func(s string) string {
		return fmt.Sprintf("<%s@diplicity-engine.appspot.com>", s)
	}
	if e.MessageID != "" {
		msg.SetHeader("Message-ID", idGen(e.MessageID))
	}
	if e.Reference != "" {
		msg.SetHeader("References", idGen(e.Reference))
		msg.SetHeader("In-Reply-To", idGen(e.Reference))
	}

	client := sendgrid.NewSendClient(sendGridConf.APIKey)
	sendLock.Lock()
	defer sendLock.Unlock()
	sendgrid.DefaultClient = &rest.Client{HTTPClient: urlfetch.Client(ctx)}
	if resp, err := client.Send(msg); err != nil {
		log.Errorf(ctx, "client.Send(%+v): %+v, %v; hope sendgrid becomes OK again", msg, resp, err)
		return err
	}

	return nil
}
