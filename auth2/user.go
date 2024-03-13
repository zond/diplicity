package auth2

import (
	"context"
	"time"

	"google.golang.org/appengine/datastore"
)

type User struct {
	Email         string
	FamilyName    string
	Gender        string
	GivenName     string
	Hd            string
	Id            string
	Link          string
	Locale        string
	Name          string
	Picture       string
	VerifiedEmail bool
	ValidUntil    time.Time
}

func (u *User) CreateKey(ctx context.Context, id string) *datastore.Key {
	return datastore.NewKey(ctx, USER_KIND, id, 0, nil)
}

func (u *User) Save(ctx context.Context) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		key := u.CreateKey(ctx, u.Id)
		if _, err := datastore.Put(ctx, key, u); err != nil {
			return err
		}
		return nil
	}, nil)
}

func (u *User) ToBase64(ctx context.Context) (string, error) {
	return encodeToBase64(u)
}

type UserFactory struct {
}

func (uf *UserFactory) CreateUserFromTokenInfo(tokenInfo TokenInfo) *User {
	return &User{
		Email:         tokenInfo.Email,
		VerifiedEmail: tokenInfo.EmailVerified,
		FamilyName:    tokenInfo.FamilyName,
		GivenName:     tokenInfo.GivenName,
		Id:            tokenInfo.Id,
		Locale:        tokenInfo.Locale,
		Name:          tokenInfo.Name,
		Picture:       tokenInfo.Picture,
	}
}

func (uf *UserFactory) CreateUserFromBase64(base64 string) (*User, error) {
	user := &User{}
	if err := decodeFromBase64(base64, user); err != nil {
		return nil, err
	}
	return user, nil
}
