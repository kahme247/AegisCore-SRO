package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var ErrCharacterVitalsRepositoryUnavailable = errors.New("character vitals repository is unavailable")

type CharacterVitalsRepository interface {
	SaveVitals(context.Context, int, int, int) error
	Resurrect(context.Context, int, int, int) (bool, error)
	SpendMP(context.Context, int, int, int) (bool, error)
	SpendHP(context.Context, int, int, int) (bool, error)
}

// SkillResurrectionRepository commits the caster MP debit and dead target
// resurrection in one transaction. Combat uses this optional capability so a
// failed or superseded resurrection cannot consume MP by itself.
type SkillResurrectionRepository interface {
	CastResurrection(context.Context, int, int, int, int, int, int) (bool, error)
}

// ReturnPointResurrectionRepository atomically revives a dead character and
// moves its persisted position to the teleport point designated as home.
type ReturnPointResurrectionRepository interface {
	ResurrectAtReturnPoint(context.Context, int, int, int) (TeleportDestination, bool, error)
}

type vitalsTransactionBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func (r *SQLCharacterVitalsRepository) SpendMP(ctx context.Context, characterID, expectedMP, cost int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || expectedMP < 0 || cost < 0 {
		return false, fmt.Errorf("invalid skill MP cost")
	}
	if cost == 0 {
		return true, nil
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `MP`=`MP`-? WHERE `ID`=? AND `MP`=? AND `MP`>=?", cost, characterID, expectedMP, cost)
	if err != nil {
		return false, fmt.Errorf("spend skill MP: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read skill MP result: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("spend skill MP affected %d rows, want at most 1", affected)
	}
	return affected == 1, nil
}

// SpendHP atomically debits an authored skill HP cost while preserving at
// least one hit point. Native pre-engage rejects a cast that would kill its
// owner; the strict HP predicate keeps the SQL and in-memory gates aligned.
func (r *SQLCharacterVitalsRepository) SpendHP(ctx context.Context, characterID, expectedHP, cost int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || expectedHP <= 0 || cost < 0 {
		return false, fmt.Errorf("invalid skill HP cost")
	}
	if cost == 0 {
		return true, nil
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=`HP`-? WHERE `ID`=? AND `HP`=? AND `HP`>?", cost, characterID, expectedHP, cost)
	if err != nil {
		return false, fmt.Errorf("spend skill HP: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read skill HP result: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("spend skill HP affected %d rows, want at most 1", affected)
	}
	return affected == 1, nil
}

type SQLCharacterVitalsRepository struct{ shard frameworkdb.Execer }

func NewCharacterVitalsRepository(shard frameworkdb.Execer) (*SQLCharacterVitalsRepository, error) {
	if shard == nil {
		return nil, ErrCharacterVitalsRepositoryUnavailable
	}
	return &SQLCharacterVitalsRepository{shard: shard}, nil
}

func (r *SQLCharacterVitalsRepository) CastResurrection(ctx context.Context, casterID, targetID, expectedCasterMP, cost, targetHP, targetMP int) (committed bool, resultErr error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if casterID <= 0 || targetID <= 0 || casterID == targetID || expectedCasterMP < 0 || cost < 0 || expectedCasterMP < cost || targetHP <= 0 || targetMP < 0 {
		return false, fmt.Errorf("invalid skill resurrection")
	}
	beginner, ok := r.shard.(vitalsTransactionBeginner)
	if !ok {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin skill resurrection: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback skill resurrection: %w", rollbackErr))
		}
	}()
	var lockedCasterID int
	if err = tx.QueryRowContext(ctx, "SELECT `ID` FROM `SRO_SHARD`.`CHAR` WHERE `ID`=? AND `MP`=? AND `MP`>=? FOR UPDATE", casterID, expectedCasterMP, cost).Scan(&lockedCasterID); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("lock resurrection caster: %w", err)
	}
	result, err := tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=?,`MP`=? WHERE `ID`=? AND `HP`=0", targetHP, targetMP, targetID)
	if err != nil {
		return false, fmt.Errorf("resurrect skill target: %w", err)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		if affectedErr != nil {
			return false, fmt.Errorf("read skill target resurrection result: %w", affectedErr)
		}
		return false, nil
	}
	if cost > 0 {
		result, err = tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `MP`=`MP`-? WHERE `ID`=? AND `MP`=?", cost, casterID, expectedCasterMP)
		if err != nil {
			return false, fmt.Errorf("debit resurrection caster MP: %w", err)
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			if affectedErr != nil {
				return false, fmt.Errorf("read resurrection MP debit result: %w", affectedErr)
			}
			return false, nil
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit skill resurrection: %w", err)
	}
	return true, nil
}

func (r *SQLCharacterVitalsRepository) SaveVitals(ctx context.Context, characterID, hp, mp int) error {
	if r == nil || r.shard == nil {
		return ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || hp < 0 || mp < 0 {
		return fmt.Errorf("invalid character vitals")
	}
	if hp == 0 {
		_, err := r.shard.ExecContext(ctx, "INSERT INTO `SRO_SHARD`.`CHAR_DEATH_POSITION` (`CHAR_ID`,`REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID`) SELECT `ID`,`FK_REGION`,`POS_X`,`POS_Y`,`POS_Z`,1 FROM `SRO_SHARD`.`CHAR` WHERE `ID`=? AND `HP`>0 ON DUPLICATE KEY UPDATE `REGION_ID`=VALUES(`REGION_ID`),`POS_X`=VALUES(`POS_X`),`POS_Y`=VALUES(`POS_Y`),`POS_Z`=VALUES(`POS_Z`),`WORLD_ID`=VALUES(`WORLD_ID`),`SAVED_AT`=CURRENT_TIMESTAMP(6)", characterID)
		if err != nil {
			return fmt.Errorf("save character death position: %w", err)
		}
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=?, `MP`=? WHERE `ID`=?", hp, mp, characterID)
	if err != nil {
		return fmt.Errorf("save character vitals: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read character vitals result: %w", err)
	}
	if affected > 1 {
		return fmt.Errorf("save character vitals affected %d rows, want at most 1", affected)
	}
	return nil
}

func (r *SQLCharacterVitalsRepository) Resurrect(ctx context.Context, characterID, hp, mp int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || hp <= 0 || mp < 0 {
		return false, fmt.Errorf("invalid resurrection vitals")
	}
	result, err := r.shard.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=?, `MP`=? WHERE `ID`=? AND `HP`=0", hp, mp, characterID)
	if err != nil {
		return false, fmt.Errorf("resurrect character: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read resurrection result: %w", err)
	}
	if affected > 1 {
		return false, fmt.Errorf("resurrection affected %d rows, want at most 1", affected)
	}
	return affected == 1, nil
}

func (r *SQLCharacterVitalsRepository) ResurrectAtReturnPoint(ctx context.Context, characterID, hp, mp int) (destination TeleportDestination, updated bool, resultErr error) {
	if r == nil || r.shard == nil {
		return destination, false, ErrCharacterVitalsRepositoryUnavailable
	}
	if characterID <= 0 || hp <= 0 || mp < 0 {
		return destination, false, fmt.Errorf("invalid return-point resurrection")
	}
	beginner, ok := r.shard.(vitalsTransactionBeginner)
	if !ok {
		return destination, false, ErrCharacterVitalsRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return destination, false, fmt.Errorf("begin return-point resurrection: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback return-point resurrection: %w", rollbackErr))
		}
	}()
	err = tx.QueryRowContext(ctx, "SELECT t.`ID`,t.`REGION_ID`,t.`POS_X`,t.`POS_Y`,t.`POS_Z`,t.`WORLD_ID` FROM `SRO_SHARD`.`CHAR` c JOIN `SRO_SHARD`.`TELEPORT_POINT` t ON t.`ID`=c.`RETURN_TELEPORT_ID` AND t.`SERVICE`=1 WHERE c.`ID`=? FOR UPDATE", characterID).Scan(
		&destination.ID, &destination.Region, &destination.X, &destination.Y, &destination.Z, &destination.WorldID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return destination, false, nil
	}
	if err != nil {
		return destination, false, fmt.Errorf("resolve resurrection return point: %w", err)
	}
	result, err := tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `HP`=?,`MP`=?,`FK_REGION`=?,`POS_X`=?,`POS_Y`=?,`POS_Z`=?,`UTIME`=CURRENT_TIMESTAMP WHERE `ID`=?", hp, mp, destination.Region, destination.X, destination.Y, destination.Z, characterID)
	if err != nil {
		return destination, false, fmt.Errorf("persist return-point resurrection: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return destination, false, fmt.Errorf("read return-point resurrection result: %w", err)
	}
	if affected != 1 {
		return destination, false, nil
	}
	if err = tx.Commit(); err != nil {
		return destination, false, fmt.Errorf("commit return-point resurrection: %w", err)
	}
	return destination, true, nil
}
