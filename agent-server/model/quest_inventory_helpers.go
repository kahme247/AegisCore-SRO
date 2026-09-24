package model

import (
	"context"
	"database/sql"
	"fmt"
	"math"
)

type questInventoryItem struct {
	id    int64
	refID uint32
	stack int
}

func lockQuestInventory(ctx context.Context, tx *sql.Tx, characterID int) (map[byte]questInventoryItem, error) {
	rows, err := tx.QueryContext(ctx, "SELECT i.`SLOT`,i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` WHERE i.`FK_CHAR`=? ORDER BY i.`SLOT` FOR UPDATE", characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[byte]questInventoryItem)
	for rows.Next() {
		var slot byte
		var item questInventoryItem
		if err = rows.Scan(&slot, &item.id, &item.refID, &item.stack); err != nil {
			return nil, err
		}
		items[slot] = item
	}
	return items, rows.Err()
}

func findQuestItem(items map[byte]questInventoryItem, refID uint32) (byte, questInventoryItem, bool) {
	for slot := byte(13); slot < 255; slot++ {
		if item, ok := items[slot]; ok && item.refID == refID && item.stack > 0 {
			return slot, item, true
		}
	}
	return 0, questInventoryItem{}, false
}

func questInventoryContains(items map[byte]questInventoryItem, required map[uint32]uint64) bool {
	totals := make(map[uint32]uint64, len(required))
	for _, item := range items {
		if _, wanted := required[item.refID]; wanted && item.stack > 0 {
			amount := uint64(item.stack)
			if totals[item.refID] > math.MaxUint64-amount {
				return false
			}
			totals[item.refID] += amount
		}
	}
	for refID, quantity := range required {
		if totals[refID] < quantity {
			return false
		}
	}
	return true
}

func freeQuestInventorySlot(items map[byte]questInventoryItem, capacity byte) (byte, bool) {
	if capacity == 0 {
		capacity = BaseInventorySize
	}
	for slot := byte(13); slot < capacity; slot++ {
		if _, occupied := items[slot]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func insertQuestItem(ctx context.Context, tx *sql.Tx, characterID int, slot byte, refID uint32) (Item, error) {
	return insertQuestItemStack(ctx, tx, characterID, slot, refID, 1)
}

func insertQuestItemStack(ctx context.Context, tx *sql.Tx, characterID int, slot byte, refID uint32, quantity int) (Item, error) {
	if _, ok := RefItems[refID]; !ok {
		return Item{}, fmt.Errorf("%w: %d", ErrInventoryReferenceItemNotFound, refID)
	}
	if quantity <= 0 {
		return Item{}, ErrQuestStateConflict
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (0,?,?,0,0,0,0,0,0,0,0,0)", refID, quantity)
	if err != nil {
		return Item{}, err
	}
	itemID, err := insert.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); err != nil {
		return Item{}, err
	}
	return economyItem(itemID, refID, 0, quantity, 0, 0, RentInfo{})
}

func loadQuestInventory(ctx context.Context, shard *sql.DB, characterID int) (Inventory, error) {
	repository, err := NewInventoryRepository(shard)
	if err != nil {
		return Inventory{}, err
	}
	return repository.GetCharacterInventory(ctx, int64(characterID))
}
