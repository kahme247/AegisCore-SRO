package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type FortressExpenditureReason byte

const (
	FortressExpenditureBuff FortressExpenditureReason = iota
	FortressExpenditureStructure
	FortressExpenditureNPC
	FortressExpenditureGuard
	FortressExpenditureItemCreation
	FortressExpenditureGrowth
	FortressExpenditureGuildItem
	FortressExpenditureDungeon
	FortressExpenditureMiscellaneous
)

func (r FortressExpenditureReason) Valid() bool { return r <= FortressExpenditureMiscellaneous }

type FortressExpenditure struct {
	Timestamp     time.Time
	CharacterName string
	Amount        uint32
	Reason        FortressExpenditureReason
}

type FortressExpenditureRepository interface {
	FortressExpenditureHistory(context.Context, int, int) ([]FortressExpenditure, error)
}

type FortressPeriod byte

const (
	FortressPeriodDefault     FortressPeriod = 0
	FortressPeriodBattle      FortressPeriod = 1
	FortressPeriodApplication FortressPeriod = 2
	FortressPeriodTax         FortressPeriod = 4
)

type FortressRequestType byte

const (
	FortressRequestAttack FortressRequestType = 1
	FortressRequestDefend FortressRequestType = 2
)

type FortressApplication struct {
	FortressID int
	GuildID    int
	ActorID    int
	Type       FortressRequestType
	AppliedAt  time.Time
}

type FortressState struct {
	FortressID       int
	OwnerGuildID     int
	TemporaryGuildID int
	Period           FortressPeriod
	PeriodStartedAt  time.Time
	PeriodEndsAt     time.Time
}

type FortressStructure struct {
	FortressID     int
	RefEventID     uint32
	UniqueID       uint32
	RefObjectID    uint32
	Heart          bool
	CurrentHP      uint32
	MaximumHP      uint32
	State          uint16
	OccupyingGuild int
	UpdatedAt      time.Time
}

type FortressDamageResult struct {
	Structure FortressStructure
	Applied   uint32
	Destroyed bool
	Captured  bool
	Siege     FortressState
}

type FortressSiegeRepository interface {
	ResolveFortressApplicationTarget(context.Context) (int, error)
	ApplyFortressSiege(context.Context, int, int, FortressRequestType, time.Time) (FortressApplication, error)
	CancelFortressSiege(context.Context, int, int, time.Time) (FortressApplication, error)
	SetFortressPeriod(context.Context, int, FortressPeriod, time.Time, time.Time) (FortressState, error)
	CaptureFortress(context.Context, int, int, time.Time) (FortressState, error)
	FinishFortressBattle(context.Context, int, time.Time) (FortressState, error)
	RegisterFortressStructure(context.Context, FortressStructure) error
	DamageFortressStructure(context.Context, int, uint32, int, uint32, time.Time) (FortressDamageResult, error)
	RecordFortressBattleKill(context.Context, int, int, int, time.Time) error
}

func (r *SQLSocialRepository) ResolveFortressApplicationTarget(ctx context.Context) (int, error) {
	if r == nil || r.shard == nil {
		return 0, ErrSocialRepositoryUnavailable
	}
	rows, err := r.shard.QueryContext(socialContext(ctx), "SELECT ID FROM FORTRESS WHERE PERIOD=? ORDER BY ID LIMIT 2", FortressPeriodApplication)
	if err != nil {
		return 0, fmt.Errorf("resolve active fortress application: %w", err)
	}
	defer rows.Close()
	fortressID := 0
	for rows.Next() {
		if fortressID != 0 {
			return 0, ErrSocialConflict
		}
		if err = rows.Scan(&fortressID); err != nil {
			return 0, fmt.Errorf("scan active fortress application: %w", err)
		}
	}
	if err = rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate active fortress application: %w", err)
	}
	if fortressID == 0 {
		return 0, ErrSocialNotFound
	}
	return fortressID, nil
}

func (t FortressRequestType) Valid() bool {
	return t == FortressRequestAttack || t == FortressRequestDefend
}

func (p FortressPeriod) Valid() bool {
	return p == FortressPeriodDefault || p == FortressPeriodBattle || p == FortressPeriodApplication || p == FortressPeriodTax
}

func validFortressPeriodTransition(from, to FortressPeriod) bool {
	if !from.Valid() || !to.Valid() || from == to {
		return false
	}
	switch from {
	case FortressPeriodDefault:
		return to == FortressPeriodApplication || to == FortressPeriodBattle || to == FortressPeriodTax
	case FortressPeriodApplication:
		return to == FortressPeriodDefault || to == FortressPeriodBattle
	case FortressPeriodBattle:
		return to == FortressPeriodDefault || to == FortressPeriodTax
	case FortressPeriodTax:
		return to == FortressPeriodDefault || to == FortressPeriodApplication
	default:
		return false
	}
}

func (r *SQLSocialRepository) ApplyFortressSiege(ctx context.Context, fortressID, actor int, requestType FortressRequestType, at time.Time) (application FortressApplication, err error) {
	if r == nil || r.shard == nil {
		return application, ErrSocialRepositoryUnavailable
	}
	if fortressID <= 0 || actor <= 0 || !requestType.Valid() || at.IsZero() {
		return application, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID, guildLevel, memberCount int
		if scanErr := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID, &guildLevel); scanErr != nil {
			return ErrSocialAccessDenied
		}
		if guildLevel < 7 {
			return ErrSocialInsufficientLevel
		}
		if scanErr := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_MEMBER WHERE FK_GUILD=?", guildID).Scan(&memberCount); scanErr != nil {
			return fmt.Errorf("count fortress applicant members: %w", scanErr)
		}
		if memberCount < 3 {
			return ErrSocialCapacity
		}
		var period FortressPeriod
		var owner sql.NullInt64
		if scanErr := tx.QueryRowContext(ctx, "SELECT PERIOD,FK_GUILD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&period, &owner); scanErr != nil {
			if scanErr == sql.ErrNoRows {
				return ErrSocialNotFound
			}
			return fmt.Errorf("lock fortress application state: %w", scanErr)
		}
		if period != FortressPeriodApplication || owner.Valid && int(owner.Int64) == guildID {
			return ErrSocialConflict
		}
		result, execErr := tx.ExecContext(ctx, "INSERT INTO FORTRESS_APPLICATION (FK_FORTRESS,FK_GUILD,FK_ACTOR,REQUEST_TYPE,APPLIED_AT) VALUES (?,?,?,?,?)", fortressID, guildID, actor, requestType, at.UTC())
		if execErr != nil {
			return errors.Join(ErrSocialConflict, execErr)
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialConflict
		}
		application = FortressApplication{FortressID: fortressID, GuildID: guildID, ActorID: actor, Type: requestType, AppliedAt: at.UTC()}
		return nil
	})
	return application, err
}

func (r *SQLSocialRepository) CancelFortressSiege(ctx context.Context, fortressID, actor int, at time.Time) (application FortressApplication, err error) {
	if r == nil || r.shard == nil {
		return application, ErrSocialRepositoryUnavailable
	}
	if fortressID <= 0 || actor <= 0 || at.IsZero() {
		return application, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		if scanErr := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=? AND MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID); scanErr != nil {
			return ErrSocialAccessDenied
		}
		var period FortressPeriod
		if scanErr := tx.QueryRowContext(ctx, "SELECT PERIOD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&period); scanErr != nil {
			return ErrSocialNotFound
		}
		if period != FortressPeriodApplication {
			return ErrSocialConflict
		}
		application = FortressApplication{FortressID: fortressID, GuildID: guildID, ActorID: actor, AppliedAt: at.UTC()}
		if scanErr := tx.QueryRowContext(ctx, "SELECT REQUEST_TYPE,APPLIED_AT FROM FORTRESS_APPLICATION WHERE FK_FORTRESS=? AND FK_GUILD=? FOR UPDATE", fortressID, guildID).Scan(&application.Type, &application.AppliedAt); scanErr != nil {
			return ErrSocialNotFound
		}
		result, execErr := tx.ExecContext(ctx, "DELETE FROM FORTRESS_APPLICATION WHERE FK_FORTRESS=? AND FK_GUILD=?", fortressID, guildID)
		if execErr != nil {
			return fmt.Errorf("cancel fortress application: %w", execErr)
		}
		return requireSocialRow(result)
	})
	return application, err
}

func (r *SQLSocialRepository) SetFortressPeriod(ctx context.Context, fortressID int, period FortressPeriod, startsAt, endsAt time.Time) (state FortressState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if fortressID <= 0 || !period.Valid() || startsAt.IsZero() || (!endsAt.IsZero() && !endsAt.After(startsAt)) {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var current FortressPeriod
		if scanErr := tx.QueryRowContext(ctx, "SELECT PERIOD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&current); scanErr != nil {
			return ErrSocialNotFound
		}
		if !validFortressPeriodTransition(current, period) {
			return ErrSocialConflict
		}
		var end any
		if !endsAt.IsZero() {
			end = endsAt.UTC()
		}
		result, execErr := tx.ExecContext(ctx, "UPDATE FORTRESS SET PERIOD=?,PERIOD_STARTED_AT=?,PERIOD_ENDS_AT=?,VERSION=VERSION+1 WHERE ID=? AND PERIOD=?", period, startsAt.UTC(), end, fortressID, current)
		if execErr != nil {
			return fmt.Errorf("set fortress period: %w", execErr)
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialConflict
		}
		state = FortressState{FortressID: fortressID, Period: period, PeriodStartedAt: startsAt.UTC()}
		if !endsAt.IsZero() {
			state.PeriodEndsAt = endsAt.UTC()
		}
		return nil
	})
	return state, err
}

func (r *SQLSocialRepository) CaptureFortress(ctx context.Context, fortressID, actor int, at time.Time) (state FortressState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if fortressID <= 0 || actor <= 0 || at.IsZero() {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		if scanErr := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=? FOR UPDATE", actor).Scan(&guildID); scanErr != nil {
			return ErrSocialAccessDenied
		}
		var period FortressPeriod
		var owner, temporary sql.NullInt64
		if scanErr := tx.QueryRowContext(ctx, "SELECT PERIOD,FK_GUILD,TEMP_GUILD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&period, &owner, &temporary); scanErr != nil {
			return ErrSocialNotFound
		}
		if period != FortressPeriodBattle || owner.Valid && int(owner.Int64) == guildID {
			return ErrSocialConflict
		}
		var approved int
		if scanErr := tx.QueryRowContext(ctx, "SELECT 1 FROM FORTRESS_APPLICATION WHERE FK_FORTRESS=? AND FK_GUILD=? AND REQUEST_TYPE=?", fortressID, guildID, FortressRequestAttack).Scan(&approved); scanErr != nil {
			return ErrSocialAccessDenied
		}
		result, execErr := tx.ExecContext(ctx, "UPDATE FORTRESS SET TEMP_GUILD=?,CAPTURED_AT=?,VERSION=VERSION+1 WHERE ID=? AND PERIOD=?", guildID, at.UTC(), fortressID, FortressPeriodBattle)
		if execErr != nil {
			return fmt.Errorf("capture fortress: %w", execErr)
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialConflict
		}
		state = FortressState{FortressID: fortressID, OwnerGuildID: int(owner.Int64), TemporaryGuildID: guildID, Period: period}
		return nil
	})
	return state, err
}

func (r *SQLSocialRepository) FinishFortressBattle(ctx context.Context, fortressID int, at time.Time) (state FortressState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if fortressID <= 0 || at.IsZero() {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var period FortressPeriod
		var owner, temporary sql.NullInt64
		if scanErr := tx.QueryRowContext(ctx, "SELECT PERIOD,FK_GUILD,TEMP_GUILD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&period, &owner, &temporary); scanErr != nil {
			return ErrSocialNotFound
		}
		if period != FortressPeriodBattle {
			return ErrSocialConflict
		}
		winner := owner
		if temporary.Valid {
			winner = temporary
		}
		result, execErr := tx.ExecContext(ctx, "UPDATE FORTRESS SET FK_GUILD=?,TEMP_GUILD=NULL,PERIOD=?,PERIOD_STARTED_AT=?,PERIOD_ENDS_AT=NULL,VERSION=VERSION+1 WHERE ID=? AND PERIOD=?", winner, FortressPeriodDefault, at.UTC(), fortressID, FortressPeriodBattle)
		if execErr != nil {
			return fmt.Errorf("finish fortress battle: %w", execErr)
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialConflict
		}
		if _, execErr = tx.ExecContext(ctx, "DELETE FROM FORTRESS_APPLICATION WHERE FK_FORTRESS=?", fortressID); execErr != nil {
			return fmt.Errorf("clear fortress applications: %w", execErr)
		}
		state = FortressState{FortressID: fortressID, OwnerGuildID: int(winner.Int64), Period: FortressPeriodDefault, PeriodStartedAt: at.UTC()}
		return nil
	})
	return state, err
}

type FortressExpenditureWriter interface {
	RecordFortressExpenditure(context.Context, int, int, uint32, FortressExpenditureReason, time.Time) error
}

func (r *SQLSocialRepository) FortressExpenditureHistory(ctx context.Context, actor, limit int) ([]FortressExpenditure, error) {
	if r == nil || r.shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || limit <= 0 || limit > 255 {
		return nil, ErrSocialInvalidRequest
	}
	rows, err := r.shard.QueryContext(socialContext(ctx), "SELECT e.CREATED_AT,e.CHARACTER_NAME,e.AMOUNT,e.REASON FROM GUILD_MEMBER m JOIN FORTRESS f ON f.FK_GUILD=m.FK_GUILD JOIN FORTRESS_EXPENDITURE e ON e.FK_FORTRESS=f.ID AND e.FK_GUILD=f.FK_GUILD WHERE m.FK_CHAR=? ORDER BY e.CREATED_AT DESC,e.ID DESC LIMIT ?", actor, limit)
	if err != nil {
		return nil, fmt.Errorf("load fortress expenditure history: %w", err)
	}
	defer rows.Close()
	history := make([]FortressExpenditure, 0)
	for rows.Next() {
		var entry FortressExpenditure
		var reason byte
		if err = rows.Scan(&entry.Timestamp, &entry.CharacterName, &entry.Amount, &reason); err != nil {
			return nil, fmt.Errorf("scan fortress expenditure history: %w", err)
		}
		entry.Timestamp = entry.Timestamp.UTC()
		entry.Reason = FortressExpenditureReason(reason)
		if !entry.Reason.Valid() {
			return nil, fmt.Errorf("load fortress expenditure history: invalid reason %d", reason)
		}
		history = append(history, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fortress expenditure history: %w", err)
	}
	return history, nil
}

func (r *SQLSocialRepository) RecordFortressExpenditure(ctx context.Context, fortressID, actor int, amount uint32, reason FortressExpenditureReason, at time.Time) error {
	if r == nil || r.shard == nil {
		return ErrSocialRepositoryUnavailable
	}
	if fortressID <= 0 || actor <= 0 || amount == 0 || !reason.Valid() || at.IsZero() {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var treasury uint64
		var characterName string
		err := tx.QueryRowContext(ctx, "SELECT f.FK_GUILD,f.TREASURY_GOLD,c.CHAR_NAME FROM FORTRESS f JOIN GUILD_MEMBER m ON m.FK_GUILD=f.FK_GUILD AND m.FK_CHAR=? AND m.MEMBER_CLASS=? JOIN `CHAR` c ON c.id=m.FK_CHAR WHERE f.ID=? FOR UPDATE", actor, GuildMemberMaster, fortressID).Scan(&guildID, &treasury, &characterName)
		if err != nil {
			return ErrSocialAccessDenied
		}
		if treasury < uint64(amount) {
			return ErrSocialConflict
		}
		result, err := tx.ExecContext(ctx, "UPDATE FORTRESS SET TREASURY_GOLD=TREASURY_GOLD-? WHERE ID=? AND FK_GUILD=? AND TREASURY_GOLD>=?", amount, fortressID, guildID, amount)
		if err != nil {
			return fmt.Errorf("deduct fortress treasury: %w", err)
		}
		if err = requireSocialRow(result); err != nil {
			return ErrSocialConflict
		}
		result, err = tx.ExecContext(ctx, "INSERT INTO FORTRESS_EXPENDITURE (FK_FORTRESS,FK_GUILD,FK_CHAR,CHARACTER_NAME,AMOUNT,REASON,CREATED_AT) VALUES (?,?,?,?,?,?,?)", fortressID, guildID, actor, characterName, amount, reason, at.UTC())
		if err != nil {
			return fmt.Errorf("record fortress expenditure: %w", err)
		}
		return requireSocialRow(result)
	})
}

var _ FortressExpenditureRepository = (*SQLSocialRepository)(nil)
var _ FortressExpenditureWriter = (*SQLSocialRepository)(nil)
var _ FortressSiegeRepository = (*SQLSocialRepository)(nil)
