package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

var (
	ErrSafeTradeUnavailable    = errors.New("safe-trade repository is unavailable")
	ErrSafeTradeInvalidRequest = errors.New("invalid safe-trade request")
	ErrSafeTradeAccessDenied   = errors.New("safe-trade access denied")
	ErrSafeTradeOutcomeUnknown = errors.New("safe-trade transaction outcome is unknown")
)

type SafeTradeState struct {
	AbleCount     byte
	Status        byte
	LastSafeTrade time.Time
}

type SafeTradeAdmission struct {
	State SafeTradeState
	Code  byte
}

type SafeTradeRepository interface {
	Prices(context.Context, uint32) (map[uint32]uint32, error)
	State(context.Context, int) (SafeTradeState, error)
	Admit(context.Context, int, byte, byte) (SafeTradeAdmission, error)
}

type SQLSafeTradeRepository struct{ shard *sql.DB }

func NewSafeTradeRepository(shard *sql.DB) (*SQLSafeTradeRepository, error) {
	if shard == nil {
		return nil, ErrSafeTradeUnavailable
	}
	return &SQLSafeTradeRepository{shard: shard}, nil
}

func SafeTradeScale(quantity uint64, thresholds []uint64) byte {
	if quantity == 0 {
		return 0
	}
	for index, threshold := range thresholds {
		if quantity <= threshold {
			return byte(index + 1)
		}
	}
	return byte(len(thresholds) + 1)
}

// SafeTradeScaleRequiresReset mirrors SR_GameServer 0x005237B0: after the
// scale setter observes a change, every new scale except one clears the
// active safe-trade state and publishes transition 0x40.
func SafeTradeScaleRequiresReset(previous, current byte) bool {
	return previous != current && current != 1
}

func applySafeTradeAdmission(state SafeTradeState, option, limit, scale byte) (SafeTradeAdmission, error) {
	if option > 2 || limit == 0 {
		return SafeTradeAdmission{}, ErrSafeTradeInvalidRequest
	}
	wasActive := state.Status != 0
	state.Status = 0
	admission := SafeTradeAdmission{State: state, Code: 0x40}
	if option == 1 || scale != 1 {
		return admission, nil
	}
	if option == 0 {
		if state.AbleCount >= limit {
			admission.Code = 0x41
			return admission, nil
		}
		if !wasActive {
			state.AbleCount++
		}
	}
	state.Status = 1
	admission.State, admission.Code = state, 0x50
	return admission, nil
}

// SafeTradeNormalizedScore reproduces SR_GameServer 0x0060C1F0 and
// 0x004111F0. levelFactor is LevelData.txt column 6 (the separately stored
// CLevelData +0x30 value), not one of the three JOB experience columns.
func SafeTradeNormalizedScore(quantity, levelFactor uint64, rarity int, referenceCapacity uint64) (uint64, error) {
	if quantity == 0 {
		return 0, nil
	}
	if levelFactor == 0 || levelFactor > math.MaxUint32 {
		return 0, ErrSafeTradeInvalidRequest
	}
	capacity := referenceCapacity
	switch rarity {
	case 1:
		capacity = 40
	case 2:
		capacity = 30
	}
	if capacity == 0 || capacity > math.MaxUint16 || quantity > math.MaxUint32 {
		return 0, ErrSafeTradeInvalidRequest
	}
	// The stock helper converts levelFactor * 10 * 0.125 using the default
	// x87 nearest-even mode. Keep the rational form so .5 ties stay exact.
	levelDenominator := roundRatioToEven(levelFactor*5, 4)
	if levelDenominator == 0 || quantity > math.MaxUint64/(348*capacity) {
		return 0, ErrSafeTradeInvalidRequest
	}
	numerator := quantity * 348 * capacity
	denominator := levelDenominator * 40
	if denominator == 0 {
		return 0, ErrSafeTradeInvalidRequest
	}
	// Stock forces every positive sub-one result to one, then applies the
	// same nearest-even conversion helper to all other positive values.
	score := roundRatioToEven(numerator, denominator)
	if score == 0 {
		score = 1
	}
	return score, nil
}

func roundRatioToEven(numerator, denominator uint64) uint64 {
	quotient, remainder := numerator/denominator, numerator%denominator
	if remainder > denominator/2 || (denominator%2 == 0 && remainder == denominator/2 && quotient%2 != 0) {
		quotient++
	}
	return quotient
}

// SpecialtyQuotationReward is the stock special-merchant margin before the
// common server gold-rate/conversion helpers. SR_GameServer 0x004C8D10 uses
// (current quotation * quantity) - (base quotation * quantity); it does not
// apply an origin-market penalty.
func SpecialtyQuotationReward(base, current uint32, quantity uint16) (uint64, error) {
	if quantity == 0 || current < base {
		return 0, ErrSafeTradeInvalidRequest
	}
	return uint64(current-base) * uint64(quantity), nil
}

// SpecialtyTriJobReward matches round(payout * 0.1) for a non-negative
// integer payout without introducing floating-point precision loss.
func SpecialtyTriJobReward(payout uint64) uint64 {
	reward, remainder := payout/10, payout%10
	if remainder >= 5 {
		reward++
	}
	return reward
}

// SpecialtyCurrentQuotation reproduces the initial runtime quotation built by
// SR_GameServer 0x004C8220. ITEMDATA.SellPrice is the base quotation and each
// complete FluctuateAmount departure from BaseStockAmount changes it by two.
func SpecialtyCurrentQuotation(basePrice uint32, baseStock, fluctuate, currentStock int) (uint32, error) {
	if basePrice == 0 || baseStock <= 0 || fluctuate <= 0 || currentStock < 0 {
		return 0, ErrSafeTradeInvalidRequest
	}
	stockSteps := (currentStock - baseStock) / fluctuate
	current := int64(basePrice) - int64(stockSteps)*2
	if current <= 0 || current > math.MaxUint32 {
		return 0, ErrSafeTradeInvalidRequest
	}
	return uint32(current), nil
}

func (r *SQLSafeTradeRepository) Prices(ctx context.Context, marketID uint32) (map[uint32]uint32, error) {
	if r == nil || r.shard == nil || marketID == 0 {
		return nil, ErrSafeTradeInvalidRequest
	}
	rows, err := r.shard.QueryContext(normalizeSafeTradeContext(ctx), "SELECT q.`REF_ITEM_ID`,d.`SellPrice`,q.`BASE_STOCK_AMOUNT`,q.`FLUCTUATE_AMOUNT`,q.`CUR_STOCK_AMOUNT` FROM `ITEM_QUOTATION` q JOIN `ITEMDATA` d ON d.`ItemID`=q.`REF_ITEM_ID` WHERE q.`ASSOC_NPC`=? AND q.`SERVICE`=1 ORDER BY q.`REF_ITEM_ID`", marketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prices := make(map[uint32]uint32)
	for rows.Next() {
		var item, basePrice uint32
		var baseStock, fluctuate, currentStock int
		if err = rows.Scan(&item, &basePrice, &baseStock, &fluctuate, &currentStock); err != nil {
			return nil, err
		}
		price, quotationErr := SpecialtyCurrentQuotation(basePrice, baseStock, fluctuate, currentStock)
		if quotationErr != nil {
			return nil, quotationErr
		}
		prices[item] = price
	}
	return prices, rows.Err()
}

func (r *SQLSafeTradeRepository) State(ctx context.Context, characterID int) (SafeTradeState, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return SafeTradeState{}, ErrSafeTradeInvalidRequest
	}
	var state SafeTradeState
	err := r.shard.QueryRowContext(normalizeSafeTradeContext(ctx), "SELECT `ABLE_COUNT`,`STATUS`,`LAST_SAFE_TRADE` FROM `CHARACTER_SAFE_TRADE` WHERE `FK_CHAR`=?", characterID).Scan(&state.AbleCount, &state.Status, &state.LastSafeTrade)
	if errors.Is(err, sql.ErrNoRows) {
		return SafeTradeState{}, ErrSafeTradeUnavailable
	}
	return state, err
}

func (r *SQLSafeTradeRepository) Admit(ctx context.Context, characterID int, option, limit byte) (SafeTradeAdmission, error) {
	if r == nil || r.shard == nil || characterID <= 0 || option > 2 || limit == 0 {
		return SafeTradeAdmission{}, ErrSafeTradeInvalidRequest
	}
	ctx = normalizeSafeTradeContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return SafeTradeAdmission{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var state SafeTradeState
	var jobType byte
	if err = tx.QueryRowContext(ctx, "SELECT j.`JOB_TYPE`,s.`ABLE_COUNT`,s.`STATUS`,s.`LAST_SAFE_TRADE` FROM `CHARACTER_JOB` j JOIN `CHARACTER_SAFE_TRADE` s ON s.`FK_CHAR`=j.`FK_CHAR` WHERE j.`FK_CHAR`=? FOR UPDATE", characterID).Scan(&jobType, &state.AbleCount, &state.Status, &state.LastSafeTrade); err != nil || jobType != JobTrader {
		return SafeTradeAdmission{}, ErrSafeTradeAccessDenied
	}
	var cosID uint64
	if err = tx.QueryRowContext(ctx, "SELECT `ID` FROM `CHAR_COS` WHERE `OWNER_CHAR_ID`=? AND `KIND`=? ORDER BY `ID` DESC LIMIT 1 FOR UPDATE", characterID, byte(COSJobTransport)).Scan(&cosID); err != nil {
		return SafeTradeAdmission{}, ErrSafeTradeAccessDenied
	}
	score, thresholds, err := safeTradeScaleData(ctx, tx, characterID, cosID)
	if err != nil {
		return SafeTradeAdmission{}, err
	}
	admission, err := applySafeTradeAdmission(state, option, limit, SafeTradeScale(score, thresholds))
	if err != nil {
		return SafeTradeAdmission{}, err
	}
	updated, err := tx.ExecContext(ctx, "UPDATE `CHARACTER_SAFE_TRADE` SET `ABLE_COUNT`=?,`STATUS`=?,`LAST_SAFE_TRADE`=CURRENT_TIMESTAMP(6) WHERE `FK_CHAR`=?", admission.State.AbleCount, admission.State.Status, characterID)
	if err != nil {
		return SafeTradeAdmission{}, err
	}
	if affected, affectedErr := updated.RowsAffected(); affectedErr != nil || affected != 1 {
		return SafeTradeAdmission{}, ErrSafeTradeAccessDenied
	}
	if err = tx.Commit(); err != nil {
		return SafeTradeAdmission{}, errors.Join(ErrSafeTradeOutcomeUnknown, err)
	}
	committed = true
	return admission, nil
}

func safeTradeScaleData(ctx context.Context, tx *sql.Tx, characterID int, cosID uint64) (uint64, []uint64, error) {
	var quantity uint64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(i.`STACK_COUNT`),0) FROM `COS_INVENTORY` ci JOIN `ITEM` i ON i.`id`=ci.`FK_ITEM` WHERE ci.`COS_ID`=? AND i.`CREATOR_NAME`<>''", cosID).Scan(&quantity); err != nil {
		return 0, nil, err
	}
	var level int
	var rarity int
	var referenceCapacity uint64
	if err := tx.QueryRowContext(ctx, "SELECT c.`CURRENT_LEVEL`,r.`Rarity`,r.`Inventory_Size` FROM `CHAR` c JOIN `CHAR_COS` cos ON cos.`OWNER_CHAR_ID`=c.`id` JOIN `CHAR_REF_DATA` r ON r.`RefObjID`=cos.`REF_OBJECT_ID` WHERE c.`id`=? AND cos.`ID`=?", characterID, cosID).Scan(&level, &rarity, &referenceCapacity); err != nil {
		return 0, nil, err
	}
	if level < 20 {
		level = 20
	} else if level > 140 {
		level = 140
	}
	var levelFactor uint64
	if err := tx.QueryRowContext(ctx, "SELECT `JOB_ACTIVITY_FACTOR` FROM `REF_LEVEL_DATA` WHERE `LEVEL`=?", level).Scan(&levelFactor); err != nil {
		return 0, nil, err
	}
	score, err := SafeTradeNormalizedScore(quantity, levelFactor, rarity, referenceCapacity)
	if err != nil {
		return 0, nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT `MAX_QUANTITY` FROM `REF_SAFE_TRADE_SCALE` ORDER BY `SCALE`")
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	thresholds := make([]uint64, 0, 4)
	for rows.Next() {
		var threshold uint64
		if err = rows.Scan(&threshold); err != nil {
			return 0, nil, err
		}
		thresholds = append(thresholds, threshold)
	}
	if len(thresholds) == 0 {
		return 0, nil, ErrSafeTradeUnavailable
	}
	return score, thresholds, rows.Err()
}

func normalizeSafeTradeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
