package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrMonsterMaskRepositoryUnavailable = errors.New("monster mask repository is unavailable")
	ErrMonsterMaskInventoryFull         = errors.New("monster mask inventory is full")
)

type MonsterMaskRepository interface {
	CreateMonsterMask(context.Context, int64, uint32, uint32) (Inventory, byte, error)
}

type SQLMonsterMaskRepository struct {
	shard *sql.DB
}

func NewMonsterMaskRepository(shard *sql.DB) (*SQLMonsterMaskRepository, error) {
	if shard == nil {
		return nil, ErrMonsterMaskRepositoryUnavailable
	}
	return &SQLMonsterMaskRepository{shard: shard}, nil
}

func (r *SQLMonsterMaskRepository) CreateMonsterMask(ctx context.Context, characterID int64, refItemID, refCharID uint32) (inventory Inventory, slot byte, err error) {
	if r == nil || r.shard == nil {
		return emptyInventory(), 0, ErrMonsterMaskRepositoryUnavailable
	}
	if characterID <= 0 || refItemID == 0 || refCharID == 0 {
		return emptyInventory(), 0, fmt.Errorf("invalid monster mask request")
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return emptyInventory(), 0, fmt.Errorf("begin monster mask: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var capacity int
	if err = tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `SRO_SHARD`.`CHAR` WHERE `ID`=? FOR UPDATE", characterID).Scan(&capacity); err != nil {
		return emptyInventory(), 0, fmt.Errorf("lock monster mask owner: %w", err)
	}
	if capacity <= 0 || capacity > int(^byte(0)) {
		capacity = int(BaseInventorySize)
	}
	rows, queryErr := tx.QueryContext(ctx, "SELECT `SLOT` FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? FOR UPDATE", characterID)
	if queryErr != nil {
		err = fmt.Errorf("lock monster mask inventory: %w", queryErr)
		return emptyInventory(), 0, err
	}
	occupied := make(map[byte]struct{})
	for rows.Next() {
		var persisted int
		if scanErr := rows.Scan(&persisted); scanErr != nil {
			_ = rows.Close()
			err = fmt.Errorf("scan monster mask inventory: %w", scanErr)
			return emptyInventory(), 0, err
		}
		if persisted >= 0 && persisted <= int(^byte(0)) {
			occupied[byte(persisted)] = struct{}{}
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close()
		err = fmt.Errorf("iterate monster mask inventory: %w", rowsErr)
		return emptyInventory(), 0, err
	}
	if closeErr := rows.Close(); closeErr != nil {
		err = fmt.Errorf("close monster mask inventory: %w", closeErr)
		return emptyInventory(), 0, err
	}
	found := false
	for candidate := 13; candidate < capacity; candidate++ {
		if _, exists := occupied[byte(candidate)]; !exists {
			slot, found = byte(candidate), true
			break
		}
	}
	if !found {
		err = ErrMonsterMaskInventoryFull
		return emptyInventory(), 0, err
	}
	result, execErr := tx.ExecContext(ctx, "INSERT INTO `SRO_SHARD`.`ITEM` (`FK_REF_ITEM`,`DATA`) VALUES (?,?)", refItemID, refCharID)
	if execErr != nil {
		err = fmt.Errorf("create monster mask item: %w", execErr)
		return emptyInventory(), 0, err
	}
	itemID, idErr := result.LastInsertId()
	if idErr != nil {
		err = fmt.Errorf("read monster mask item id: %w", idErr)
		return emptyInventory(), 0, err
	}
	if _, execErr = tx.ExecContext(ctx, InsertInventoryItem, characterID, slot, itemID); execErr != nil {
		err = fmt.Errorf("insert monster mask inventory slot: %w", execErr)
		return emptyInventory(), 0, err
	}
	if err = tx.Commit(); err != nil {
		return emptyInventory(), 0, fmt.Errorf("commit monster mask: %w", err)
	}
	inventoryRepository, repositoryErr := NewInventoryRepository(r.shard)
	if repositoryErr != nil {
		return emptyInventory(), 0, repositoryErr
	}
	inventory, err = inventoryRepository.GetCharacterInventory(ctx, characterID)
	if err != nil {
		return emptyInventory(), 0, err
	}
	return inventory, slot, nil
}
