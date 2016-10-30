package game

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	"github.com/zond/diplicity/auth"
	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	orderKind = "Order"
)

type Orders []Order

func (o Orders) Item(r Request, gameID *datastore.Key, ordinal int64) *Item {
	orderItems := make(List, len(o))
	for i := range o {
		orderItems[i] = o[i].Item(r)
	}
	ordersItem := NewItem(orderItems).SetName("orders").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListOrdersRoute,
		RouteParams: []string{"game_id", gameID.Encode(), "ordinal", fmt.Sprint(ordinal)},
	}))
	return ordersItem
}

type Order struct {
	GameID   *datastore.Key
	Ordinal  int64
	Nation   dip.Nation
	Province dip.Province
	Parts    []string
}

func (o *Order) Item(r Request) *Item {
	return NewItem(o).SetName(strings.Join(o.Parts, " "))
}

func listOrders(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return err
	}

	ordinal, err := strconv.ParseInt(r.Vars()["ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, ordinal)
	if err != nil {
		return err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return err
	}

	var nation dip.Nation

	phase := &Phase{}
	member := &Member{}
	err = datastore.GetMulti(ctx, []*datastore.Key{phaseID, memberID}, []interface{}{phase, member})
	if err == nil {
		nation = member.Nation
	} else if merr, ok := err.(appengine.MultiError); ok {
		if merr[0] != nil {
			return merr[0]
		}
	} else {
		return err
	}

	found := Orders{}
	_, err = datastore.NewQuery(orderKind).Ancestor(phaseID).GetAll(ctx, &found)
	if err != nil {
		return err
	}

	toReturn := Orders{}
	for _, order := range found {
		if phase.Resolved || order.Nation == nation {
			toReturn = append(toReturn, order)
		}
	}

	w.SetContent(toReturn.Item(r, gameID, ordinal))
	return nil
}
