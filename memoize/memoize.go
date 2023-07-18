package memoize

import (
	"fmt"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/appengine/v2/datastore"
	"google.golang.org/appengine/v2/log"
	"google.golang.org/appengine/v2/memcache"
)

func PutAll(ctx context.Context, expiration time.Duration, keys []*datastore.Key, srcs []interface{}) error {
	if len(keys) != len(srcs) {
		return fmt.Errorf("PutAll: keys (%+v) and srcs (%+v) are of unequal lengths", keys, srcs)
	}
	for idx := range keys {
		if err := Put(ctx, expiration, keys[idx], srcs[idx]); err != nil {
			return err
		}
	}
	return nil
}

func Put(ctx context.Context, expiration time.Duration, key *datastore.Key, src interface{}) error {
	props, err := datastore.SaveStruct(src)
	if err != nil {
		log.Errorf(ctx, "datastore.SaveStruct(%+v): %v", src, err)
		return err
	}
	return memcache.JSON.Set(ctx, &memcache.Item{
		Key:        key.Encode(),
		Object:     props,
		Expiration: expiration,
	})
}

func GetAll(ctx context.Context, keys []*datastore.Key, dsts []interface{}) (bool, error) {
	if len(keys) != len(dsts) {
		return false, fmt.Errorf("GetAll: keys (%+v) and dsts (%+v) of unequal lengths", keys, dsts)
	}
	for idx := range keys {
		found, err := Get(ctx, keys[idx], dsts[idx])
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func Get(ctx context.Context, key *datastore.Key, dst interface{}) (bool, error) {
	props := []datastore.Property{}
	if _, err := memcache.JSON.Get(ctx, key.Encode(), &props); err == memcache.ErrCacheMiss {
		return false, nil
	} else if err != nil {
		log.Errorf(ctx, "memcache.JSON.Get(..., %q, %+v): %v", key, dst, err)
		return false, err
	}
	if err := datastore.LoadStruct(dst, props); err != nil {
		log.Errorf(ctx, "datastore.LoadStruct(%+v, %+v): %v", dst, props, err)
		return false, err
	}
	return true, nil
}
