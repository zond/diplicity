package game

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zond/diplicity/auth"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"

	. "github.com/zond/goaeoas"
	dip "github.com/zond/godip/common"
)

const (
	orderKind = "Order"
)

var OrderResource = &Resource{
	Create:     createOrder,
	CreatePath: "/Game/{game_id}/Phase/{ordinal}/Order",
}

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
	PhaseID  *datastore.Key
	Nation   dip.Nation
	Province dip.Province `methods:"POST"`
	Parts    []string     `methods:"POST"`
}

func OrderID(ctx context.Context, phaseID *datastore.Key, province dip.Province) (*datastore.Key, error) {
	if phaseID == nil || province == "" {
		return nil, fmt.Errorf("orders must have phases and provinces")
	}
	return datastore.NewKey(ctx, orderKind, string(province), 0, phaseID), nil
}

func (o *Order) ID(ctx context.Context) (*datastore.Key, error) {
	return OrderID(ctx, o.PhaseID, o.Province)
}

func (o *Order) Save(ctx context.Context) error {
	key, err := o.ID(ctx)
	if err != nil {
		return err
	}
	_, err = datastore.Put(ctx, key, o)
	return err
}

func (o *Order) Item(r Request) *Item {
	return NewItem(o).SetName(strings.Join(o.Parts, " "))
}

func createOrder(w ResponseWriter, r Request) (*Order, error) {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return nil, nil
	}

	gameID, err := datastore.DecodeKey(r.Vars()["game_id"])
	if err != nil {
		return nil, err
	}

	ordinal, err := strconv.ParseInt(r.Vars()["ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	phaseID, err := PhaseID(ctx, gameID, ordinal)
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	phase := &Phase{}
	member := &Member{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{phaseID, memberID}, []interface{}{phase, member}); err != nil {
		return nil, err
	}

	order := &Order{}
	err = Copy(order, r, "POST")
	if err != nil {
		return nil, err
	}
	order.PhaseID = phaseID
	order.Nation = member.Nation

	if err := order.Save(ctx); err != nil {
		return nil, err
	}

	return order, nil
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
