package game

import (
	"fmt"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/memcache"

	. "github.com/zond/goaeoas"
)

const (
	forumMailKind       = "ForumMail"
	forumAddressPattern = "forum-mail-%s@diplicity-engine.appspotmail.com"
)

var (
	ForumMailResource *Resource
)

func init() {
	ForumMailResource = &Resource{
		FullPath: "/ForumMail",
		Load:     loadForumMail,
	}
}

func loadForumMail(w ResponseWriter, r Request) (*ForumMail, error) {
	return GetForumMail(appengine.NewContext(r.Req()))
}

func getForumMailKey(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, forumMailKind, prodKey, 0, nil)
}

type ForumMail struct {
	Secret  string `json:"-"`
	Subject string
	Body    string
}

func (f *ForumMail) Item(r Request) *Item {
	return NewItem(f).SetName("ForumMail")
}

func (f *ForumMail) Address() string {
	return fmt.Sprintf(forumAddressPattern, f.Secret)
}

func (f *ForumMail) Save(ctx context.Context) error {
	_, err := datastore.Put(ctx, getForumMailKey(ctx), f)
	return err
}

func GetForumMail(ctx context.Context) (*ForumMail, error) {
	// check if in memcache
	forumMail := &ForumMail{}
	_, err := memcache.JSON.Get(ctx, forumMailKind, forumMail)
	if err == nil {
		return forumMail, nil
	} else if err != memcache.ErrCacheMiss {
		return nil, err
	}

	// nope, check if in datastore
	if err := datastore.Get(ctx, getForumMailKey(ctx), forumMail); err == nil {
		if err := memcache.JSON.Set(ctx, &memcache.Item{
			Key:        forumMailKind,
			Object:     forumMail,
			Expiration: time.Hour,
		}); err != nil {
			return nil, err
		}
		return forumMail, nil
	} else if err != datastore.ErrNoSuchEntity {
		return nil, err
	}
	return nil, nil
}
