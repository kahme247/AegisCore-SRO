package market

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

const (
	maxRegistrationItems = 8
	marketPageSize       = 10
	marketLifetime       = 72 * time.Hour
)

type worldLookup interface {
	GetPlayerByCharacterID(int) (*model.Player, error)
}

type Handler struct {
	repo      model.OpenMarketRepository
	inventory model.InventoryRepository
	world     worldLookup
	now       func() time.Time
	send      func(*server.Session, network.Packet) error
}

func RegisterHandlers(target *server.Server, repo model.OpenMarketRepository, inventory model.InventoryRepository, world worldLookup) (*Handler, error) {
	if target == nil || repo == nil || inventory == nil || world == nil {
		return nil, model.ErrMarketUnavailable
	}
	h := &Handler{repo: repo, inventory: inventory, world: world, now: time.Now}
	target.RegisterHandler(opcode.OpenMarketDetailRequest, h.detail)
	target.RegisterHandler(opcode.OpenMarketCloseRequest, h.close)
	target.RegisterHandler(opcode.OpenMarketRegisterRequest, h.register)
	target.RegisterHandler(opcode.OpenMarketUnregisterRequest, h.unregister)
	target.RegisterHandler(opcode.OpenMarketBuyRequest, h.buy)
	target.RegisterHandler(opcode.OpenMarketSettleRequest, h.settle)
	target.RegisterHandler(opcode.OpenMarketSearchRequest, h.search)
	target.RegisterHandler(opcode.OpenMarketListRequest, h.list)
	return h, nil
}

func characterID(session *server.Session) (int, error) {
	if session == nil || session.UserContext.CharacterID <= 0 {
		return 0, model.ErrMarketInvalidCommand
	}
	return session.UserContext.CharacterID, nil
}

func (h *Handler) sendPacket(session *server.Session, packet network.Packet) error {
	if err := packet.WriteError(); err != nil {
		return err
	}
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func marketResponse(messageID uint16, err error) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	if err == nil {
		packet.WriteByte(1)
	} else {
		packet.WriteByte(2)
		packet.WriteUInt16(marketErrorCode(err))
	}
	return packet
}

func marketErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrMarketInsufficientGold):
		return 0x700D
	case errors.Is(err, model.ErrMarketInventoryFull):
		return 0x701F
	case errors.Is(err, model.ErrMarketListingUnavailable):
		return 0x7019
	case errors.Is(err, model.ErrMarketInvalidCommand):
		return 0x701B
	default:
		return 0x700F
	}
}

func commandKey(session *server.Session, packet network.Packet, action string, index int) string {
	return fmt.Sprintf("market:%s:%02x:%04x:%s:%d", session.ID, packet.Sequence, packet.MessageID, action, index)
}

func feeTier(price uint64) int {
	switch {
	case price < 1_000_000:
		return 0
	case price < 10_000_000:
		return 1
	case price < 100_000_000:
		return 2
	default:
		return 3
	}
}

func marketFees(price uint64) (deposit, sellFee uint64) {
	// SR_GameServer 0x0047A410/0x0047A4A0/0x0047A4F0 uses four price
	// tiers and computes price/100*rate. Constructor 0x00478E30 installs
	// default SET_FEE_RATE [0,1,1,1] and SELL_FEE_RATE [0,3,3,3].
	if feeTier(price) == 0 {
		return 0, 0
	}
	base := price / 100
	return base, base * 3
}

func saleStatus(status model.MarketListingStatus) byte {
	switch status {
	case model.MarketListingActive:
		return 0
	case model.MarketListingSold:
		return 1
	case model.MarketListingExpired:
		return 2
	default:
		return math.MaxUint8
	}
}

func writeGenericItem(packet *network.Packet, item model.Item) {
	model.WriteRentInfo(packet, item)
	packet.WriteUInt32(item.GetRefObjectID())
	model.WriteInventoryItem(packet, item)
}

func (h *Handler) detail(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := characterID(session); err != nil {
		return err
	}
	detailType, err := packet.ReadByte()
	if err != nil || detailType > 1 {
		return model.ErrMarketInvalidCommand
	}
	seller, err := packet.ReadString()
	if err != nil {
		return err
	}
	listingID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	refID, err := packet.ReadUInt32()
	if err != nil || packet.Remaining() != 0 {
		return model.ErrMarketInvalidCommand
	}
	view, err := h.repo.Detail(ctx, uint64(listingID), seller, int(refID))
	response := marketResponse(opcode.OpenMarketDetailResponse, err)
	if err == nil {
		response.WriteByte(detailType)
		response.WriteString(view.Seller)
		response.WriteUInt32(uint32(view.Listing.ID))
		writeGenericItem(&response, view.Item)
	}
	return h.sendPacket(session, response)
}

func (h *Handler) close(_ context.Context, session *server.Session, packet network.Packet) error {
	if _, err := characterID(session); err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return model.ErrMarketInvalidCommand
	}
	return h.sendPacket(session, marketResponse(opcode.OpenMarketCloseResponse, nil))
}

type registration struct {
	slot     byte
	quantity uint16
	refID    int
	tid      uint32
	price    uint64
}

func readRegistrations(packet *network.Packet) ([]registration, error) {
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > maxRegistrationItems {
		return nil, model.ErrMarketInvalidCommand
	}
	registrations := make([]registration, 0, count)
	for range count {
		slot, readErr := packet.ReadByte()
		if readErr != nil {
			return nil, readErr
		}
		quantity, readErr := packet.ReadUInt32()
		if readErr != nil || quantity == 0 || quantity > math.MaxUint16 {
			return nil, model.ErrMarketInvalidCommand
		}
		refID, readErr := packet.ReadUInt32()
		if readErr != nil || refID == 0 || refID > math.MaxInt32 {
			return nil, model.ErrMarketInvalidCommand
		}
		tid, readErr := packet.ReadUInt32()
		if readErr != nil {
			return nil, readErr
		}
		price, readErr := packet.ReadUInt64()
		if readErr != nil || price == 0 {
			return nil, model.ErrMarketInvalidCommand
		}
		registrations = append(registrations, registration{slot: slot, quantity: uint16(quantity), refID: int(refID), tid: tid, price: price})
	}
	if packet.Remaining() != 0 {
		return nil, model.ErrMarketInvalidCommand
	}
	return registrations, nil
}

func (h *Handler) register(ctx context.Context, session *server.Session, packet network.Packet) error {
	seller, err := characterID(session)
	if err != nil {
		return err
	}
	registrations, err := readRegistrations(&packet)
	if err != nil {
		return h.sendPacket(session, marketResponse(opcode.OpenMarketRegisterResponse, err))
	}
	commands := make([]model.MarketRegisterCommand, 0, len(registrations))
	for i, item := range registrations {
		deposit, fee := marketFees(item.price)
		commands = append(commands, model.MarketRegisterCommand{SellerCharacterID: seller, SourceSlot: item.slot, Quantity: item.quantity, ReferenceItemID: item.refID, TIDGroupID: item.tid, Price: item.price, Deposit: deposit, SellFee: fee, ExpiresAt: h.now().UTC().Add(model.RuntimeMarketLifetime()), CommandKey: commandKey(session, packet, "register", i)})
	}
	listings, err := h.repo.RegisterBatch(ctx, commands)
	if err != nil {
		return h.sendPacket(session, marketResponse(opcode.OpenMarketRegisterResponse, err))
	}
	response := marketResponse(opcode.OpenMarketRegisterResponse, nil)
	response.WriteByte(byte(len(listings)))
	for i, listing := range listings {
		response.WriteByte(registrations[i].slot)
		response.WriteByte(saleStatus(listing.Status))
		response.WriteUInt32(uint32(listing.ID))
		response.WriteUInt32(uint32(listing.ReferenceItemID))
		response.WriteUInt64(listing.Deposit)
		response.WriteUInt64(listing.SellFee)
		response.WriteUInt32(uint32(listing.ExpiresAt.Unix()))
	}
	h.refreshPlayer(ctx, seller)
	return h.sendPacket(session, response)
}

func readListingIDs(packet *network.Packet) ([]uint32, error) {
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > maxRegistrationItems {
		return nil, model.ErrMarketInvalidCommand
	}
	ids := make([]uint32, count)
	for i := range ids {
		ids[i], err = packet.ReadUInt32()
		if err != nil || ids[i] == 0 {
			return nil, model.ErrMarketInvalidCommand
		}
	}
	if packet.Remaining() != 0 {
		return nil, model.ErrMarketInvalidCommand
	}
	return ids, nil
}

func (h *Handler) unregister(ctx context.Context, session *server.Session, packet network.Packet) error {
	seller, err := characterID(session)
	if err != nil {
		return err
	}
	ids, err := readListingIDs(&packet)
	if err != nil {
		return h.sendPacket(session, marketResponse(opcode.OpenMarketUnregisterResponse, err))
	}
	views, err := h.repo.SellerListings(ctx, seller)
	byID := make(map[uint64]model.MarketListing, len(views))
	for _, view := range views {
		byID[view.Listing.ID] = view.Listing
	}
	type returned struct {
		id   uint32
		slot byte
		item model.Item
	}
	results := make([]returned, 0, len(ids))
	commands := make([]model.MarketReturnCommand, 0, len(ids))
	for i, id := range ids {
		listing, found := byID[uint64(id)]
		if !found || listing.Status != model.MarketListingActive {
			err = model.ErrMarketListingUnavailable
			break
		}
		commands = append(commands, model.MarketReturnCommand{SellerCharacterID: seller, ListingID: uint64(id), ExpectedVersion: listing.Version, CommandKey: commandKey(session, packet, "cancel", i)})
	}
	var returnedListings []model.MarketExpiryReturn
	if err == nil {
		returnedListings, err = h.repo.CancelBatch(ctx, commands)
	}
	if err == nil {
		inventory, loadErr := h.inventory.GetCharacterInventory(ctx, int64(seller))
		if loadErr != nil {
			err = loadErr
		}
		for i, returnedListing := range returnedListings {
			item, found := inventory.Items[returnedListing.Slot]
			if !found {
				err = model.ErrMarketListingUnavailable
				break
			}
			results = append(results, returned{id: ids[i], slot: returnedListing.Slot, item: item})
		}
	}
	response := marketResponse(opcode.OpenMarketUnregisterResponse, err)
	if err == nil {
		response.WriteByte(byte(len(results)))
		for _, result := range results {
			response.WriteUInt32(result.id)
			response.WriteByte(result.slot)
			writeGenericItem(&response, result.item)
		}
		h.refreshPlayer(ctx, seller)
	}
	return h.sendPacket(session, response)
}

func (h *Handler) buy(ctx context.Context, session *server.Session, packet network.Packet) error {
	buyer, err := characterID(session)
	if err != nil {
		return err
	}
	seller, err := packet.ReadString()
	if err != nil {
		return err
	}
	listingID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	refID, err := packet.ReadUInt32()
	if err != nil || packet.Remaining() != 0 {
		return model.ErrMarketInvalidCommand
	}
	view, err := h.repo.Detail(ctx, uint64(listingID), seller, int(refID))
	var slot byte
	if err == nil {
		_, slot, err = h.repo.Purchase(ctx, model.MarketPurchaseCommand{BuyerCharacterID: buyer, ListingID: uint64(listingID), ExpectedVersion: view.Listing.Version, ExpectedReferenceItemID: int(refID), CommandKey: commandKey(session, packet, "buy", 0)})
	}
	response := marketResponse(opcode.OpenMarketBuyResponse, err)
	if err == nil {
		inventory, loadErr := h.inventory.GetCharacterInventory(ctx, int64(buyer))
		item, found := inventory.Items[slot]
		if loadErr != nil || !found {
			return model.ErrMarketListingUnavailable
		}
		response.WriteUInt32(listingID)
		response.WriteByte(slot)
		writeGenericItem(&response, item)
		h.refreshPlayer(ctx, buyer)
	}
	return h.sendPacket(session, response)
}

func (h *Handler) settle(ctx context.Context, session *server.Session, packet network.Packet) error {
	seller, err := characterID(session)
	if err != nil {
		return err
	}
	ids, err := readListingIDs(&packet)
	if err == nil {
		commands := make([]model.MarketSettleCommand, 0, len(ids))
		for i, id := range ids {
			commands = append(commands, model.MarketSettleCommand{SellerCharacterID: seller, ListingID: uint64(id), CommandKey: commandKey(session, packet, "settle", i)})
		}
		_, err = h.repo.SettleBatch(ctx, commands)
	}
	response := marketResponse(opcode.OpenMarketSettleResponse, err)
	if err == nil {
		response.WriteByte(byte(len(ids)))
		for _, id := range ids {
			response.WriteUInt32(id)
		}
		h.refreshPlayer(ctx, seller)
	}
	return h.sendPacket(session, response)
}

func (h *Handler) search(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := characterID(session); err != nil {
		return err
	}
	searchType, err := packet.ReadByte()
	if err != nil || searchType < 1 || searchType > 3 {
		return h.sendPacket(session, marketResponse(opcode.OpenMarketSearchResponse, model.ErrMarketInvalidCommand))
	}
	page, err := packet.ReadByte()
	if err != nil {
		return err
	}
	tid, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	degree, err := packet.ReadByte()
	if err != nil {
		return err
	}
	filter, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return h.sendPacket(session, marketResponse(opcode.OpenMarketSearchResponse, model.ErrMarketInvalidCommand))
	}
	views, pages, err := h.repo.Search(ctx, model.MarketSearch{TIDGroupID: tid, Degree: degree, Filter: filter, Page: page, PageSize: marketPageSize})
	response := marketResponse(opcode.OpenMarketSearchResponse, err)
	if err == nil {
		response.WriteByte(byte(len(views)))
		response.WriteByte(byte(min(pages, math.MaxUint8)))
		for _, view := range views {
			response.WriteUInt32(uint32(view.Listing.ID))
			response.WriteString(view.Seller)
			response.WriteByte(saleStatus(view.Listing.Status))
			response.WriteUInt32(uint32(view.Listing.ReferenceItemID))
			response.WriteUInt32(uint32(view.Listing.Quantity))
			response.WriteUInt64(view.Listing.Price)
			response.WriteUInt32(uint32(view.Listing.RegisteredAt.Unix()))
		}
	}
	return h.sendPacket(session, response)
}

func (h *Handler) list(ctx context.Context, session *server.Session, packet network.Packet) error {
	seller, err := characterID(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return model.ErrMarketInvalidCommand
	}
	views, err := h.repo.SellerListings(ctx, seller)
	response := marketResponse(opcode.OpenMarketListResponse, err)
	if err == nil {
		response.WriteByte(byte(len(views)))
		for _, view := range views {
			response.WriteUInt32(uint32(view.Listing.ID))
			response.WriteByte(saleStatus(view.Listing.Status))
			response.WriteUInt32(uint32(view.Listing.ReferenceItemID))
			response.WriteUInt32(uint32(view.Listing.Quantity))
			response.WriteUInt64(view.Listing.Price)
			response.WriteUInt64(view.Listing.Deposit)
			response.WriteUInt64(view.Listing.SellFee)
			response.WriteUInt32(uint32(view.Listing.ExpiresAt.Unix()))
		}
	}
	return h.sendPacket(session, response)
}

func (h *Handler) refreshPlayer(ctx context.Context, characterID int) {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return
	}
	if inventory, loadErr := h.inventory.GetCharacterInventory(ctx, int64(characterID)); loadErr == nil {
		player.CommitInventory(inventory)
	}
	if gold, loadErr := h.repo.Balance(ctx, characterID); loadErr == nil {
		player.SetGold(gold)
	}
}
