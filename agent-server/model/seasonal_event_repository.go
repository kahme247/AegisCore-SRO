package model

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrChristmasSocksMissing = errors.New("Christmas red socks not found")
	ErrChristmasPoolEmpty    = errors.New("Christmas reward pool is empty")
)

type ChristmasExchangeResult struct {
	SocksSlot       byte
	SocksRemaining  uint16
	RewardSlot      byte
	RewardRefItemID uint32
	RewardQuantity  uint16
	RewardItem      Item
	Inventory       Inventory
}

type SeasonalEventRepository interface {
	ExchangeChristmasSocks(context.Context, int64) (ChristmasExchangeResult, error)
}

type SQLSeasonalEventRepository struct {
	shard *sql.DB
	rand  func(uint64) (uint64, error)
}

func NewSeasonalEventRepository(shard *sql.DB) (SeasonalEventRepository, error) {
	if shard == nil {
		return nil, errors.New("seasonal-event shard database is required")
	}
	return &SQLSeasonalEventRepository{shard: shard, rand: secureSeasonalRoll}, nil
}

func (repository *SQLSeasonalEventRepository) ExchangeChristmasSocks(ctx context.Context, characterID int64) (result ChristmasExchangeResult, resultErr error) {
	if characterID <= 0 {
		return result, ErrChristmasSocksMissing
	}
	tx, err := repository.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	var capacity int
	if err = tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&capacity); err != nil {
		return result, err
	}
	var socksItemID int64
	var socksSlot byte
	var socksStack uint32
	err = tx.QueryRowContext(ctx, "SELECT inv.`FK_ITEM`,inv.`SLOT`,it.`STACK_COUNT` FROM `INVENTORY` inv JOIN `ITEM` it ON it.`ID`=inv.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=it.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND ref.`CodeName`='ITEM_ETC_E041225_RED_SOCKS' ORDER BY inv.`SLOT` LIMIT 1 FOR UPDATE", characterID).Scan(&socksItemID, &socksSlot, &socksStack)
	if errors.Is(err, sql.ErrNoRows) || socksStack == 0 {
		return result, ErrChristmasSocksMissing
	}
	if err != nil {
		return result, err
	}
	type reward struct {
		refID    uint32
		quantity uint16
		weight   uint64
	}
	rows, err := tx.QueryContext(ctx, "SELECT `REWARD_REF_ITEM_ID`,`QUANTITY`,`WEIGHT` FROM `CHRISTMAS_REWARD_POOL` WHERE `ENABLED`=1 ORDER BY `ID` FOR UPDATE")
	if err != nil {
		return result, err
	}
	var rewards []reward
	var total uint64
	for rows.Next() {
		var candidate reward
		if err = rows.Scan(&candidate.refID, &candidate.quantity, &candidate.weight); err != nil {
			_ = rows.Close()
			return result, err
		}
		if candidate.refID != 0 && candidate.quantity != 0 && candidate.weight != 0 && total <= ^uint64(0)-candidate.weight {
			total += candidate.weight
			rewards = append(rewards, candidate)
		}
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if total == 0 {
		return result, ErrChristmasPoolEmpty
	}
	roll, err := repository.rand(total)
	if err != nil {
		return result, err
	}
	selected := rewards[len(rewards)-1]
	for _, candidate := range rewards {
		if roll < candidate.weight {
			selected = candidate
			break
		}
		roll -= candidate.weight
	}
	rewardSlot, err := lockGachaRewardSlot(ctx, tx, characterID, capacity, socksSlot, socksStack)
	if err != nil {
		return result, err
	}
	if socksStack == 1 {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `FK_ITEM`=?", characterID, socksItemID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `ID`=?", socksItemID); err != nil {
			return result, err
		}
	} else if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1,`DATA`=GREATEST(`DATA`-1,0) WHERE `ID`=?", socksItemID); err != nil {
		return result, err
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`) VALUES (0,?,?,0,?)", selected.refID, selected.quantity, selected.quantity)
	if err != nil {
		return result, err
	}
	rewardItemID, err := insert.LastInsertId()
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, rewardSlot, rewardItemID); err != nil {
		return result, err
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	result.SocksSlot, result.RewardSlot = socksSlot, rewardSlot
	if socksStack > 1 {
		result.SocksRemaining = uint16(socksStack - 1)
	}
	result.RewardRefItemID, result.RewardQuantity = selected.refID, selected.quantity
	result.RewardItem = result.Inventory.Items[rewardSlot]
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func secureSeasonalRoll(limit uint64) (uint64, error) {
	if limit == 0 {
		return 0, ErrChristmasPoolEmpty
	}
	value, err := rand.Int(rand.Reader, new(big.Int).SetUint64(limit))
	if err != nil {
		return 0, fmt.Errorf("roll Christmas reward: %w", err)
	}
	return value.Uint64(), nil
}
