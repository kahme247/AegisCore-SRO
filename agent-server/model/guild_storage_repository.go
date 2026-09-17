package model

import (
	"context"
	"database/sql"
	"errors"
)

const (
	minimumGuildStorageLevel = 2
	guildStorageLeaseSeconds = 120
)

var (
	ErrGuildStorageRepositoryUnavailable = errors.New("guild storage repository is unavailable")
	ErrGuildStorageConflict              = errors.New("guild storage state conflicts with the request")
	ErrGuildStorageAccessDenied          = errors.New("guild storage access denied")
	ErrGuildStorageLevelTooLow           = errors.New("guild level is too low for storage")
	ErrGuildStorageInUse                 = errors.New("guild storage is in use")
	ErrGuildStorageLeaseRequired         = errors.New("guild storage lease is required")
	ErrGuildStorageInsufficientGold      = errors.New("insufficient gold")
	ErrGuildStorageOutcomeUnknown        = errors.New("guild storage transaction outcome is unknown")
)

// GuildStorageRepository is the persistence boundary for the guild-wide
// chest. The guild identity is deliberately derived from FK_CHAR inside each
// transaction; accepting a guild ID from a packet would permit cross-guild
// access when a session or runtime snapshot is stale.
type GuildStorageRepository interface {
	OpenGuildStorage(context.Context, int64) error
	LoadGuildStorage(context.Context, int64) (StorageSnapshot, error)
	CloseGuildStorage(context.Context, int64) error
	MoveGuildStorage(context.Context, int64, byte, byte, uint16) error
	DepositGuildItem(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	WithdrawGuildItem(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	DepositGuildGold(context.Context, int64, uint32, uint64) (EconomyResult, error)
	WithdrawGuildGold(context.Context, int64, uint32, uint64) (EconomyResult, error)
}

type SQLGuildStorageRepository struct{ shard *sql.DB }

var _ GuildStorageRepository = (*SQLGuildStorageRepository)(nil)

func NewGuildStorageRepository(shard *sql.DB) (*SQLGuildStorageRepository, error) {
	if shard == nil {
		return nil, ErrGuildStorageRepositoryUnavailable
	}
	return &SQLGuildStorageRepository{shard: shard}, nil
}

type lockedGuildStorage struct {
	guildID  int64
	capacity byte
	gold     uint64
}

func validateGuildStorageAccess(rights bool, level byte) error {
	if level < minimumGuildStorageLevel {
		return ErrGuildStorageLevelTooLow
	}
	if !rights {
		return ErrGuildStorageAccessDenied
	}
	return nil
}

func validateGuildStorageSlots(source, destination, capacity byte) error {
	if capacity == 0 || source >= capacity || destination >= capacity || source == destination {
		return ErrGuildStorageConflict
	}
	return nil
}

func validateGuildInventorySlot(slot byte) error {
	if slot < 13 || slot >= BaseInventorySize {
		return ErrGuildStorageConflict
	}
	return nil
}

func normalizeGuildStorageContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *SQLGuildStorageRepository) begin(ctx context.Context) (*sql.Tx, error) {
	if r == nil || r.shard == nil {
		return nil, ErrGuildStorageRepositoryUnavailable
	}
	ctx = normalizeGuildStorageContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.shard.BeginTx(ctx, nil)
}

func finishGuildStorageTransaction(tx *sql.Tx, committed *bool) {
	if tx == nil || committed == nil || *committed {
		return
	}
	_ = tx.Rollback()
}

func commitGuildStorageTransaction(tx *sql.Tx, committed *bool) error {
	if err := tx.Commit(); err != nil {
		return errors.Join(ErrGuildStorageOutcomeUnknown, err)
	}
	*committed = true
	return nil
}

// lockGuildStorageAccess locks the membership and guild row. The guild row is
// the common serialization point for different members, whose in-memory
// player mutexes cannot serialize one another.
func lockGuildStorageAccess(ctx context.Context, tx *sql.Tx, characterID int64) (lockedGuildStorage, error) {
	if characterID <= 0 {
		return lockedGuildStorage{}, ErrGuildStorageAccessDenied
	}
	var state lockedGuildStorage
	var rights bool
	var level byte
	err := tx.QueryRowContext(ctx, "SELECT gm.`FK_GUILD`,gm.`STORAGE_RIGHTS`,g.`LEVEL`,g.`STORAGE_CAPACITY`,g.`STORAGE_GOLD` FROM `GUILD_MEMBER` gm JOIN `GUILD` g ON g.`ID`=gm.`FK_GUILD` WHERE gm.`FK_CHAR`=? FOR UPDATE", characterID).Scan(
		&state.guildID, &legacyBoolScanner{target: &rights}, &level, &state.capacity, &state.gold,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return lockedGuildStorage{}, ErrGuildStorageAccessDenied
	}
	if err != nil {
		return lockedGuildStorage{}, err
	}
	if err = validateGuildStorageAccess(rights, level); err != nil {
		return lockedGuildStorage{}, err
	}
	return state, nil
}

func requireGuildStorageLease(ctx context.Context, tx *sql.Tx, guildID, characterID int64) error {
	var holder int64
	var active bool
	err := tx.QueryRowContext(ctx, "SELECT `FK_CHAR`,`EXPIRES_AT`>CURRENT_TIMESTAMP(6) FROM `GUILD_STORAGE_LEASE` WHERE `FK_GUILD`=? FOR UPDATE", guildID).Scan(&holder, &legacyBoolScanner{target: &active})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGuildStorageLeaseRequired
	}
	if err != nil {
		return err
	}
	if !active || holder != characterID {
		return ErrGuildStorageLeaseRequired
	}
	// Active use renews the short lease so a legitimately open window does not
	// expire while the member is loading pages or moving several items.
	return executeGuildStorageWrite(ctx, tx, "UPDATE `GUILD_STORAGE_LEASE` SET `EXPIRES_AT`=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL ? SECOND) WHERE `FK_GUILD`=? AND `FK_CHAR`=?", guildStorageLeaseSeconds, guildID, characterID)
}

func (r *SQLGuildStorageRepository) OpenGuildStorage(ctx context.Context, characterID int64) (err error) {
	ctx = normalizeGuildStorageContext(ctx)
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer finishGuildStorageTransaction(tx, &committed)

	state, err := lockGuildStorageAccess(ctx, tx, characterID)
	if err != nil {
		return err
	}
	var holder int64
	var active bool
	err = tx.QueryRowContext(ctx, "SELECT `FK_CHAR`,`EXPIRES_AT`>CURRENT_TIMESTAMP(6) FROM `GUILD_STORAGE_LEASE` WHERE `FK_GUILD`=? FOR UPDATE", state.guildID).Scan(&holder, &legacyBoolScanner{target: &active})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var write sql.Result
		write, err = tx.ExecContext(ctx, "INSERT INTO `GUILD_STORAGE_LEASE` (`FK_GUILD`,`FK_CHAR`,`EXPIRES_AT`) VALUES (?,?,DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL ? SECOND))", state.guildID, characterID, guildStorageLeaseSeconds)
		if err == nil {
			err = requireOneGuildStorageRow(write)
		}
	case err != nil:
		return err
	case active && holder != characterID:
		return ErrGuildStorageInUse
	case holder == characterID:
		err = executeGuildStorageWrite(ctx, tx, "UPDATE `GUILD_STORAGE_LEASE` SET `EXPIRES_AT`=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL ? SECOND) WHERE `FK_GUILD`=? AND `FK_CHAR`=?", guildStorageLeaseSeconds, state.guildID, characterID)
	default:
		err = executeGuildStorageWrite(ctx, tx, "UPDATE `GUILD_STORAGE_LEASE` SET `FK_CHAR`=?,`EXPIRES_AT`=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL ? SECOND) WHERE `FK_GUILD`=?", characterID, guildStorageLeaseSeconds, state.guildID)
	}
	if err != nil {
		return err
	}
	return commitGuildStorageTransaction(tx, &committed)
}

func (r *SQLGuildStorageRepository) LoadGuildStorage(ctx context.Context, characterID int64) (snapshot StorageSnapshot, err error) {
	ctx = normalizeGuildStorageContext(ctx)
	tx, err := r.begin(ctx)
	if err != nil {
		return snapshot, err
	}
	committed := false
	defer finishGuildStorageTransaction(tx, &committed)
	state, err := lockGuildStorageAccess(ctx, tx, characterID)
	if err != nil {
		return snapshot, err
	}
	if err = requireGuildStorageLease(ctx, tx, state.guildID, characterID); err != nil {
		return snapshot, err
	}
	snapshot, err = loadGuildStorageSnapshot(ctx, tx, state)
	if err != nil {
		return StorageSnapshot{}, err
	}
	if err = commitGuildStorageTransaction(tx, &committed); err != nil {
		return StorageSnapshot{}, err
	}
	return snapshot, nil
}

func loadGuildStorageSnapshot(ctx context.Context, tx *sql.Tx, state lockedGuildStorage) (StorageSnapshot, error) {
	snapshot := StorageSnapshot{Capacity: state.capacity, Gold: state.gold, Items: make(map[byte]Item)}
	rows, err := tx.QueryContext(ctx, "SELECT s.`SLOT`,i.`id`,i.`VARIANCE`,i.`FK_REF_ITEM`,i.`STACK_COUNT`,i.`OPT_LEVEL`,i.`DATA`,i.`RENT_TYPE`,i.`RENT_CAN_DELETE`,i.`RENT_CAN_RECHARGE`,i.`RENT_PERIOD_BEGIN_TIME`,i.`RENT_PERIOD_END_TIME`,i.`RENT_METER_RATE_TIME`,i.`RENT_PACKING_TIME` FROM `GUILD_STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` WHERE s.`FK_GUILD`=? ORDER BY s.`SLOT` FOR UPDATE", state.guildID)
	if err != nil {
		return StorageSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var slot byte
		var id int64
		var variance uint64
		var refID uint32
		var stack int
		var opt byte
		var data uint32
		var rent RentInfo
		if err = rows.Scan(&slot, &id, &variance, &refID, &stack, &opt, &data, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime); err != nil {
			return StorageSnapshot{}, err
		}
		if slot >= state.capacity {
			return StorageSnapshot{}, ErrGuildStorageConflict
		}
		item, itemErr := economyItem(id, refID, variance, stack, opt, data, rent)
		if itemErr != nil {
			return StorageSnapshot{}, itemErr
		}
		snapshot.Items[slot] = item
	}
	return snapshot, rows.Err()
}

func (r *SQLGuildStorageRepository) CloseGuildStorage(ctx context.Context, characterID int64) (err error) {
	if characterID <= 0 {
		return ErrGuildStorageAccessDenied
	}
	ctx = normalizeGuildStorageContext(ctx)
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer finishGuildStorageTransaction(tx, &committed)
	if _, err = tx.ExecContext(ctx, "DELETE FROM `GUILD_STORAGE_LEASE` WHERE `FK_CHAR`=?", characterID); err != nil {
		return err
	}
	return commitGuildStorageTransaction(tx, &committed)
}

func (r *SQLGuildStorageRepository) MoveGuildStorage(ctx context.Context, characterID int64, source, destination byte, amount uint16) (err error) {
	ctx = normalizeGuildStorageContext(ctx)
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer finishGuildStorageTransaction(tx, &committed)
	state, err := lockGuildStorageAccess(ctx, tx, characterID)
	if err != nil {
		return err
	}
	if err = requireGuildStorageLease(ctx, tx, state.guildID, characterID); err != nil {
		return err
	}
	if err = validateGuildStorageSlots(source, destination, state.capacity); err != nil {
		return err
	}
	var itemID int64
	var refID uint32
	var stack, maxStack int
	if err = tx.QueryRowContext(ctx, "SELECT s.`FK_ITEM`,i.`FK_REF_ITEM`,i.`STACK_COUNT`,COALESCE(NULLIF(d.`Stack_Size`,0),1) FROM `GUILD_STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=i.`FK_REF_ITEM` WHERE s.`FK_GUILD`=? AND s.`SLOT`=? FOR UPDATE", state.guildID, source).Scan(&itemID, &refID, &stack, &maxStack); err != nil {
		return ErrGuildStorageConflict
	}
	var occupied int64
	var occupiedRef uint32
	var occupiedStack int
	scanErr := tx.QueryRowContext(ctx, "SELECT s.`FK_ITEM`,i.`FK_REF_ITEM`,i.`STACK_COUNT` FROM `GUILD_STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` WHERE s.`FK_GUILD`=? AND s.`SLOT`=? FOR UPDATE", state.guildID, destination).Scan(&occupied, &occupiedRef, &occupiedStack)
	if resolved, valid := resolveStorageMoveAmount(stack, maxStack, amount, scanErr == nil, occupiedRef == refID, occupiedStack); valid {
		amount = uint16(resolved)
	} else {
		return ErrGuildStorageConflict
	}
	if scanErr == nil {
		if occupiedRef != refID {
			return ErrGuildStorageConflict
		}
		if result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", occupiedStack+int(amount), occupied, occupiedStack); updateErr != nil || !singleAffectedRow(result) {
			return ErrGuildStorageConflict
		}
		if int(amount) == stack {
			if result, deleteErr := tx.ExecContext(ctx, "DELETE FROM `GUILD_STORAGE` WHERE `FK_GUILD`=? AND `SLOT`=? AND `FK_ITEM`=?", state.guildID, source, itemID); deleteErr != nil || !singleAffectedRow(result) {
				return ErrGuildStorageConflict
			}
			if result, deleteErr := tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=? AND `STACK_COUNT`=?", itemID, stack); deleteErr != nil || !singleAffectedRow(result) {
				return ErrGuildStorageConflict
			}
		} else if result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", stack-int(amount), itemID, stack); updateErr != nil || !singleAffectedRow(result) {
			return ErrGuildStorageConflict
		}
	} else if !errors.Is(scanErr, sql.ErrNoRows) {
		return scanErr
	} else if int(amount) == stack {
		result, moveErr := tx.ExecContext(ctx, "UPDATE `GUILD_STORAGE` SET `SLOT`=? WHERE `FK_GUILD`=? AND `SLOT`=? AND `FK_ITEM`=?", destination, state.guildID, source, itemID)
		if moveErr != nil || !singleAffectedRow(result) {
			return ErrGuildStorageConflict
		}
	} else {
		newItemID, splitErr := cloneStorageItemStack(ctx, tx, itemID, amount)
		if splitErr != nil {
			return splitErr
		}
		if result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", stack-int(amount), itemID, stack); updateErr != nil || !singleAffectedRow(result) {
			return ErrGuildStorageConflict
		}
		if _, insertErr := tx.ExecContext(ctx, "INSERT INTO `GUILD_STORAGE` (`FK_GUILD`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", state.guildID, destination, newItemID); insertErr != nil {
			return insertErr
		}
	}
	return commitGuildStorageTransaction(tx, &committed)
}

func (r *SQLGuildStorageRepository) DepositGuildItem(ctx context.Context, characterID int64, userID uint32, source, destination byte) (EconomyResult, error) {
	return r.transferGuildItem(ctx, characterID, userID, source, destination, true)
}

func (r *SQLGuildStorageRepository) WithdrawGuildItem(ctx context.Context, characterID int64, userID uint32, source, destination byte) (EconomyResult, error) {
	return r.transferGuildItem(ctx, characterID, userID, source, destination, false)
}

func (r *SQLGuildStorageRepository) transferGuildItem(ctx context.Context, characterID int64, userID uint32, source, destination byte, deposit bool) (result EconomyResult, err error) {
	if characterID <= 0 || userID == 0 {
		return result, ErrGuildStorageAccessDenied
	}
	ctx = normalizeGuildStorageContext(ctx)
	tx, err := r.begin(ctx)
	if err != nil {
		return result, err
	}
	committed := false
	defer finishGuildStorageTransaction(tx, &committed)
	owner, gold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return result, ErrGuildStorageAccessDenied
	}
	state, err := lockGuildStorageAccess(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	if err = requireGuildStorageLease(ctx, tx, state.guildID, characterID); err != nil {
		return result, err
	}
	if deposit {
		if err = validateGuildInventorySlot(source); err != nil || destination >= state.capacity {
			return result, ErrGuildStorageConflict
		}
	} else if source >= state.capacity || validateGuildInventorySlot(destination) != nil {
		return result, ErrGuildStorageConflict
	}
	var itemID int64
	if deposit {
		// CanBorrow uses the original client bit mask: normal storage (0x80)
		// must accompany guild storage (0x40) for the item to be admissible.
		err = tx.QueryRowContext(ctx, "SELECT inv.`FK_ITEM` FROM `INVENTORY` inv JOIN `ITEM` i ON i.`id`=inv.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=i.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND inv.`SLOT`=? AND (COALESCE(d.`CanBorrow`,0)&192)=192 FOR UPDATE", characterID, source).Scan(&itemID)
		if err != nil {
			return result, ErrGuildStorageConflict
		}
		if err = requireEmptyGuildStorageSlot(ctx, tx, state.guildID, destination); err != nil {
			return result, err
		}
		if err = executeGuildStorageWrite(ctx, tx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, source, itemID); err != nil {
			return result, err
		}
		if err = executeGuildStorageWrite(ctx, tx, "INSERT INTO `GUILD_STORAGE` (`FK_GUILD`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", state.guildID, destination, itemID); err != nil {
			return result, err
		}
	} else {
		err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `GUILD_STORAGE` WHERE `FK_GUILD`=? AND `SLOT`=? FOR UPDATE", state.guildID, source).Scan(&itemID)
		if err != nil {
			return result, ErrGuildStorageConflict
		}
		if err = requireEmptyInventorySlot(ctx, tx, characterID, destination); err != nil {
			return result, err
		}
		if err = executeGuildStorageWrite(ctx, tx, "DELETE FROM `GUILD_STORAGE` WHERE `FK_GUILD`=? AND `SLOT`=? AND `FK_ITEM`=?", state.guildID, source, itemID); err != nil {
			return result, err
		}
		if err = executeGuildStorageWrite(ctx, tx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, destination, itemID); err != nil {
			return result, err
		}
	}
	result.Inventory, err = loadInventoryInExchangeTransaction(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	result.Gold = gold
	result.SourceSlot = source
	result.DestinationSlots = []byte{destination}
	if err = commitGuildStorageTransaction(tx, &committed); err != nil {
		return EconomyResult{}, err
	}
	return result, nil
}

func requireEmptyGuildStorageSlot(ctx context.Context, tx *sql.Tx, guildID int64, slot byte) error {
	var occupied int64
	err := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `GUILD_STORAGE` WHERE `FK_GUILD`=? AND `SLOT`=? FOR UPDATE", guildID, slot).Scan(&occupied)
	if err == nil {
		return ErrGuildStorageConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func requireEmptyInventorySlot(ctx context.Context, tx *sql.Tx, characterID int64, slot byte) error {
	var occupied int64
	err := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", characterID, slot).Scan(&occupied)
	if err == nil {
		return ErrGuildStorageConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func executeGuildStorageWrite(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	return requireOneGuildStorageRow(result)
}

func requireOneGuildStorageRow(result sql.Result) error {
	if result == nil {
		return ErrGuildStorageConflict
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrGuildStorageConflict
	}
	return nil
}

func (r *SQLGuildStorageRepository) DepositGuildGold(ctx context.Context, characterID int64, userID uint32, amount uint64) (EconomyResult, error) {
	return r.transferGuildGold(ctx, characterID, userID, amount, true)
}

func (r *SQLGuildStorageRepository) WithdrawGuildGold(ctx context.Context, characterID int64, userID uint32, amount uint64) (EconomyResult, error) {
	return r.transferGuildGold(ctx, characterID, userID, amount, false)
}

func (r *SQLGuildStorageRepository) transferGuildGold(ctx context.Context, characterID int64, userID uint32, amount uint64, deposit bool) (result EconomyResult, err error) {
	if characterID <= 0 || userID == 0 || amount == 0 {
		return result, ErrGuildStorageConflict
	}
	ctx = normalizeGuildStorageContext(ctx)
	tx, err := r.begin(ctx)
	if err != nil {
		return result, err
	}
	committed := false
	defer finishGuildStorageTransaction(tx, &committed)
	owner, characterGold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return result, ErrGuildStorageAccessDenied
	}
	state, err := lockGuildStorageAccess(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	if err = requireGuildStorageLease(ctx, tx, state.guildID, characterID); err != nil {
		return result, err
	}
	var nextCharacterGold, nextGuildGold uint64
	if deposit {
		if characterGold < amount || state.gold > ^uint64(0)-amount {
			return result, ErrGuildStorageInsufficientGold
		}
		nextCharacterGold, nextGuildGold = characterGold-amount, state.gold+amount
	} else {
		if state.gold < amount || characterGold > ^uint64(0)-amount {
			return result, ErrGuildStorageInsufficientGold
		}
		nextCharacterGold, nextGuildGold = characterGold+amount, state.gold-amount
	}
	if err = executeGuildStorageWrite(ctx, tx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=? AND `FK_USER`=?", nextCharacterGold, characterID, userID); err != nil {
		return result, err
	}
	if err = executeGuildStorageWrite(ctx, tx, "UPDATE `GUILD` SET `STORAGE_GOLD`=? WHERE `ID`=?", nextGuildGold, state.guildID); err != nil {
		return result, err
	}
	result.Gold = nextCharacterGold
	result.Amount = 0
	if err = commitGuildStorageTransaction(tx, &committed); err != nil {
		return EconomyResult{}, err
	}
	return result, nil
}
