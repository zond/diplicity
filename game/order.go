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

func OrderID(ctx context.Context, phaseID *datastore.Key, srcProvince dip.Province) (*datastore.Key, error) {
	if phaseID == nil || srcProvince == "" {
		return nil, fmt.Errorf("orders must have phases and source provinces")
	}
	return datastore.NewKey(ctx, orderKind, string(srcProvince), 0, phaseID), nil
}

func (o *Order) ID(ctx context.Context) (*datastore.Key, error) {
	phaseID, err := PhaseID(ctx, o.GameID, o.PhaseOrdinal)
	if err != nil {
		return nil, err
	}
	return OrderID(ctx, phaseID, o.Province)
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

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	srcProvince := r.Vars()["src_province"]
	orderID, err := OrderID(ctx, phaseID, dip.Province(srcProvince))
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	order := &Order{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, orderID}, []interface{}{game, phase, order}); err != nil {
			return err
		}
		game.ID = gameID
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return fmt.Errorf("can only delete orders in member games")
		}
		if phase.Resolved {
			return fmt.Errorf("can only delete orders for unresolved phases")
		}

		if order.Nation != member.Nation {
			return fmt.Errorf("can only update your own orders")
		}

		return datastore.Delete(ctx, orderID)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
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

	phaseID, err := PhaseID(ctx, gameID, phaseOrdinal)
	if err != nil {
		return nil, err
	}

	srcProvince := r.Vars()["src_province"]
	orderID, err := OrderID(ctx, phaseID, dip.Province(srcProvince))
	if err != nil {
		return nil, err
	}

	game := &Game{}
	phase := &Phase{}
	order := &Order{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, orderID}, []interface{}{game, phase, order}); err != nil {
			return err
		}
		game.ID = gameID
		if phase.Resolved {
			return fmt.Errorf("can only update orders for unresolved phases")
		}
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return fmt.Errorf("can only update orders in member games")
		}

		if order.Nation != member.Nation {
			return fmt.Errorf("can only update your own orders")
		}

		err = Copy(order, r, "POST")
		if err != nil {
			return err
		}

		order.GameID = gameID
		order.PhaseOrdinal = phaseOrdinal
		order.Nation = member.Nation

		variant := variants.Variants[game.Variant]

		parsedOrder, err := variant.ParseOrder(order.Parts)
		if err != nil {
			return err
		}

		s, err := phase.State(ctx, variant, false)
		if err != nil {
			return err
		}

		validNation, err := parsedOrder.Validate(s)
		if err != nil {
			return err
		}
		if validNation != member.Nation {
			return fmt.Errorf("can't issue orders for others")
		}

		if order.Province != dip.Province(srcProvince) {
			return fmt.Errorf("unable to change source province for order")
		}

		return order.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
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

	game := &Game{}
	phase := &Phase{}
	order := &Order{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
			return err
		}
		game.ID = gameID
		if phase.Resolved {
			return fmt.Errorf("can only create orders for unresolved phases")
		}
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return fmt.Errorf("can only create orders for member games")
		}

		err = Copy(order, r, "POST")
		if err != nil {
			return err
		}

		order.GameID = gameID
		order.PhaseOrdinal = phaseOrdinal
		order.Nation = member.Nation

		variant := variants.Variants[game.Variant]

		parsedOrder, err := variant.ParseOrder(order.Parts)
		if err != nil {
			return err
		}

		s, err := phase.State(ctx, variant, false)
		if err != nil {
			return err
		}

		validNation, err := parsedOrder.Validate(s)
		if err != nil {
			return err
		}
		if validNation != member.Nation {
			return fmt.Errorf("can't issue orders for others")
		}

		return order.Save(ctx)
	}, &datastore.TransactionOptions{XG: false}); err != nil {
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

	game := &Game{}
	phase := &Phase{}
	err = datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase})
	if err != nil {
		return err
	}
	game.ID = gameID

	var nation dip.Nation

	if member, found := game.GetMember(user.Id); found {
		nation = member.Nation
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
