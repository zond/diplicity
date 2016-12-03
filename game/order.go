package game

import (
	"fmt"
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

var OrderResource *Resource

func init() {
	OrderResource = &Resource{
		Create:     createOrder,
		Update:     updateOrder,
		Delete:     deleteOrder,
		CreatePath: "/Game/{game_id}/Phase/{phase_ordinal}/Order",
		FullPath:   "/Game/{game_id}/Phase/{phase_ordinal}/Order/{src_province}",
		Listers: []Lister{
			{
				Path:    "/Game/{game_id}/Phase/{phase_ordinal}/Orders",
				Route:   ListOrdersRoute,
				Handler: listOrders,
			},
		},
	}
}

type Orders []Order

func (o Orders) Item(r Request, gameID *datastore.Key, phase *Phase) *Item {
	if !phase.Resolved {
		r.Values()["is-unresolved"] = true
	}
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
	return OrderID(ctx, phaseID, dip.Province(o.Parts[0]))
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
		return nil, HTTPErr{"unauthorized", 401}
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

	order := &Order{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		phase := &Phase{}
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, orderID}, []interface{}{game, phase, order}); err != nil {
			return err
		}
		game.ID = gameID
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return HTTPErr{"can only delete orders in member games", 404}
		}
		if phase.Resolved {
			return HTTPErr{"can only delete orders for unresolved phases", 412}
		}

		if order.Nation != member.Nation {
			return HTTPErr{"can only update your own orders", 403}
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
		return nil, HTTPErr{"unauthorized", 401}
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

	order := &Order{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		phase := &Phase{}
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID, orderID}, []interface{}{game, phase, order}); err != nil {
			return err
		}
		game.ID = gameID
		if phase.Resolved {
			return HTTPErr{"can only update orders for unresolved phases", 412}
		}
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return HTTPErr{"can only update orders in member games", 404}
		}

		if order.Nation != member.Nation {
			return HTTPErr{"can only update your own orders", 403}
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

		s, err := phase.State(ctx, variant, nil)
		if err != nil {
			return err
		}

		validNation, err := parsedOrder.Validate(s)
		if err != nil {
			return err
		}
		if validNation != member.Nation {
			return HTTPErr{"can't issue orders for others", 403}
		}

		if order.Parts[0] != srcProvince {
			return HTTPErr{"unable to change source province for order", 400}
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
		return nil, HTTPErr{"unauthorized", 401}
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

	order := &Order{}
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		game := &Game{}
		phase := &Phase{}
		if err := datastore.GetMulti(ctx, []*datastore.Key{gameID, phaseID}, []interface{}{game, phase}); err != nil {
			return err
		}
		game.ID = gameID
		if phase.Resolved {
			return HTTPErr{"can only create orders for unresolved phases", 412}
		}
		member, isMember := game.GetMember(user.Id)
		if !isMember {
			return HTTPErr{"can only create orders for member games", 404}
		}

		keysToSave := []*datastore.Key{}
		valuesToSave := []interface{}{}

		phaseState := &PhaseState{}
		phaseStateID, err := PhaseStateID(ctx, phaseID, member.Nation)
		if err != nil {
			return err
		}
		if err := datastore.Get(ctx, phaseStateID, phaseState); err == nil && phaseState.OnProbation {
			phaseState.OnProbation = false
			phaseState.Note = fmt.Sprintf("Auto updated to OnProbation = false due to order creation.")
			keysToSave = append(keysToSave, phaseStateID)
			valuesToSave = append(valuesToSave, phaseState)
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

		s, err := phase.State(ctx, variant, nil)
		if err != nil {
			return err
		}

		validNation, err := parsedOrder.Validate(s)
		if err != nil {
			return err
		}
		if validNation != member.Nation {
			return HTTPErr{"can't issue orders for others", 403}
		}

		orderID, err := OrderID(ctx, phaseID, dip.Province(order.Parts[0]))
		if err != nil {
			return err
		}

		keysToSave = append(keysToSave, orderID)
		valuesToSave = append(valuesToSave, order)
		_, err = datastore.PutMulti(ctx, keysToSave, valuesToSave)
		return err
	}, &datastore.TransactionOptions{XG: false}); err != nil {
		return nil, err
	}

	return order, nil
}

func listOrders(w ResponseWriter, r Request) error {
	ctx := appengine.NewContext(r.Req())

	user, ok := r.Values()["user"].(*auth.User)
	if !ok {
		return HTTPErr{"unauthorized", 401}
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
