package market

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type repositoryStub struct {
	register    func(model.MarketRegisterCommand) (model.MarketListing, error)
	search      func(model.MarketSearch) ([]model.MarketListingView, uint16, error)
	list        func(int) ([]model.MarketListingView, error)
	purchase    func(model.MarketPurchaseCommand) (model.MarketListing, byte, error)
	cancelBatch func([]model.MarketReturnCommand) ([]model.MarketExpiryReturn, error)
	detail      func(uint64, string, int) (model.MarketListingView, error)
	balance     func(int) (uint64, error)
}

func (r repositoryStub) Register(_ context.Context, command model.MarketRegisterCommand) (model.MarketListing, error) {
	return r.register(command)
}
func (r repositoryStub) RegisterBatch(_ context.Context, commands []model.MarketRegisterCommand) ([]model.MarketListing, error) {
	listings := make([]model.MarketListing, 0, len(commands))
	for _, command := range commands {
		listing, err := r.register(command)
		if err != nil {
			return nil, err
		}
		listings = append(listings, listing)
	}
	return listings, nil
}
func (r repositoryStub) Purchase(_ context.Context, command model.MarketPurchaseCommand) (model.MarketListing, byte, error) {
	if r.purchase == nil {
		return model.MarketListing{}, 0, errors.New("unexpected Purchase")
	}
	return r.purchase(command)
}
func (repositoryStub) Cancel(context.Context, model.MarketReturnCommand) (model.MarketListing, byte, error) {
	return model.MarketListing{}, 0, errors.New("unexpected Cancel")
}
func (r repositoryStub) CancelBatch(_ context.Context, commands []model.MarketReturnCommand) ([]model.MarketExpiryReturn, error) {
	if r.cancelBatch == nil {
		return nil, errors.New("unexpected CancelBatch")
	}
	return r.cancelBatch(commands)
}
func (repositoryStub) Settle(context.Context, model.MarketSettleCommand) (uint64, error) {
	return 0, errors.New("unexpected Settle")
}
func (repositoryStub) SettleBatch(context.Context, []model.MarketSettleCommand) ([]uint64, error) {
	return nil, errors.New("unexpected SettleBatch")
}
func (r repositoryStub) Detail(_ context.Context, listingID uint64, seller string, refID int) (model.MarketListingView, error) {
	if r.detail == nil {
		return model.MarketListingView{}, errors.New("unexpected Detail")
	}
	return r.detail(listingID, seller, refID)
}
func (r repositoryStub) SellerListings(_ context.Context, characterID int) ([]model.MarketListingView, error) {
	return r.list(characterID)
}
func (r repositoryStub) Search(_ context.Context, search model.MarketSearch) ([]model.MarketListingView, uint16, error) {
	return r.search(search)
}
func (repositoryStub) ExpireDue(context.Context, time.Time, int) ([]model.MarketExpiryReturn, error) {
	return nil, errors.New("unexpected ExpireDue")
}
func (r repositoryStub) Balance(_ context.Context, characterID int) (uint64, error) {
	if r.balance == nil {
		return 0, errors.New("offline")
	}
	return r.balance(characterID)
}

type inventoryStub struct {
	inventory model.Inventory
	err       error
}

func (s inventoryStub) GetCharacterInventory(context.Context, int64) (model.Inventory, error) {
	if s.err != nil {
		return model.Inventory{}, s.err
	}
	return s.inventory, nil
}
func (inventoryStub) AddItemToInventory(context.Context, int64, int64, byte) error { return nil }

type worldStub struct {
	player *model.Player
}

func (s worldStub) GetPlayerByCharacterID(int) (*model.Player, error) {
	if s.player == nil {
		return nil, errors.New("offline")
	}
	return s.player, nil
}

func testSession() *server.Session {
	return &server.Session{ID: "session", UserContext: server.UserContext{CharacterID: 77}}
}

func captureHandler(repo model.OpenMarketRepository) (*Handler, *network.Packet) {
	captured := &network.Packet{}
	h := &Handler{repo: repo, inventory: inventoryStub{err: errors.New("offline")}, world: worldStub{}, now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }}
	h.send = func(_ *server.Session, packet network.Packet) error {
		*captured = packet
		return nil
	}
	return h, captured
}

func TestMarketFeesUseCurrentUnifiedNonzeroRate(t *testing.T) {
	tests := []struct {
		price       uint64
		wantDeposit uint64
		wantSellFee uint64
	}{
		{price: 999_999},
		{price: 1_000_000, wantDeposit: 10_000, wantSellFee: 30_000},
		{price: 10_000_000, wantDeposit: 100_000, wantSellFee: 300_000},
		{price: 100_000_000, wantDeposit: 1_000_000, wantSellFee: 3_000_000},
	}

	for _, test := range tests {
		deposit, sellFee := marketFees(test.price)
		if deposit != test.wantDeposit || sellFee != test.wantSellFee {
			t.Errorf("marketFees(%d) = (%d,%d), want (%d,%d)", test.price, deposit, sellFee, test.wantDeposit, test.wantSellFee)
		}
	}
}

func TestRegisterUsesRecoveredFeeLifetimeAndResponseShape(t *testing.T) {
	var got model.MarketRegisterCommand
	repo := repositoryStub{
		register: func(command model.MarketRegisterCommand) (model.MarketListing, error) {
			got = command
			return model.MarketListing{ID: 0x11223344, ReferenceItemID: 0x55667788, Status: model.MarketListingActive, Deposit: command.Deposit, SellFee: command.SellFee, ExpiresAt: command.ExpiresAt}, nil
		},
		list:   func(int) ([]model.MarketListingView, error) { return nil, nil },
		search: func(model.MarketSearch) ([]model.MarketListingView, uint16, error) { return nil, 0, nil },
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.MessageID = opcode.OpenMarketRegisterRequest
	request.Sequence = 0x5A
	request.WriteByte(1)
	request.WriteByte(13)
	request.WriteUInt32(2)
	request.WriteUInt32(0x55667788)
	request.WriteUInt32(0x10203040)
	request.WriteUInt64(10_000_000)
	if err := h.register(context.Background(), testSession(), request); err != nil {
		t.Fatalf("register() error = %v", err)
	}
	if got.Deposit != 100_000 || got.SellFee != 300_000 {
		t.Fatalf("fees = (%d,%d), want (100000,300000)", got.Deposit, got.SellFee)
	}
	if got.ExpiresAt.Unix() != 1_700_259_200 {
		t.Fatalf("expiry = %d", got.ExpiresAt.Unix())
	}
	want := "01010d004433221188776655a086010000000000e09304000000000080e55765"
	if value := hex.EncodeToString(captured.Payload()); value != want {
		t.Fatalf("response payload = %s, want %s", value, want)
	}
}

func TestSearchReadsClientStringFilterAndWritesRecoveredEntryShape(t *testing.T) {
	registered := time.Unix(1_700_000_000, 0).UTC()
	repo := repositoryStub{
		register: func(model.MarketRegisterCommand) (model.MarketListing, error) { return model.MarketListing{}, nil },
		list:     func(int) ([]model.MarketListingView, error) { return nil, nil },
		search: func(search model.MarketSearch) ([]model.MarketListingView, uint16, error) {
			if search.Filter != "Alice" || search.TIDGroupID != 0x10203040 || search.Degree != 9 || search.Page != 0 {
				t.Fatalf("search = %+v", search)
			}
			return []model.MarketListingView{{Seller: "Alice", Listing: model.MarketListing{ID: 0x11223344, Status: model.MarketListingActive, ReferenceItemID: 0x55667788, Quantity: 3, Price: 99, RegisteredAt: registered}}}, 4, nil
		},
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.MessageID = opcode.OpenMarketSearchRequest
	request.WriteByte(1)
	request.WriteByte(0)
	request.WriteUInt32(0x10203040)
	request.WriteByte(9)
	request.WriteString("Alice")
	if err := h.search(context.Background(), testSession(), request); err != nil {
		t.Fatalf("search() error = %v", err)
	}
	want := "010104443322110500416c696365008877665503000000630000000000000000f15365"
	if value := hex.EncodeToString(captured.Payload()); value != want {
		t.Fatalf("response payload = %s, want %s", value, want)
	}
}

func TestSearchUsesNativeTypeThreeCursor(t *testing.T) {
	var pages []byte
	repo := repositoryStub{
		search: func(search model.MarketSearch) ([]model.MarketListingView, uint16, error) {
			pages = append(pages, search.Page)
			if search.TIDGroupID != 0x10203040 || search.Degree != 9 || search.Filter != "Alice" {
				t.Fatalf("search = %+v", search)
			}
			return nil, 1, nil
		},
	}
	h, _ := captureHandler(repo)
	initial := network.EmptyClientPacket()
	initial.WriteByte(1)
	initial.WriteByte(0)
	initial.WriteUInt32(0x10203040)
	initial.WriteByte(9)
	initial.WriteString("Alice")
	if err := h.search(context.Background(), testSession(), initial); err != nil {
		t.Fatalf("initial search() error = %v", err)
	}
	next := network.EmptyClientPacket()
	next.WriteByte(3)
	next.WriteByte(2)
	if err := h.search(context.Background(), testSession(), next); err != nil {
		t.Fatalf("next-page search() error = %v", err)
	}
	if len(pages) != 2 || pages[0] != 0 || pages[1] != 2 {
		t.Fatalf("pages = %v, want [0 2]", pages)
	}
}

func TestListWritesRecoveredSellerRecordShape(t *testing.T) {
	expires := time.Unix(1_700_259_200, 0).UTC()
	repo := repositoryStub{
		register: func(model.MarketRegisterCommand) (model.MarketListing, error) { return model.MarketListing{}, nil },
		search:   func(model.MarketSearch) ([]model.MarketListingView, uint16, error) { return nil, 0, nil },
		list: func(characterID int) ([]model.MarketListingView, error) {
			if characterID != 77 {
				t.Fatalf("character ID = %d", characterID)
			}
			return []model.MarketListingView{{Listing: model.MarketListing{ID: 7, Status: model.MarketListingSold, ReferenceItemID: 8, Quantity: 2, Price: 1000, Deposit: 10, SellFee: 30, ExpiresAt: expires}}}, nil
		},
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.MessageID = opcode.OpenMarketListRequest
	if err := h.list(context.Background(), testSession(), request); err != nil {
		t.Fatalf("list() error = %v", err)
	}
	want := "010107000000010800000002000000e8030000000000000a000000000000001e0000000000000080e55765"
	if value := hex.EncodeToString(captured.Payload()); value != want {
		t.Fatalf("response payload = %s, want %s", value, want)
	}
}

func TestBuyReturnsPurchasedGenericItem(t *testing.T) {
	repo := repositoryStub{
		purchase: func(command model.MarketPurchaseCommand) (model.MarketListing, byte, error) {
			if command.BuyerCharacterID != 77 || command.ListingID != 1 || command.ExpectedReferenceItemID != 2 {
				t.Fatalf("purchase command = %#v", command)
			}
			return model.MarketListing{}, 5, nil
		},
		detail: func(uint64, string, int) (model.MarketListingView, error) {
			return model.MarketListingView{Listing: model.MarketListing{Version: 1}}, nil
		},
	}
	h, captured := captureHandler(repo)
	h.inventory = inventoryStub{inventory: model.NewInventory(map[byte]model.Item{
		5: {ID: 3, SRObject: model.SRObject{RefObjectID: 2}, StackSize: 1},
	})}
	request := network.EmptyClientPacket()
	request.WriteString("seller")
	request.WriteUInt32(1)
	request.WriteUInt32(2)
	if err := h.buy(context.Background(), testSession(), request); err != nil {
		t.Fatalf("buy() error = %v", err)
	}
	if got := hex.EncodeToString(captured.Payload()); got != "0101000000050000000002000000" {
		t.Fatalf("response payload = %s, want 0101000000050000000002000000", got)
	}
}

func TestUnregisterUsesNativeModeOneRequestAndResponse(t *testing.T) {
	repo := repositoryStub{
		list: func(int) ([]model.MarketListingView, error) {
			return []model.MarketListingView{{Listing: model.MarketListing{ID: 1, Status: model.MarketListingActive, Version: 1}}}, nil
		},
		cancelBatch: func(commands []model.MarketReturnCommand) ([]model.MarketExpiryReturn, error) {
			if len(commands) != 1 || commands[0].ListingID != 1 {
				t.Fatalf("cancel commands = %#v", commands)
			}
			return []model.MarketExpiryReturn{{Slot: 5}}, nil
		},
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.WriteByte(1)
	request.WriteUInt32(1)
	if err := h.unregister(context.Background(), testSession(), request); err != nil {
		t.Fatalf("unregister() error = %v", err)
	}
	if got := hex.EncodeToString(captured.Payload()); got != "01010100000005" {
		t.Fatalf("response payload = %s, want 01010100000005", got)
	}
}

func TestUnregisterUsesNativeModeZeroForAllActiveListings(t *testing.T) {
	repo := repositoryStub{
		list: func(int) ([]model.MarketListingView, error) {
			return []model.MarketListingView{
				{Listing: model.MarketListing{ID: 1, Status: model.MarketListingActive, Version: 1}},
				{Listing: model.MarketListing{ID: 2, Status: model.MarketListingSold, Version: 1}},
				{Listing: model.MarketListing{ID: 3, Status: model.MarketListingActive, Version: 1}},
			}, nil
		},
		cancelBatch: func(commands []model.MarketReturnCommand) ([]model.MarketExpiryReturn, error) {
			if len(commands) != 2 || commands[0].ListingID != 1 || commands[1].ListingID != 3 {
				t.Fatalf("cancel commands = %#v", commands)
			}
			return []model.MarketExpiryReturn{{Slot: 5}, {Slot: 6}}, nil
		},
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.WriteByte(0)
	if err := h.unregister(context.Background(), testSession(), request); err != nil {
		t.Fatalf("unregister() error = %v", err)
	}
	if got := hex.EncodeToString(captured.Payload()); got != "010201000000050300000006" {
		t.Fatalf("response payload = %s, want 010201000000050300000006", got)
	}
}

func TestDetailUsesNativeRequestAndResponsePrefix(t *testing.T) {
	repo := repositoryStub{
		detail: func(listingID uint64, seller string, referenceItemID int) (model.MarketListingView, error) {
			if listingID != 0x11223344 || seller != "Alice" || referenceItemID != 0x55667788 {
				t.Fatalf("detail lookup = (%#x,%q,%#x)", listingID, seller, referenceItemID)
			}
			return model.MarketListingView{Listing: model.MarketListing{ID: listingID}}, nil
		},
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.WriteByte(0)
	request.WriteString("Alice")
	request.WriteUInt32(0x11223344)
	request.WriteUInt32(0x55667788)
	if err := h.detail(context.Background(), testSession(), request); err != nil {
		t.Fatalf("detail() error = %v", err)
	}
	got := hex.EncodeToString(captured.Payload())
	if len(got) < len("010044332211") || got[:len("010044332211")] != "010044332211" {
		t.Fatalf("response prefix = %s, want 010044332211", got)
	}
}

func TestReadUnregisterRequestUsesNativeModes(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		all     bool
		id      uint32
		wantErr bool
	}{
		{name: "all", payload: []byte{0}, all: true},
		{name: "one", payload: []byte{1, 1, 0, 0, 0}, id: 1},
		{name: "unsupported mode", payload: []byte{2}, wantErr: true},
		{name: "trailing all", payload: []byte{0, 1}, wantErr: true},
		{name: "trailing one", payload: []byte{1, 1, 0, 0, 0, 1}, wantErr: true},
		{name: "zero selector", payload: []byte{1, 0, 0, 0, 0}, wantErr: true},
	}
	for _, test := range tests {
		packet := network.EmptyClientPacket()
		if err := packet.WriteBytes(test.payload); err != nil {
			t.Fatalf("%s: WriteBytes() error = %v", test.name, err)
		}
		all, id, err := readUnregisterRequest(&packet)
		if (err != nil) != test.wantErr {
			t.Fatalf("%s: error = %v, want error = %v", test.name, err, test.wantErr)
		}
		if err == nil && (all != test.all || id != test.id) {
			t.Fatalf("%s: result = (%t,%d), want (%t,%d)", test.name, all, id, test.all, test.id)
		}
	}
}

func TestRefreshPlayerSendsCurrentGoldBalance(t *testing.T) {
	session := testSession()
	player := &model.Player{Session: session}
	var sent []network.Packet
	h := &Handler{
		repo: repositoryStub{balance: func(characterID int) (uint64, error) {
			if characterID != 77 {
				t.Fatalf("balance character ID = %d, want 77", characterID)
			}
			return 0x0102030405060708, nil
		}},
		inventory: inventoryStub{},
		world:     worldStub{player: player},
		send: func(gotSession *server.Session, packet network.Packet) error {
			if gotSession != session {
				t.Fatal("gold update sent to an unexpected session")
			}
			sent = append(sent, packet)
			return nil
		},
	}

	h.refreshPlayer(context.Background(), 77)

	if player.Gold != 0x0102030405060708 {
		t.Fatalf("player gold = %#x", player.Gold)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.EntityUpdatePoints {
		t.Fatalf("gold update packets = %#v", sent)
	}
	if got := hex.EncodeToString(sent[0].Payload()); got != "01080706050403020100" {
		t.Fatalf("gold update payload = %s, want 01080706050403020100", got)
	}
}
