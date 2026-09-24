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
	register func(model.MarketRegisterCommand) (model.MarketListing, error)
	search   func(model.MarketSearch) ([]model.MarketListingView, uint16, error)
	list     func(int) ([]model.MarketListingView, error)
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
func (repositoryStub) Purchase(context.Context, model.MarketPurchaseCommand) (model.MarketListing, byte, error) {
	return model.MarketListing{}, 0, errors.New("unexpected Purchase")
}
func (repositoryStub) Cancel(context.Context, model.MarketReturnCommand) (model.MarketListing, byte, error) {
	return model.MarketListing{}, 0, errors.New("unexpected Cancel")
}
func (repositoryStub) CancelBatch(context.Context, []model.MarketReturnCommand) ([]model.MarketExpiryReturn, error) {
	return nil, errors.New("unexpected CancelBatch")
}
func (repositoryStub) Settle(context.Context, model.MarketSettleCommand) (uint64, error) {
	return 0, errors.New("unexpected Settle")
}
func (repositoryStub) SettleBatch(context.Context, []model.MarketSettleCommand) ([]uint64, error) {
	return nil, errors.New("unexpected SettleBatch")
}
func (repositoryStub) Detail(context.Context, uint64, string, int) (model.MarketListingView, error) {
	return model.MarketListingView{}, errors.New("unexpected Detail")
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
func (repositoryStub) Balance(context.Context, int) (uint64, error) {
	return 0, errors.New("offline")
}

type inventoryStub struct{}

func (inventoryStub) GetCharacterInventory(context.Context, int64) (model.Inventory, error) {
	return model.Inventory{}, errors.New("offline")
}
func (inventoryStub) AddItemToInventory(context.Context, int64, int64, byte) error { return nil }

type worldStub struct{}

func (worldStub) GetPlayerByCharacterID(int) (*model.Player, error) {
	return nil, errors.New("offline")
}

func testSession() *server.Session {
	return &server.Session{ID: "session", UserContext: server.UserContext{CharacterID: 77}}
}

func captureHandler(repo model.OpenMarketRepository) (*Handler, *network.Packet) {
	captured := &network.Packet{}
	h := &Handler{repo: repo, inventory: inventoryStub{}, world: worldStub{}, now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }}
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
			if search.Filter != "Alice" || search.TIDGroupID != 0x10203040 || search.Degree != 9 || search.Page != 2 {
				t.Fatalf("search = %+v", search)
			}
			return []model.MarketListingView{{Seller: "Alice", Listing: model.MarketListing{ID: 0x11223344, Status: model.MarketListingActive, ReferenceItemID: 0x55667788, Quantity: 3, Price: 99, RegisteredAt: registered}}}, 4, nil
		},
	}
	h, captured := captureHandler(repo)
	request := network.EmptyClientPacket()
	request.MessageID = opcode.OpenMarketSearchRequest
	request.WriteByte(3)
	request.WriteByte(2)
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
