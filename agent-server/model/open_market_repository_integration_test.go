package model

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestOpenMarketRepositoryLifecycleIntegration(t *testing.T) {
	dsn := os.Getenv("MARKET_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set MARKET_INTEGRATION_DSN to run against a disposable market database")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		t.Fatal(err)
	}

	const (
		sellerID      = 2000000001
		buyerID       = 2000000002
		itemID        = 2000000003
		cancelItemID  = 2000000004
		cancelItemID2 = 2000000005
		refID         = 1001
	)
	previousRefs := RefItems
	RefItems = map[uint32]RefItem{refID: {RefObject: RefObject{ID: refID}}}
	t.Cleanup(func() { RefItems = previousRefs })
	for _, table := range []string{"OPEN_MARKET_ACTIVE_ITEM_GUARD", "AEGIS_OUTBOX_MESSAGE", "OPEN_MARKET_RECEIVABLE", "OPEN_MARKET_LISTING", "INVENTORY", "ITEM", "ITEMDATA", "CHAR"} {
		if _, err := db.Exec("DELETE FROM `" + table + "` WHERE " + map[string]string{
			"OPEN_MARKET_ACTIVE_ITEM_GUARD": "ITEM_ID IN (2000000003,2000000004,2000000005)",
			"AEGIS_OUTBOX_MESSAGE":   "MESSAGE_ID LIKE 'market-integration:%'",
			"OPEN_MARKET_RECEIVABLE": "SELLER_CHAR_ID IN (2000000001,2000000002)",
			"OPEN_MARKET_LISTING":    "SELLER_CHAR_ID IN (2000000001,2000000002)",
			"INVENTORY":              "FK_CHAR IN (2000000001,2000000002)",
			"ITEM":                   "ID IN (2000000003,2000000004,2000000005)",
			"ITEMDATA":               "ItemID=1001",
			"CHAR":                   "ID IN (2000000001,2000000002)",
		}[table]); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM `OPEN_MARKET_ACTIVE_ITEM_GUARD` WHERE ITEM_ID IN (?,?,?)", itemID, cancelItemID, cancelItemID2)
		_, _ = db.Exec("DELETE FROM `AEGIS_OUTBOX_MESSAGE` WHERE MESSAGE_ID LIKE 'market-integration:%'")
		_, _ = db.Exec("DELETE FROM `OPEN_MARKET_RECEIVABLE` WHERE SELLER_CHAR_ID IN (?,?)", sellerID, buyerID)
		_, _ = db.Exec("DELETE FROM `OPEN_MARKET_LISTING` WHERE SELLER_CHAR_ID IN (?,?)", sellerID, buyerID)
		_, _ = db.Exec("DELETE FROM `INVENTORY` WHERE FK_CHAR IN (?,?)", sellerID, buyerID)
		_, _ = db.Exec("DELETE FROM `ITEM` WHERE ID IN (?,?,?)", itemID, cancelItemID, cancelItemID2)
		_, _ = db.Exec("DELETE FROM `ITEMDATA` WHERE ItemID=?", refID)
		_, _ = db.Exec("DELETE FROM `CHAR` WHERE ID IN (?,?)", sellerID, buyerID)
	})
	for _, character := range []struct {
		id, user, gold int
		name           string
	}{{sellerID, sellerID, 1000, "MarketTestSeller"}, {buyerID, buyerID, 2000, "MarketTestBuyer"}} {
		if _, err := db.Exec("INSERT INTO `CHAR` (ID,REF_OBJ_ID,FK_USER,FK_SHARD,CHAR_NAME,CHAR_SCALE,FK_REGION,GOLD) VALUES (?,?,?,?,?,?,?,?)", character.id, 1, character.user, 1, character.name, 100, 1, character.gold); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO `ITEMDATA` (ItemID,ItemClass) VALUES (?,?)", refID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `ITEM` (ID,FK_REF_ITEM,STACK_COUNT,CREATOR_NAME) VALUES (?,?,?,?)", itemID, refID, 1, "MarketTest"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", sellerID, 13, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `ITEM` (ID,FK_REF_ITEM,STACK_COUNT,CREATOR_NAME) VALUES (?,?,?,?)", cancelItemID, refID, 1, "MarketTest"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", sellerID, 14, cancelItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `ITEM` (ID,FK_REF_ITEM,STACK_COUNT,CREATOR_NAME) VALUES (?,?,?,?)", cancelItemID2, refID, 1, "MarketTest"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", sellerID, 15, cancelItemID2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS=1"); err != nil {
		t.Fatal(err)
	}

	repository, err := NewOpenMarketRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	listing, err := repository.Register(ctx, MarketRegisterCommand{SellerCharacterID: sellerID, SourceSlot: 13, Quantity: 1, ReferenceItemID: refID, TIDGroupID: 1, Price: 500, Deposit: 50, SellFee: 20, ExpiresAt: time.Now().Add(time.Hour), CommandKey: "market-integration:register"})
	if err != nil {
		t.Fatal(err)
	}
	if got := marketIntegrationGold(t, db, sellerID); got != 950 {
		t.Fatalf("seller gold after register = %d, want 950", got)
	}
	sellerListings, err := repository.SellerListings(ctx, sellerID)
	if err != nil || len(sellerListings) != 1 || sellerListings[0].Seller != "MarketTestSeller" {
		t.Fatalf("SellerListings() = %#v, %v; want one seller listing", sellerListings, err)
	}
	searchResults, pages, err := repository.Search(ctx, MarketSearch{Filter: "MarketTestSeller", PageSize: 10})
	if err != nil || pages != 1 || len(searchResults) != 1 || searchResults[0].Listing.ID != listing.ID {
		t.Fatalf("Search() = %#v, pages=%d, %v; want registered listing on one page", searchResults, pages, err)
	}
	detail, err := repository.Detail(ctx, listing.ID, "MarketTestSeller", refID)
	if err != nil || detail.Listing.ID != listing.ID {
		t.Fatalf("Detail() = %#v, %v; want registered listing", detail, err)
	}
	var activeGuardCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM `OPEN_MARKET_ACTIVE_ITEM_GUARD` WHERE LISTING_ID=? AND ITEM_ID=?", listing.ID, itemID).Scan(&activeGuardCount); err != nil || activeGuardCount != 1 {
		t.Fatalf("active listing guard = %d, %v; want one guard", activeGuardCount, err)
	}
	if _, err := db.Exec("DELETE FROM `ITEM` WHERE ID=?", itemID); err == nil {
		t.Fatal("active listing item deletion succeeded; want escrow guard rejection")
	}
	if _, _, err := repository.Purchase(ctx, MarketPurchaseCommand{BuyerCharacterID: buyerID, ListingID: listing.ID, ExpectedVersion: listing.Version, ExpectedReferenceItemID: refID, CommandKey: "market-integration:purchase"}); err != nil {
		t.Fatal(err)
	}
	if got := marketIntegrationGold(t, db, buyerID); got != 1500 {
		t.Fatalf("buyer gold after purchase = %d, want 1500", got)
	}
	var buyerItems int
	if err := db.QueryRow("SELECT COUNT(*) FROM `INVENTORY` WHERE FK_CHAR=? AND FK_ITEM=?", buyerID, itemID).Scan(&buyerItems); err != nil || buyerItems != 1 {
		t.Fatalf("buyer escrow delivery = %d, %v; want one item", buyerItems, err)
	}
	amount, err := repository.Settle(ctx, MarketSettleCommand{SellerCharacterID: sellerID, ListingID: listing.ID, CommandKey: "market-integration:settle"})
	if err != nil {
		t.Fatal(err)
	}
	if amount != 480 || marketIntegrationGold(t, db, sellerID) != 1430 {
		t.Fatalf("settlement amount/gold = %d/%d, want 480/1430", amount, marketIntegrationGold(t, db, sellerID))
	}
	if _, err := db.Exec("DELETE FROM `INVENTORY` WHERE FK_CHAR=? AND FK_ITEM=?", buyerID, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM `ITEM` WHERE ID=?", itemID); err != nil {
		t.Fatalf("terminal listing item deletion: %v", err)
	}
	var terminalItemCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM `ITEM` WHERE ID=?", itemID).Scan(&terminalItemCount); err != nil || terminalItemCount != 0 {
		t.Fatalf("terminal listing item count = %d, %v; want zero", terminalItemCount, err)
	}
	cancelled, err := repository.RegisterBatch(ctx, []MarketRegisterCommand{
		{SellerCharacterID: sellerID, SourceSlot: 14, Quantity: 1, ReferenceItemID: refID, TIDGroupID: 1, Price: 500, ExpiresAt: time.Now().Add(time.Hour), CommandKey: "market-integration:cancel-register-1"},
		{SellerCharacterID: sellerID, SourceSlot: 15, Quantity: 1, ReferenceItemID: refID, TIDGroupID: 1, Price: 500, ExpiresAt: time.Now().Add(time.Hour), CommandKey: "market-integration:cancel-register-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	returned, err := repository.CancelBatch(ctx, []MarketReturnCommand{
		{SellerCharacterID: sellerID, ListingID: cancelled[0].ID, ExpectedVersion: cancelled[0].Version, CommandKey: "market-integration:cancel-1"},
		{SellerCharacterID: sellerID, ListingID: cancelled[1].ID, ExpectedVersion: cancelled[1].Version, CommandKey: "market-integration:cancel-2"},
	})
	if err != nil || len(returned) != 2 {
		t.Fatalf("CancelBatch() = %d returns, %v; want two returns and nil", len(returned), err)
	}
	var returnedItems int
	if err := db.QueryRow("SELECT COUNT(*) FROM `INVENTORY` WHERE FK_CHAR=? AND FK_ITEM IN (?,?)", sellerID, cancelItemID, cancelItemID2).Scan(&returnedItems); err != nil || returnedItems != 2 {
		t.Fatalf("cancelled item returns = %d, %v; want two items", returnedItems, err)
	}
}

func marketIntegrationGold(t *testing.T, db *sql.DB, characterID int) uint64 {
	t.Helper()
	var gold uint64
	if err := db.QueryRow("SELECT GOLD FROM `CHAR` WHERE ID=?", characterID).Scan(&gold); err != nil {
		t.Fatal(err)
	}
	return gold
}
