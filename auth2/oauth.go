package auth2

import (
	"context"
	"net/url"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/appengine/datastore"
)

type OAuth struct {
	ClientID string
	Secret   string
}

func (o *OAuth) GetOAuthConfig(redirectUrl *url.URL) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.Secret,
		RedirectURL:  redirectUrl.String(),
		Scopes: []string{
			"openid",
			"profile",
			"email",
		},
		Endpoint: google.Endpoint,
	}
}

type OAuthSingleton struct {
	instance *OAuth
	lock     sync.RWMutex
}

func (o *OAuthSingleton) GetOAuth(ctx context.Context) (*OAuth, error) {
	o.lock.RLock()
	defer o.lock.RUnlock()
	if o.instance == nil {
		o.instance = &OAuth{}
		if err := datastore.Get(ctx, o.GetOAuthKey(ctx), o.instance); err != nil {
			return nil, err
		}
	}
	return o.instance, nil
}

func (o *OAuthSingleton) GetOAuthKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, OAUTH_KIND, PROD_KEY, 0, nil)
}

var oauthSingleton = &OAuthSingleton{
	instance: nil,
	lock:     sync.RWMutex{},
}
