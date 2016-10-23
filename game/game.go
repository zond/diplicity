package game

import "cloud.google.com/go/datastore"

type Game struct {
	ID *datastore.Key
}
