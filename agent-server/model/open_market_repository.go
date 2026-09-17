package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type MarketListingStatus byte

const (
	MarketListingActive    MarketListingStatus = 1
	MarketListingSold      MarketListingStatus = 2
	MarketListingCancelled MarketListingStatus = 3
	MarketListingExpired   MarketListingStatus = 4
	MarketListingSettled   MarketListingStatus = 5
)

var (
	ErrMarketUnavailable        = errors.New("open market repository is unavailable")
	ErrMarketInvalidCommand     = errors.New("open market command is invalid")
	ErrMarketListingUnavailable = errors.New("open market listing is unavailable")
	ErrMarketInsufficientGold   = errors.New("open market buyer has insufficient gold")
	ErrMarketInventoryFull      = errors.New("open market destination inventory is full")
)

type MarketListing struct {
	ID                 uint64
	SellerCharacterID  int
	BuyerCharacterID   int
	ItemID             int
	ReferenceItemID    int
	TIDGroupID         uint32
	Quantity           uint16
	Price              uint64
	Deposit            uint64
	SellFee            uint64
	Status             MarketListingStatus
	Version            uint64
	RegisteredAt       time.Time
	ExpiresAt          time.Time
	PurchaseCommandKey string
	ReturnCommandKey   string
}

type MarketListingView struct {
	Listing   MarketListing
	Seller    string
	Item      Item
	HasPayout bool
}

type MarketSearch struct {
	TIDGroupID uint32
	Degree     byte
	Filter     string
	Page       byte
	PageSize   byte
}

type MarketExpiryReturn struct {
	Listing MarketListing
	Slot    byte
}

type MarketRegisterCommand struct {
	SellerCharacterID int
	SourceSlot        byte
	Quantity          uint16
	ReferenceItemID   int
	TIDGroupID        uint32
	Price             uint64
	Deposit           uint64
	SellFee           uint64
	ExpiresAt         time.Time
	CommandKey        string
}

type MarketPurchaseCommand struct {
	BuyerCharacterID        int
	ListingID               uint64
	ExpectedVersion         uint64
	ExpectedReferenceItemID int
	CommandKey              string
}

type MarketReturnCommand struct {
	SellerCharacterID int
	ListingID         uint64
	ExpectedVersion   uint64
	CommandKey        string
}

type MarketSettleCommand struct {
	SellerCharacterID int
	ListingID         uint64
	CommandKey        string
}

type OpenMarketRepository interface {
	Register(context.Context, MarketRegisterCommand) (MarketListing, error)
	RegisterBatch(context.Context, []MarketRegisterCommand) ([]MarketListing, error)
	Purchase(context.Context, MarketPurchaseCommand) (MarketListing, byte, error)
	Cancel(context.Context, MarketReturnCommand) (MarketListing, byte, error)
	CancelBatch(context.Context, []MarketReturnCommand) ([]MarketExpiryReturn, error)
	Settle(context.Context, MarketSettleCommand) (uint64, error)
	SettleBatch(context.Context, []MarketSettleCommand) ([]uint64, error)
	Detail(context.Context, uint64, string, int) (MarketListingView, error)
	SellerListings(context.Context, int) ([]MarketListingView, error)
	Search(context.Context, MarketSearch) ([]MarketListingView, uint16, error)
	ExpireDue(context.Context, time.Time, int) ([]MarketExpiryReturn, error)
	Balance(context.Context, int) (uint64, error)
}

type SQLOpenMarketRepository struct{ shard *sql.DB }

func NewOpenMarketRepository(shard *sql.DB) (*SQLOpenMarketRepository, error) {
	if shard == nil {
		return nil, ErrMarketUnavailable
	}
	return &SQLOpenMarketRepository{shard: shard}, nil
}

func validateMarketKey(key string) bool {
	if key == "" || len(key) > 128 || strings.TrimSpace(key) != key {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return true
}

func (r *SQLOpenMarketRepository) Register(ctx context.Context, command MarketRegisterCommand) (MarketListing, error) {
	listings, err := r.RegisterBatch(ctx, []MarketRegisterCommand{command})
	if err != nil {
		return MarketListing{}, err
	}
	return listings[0], nil
}

func (r *SQLOpenMarketRepository) RegisterBatch(ctx context.Context, commands []MarketRegisterCommand) ([]MarketListing, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMarketUnavailable
	}
	if len(commands) == 0 || len(commands) > 8 {
		return nil, ErrMarketInvalidCommand
	}
	seenSlots := make(map[byte]struct{}, len(commands))
	seller := commands[0].SellerCharacterID
	for _, command := range commands {
		if command.SellerCharacterID <= 0 || command.SellerCharacterID != seller || command.Quantity == 0 || command.ReferenceItemID <= 0 || command.Price == 0 || !validateMarketKey(command.CommandKey) || command.ExpiresAt.IsZero() {
			return nil, ErrMarketInvalidCommand
		}
		if _, exists := seenSlots[command.SourceSlot]; exists {
			return nil, ErrMarketInvalidCommand
		}
		seenSlots[command.SourceSlot] = struct{}{}
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin market registration: %w", err)
	}
	defer tx.Rollback()
	listings := make([]MarketListing, 0, len(commands))
	for _, command := range commands {
		listing, registerErr := registerMarketListing(ctx, tx, command)
		if registerErr != nil {
			return nil, registerErr
		}
		listings = append(listings, listing)
		if err = enqueueMarketTransition(ctx, tx, command.CommandKey, listing, "market.listing.registered"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit market registration: %w", err)
	}
	return listings, nil
}

func registerMarketListing(ctx context.Context, tx *sql.Tx, command MarketRegisterCommand) (MarketListing, error) {
	var err error
	if existing, found := marketListingByCommand(ctx, tx, "REGISTER_COMMAND_KEY", command.CommandKey); found {
		if existing.SellerCharacterID != command.SellerCharacterID || existing.ReferenceItemID != command.ReferenceItemID || existing.Quantity != command.Quantity || existing.Price != command.Price {
			return MarketListing{}, ErrMarketInvalidCommand
		}
		return existing, nil
	}
	if command.Deposit > 0 {
		result, debitErr := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE ID=? AND GOLD>=?", command.Deposit, command.SellerCharacterID, command.Deposit)
		if debitErr != nil {
			return MarketListing{}, debitErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return MarketListing{}, ErrMarketInsufficientGold
		}
	}
	var itemID, refID int
	var stack uint16
	err = tx.QueryRowContext(ctx, "SELECT i.ID,i.FK_REF_ITEM,i.STACK_COUNT FROM `INVENTORY` inv JOIN `ITEM` i ON i.ID=inv.FK_ITEM WHERE inv.FK_CHAR=? AND inv.SLOT=? FOR UPDATE", command.SellerCharacterID, command.SourceSlot).Scan(&itemID, &refID, &stack)
	if err != nil || refID != command.ReferenceItemID || command.Quantity > stack {
		return MarketListing{}, ErrMarketListingUnavailable
	}
	escrowID := itemID
	if command.Quantity == stack {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE FK_CHAR=? AND SLOT=? AND FK_ITEM=?", command.SellerCharacterID, command.SourceSlot, itemID); err != nil {
			return MarketListing{}, fmt.Errorf("escrow market item: %w", err)
		}
	} else {
		result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET STACK_COUNT=STACK_COUNT-? WHERE ID=? AND STACK_COUNT=?", command.Quantity, itemID, stack)
		if updateErr != nil {
			err = updateErr
			return MarketListing{}, fmt.Errorf("split market stack: %w", err)
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return MarketListing{}, ErrMarketListingUnavailable
		}
		result, cloneErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) SELECT `VARIANCE`,`FK_REF_ITEM`,?,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME` FROM `ITEM` WHERE ID=?", command.Quantity, itemID)
		if cloneErr != nil {
			return MarketListing{}, fmt.Errorf("clone market stack: %w", cloneErr)
		}
		id, _ := result.LastInsertId()
		escrowID = int(id)
		for _, q := range []string{"INSERT INTO `ITEM_MAGIC_OPTION` SELECT ?,`SLOT`,`TYPE`,`VALUE` FROM `ITEM_MAGIC_OPTION` WHERE FK_ITEM=?", "INSERT INTO `ITEM_BINDING_OPTION` SELECT ?,`OPTION_TYPE`,`SLOT`,`OPTION_ID`,`OPTION_VALUE` FROM `ITEM_BINDING_OPTION` WHERE FK_ITEM=?", "INSERT INTO `ITEM_ALCHEMY_PROTECTION` SELECT ?,`ASTRAL`,`IMMORTAL`,`STEADY` FROM `ITEM_ALCHEMY_PROTECTION` WHERE FK_ITEM=?"} {
			if _, err = tx.ExecContext(ctx, q, escrowID, itemID); err != nil {
				return MarketListing{}, fmt.Errorf("clone market item options: %w", err)
			}
		}
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO `OPEN_MARKET_LISTING` (`SELLER_CHAR_ID`,`ITEM_ID`,`REF_ITEM_ID`,`TID_GROUP_ID`,`QUANTITY`,`PRICE`,`DEPOSIT`,`SELL_FEE`,`REGISTER_COMMAND_KEY`,`RETURN_SLOT`,`EXPIRES_AT`) VALUES (?,?,?,?,?,?,?,?,?,?,?)", command.SellerCharacterID, escrowID, refID, command.TIDGroupID, command.Quantity, command.Price, command.Deposit, command.SellFee, command.CommandKey, command.SourceSlot, command.ExpiresAt.UTC())
	if err != nil {
		return MarketListing{}, fmt.Errorf("insert market listing: %w", err)
	}
	id, _ := result.LastInsertId()
	return MarketListing{ID: uint64(id), SellerCharacterID: command.SellerCharacterID, ItemID: escrowID, ReferenceItemID: refID, TIDGroupID: command.TIDGroupID, Quantity: command.Quantity, Price: command.Price, Deposit: command.Deposit, SellFee: command.SellFee, Status: MarketListingActive, Version: 1, ExpiresAt: command.ExpiresAt.UTC()}, nil
}

func freeInventorySlot(ctx context.Context, tx *sql.Tx, characterID int) (byte, error) {
	var size int
	if err := tx.QueryRowContext(ctx, "SELECT INVENTORY_SIZE FROM `CHAR` WHERE ID=? FOR UPDATE", characterID).Scan(&size); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT SLOT FROM `INVENTORY` WHERE FK_CHAR=? FOR UPDATE", characterID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var slot int
		if err = rows.Scan(&slot); err != nil {
			return 0, err
		}
		used[slot] = true
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for slot := 13; slot < size && slot < 256; slot++ {
		if !used[slot] {
			return byte(slot), nil
		}
	}
	return 0, ErrMarketInventoryFull
}

const marketListingColumns = "ID,SELLER_CHAR_ID,BUYER_CHAR_ID,ITEM_ID,REF_ITEM_ID,TID_GROUP_ID,QUANTITY,PRICE,DEPOSIT,SELL_FEE,STATUS,VERSION,REGISTERED_AT,EXPIRES_AT,COALESCE(PURCHASE_COMMAND_KEY,''),COALESCE(RETURN_COMMAND_KEY,'')"

type marketListingScanner interface{ Scan(...any) error }

func scanMarketListing(row marketListingScanner) (MarketListing, error) {
	var l MarketListing
	var buyer sql.NullInt64
	err := row.Scan(&l.ID, &l.SellerCharacterID, &buyer, &l.ItemID, &l.ReferenceItemID, &l.TIDGroupID, &l.Quantity, &l.Price, &l.Deposit, &l.SellFee, &l.Status, &l.Version, &l.RegisteredAt, &l.ExpiresAt, &l.PurchaseCommandKey, &l.ReturnCommandKey)
	if err != nil {
		return MarketListing{}, ErrMarketListingUnavailable
	}
	if buyer.Valid {
		l.BuyerCharacterID = int(buyer.Int64)
	}
	return l, nil
}

func scanMarketListingView(row marketListingScanner) (MarketListingView, error) {
	var view MarketListingView
	var buyer sql.NullInt64
	var payout int
	err := row.Scan(&view.Listing.ID, &view.Listing.SellerCharacterID, &buyer, &view.Listing.ItemID, &view.Listing.ReferenceItemID, &view.Listing.TIDGroupID, &view.Listing.Quantity, &view.Listing.Price, &view.Listing.Deposit, &view.Listing.SellFee, &view.Listing.Status, &view.Listing.Version, &view.Listing.RegisteredAt, &view.Listing.ExpiresAt, &view.Listing.PurchaseCommandKey, &view.Listing.ReturnCommandKey, &view.Seller, &payout)
	if err != nil {
		return MarketListingView{}, err
	}
	if buyer.Valid {
		view.Listing.BuyerCharacterID = int(buyer.Int64)
	}
	view.HasPayout = payout != 0
	return view, nil
}

const marketViewColumns = "l.ID,l.SELLER_CHAR_ID,l.BUYER_CHAR_ID,l.ITEM_ID,l.REF_ITEM_ID,l.TID_GROUP_ID,l.QUANTITY,l.PRICE,l.DEPOSIT,l.SELL_FEE,l.STATUS,l.VERSION,l.REGISTERED_AT,l.EXPIRES_AT,COALESCE(l.PURCHASE_COMMAND_KEY,''),COALESCE(l.RETURN_COMMAND_KEY,''),c.CHARNAME,CASE WHEN r.LISTING_ID IS NULL OR r.SETTLED_AT IS NOT NULL THEN 0 ELSE 1 END"

func marketViewJoins() string {
	return " FROM `OPEN_MARKET_LISTING` l JOIN `CHAR` c ON c.ID=l.SELLER_CHAR_ID JOIN `ITEMDATA` ri ON ri.ItemID=l.REF_ITEM_ID LEFT JOIN `OPEN_MARKET_RECEIVABLE` r ON r.LISTING_ID=l.ID"
}

func (r *SQLOpenMarketRepository) Detail(ctx context.Context, listingID uint64, seller string, referenceItemID int) (MarketListingView, error) {
	if r == nil || r.shard == nil {
		return MarketListingView{}, ErrMarketUnavailable
	}
	if listingID == 0 || listingID > math.MaxUint32 || seller == "" || referenceItemID <= 0 {
		return MarketListingView{}, ErrMarketInvalidCommand
	}
	view, err := scanMarketListingView(r.shard.QueryRowContext(ctx, "SELECT "+marketViewColumns+marketViewJoins()+" WHERE l.ID=? AND c.CHARNAME=? AND l.REF_ITEM_ID=?", listingID, seller, referenceItemID))
	if err != nil {
		return MarketListingView{}, ErrMarketListingUnavailable
	}
	item, err := r.loadListingItem(ctx, view.Listing)
	if err != nil {
		return MarketListingView{}, err
	}
	view.Item = item
	return view, nil
}

func (r *SQLOpenMarketRepository) SellerListings(ctx context.Context, sellerCharacterID int) ([]MarketListingView, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMarketUnavailable
	}
	if sellerCharacterID <= 0 {
		return nil, ErrMarketInvalidCommand
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT "+marketViewColumns+marketViewJoins()+" WHERE l.SELLER_CHAR_ID=? AND l.STATUS IN (1,2,4) ORDER BY l.REGISTERED_AT DESC,l.ID DESC LIMIT 8", sellerCharacterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := make([]MarketListingView, 0, 8)
	for rows.Next() {
		view, scanErr := scanMarketListingView(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func (r *SQLOpenMarketRepository) Search(ctx context.Context, search MarketSearch) ([]MarketListingView, uint16, error) {
	if r == nil || r.shard == nil {
		return nil, 0, ErrMarketUnavailable
	}
	if search.PageSize == 0 || search.PageSize > 20 || len(search.Filter) > 64 || strings.TrimSpace(search.Filter) != search.Filter {
		return nil, 0, ErrMarketInvalidCommand
	}
	filter := "%" + search.Filter + "%"
	var total uint64
	if err := r.shard.QueryRowContext(ctx, "SELECT COUNT(*)"+marketViewJoins()+" WHERE l.STATUS=1 AND l.EXPIRES_AT>CURRENT_TIMESTAMP(6) AND (?=0 OR l.TID_GROUP_ID=?) AND (?=0 OR ((ri.ItemClass-1) DIV 3)+1=?) AND (?='' OR c.CHARNAME LIKE ?)", search.TIDGroupID, search.TIDGroupID, search.Degree, search.Degree, search.Filter, filter).Scan(&total); err != nil {
		return nil, 0, err
	}
	pages := (total + uint64(search.PageSize) - 1) / uint64(search.PageSize)
	if pages > math.MaxUint16 {
		pages = math.MaxUint16
	}
	offset := uint64(search.Page) * uint64(search.PageSize)
	rows, err := r.shard.QueryContext(ctx, "SELECT "+marketViewColumns+marketViewJoins()+" WHERE l.STATUS=1 AND l.EXPIRES_AT>CURRENT_TIMESTAMP(6) AND (?=0 OR l.TID_GROUP_ID=?) AND (?=0 OR ((ri.ItemClass-1) DIV 3)+1=?) AND (?='' OR c.CHARNAME LIKE ?) ORDER BY l.REGISTERED_AT DESC,l.ID DESC LIMIT ? OFFSET ?", search.TIDGroupID, search.TIDGroupID, search.Degree, search.Degree, search.Filter, filter, search.PageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	views := make([]MarketListingView, 0, search.PageSize)
	for rows.Next() {
		view, scanErr := scanMarketListingView(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		views = append(views, view)
	}
	return views, uint16(pages), rows.Err()
}

func (r *SQLOpenMarketRepository) Balance(ctx context.Context, characterID int) (uint64, error) {
	if r == nil || r.shard == nil {
		return 0, ErrMarketUnavailable
	}
	if characterID <= 0 {
		return 0, ErrMarketInvalidCommand
	}
	var gold uint64
	if err := r.shard.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE ID=?", characterID).Scan(&gold); err != nil {
		return 0, err
	}
	return gold, nil
}

const selectOpenMarketItem = "SELECT 0,it.ID,it.VARIANCE,it.FK_REF_ITEM,it.STACK_COUNT,it.OPT_LEVEL,it.DATA,it.CREATOR_NAME,it.RENT_TYPE,it.RENT_CAN_DELETE,it.RENT_CAN_RECHARGE,it.RENT_PERIOD_BEGIN_TIME,it.RENT_PERIOD_END_TIME,it.RENT_METER_RATE_TIME,it.RENT_PACKING_TIME,COALESCE(cos.REF_OBJECT_ID,0),COALESCE(cos.KIND,0),COALESCE(cos.STATE,0),COALESCE(cos.NAME,''),COALESCE(cos.LEVEL,0),cos.RENT_ENDS_AT,COALESCE(cos.INVENTORY_SLOT,0),COALESCE(cos.HEALTH,0),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',mo.SLOT,'type',mo.TYPE,'value',mo.VALUE,'paramType',COALESCE(ro.PARAM_TYPE,0))) FROM `ITEM_MAGIC_OPTION` mo LEFT JOIN `REF_MAGIC_OPTION` ro ON ro.ID=mo.TYPE WHERE mo.FK_ITEM=it.ID),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=1),JSON_ARRAY()),COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('slot',bo.SLOT,'id',bo.OPTION_ID,'value',bo.OPTION_VALUE)) FROM `ITEM_BINDING_OPTION` bo WHERE bo.FK_ITEM=it.ID AND bo.OPTION_TYPE=2),JSON_ARRAY()),COALESCE(ap.ASTRAL,0),COALESCE(ap.IMMORTAL,0),COALESCE(ap.STEADY,0) FROM `ITEM` it LEFT JOIN `CHAR_COS` cos ON cos.ID=it.DATA AND cos.OWNER_CHAR_ID=? LEFT JOIN `ITEM_ALCHEMY_PROTECTION` ap ON ap.FK_ITEM=it.ID WHERE it.ID=?"

func (r *SQLOpenMarketRepository) loadListingItem(ctx context.Context, listing MarketListing) (Item, error) {
	_, item, err := scanInventoryItem(r.shard.QueryRowContext(ctx, selectOpenMarketItem, listing.SellerCharacterID, listing.ItemID))
	if err != nil {
		return Item{}, fmt.Errorf("load open-market item: %w", err)
	}
	return item, nil
}

func (r *SQLOpenMarketRepository) ExpireDue(ctx context.Context, now time.Time, limit int) ([]MarketExpiryReturn, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMarketUnavailable
	}
	if now.IsZero() || limit < 1 || limit > 100 {
		return nil, ErrMarketInvalidCommand
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT ID FROM `OPEN_MARKET_LISTING` WHERE STATUS=1 AND EXPIRES_AT<=? ORDER BY EXPIRES_AT,ID LIMIT ?", now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	returns := make([]MarketExpiryReturn, 0, len(ids))
	for _, id := range ids {
		returned, found, expireErr := r.expireOne(ctx, now.UTC(), id)
		if expireErr != nil {
			if errors.Is(expireErr, ErrMarketInventoryFull) {
				continue
			}
			return returns, expireErr
		}
		if found {
			returns = append(returns, returned)
		}
	}
	return returns, nil
}

func (r *SQLOpenMarketRepository) expireOne(ctx context.Context, now time.Time, listingID uint64) (MarketExpiryReturn, bool, error) {
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MarketExpiryReturn{}, false, err
	}
	defer tx.Rollback()
	listing, err := scanLockedListing(ctx, tx, listingID)
	if err != nil || listing.Status != MarketListingActive || listing.ExpiresAt.After(now) {
		return MarketExpiryReturn{}, false, nil
	}
	slot, err := freeInventorySlot(ctx, tx, listing.SellerCharacterID)
	if err != nil {
		return MarketExpiryReturn{}, false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", listing.SellerCharacterID, slot, listing.ItemID); err != nil {
		return MarketExpiryReturn{}, false, err
	}
	key := fmt.Sprintf("expire:%d", listing.ID)
	result, err := tx.ExecContext(ctx, "UPDATE `OPEN_MARKET_LISTING` SET STATUS=4,VERSION=VERSION+1,RETURN_COMMAND_KEY=?,RETURN_SLOT=? WHERE ID=? AND STATUS=1 AND VERSION=? AND EXPIRES_AT<=?", key, slot, listing.ID, listing.Version, now)
	if err != nil {
		return MarketExpiryReturn{}, false, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return MarketExpiryReturn{}, false, nil
	}
	listing.Status = MarketListingExpired
	listing.Version++
	listing.ReturnCommandKey = key
	if err = enqueueMarketTransition(ctx, tx, key, listing, "market.listing.expired"); err != nil {
		return MarketExpiryReturn{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return MarketExpiryReturn{}, false, err
	}
	return MarketExpiryReturn{Listing: listing, Slot: slot}, true, nil
}

func scanLockedListing(ctx context.Context, tx *sql.Tx, id uint64) (MarketListing, error) {
	return scanMarketListing(tx.QueryRowContext(ctx, "SELECT "+marketListingColumns+" FROM `OPEN_MARKET_LISTING` WHERE ID=? FOR UPDATE", id))
}

func marketListingByCommand(ctx context.Context, tx *sql.Tx, column, key string) (MarketListing, bool) {
	if column != "REGISTER_COMMAND_KEY" && column != "PURCHASE_COMMAND_KEY" && column != "RETURN_COMMAND_KEY" {
		return MarketListing{}, false
	}
	listing, err := scanMarketListing(tx.QueryRowContext(ctx, "SELECT "+marketListingColumns+" FROM `OPEN_MARKET_LISTING` WHERE `"+column+"`=? FOR UPDATE", key))
	return listing, err == nil
}

func (r *SQLOpenMarketRepository) Purchase(ctx context.Context, command MarketPurchaseCommand) (MarketListing, byte, error) {
	if r == nil || r.shard == nil {
		return MarketListing{}, 0, ErrMarketUnavailable
	}
	if command.BuyerCharacterID <= 0 || command.ListingID == 0 || command.ExpectedVersion == 0 || command.ExpectedReferenceItemID <= 0 || !validateMarketKey(command.CommandKey) {
		return MarketListing{}, 0, ErrMarketInvalidCommand
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MarketListing{}, 0, err
	}
	defer tx.Rollback()
	l, err := scanLockedListing(ctx, tx, command.ListingID)
	if err != nil {
		return MarketListing{}, 0, err
	}
	if l.PurchaseCommandKey == command.CommandKey && l.BuyerCharacterID == command.BuyerCharacterID {
		var slot byte
		if err := tx.QueryRowContext(ctx, "SELECT SLOT FROM `INVENTORY` WHERE FK_CHAR=? AND FK_ITEM=?", command.BuyerCharacterID, l.ItemID).Scan(&slot); err != nil {
			return MarketListing{}, 0, ErrMarketListingUnavailable
		}
		return l, slot, nil
	}
	if l.Status != MarketListingActive || l.Version != command.ExpectedVersion || l.ReferenceItemID != command.ExpectedReferenceItemID || !time.Now().UTC().Before(l.ExpiresAt) || l.SellerCharacterID == command.BuyerCharacterID {
		return MarketListing{}, 0, ErrMarketListingUnavailable
	}
	slot, err := freeInventorySlot(ctx, tx, command.BuyerCharacterID)
	if err != nil {
		return MarketListing{}, 0, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE ID=? AND GOLD>=?", l.Price, command.BuyerCharacterID, l.Price)
	if err != nil {
		return MarketListing{}, 0, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return MarketListing{}, 0, ErrMarketInsufficientGold
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", command.BuyerCharacterID, slot, l.ItemID); err != nil {
		return MarketListing{}, 0, err
	}
	amount := l.Price
	if l.SellFee > amount {
		amount = 0
	} else {
		amount -= l.SellFee
	}
	if amount == 0 {
		return MarketListing{}, 0, ErrMarketInvalidCommand
	}
	result, err = tx.ExecContext(ctx, "UPDATE `OPEN_MARKET_LISTING` SET BUYER_CHAR_ID=?,STATUS=2,VERSION=VERSION+1,PURCHASE_COMMAND_KEY=?,SOLD_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND STATUS=1 AND VERSION=?", command.BuyerCharacterID, command.CommandKey, l.ID, l.Version)
	if err != nil {
		return MarketListing{}, 0, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return MarketListing{}, 0, ErrMarketListingUnavailable
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `OPEN_MARKET_RECEIVABLE` (LISTING_ID,SELLER_CHAR_ID,AMOUNT) VALUES (?,?,?)", l.ID, l.SellerCharacterID, amount); err != nil {
		return MarketListing{}, 0, err
	}
	l.Status = MarketListingSold
	l.Version++
	l.BuyerCharacterID = command.BuyerCharacterID
	if err = enqueueMarketTransition(ctx, tx, command.CommandKey, l, "market.listing.purchased"); err != nil {
		return MarketListing{}, 0, err
	}
	if err = tx.Commit(); err != nil {
		return MarketListing{}, 0, err
	}
	return l, slot, nil
}

func (r *SQLOpenMarketRepository) Cancel(ctx context.Context, command MarketReturnCommand) (MarketListing, byte, error) {
	returns, err := r.CancelBatch(ctx, []MarketReturnCommand{command})
	if err != nil {
		return MarketListing{}, 0, err
	}
	return returns[0].Listing, returns[0].Slot, nil
}

func (r *SQLOpenMarketRepository) CancelBatch(ctx context.Context, commands []MarketReturnCommand) ([]MarketExpiryReturn, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMarketUnavailable
	}
	if len(commands) == 0 || len(commands) > 8 {
		return nil, ErrMarketInvalidCommand
	}
	seller := commands[0].SellerCharacterID
	seen := make(map[uint64]struct{}, len(commands))
	for _, command := range commands {
		if command.SellerCharacterID <= 0 || command.SellerCharacterID != seller || command.ListingID == 0 || command.ExpectedVersion == 0 || !validateMarketKey(command.CommandKey) {
			return nil, ErrMarketInvalidCommand
		}
		if _, exists := seen[command.ListingID]; exists {
			return nil, ErrMarketInvalidCommand
		}
		seen[command.ListingID] = struct{}{}
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	returns := make([]MarketExpiryReturn, 0, len(commands))
	for _, command := range commands {
		listing, slot, cancelErr := cancelMarketListing(ctx, tx, command)
		if cancelErr != nil {
			return nil, cancelErr
		}
		returns = append(returns, MarketExpiryReturn{Listing: listing, Slot: slot})
		if err = enqueueMarketTransition(ctx, tx, command.CommandKey, listing, "market.listing.cancelled"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return returns, nil
}

func cancelMarketListing(ctx context.Context, tx *sql.Tx, command MarketReturnCommand) (MarketListing, byte, error) {
	l, err := scanLockedListing(ctx, tx, command.ListingID)
	if err != nil {
		return MarketListing{}, 0, err
	}
	if l.ReturnCommandKey == command.CommandKey && l.SellerCharacterID == command.SellerCharacterID {
		var slot byte
		if err := tx.QueryRowContext(ctx, "SELECT SLOT FROM `INVENTORY` WHERE FK_CHAR=? AND FK_ITEM=?", command.SellerCharacterID, l.ItemID).Scan(&slot); err != nil {
			return MarketListing{}, 0, ErrMarketListingUnavailable
		}
		return l, slot, nil
	}
	if l.SellerCharacterID != command.SellerCharacterID || l.Status != MarketListingActive || l.Version != command.ExpectedVersion {
		return MarketListing{}, 0, ErrMarketListingUnavailable
	}
	slot, err := freeInventorySlot(ctx, tx, command.SellerCharacterID)
	if err != nil {
		return MarketListing{}, 0, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (FK_CHAR,SLOT,FK_ITEM) VALUES (?,?,?)", command.SellerCharacterID, slot, l.ItemID); err != nil {
		return MarketListing{}, 0, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE `OPEN_MARKET_LISTING` SET STATUS=3,VERSION=VERSION+1,RETURN_COMMAND_KEY=? WHERE ID=? AND STATUS=1 AND VERSION=?", command.CommandKey, l.ID, l.Version)
	if err != nil {
		return MarketListing{}, 0, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return MarketListing{}, 0, ErrMarketListingUnavailable
	}
	l.Status = MarketListingCancelled
	l.Version++
	return l, slot, nil
}

func (r *SQLOpenMarketRepository) Settle(ctx context.Context, command MarketSettleCommand) (uint64, error) {
	amounts, err := r.SettleBatch(ctx, []MarketSettleCommand{command})
	if err != nil {
		return 0, err
	}
	return amounts[0], nil
}

func (r *SQLOpenMarketRepository) SettleBatch(ctx context.Context, commands []MarketSettleCommand) ([]uint64, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMarketUnavailable
	}
	if len(commands) == 0 || len(commands) > 8 {
		return nil, ErrMarketInvalidCommand
	}
	seller := commands[0].SellerCharacterID
	seen := make(map[uint64]struct{}, len(commands))
	for _, command := range commands {
		if command.SellerCharacterID <= 0 || command.SellerCharacterID != seller || command.ListingID == 0 || !validateMarketKey(command.CommandKey) {
			return nil, ErrMarketInvalidCommand
		}
		if _, exists := seen[command.ListingID]; exists {
			return nil, ErrMarketInvalidCommand
		}
		seen[command.ListingID] = struct{}{}
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	amounts := make([]uint64, 0, len(commands))
	for _, command := range commands {
		amount, settleErr := settleMarketListing(ctx, tx, command)
		if settleErr != nil {
			return nil, settleErr
		}
		amounts = append(amounts, amount)
		listing := MarketListing{ID: command.ListingID, SellerCharacterID: command.SellerCharacterID, Status: MarketListingSettled}
		if err = enqueueMarketTransition(ctx, tx, command.CommandKey, listing, "market.listing.settled"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return amounts, nil
}

func enqueueMarketTransition(ctx context.Context, tx *sql.Tx, messageID string, listing MarketListing, topic string) error {
	aggregateID := strconv.FormatUint(listing.ID, 10)
	var existingVersion uint64
	err := tx.QueryRowContext(ctx, "SELECT `AGGREGATE_VERSION` FROM `AEGIS_OUTBOX_MESSAGE` WHERE `MESSAGE_ID`=?", messageID).Scan(&existingVersion)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check market transition replay: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO `AEGIS_AGGREGATE_VERSION` (`AGGREGATE_KIND`,`AGGREGATE_ID`,`VERSION`) VALUES ('market',?,1) ON DUPLICATE KEY UPDATE `VERSION`=`VERSION`+1", aggregateID); err != nil {
		return fmt.Errorf("advance market aggregate version: %w", err)
	}
	var version uint64
	if err := tx.QueryRowContext(ctx, "SELECT `VERSION` FROM `AEGIS_AGGREGATE_VERSION` WHERE `AGGREGATE_KIND`='market' AND `AGGREGATE_ID`=?", aggregateID).Scan(&version); err != nil {
		return fmt.Errorf("read market aggregate version: %w", err)
	}
	payload := fmt.Sprintf(`{"listing_id":%d,"seller_character_id":%d,"buyer_character_id":%d,"status":%d,"listing_version":%d}`, listing.ID, listing.SellerCharacterID, listing.BuyerCharacterID, listing.Status, listing.Version)
	if _, err := tx.ExecContext(ctx, "INSERT INTO `AEGIS_OUTBOX_MESSAGE` (`MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`OCCURRED_AT`) VALUES (?,'market',?,?,?,?,CURRENT_TIMESTAMP(6))", messageID, aggregateID, version, topic, []byte(payload)); err != nil {
		return fmt.Errorf("enqueue market transition: %w", err)
	}
	return nil
}

func settleMarketListing(ctx context.Context, tx *sql.Tx, command MarketSettleCommand) (uint64, error) {
	var amount uint64
	var settled sql.NullTime
	var settlementKey sql.NullString
	var err error
	if err = tx.QueryRowContext(ctx, "SELECT AMOUNT,SETTLED_AT,SETTLEMENT_COMMAND_KEY FROM `OPEN_MARKET_RECEIVABLE` WHERE LISTING_ID=? AND SELLER_CHAR_ID=? FOR UPDATE", command.ListingID, command.SellerCharacterID).Scan(&amount, &settled, &settlementKey); err != nil {
		return 0, ErrMarketListingUnavailable
	}
	if settled.Valid {
		if !settlementKey.Valid || settlementKey.String != command.CommandKey {
			return 0, ErrMarketInvalidCommand
		}
		return amount, nil
	}
	result, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD+? WHERE ID=?", amount, command.SellerCharacterID)
	if err != nil {
		return 0, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return 0, ErrMarketListingUnavailable
	}
	result, err = tx.ExecContext(ctx, "UPDATE `OPEN_MARKET_RECEIVABLE` SET SETTLEMENT_COMMAND_KEY=?,SETTLED_AT=CURRENT_TIMESTAMP(6) WHERE LISTING_ID=? AND SETTLED_AT IS NULL", command.CommandKey, command.ListingID)
	if err != nil {
		return 0, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return 0, ErrMarketListingUnavailable
	}
	result, err = tx.ExecContext(ctx, "UPDATE `OPEN_MARKET_LISTING` SET STATUS=5,VERSION=VERSION+1,SETTLED_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND STATUS=2", command.ListingID)
	if err != nil {
		return 0, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return 0, ErrMarketListingUnavailable
	}
	return amount, nil
}
