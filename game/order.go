package game

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zond/diplicity/auth"
	"github.com/zond/godip/variants"
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
	Update:     updateOrder,
	Delete:     deleteOrder,
	CreatePath: "/Game/{game_id}/Phase/{phase_ordinal}/Order",
	FullPath:   "/Game/{game_id}/Phase/{phase_ordinal}/Order/{src_province}",
}

type Orders []Order

func (o Orders) Item(r Request, gameID *datastore.Key, phase *Phase) *Item {
	r.Values()["is-unresolved"] = !phase.Resolved
	orderItems := make(List, len(o))
	for i := range o {
		orderItems[i] = o[i].Item(r)
	}
	ordersItem := NewItem(orderItems).SetName("orders").AddLink(r.NewLink(Link{
		Rel:         "self",
		Route:       ListOrdersRoute,
		RouteParams: []string{"game_id", gameID.Encode(), "phase_ordinal", fmt.Sprint(phase.PhaseOrdinal)},
	}))
	return ordersItem
}

type Order struct {
	GameID       *datastore.Key
	PhaseOrdinal int64
	Province     dip.Province `methods:"POST,PUT"`
	Nation       dip.Nation
	Parts        []string `methods:"POST,PUT" separator:" "`
}

func OrderID(ctx context.Context, gameID *datastore.Key, phaseOrdinal int64, srcProvince dip.Province) (*datastore.Key, error) {
	if gameID == nil || phaseOrdinal < 0 || srcProvince == "" {
		return nil, fmt.Errorf("phases must have games, ordinals > 0 and source provinces")
	}
	if gameID.IntID() == 0 {
		return nil, fmt.Errorf("gameIDs must have int IDs")
	}
	return datastore.NewKey(ctx, orderKind, fmt.Sprintf("%d:%d:%s", gameID.IntID(), phaseOrdinal, srcProvince), 0, nil), nil
}

func (o *Order) ID(ctx context.Context) (*datastore.Key, error) {
	return OrderID(ctx, o.GameID, o.PhaseOrdinal, o.Province)
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
	orderItem := NewItem(o).SetName(strings.Join(o.Parts, " "))
	if _, isUnresolved := r.Values()["is-unresolved"]; isUnresolved {
		orderItem.AddLink(r.NewLink(OrderResource.Link("delete", Delete, []string{"game_id", o.GameID.Encode(), "phase_ordinal", fmt.Sprint(o.PhaseOrdinal), "src_province", string(o.Parts[0])})))
		orderItem.AddLink(r.NewLink(OrderResource.Link("update", Update, []string{"game_id", o.GameID.Encode(), "phase_ordinal", fmt.Sprint(o.PhaseOrdinal), "src_province", string(o.Parts[0])})))
	}
	return orderItem
}

func deleteOrder(w ResponseWriter, r Request) (*Order, error) {
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

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	srcProvince := r.Vars()["src_province"]
	orderID, err := OrderID(ctx, gameID, phaseOrdinal, dip.Province(srcProvince))
	if err != nil {
		return nil, err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	member := &Member{}
	order := &Order{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, memberID, orderID}, []interface{}{game, phase, member, order}); err != nil {
		return nil, err
	}
	if phase.Resolved {
		return nil, fmt.Errorf("can only delete orders for unresolved phases")
	}

	if order.Nation != member.Nation {
		return nil, fmt.Errorf("can only update your own orders")
	}

	if err := datastore.Delete(ctx, orderID); err != nil {
		return nil, err
	}

	return order, nil
}

func updateOrder(w ResponseWriter, r Request) (*Order, error) {
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

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	srcProvince := r.Vars()["src_province"]
	orderID, err := OrderID(ctx, gameID, phaseOrdinal, dip.Province(srcProvince))
	if err != nil {
		return nil, err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	member := &Member{}
	order := &Order{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, memberID, orderID}, []interface{}{game, phase, member, order}); err != nil {
		return nil, err
	}
	if phase.Resolved {
		return nil, fmt.Errorf("can only update orders for unresolved phases")
	}

	if order.Nation != member.Nation {
		return nil, fmt.Errorf("can only update your own orders")
	}

	err = Copy(order, r, "POST")
	if err != nil {
		return nil, err
	}

	order.GameID = gameID
	order.PhaseOrdinal = phaseOrdinal

	variant := variants.Variants[game.Variant]

	parsedOrder, err := variant.ParseOrder(order.Parts)
	if err != nil {
		return nil, err
	}

	s, err := phase.State(ctx, variant, false)
	if err != nil {
		return nil, err
	}

	validNation, err := parsedOrder.Validate(s)
	if err != nil {
		return nil, err
	}
	if validNation != member.Nation {
		return nil, fmt.Errorf("can't issue orders for others")
	}

	if order.Province != dip.Province(srcProvince) {
		return nil, fmt.Errorf("unable to change source province for order")
	}

	if err := order.Save(ctx); err != nil {
		return nil, err
	}

	return order, nil
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

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return nil, err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	memberID, err := MemberID(ctx, gameID, user.Id)
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	member := &Member{}
	if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, memberID}, []interface{}{game, phase, member}); err != nil {
		return nil, err
	}
	if phase.Resolved {
		return nil, fmt.Errorf("can only create orders for unresolved phases")
	}

	order := &Order{}
	err = Copy(order, r, "POST")
	if err != nil {
		return nil, err
	}

	order.GameID = gameID
	order.PhaseOrdinal = phaseOrdinal
	order.Nation = member.Nation

	variant := variants.Variants[game.Variant]

	parsedOrder, err := variant.ParseOrder(order.Parts)
	if err != nil {
		return nil, err
	}

	s, err := phase.State(ctx, variant, false)
	if err != nil {
		return nil, err
	}

	validNation, err := parsedOrder.Validate(s)
	if err != nil {
		return nil, err
	}
	if validNation != member.Nation {
		return nil, fmt.Errorf("can't issue orders for others")
	}

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

	phaseOrdinal, err := strconv.ParseInt(r.Vars()["phase_ordinal"], 10, 64)
	if err != nil {
		return err
	}

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
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
	_, err = datastore.NewQuery(orderKind).Filter("GameID=", gameID).Filter("PhaseOrdinal=", phaseOrdinal).GetAll(ctx, &found)
	if err != nil {
		return err
	}

	toReturn := Orders{}
	for _, order := range found {
		if phase.Resolved || order.Nation == nation {
			toReturn = append(toReturn, order)
		}
	}

	w.SetContent(toReturn.Item(r, gameID, phase))
	return nil
}
