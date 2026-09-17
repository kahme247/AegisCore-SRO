package model

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrGachaCardNotFound   = errors.New("Gacha card not found")
	ErrGachaPoolEmpty      = errors.New("Gacha reward pool is empty")
	ErrGachaInventoryFull  = errors.New("Gacha inventory is full")
	ErrGachaInvalidRequest = errors.New("invalid Gacha request")
)

type GachaPlayCommand struct {
	CharacterID int64
	EntryID     uint32
	CardSlot    byte
}

type GachaPlayResult struct {
	CardSlot        byte
	CardRemaining   uint16
	RewardRefItemID uint32
	RewardQuantity  uint16
	RewardSlot      byte
	RewardItem      Item
	Inventory       Inventory
	Won             bool
}

type GachaExchangeCommand struct {
	CharacterID int64
}

type GachaExchangeResult struct {
	CardSlot        byte
	CardRemaining   uint16
	RewardRefItemID uint32
	RewardQuantity  uint16
	RewardSlot      byte
	RewardItem      Item
	Inventory       Inventory
}

type GachaRepository interface {
	Play(context.Context, GachaPlayCommand) (GachaPlayResult, error)
	Exchange(context.Context, GachaExchangeCommand) (GachaExchangeResult, error)
}

func (r *SQLGachaRepository) Exchange(ctx context.Context, command GachaExchangeCommand) (result GachaExchangeResult, resultErr error) {
	if command.CharacterID <= 0 {
		return result, ErrGachaInvalidRequest
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	var capacity int
	if err = tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", command.CharacterID).Scan(&capacity); err != nil {
		return result, err
	}
	var cardSlot byte
	var cardStack uint32
	var cardCode string
	var encodedReward uint64
	var cardItemID int64
	err = tx.QueryRowContext(ctx, "SELECT inv.`SLOT`,it.`ID`,it.`STACK_COUNT`,ref.`CodeName`,it.`VARIANCE` FROM `INVENTORY` inv JOIN `ITEM` it ON it.`ID`=inv.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=it.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND ref.`CodeName` IN ('ITEM_MALL_GACHA_CARD_WIN','ITEM_MALL_GACHA_CARD_LOSE') ORDER BY inv.`SLOT` LIMIT 1 FOR UPDATE", command.CharacterID).Scan(&cardSlot, &cardItemID, &cardStack, &cardCode, &encodedReward)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrGachaCardNotFound
	}
	if err != nil {
		return result, err
	}
	if cardStack == 0 || (cardCode != "ITEM_MALL_GACHA_CARD_WIN" && cardCode != "ITEM_MALL_GACHA_CARD_LOSE") {
		return result, ErrGachaCardNotFound
	}

	var rewardRefID uint32
	var rewardQuantity uint16
	rewardRefID, rewardQuantity = decodeGachaCouponReward(encodedReward)
	if rewardRefID == 0 || rewardQuantity == 0 {
		// Compatibility for coupons created before reward data moved out of the
		// client-visible magic-option list.
		rows, queryErr := tx.QueryContext(ctx, "SELECT `SLOT`,`VALUE` FROM `ITEM_MAGIC_OPTION` WHERE `FK_ITEM`=? ORDER BY `SLOT` FOR UPDATE", cardItemID)
		if queryErr != nil {
			return result, queryErr
		}
		for rows.Next() {
			var slot byte
			var value uint32
			if err = rows.Scan(&slot, &value); err != nil {
				_ = rows.Close()
				return result, err
			}
			switch slot {
			case 0:
				rewardRefID = value
			case 1:
				if value <= uint32(^uint16(0)) {
					rewardQuantity = uint16(value)
				}
			}
		}
		if err = rows.Close(); err != nil {
			return result, err
		}
		if err = rows.Err(); err != nil {
			return result, err
		}
	}
	if rewardRefID == 0 || rewardQuantity == 0 {
		return result, ErrGachaInvalidRequest
	}

	rewardSlot, err := lockGachaRewardSlot(ctx, tx, command.CharacterID, capacity, cardSlot, cardStack)
	if err != nil {
		return result, err
	}
	if cardStack == 1 {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `FK_ITEM`=?", command.CharacterID, cardItemID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `ID`=?", cardItemID); err != nil {
			return result, err
		}
	} else if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1,`DATA`=GREATEST(`DATA`-1,0) WHERE `ID`=?", cardItemID); err != nil {
		return result, err
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`) VALUES (0,?,?,0,?)", rewardRefID, rewardQuantity, rewardQuantity)
	if err != nil {
		return result, err
	}
	rewardItemID, err := insert.LastInsertId()
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", command.CharacterID, rewardSlot, rewardItemID); err != nil {
		return result, err
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, command.CharacterID)
	if err != nil {
		return result, err
	}
	result.CardSlot = cardSlot
	if cardStack > 1 {
		result.CardRemaining = uint16(cardStack - 1)
	}
	result.RewardRefItemID, result.RewardQuantity, result.RewardSlot = rewardRefID, rewardQuantity, rewardSlot
	result.RewardItem = result.Inventory.Items[rewardSlot]
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

type SQLGachaRepository struct {
	shard *sql.DB
	rand  func(uint64) (uint64, error)
}

func NewGachaRepository(shard *sql.DB) (GachaRepository, error) {
	if shard == nil {
		return nil, fmt.Errorf("Gacha shard database is required")
	}
	return &SQLGachaRepository{shard: shard, rand: secureGachaRoll}, nil
}

type gachaReward struct {
	refID    uint32
	quantity uint16
	weight   uint64
}

var gachaOptLevelPattern = regexp.MustCompile(`<O:([0-9]+)>`)
var gachaMagicOptionPattern = regexp.MustCompile(`<M:(str|int),([0-9]+),([0-9]+)>`)

func (r *SQLGachaRepository) Play(ctx context.Context, command GachaPlayCommand) (result GachaPlayResult, resultErr error) {
	if command.CharacterID <= 0 || command.EntryID == 0 {
		return result, ErrGachaInvalidRequest
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	var capacity int
	if err = tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", command.CharacterID).Scan(&capacity); err != nil {
		return result, err
	}
	cardSlot := command.CardSlot
	var cardItemID int64
	var cardRefID uint32
	var cardStack uint32
	var typeID1, typeID2, typeID3, typeID4 int
	err = tx.QueryRowContext(ctx, "SELECT it.`ID`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,ref.`TypeID_1`,ref.`TypeID_2`,ref.`TypeID_3`,ref.`TypeID_4` FROM `INVENTORY` inv JOIN `ITEM` it ON it.`ID`=inv.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=it.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND inv.`SLOT`=? FOR UPDATE", command.CharacterID, cardSlot).Scan(&cardItemID, &cardRefID, &cardStack, &typeID1, &typeID2, &typeID3, &typeID4)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrGachaCardNotFound
	}
	if err != nil {
		return result, err
	}
	if cardStack == 0 || typeID1 != 3 || typeID2 != 3 || typeID3 != 14 || typeID4 != 1 {
		return result, ErrGachaCardNotFound
	}

	var selected gachaReward
	var threshold uint64
	var descriptor string
	err = tx.QueryRowContext(ctx, "SELECT CAST(JSON_UNQUOTE(JSON_EXTRACT(`RAW_FIELDS`,'$[2]')) AS UNSIGNED),CAST(JSON_UNQUOTE(JSON_EXTRACT(`RAW_FIELDS`,'$[4]')) AS UNSIGNED),CAST(JSON_UNQUOTE(JSON_EXTRACT(`RAW_FIELDS`,'$[3]')) AS UNSIGNED),JSON_UNQUOTE(JSON_EXTRACT(`RAW_FIELDS`,'$[8]')) FROM `REF_MEDIA_TEXT_CATALOG_ROW` WHERE `FILE_NAME`='gachaitemset.txt' AND JSON_UNQUOTE(JSON_EXTRACT(`RAW_FIELDS`,'$[0]'))='1' AND CAST(JSON_UNQUOTE(JSON_EXTRACT(`RAW_FIELDS`,'$[5]')) AS UNSIGNED)=? LIMIT 1", command.EntryID).Scan(&selected.refID, &selected.quantity, &threshold, &descriptor)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrGachaPoolEmpty
	}
	if err != nil || selected.refID == 0 || selected.quantity == 0 || threshold > 10000 {
		return result, errors.Join(err, ErrGachaInvalidRequest)
	}
	roll, err := r.rand(10000)
	if err != nil {
		return result, err
	}
	won := roll < threshold
	optLevel := byte(0)
	var consolation gachaReward
	if matches := gachaOptLevelPattern.FindStringSubmatch(descriptor); won && len(matches) == 2 {
		if parsed, parseErr := strconv.ParseUint(matches[1], 10, 8); parseErr == nil {
			optLevel = byte(parsed)
		}
	}
	if !won {
		rows, queryErr := tx.QueryContext(ctx, "SELECT `REWARD_REF_ITEM_ID`,`QUANTITY`,`WEIGHT` FROM `GACHA_REWARD_POOL` WHERE `CARD_REF_ITEM_ID`=? AND `SELECTION`=255 AND `ENABLED`=1 ORDER BY `ID` FOR UPDATE", cardRefID)
		if queryErr != nil {
			return result, queryErr
		}
		var rewards []gachaReward
		var total uint64
		for rows.Next() {
			var reward gachaReward
			if err = rows.Scan(&reward.refID, &reward.quantity, &reward.weight); err != nil {
				_ = rows.Close()
				return result, err
			}
			if reward.refID != 0 && reward.quantity != 0 && reward.weight != 0 {
				total += reward.weight
				rewards = append(rewards, reward)
			}
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return result, err
		}
		if total == 0 {
			return result, ErrGachaPoolEmpty
		}
		consolationRoll, rollErr := r.rand(total)
		if rollErr != nil {
			return result, rollErr
		}
		consolation = rewards[len(rewards)-1]
		for _, reward := range rewards {
			if consolationRoll < reward.weight {
				consolation = reward
				break
			}
			consolationRoll -= reward.weight
		}
		selected = gachaReward{refID: 9240, quantity: 1}
		optLevel = 0
	}

	rewardSlot, err := lockGachaRewardSlot(ctx, tx, command.CharacterID, capacity, cardSlot, cardStack)
	if err != nil {
		return result, err
	}
	if cardStack == 1 {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `FK_ITEM`=?", command.CharacterID, cardItemID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `ID`=?", cardItemID); err != nil {
			return result, err
		}
	} else if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1,`DATA`=GREATEST(`DATA`-1,0) WHERE `ID`=?", cardItemID); err != nil {
		return result, err
	}
	var rewardData uint32 = uint32(selected.quantity)
	var rewardDegree int
	if won {
		var typeID2 int
		var durability float64
		if err = tx.QueryRowContext(ctx, "SELECT `TypeID_2`,COALESCE(`Dur_U`,0),COALESCE(`ItemClass`,0) FROM `ITEMDATA` WHERE `ItemID`=?", selected.refID).Scan(&typeID2, &durability, &rewardDegree); err != nil {
			return result, err
		}
		if typeID2 == 1 && durability > 0 {
			rewardData = uint32(durability)
		}
	}
	var rewardVariance uint64
	if !won {
		rewardVariance = encodeGachaCouponReward(consolation.refID, consolation.quantity)
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`) VALUES (?,?,?,?,?)", rewardVariance, selected.refID, selected.quantity, optLevel, rewardData)
	if err != nil {
		return result, err
	}
	rewardItemID, err := insert.LastInsertId()
	if err != nil {
		return result, err
	}
	if won {
		for slot, match := range gachaMagicOptionPattern.FindAllStringSubmatch(strings.ToLower(descriptor), -1) {
			minimum, minErr := strconv.ParseUint(match[2], 10, 32)
			maximum, maxErr := strconv.ParseUint(match[3], 10, 32)
			if minErr != nil || maxErr != nil || minimum > maximum {
				return result, ErrGachaInvalidRequest
			}
			value := minimum
			if maximum > minimum {
				rolled, rollErr := r.rand(maximum - minimum + 1)
				if rollErr != nil {
					return result, rollErr
				}
				value += rolled
			}
			optionCode := "MATTR_" + strings.ToUpper(match[1])
			var optionID uint32
			if err = tx.QueryRowContext(ctx, "SELECT `ID` FROM `REF_MAGIC_OPTION` WHERE `CODE_NAME`=? AND `OPTION_LEVEL`=? LIMIT 1", optionCode, rewardDegree).Scan(&optionID); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_MAGIC_OPTION` (`FK_ITEM`,`SLOT`,`TYPE`,`VALUE`) VALUES (?,?,?,?)", rewardItemID, slot, optionID, value); err != nil {
				return result, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", command.CharacterID, rewardSlot, rewardItemID); err != nil {
		return result, err
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, command.CharacterID)
	if err != nil {
		return result, err
	}
	result.CardSlot = cardSlot
	if cardStack > 1 {
		result.CardRemaining = uint16(cardStack - 1)
	}
	result.RewardRefItemID, result.RewardQuantity, result.RewardSlot = selected.refID, selected.quantity, rewardSlot
	result.RewardItem = result.Inventory.Items[rewardSlot]
	result.Won = won
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func encodeGachaCouponReward(refID uint32, quantity uint16) uint64 {
	return uint64(quantity)<<32 | uint64(refID)
}

func decodeGachaCouponReward(encoded uint64) (uint32, uint16) {
	return uint32(encoded), uint16(encoded >> 32)
}

func lockGachaRewardSlot(ctx context.Context, tx *sql.Tx, characterID int64, capacity int, cardSlot byte, cardStack uint32) (byte, error) {
	rows, err := tx.QueryContext(ctx, "SELECT `SLOT` FROM `INVENTORY` WHERE `FK_CHAR`=? ORDER BY `SLOT` FOR UPDATE", characterID)
	if err != nil {
		return 0, err
	}
	occupied := make(map[byte]struct{})
	for rows.Next() {
		var slot byte
		if err = rows.Scan(&slot); err != nil {
			_ = rows.Close()
			return 0, err
		}
		occupied[slot] = struct{}{}
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if cardStack == 1 {
		return cardSlot, nil
	}
	if capacity > int(MaxInventoryWireSize) {
		capacity = int(MaxInventoryWireSize)
	}
	for slot := InventoryEquipmentSlots; int(slot) < capacity; slot++ {
		if _, exists := occupied[slot]; !exists {
			return slot, nil
		}
	}
	return 0, ErrGachaInventoryFull
}

func secureGachaRoll(limit uint64) (uint64, error) {
	if limit == 0 {
		return 0, ErrGachaPoolEmpty
	}
	value, err := rand.Int(rand.Reader, new(big.Int).SetUint64(limit))
	if err != nil {
		return 0, err
	}
	return value.Uint64(), nil
}
