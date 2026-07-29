package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
	"github.com/sirupsen/logrus"
)

// ErrReferenceDataRepositoryUnavailable identifies a missing injected shard
// queryer. The live Agent must fail startup rather than falling back to a
// request-scoped database connection for reference data.
var ErrReferenceDataRepositoryUnavailable = errors.New("reference data repository is unavailable")

// ErrReferenceDataDuplicateID identifies duplicate rows in a single source
// catalog. Keeping the check at load time prevents a later row from silently
// replacing a reference in the immutable runtime snapshot.
var ErrReferenceDataDuplicateID = errors.New("duplicate reference data id")

// ReferenceDataRepositoryError preserves the operation and underlying driver,
// scan, or context error so startup can report a useful failure without
// treating a partial catalog as a valid snapshot.
type ReferenceDataRepositoryError struct {
	Dataset   string
	Operation string
	Err       error
}

func (e *ReferenceDataRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("reference data repository %s %s: %v", e.Dataset, e.Operation, e.Err)
}

func (e *ReferenceDataRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ReferenceDataSnapshot is a complete, locally assembled catalog. Callers
// should publish it to the runtime globals only after Load succeeds so a
// failed character query cannot leave a mixed-generation reference index.
type ReferenceDataSnapshot struct {
	Items      map[uint32]RefItem
	Characters map[uint32]RefChar
	Skills     map[uint32]RefSkill
}

var SelectAllRefSkills = "SELECT `ID`,`GROUP_ID`,`BASIC_GROUP`,`BASIC_ORIGINAL`,`BASIC_LEVEL`,`BASIC_ACTIVITY`,`BASIC_CHAINCODE`,`REQCOMMON_MASTERY1`,`REQCOMMON_MASTERY2`,`REQCOMMON_MASTERYLEVEL1`,`REQCOMMON_MASTERYLEVEL2`,`REQCOMMON_STR`,`REQCOMMON_INT`,`REQLEARN_SKILL1`,`REQLEARN_SKILL2`,`REQLEARN_SKILL3`,`REQLEARN_SKILLLEVEL1`,`REQLEARN_SKILLLEVEL2`,`REQLEARN_SKILLLEVEL3`,`REQLEARN_SP`,`REQLEARN_RACE`,`TARGET_REQUIRED`,`TARGETGROUP_SELF`,`TARGETGROUP_ALLY`,`TARGETGROUP_PARTY`,`TARGETGROUP_ENEMY_M`,`TARGETGROUP_ENEMY_P`,`TARGETETC_SELECTDEADBODY`,`REQCAST_WEAPON1`,`REQCAST_WEAPON2`,`CONSUME_HP`,`CONSUME_MP`,`CONSUME_HPRATIO`,`CONSUME_MPRATIO`,`ACTION_ACTIONDURATION`,`ACTION_REUSEDELAY`,`ACTION_COOLTIME`,`ACTION_RANGE`," + refSkillParameterProjection() + " FROM `SRO_SHARD`.`REF_SKILL` WHERE `SERVICE` = 1"

func refSkillParameterProjection() string {
	columns := make([]string, len(RefSkill{}.Params))
	for index := range columns {
		columns[index] = fmt.Sprintf("`PARAM%d`", index+1)
	}
	return strings.Join(columns, ",")
}

// ReferenceDataRepository loads the startup reference catalogs through an
// injected long-lived shard pool or transaction. It never opens or closes the
// dependency.
type ReferenceDataRepository interface {
	Load(ctx context.Context) (ReferenceDataSnapshot, error)
}

// SQLReferenceDataRepository is the production reference-data boundary. The
// SELECT * scan destinations intentionally mirror the legacy compatibility
// functions exactly; this code must not guess a projection or change column
// order until the deployed schema is captured and verified.
type SQLReferenceDataRepository struct {
	shard frameworkdb.Queryer
}

var _ ReferenceDataRepository = (*SQLReferenceDataRepository)(nil)

// NewReferenceDataRepository constructs a context-aware repository over an
// injected long-lived shard pool or transaction.
func NewReferenceDataRepository(shard frameworkdb.Queryer) (*SQLReferenceDataRepository, error) {
	if shard == nil {
		return nil, &ReferenceDataRepositoryError{
			Dataset:   "catalog",
			Operation: "configure",
			Err:       ErrReferenceDataRepositoryUnavailable,
		}
	}
	return &SQLReferenceDataRepository{shard: shard}, nil
}

// Load reads items, characters, and skills into local maps. It returns no
// snapshot on any failure, allowing callers to keep the previous catalog.
func (r *SQLReferenceDataRepository) Load(ctx context.Context) (ReferenceDataSnapshot, error) {
	items, err := r.loadItems(ctx)
	if err != nil {
		return ReferenceDataSnapshot{}, err
	}
	characters, err := r.loadCharacters(ctx)
	if err != nil {
		return ReferenceDataSnapshot{}, err
	}
	skills, err := r.loadSkills(ctx)
	if err != nil {
		return ReferenceDataSnapshot{}, err
	}
	return ReferenceDataSnapshot{Items: items, Characters: characters, Skills: skills}, nil
}

func (r *SQLReferenceDataRepository) loadSkills(ctx context.Context) (map[uint32]RefSkill, error) {
	ctx, err := r.context("skills", "load", ctx)
	if err != nil {
		return nil, err
	}

	rows, err := r.shard.QueryContext(ctx, SelectAllRefSkills)
	if err != nil {
		return nil, referenceDataError("skills", "query", err)
	}
	defer rows.Close()

	skills := make(map[uint32]RefSkill)
	for rows.Next() {
		var skill RefSkill
		destinations := []any{
			&skill.ID, &skill.GroupID, &skill.BasicGroup, &skill.BasicOriginal, &skill.BasicLevel, &skill.BasicActivity, &skill.BasicChainCode,
			&skill.Mastery1, &skill.Mastery2, &skill.MasteryLevel1, &skill.MasteryLevel2, &skill.RequiredStr, &skill.RequiredInt,
			&skill.RequiredSkill1, &skill.RequiredSkill2, &skill.RequiredSkill3, &skill.RequiredLevel1, &skill.RequiredLevel2,
			&skill.RequiredLevel3, &skill.RequiredSP, &skill.RequiredRace, &skill.TargetRequired, &skill.TargetSelf, &skill.TargetAlly,
			&skill.TargetParty, &skill.TargetEnemyMob, &skill.TargetEnemyPVP, &skill.TargetDeadBody, &skill.RequiredWeapon1,
			&skill.RequiredWeapon2, &skill.ConsumeHP, &skill.ConsumeMP, &skill.ConsumeHPRatio, &skill.ConsumeMPRatio,
			&skill.ActionDuration, &skill.ReuseDelay, &skill.CoolTime, &skill.ActionRange,
		}
		for index := range skill.Params {
			destinations = append(destinations, &skill.Params[index])
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, referenceDataError("skills", "scan", err)
		}
		skill.parseParameters()
		if _, exists := skills[skill.ID]; exists {
			return nil, referenceDataError("skills", "duplicate id", fmt.Errorf("%w: %d", ErrReferenceDataDuplicateID, skill.ID))
		}
		skills[skill.ID] = skill
	}
	if err := rows.Err(); err != nil {
		return nil, referenceDataError("skills", "iterate", err)
	}
	logrus.Infof("finished loading %d ref skills", len(skills))
	return skills, nil
}

func (r *SQLReferenceDataRepository) loadItems(ctx context.Context) (map[uint32]RefItem, error) {
	ctx, err := r.context("items", "load", ctx)
	if err != nil {
		return nil, err
	}

	count, err := r.count(ctx, "items", SelectCountOfRefItems)
	if err != nil {
		return nil, err
	}
	logrus.Infof("loading %d ref items from database", count)

	rows, err := r.shard.QueryContext(ctx, SelectAllRefItems)
	if err != nil {
		return nil, referenceDataError("items", "query", err)
	}
	defer rows.Close()

	items := make(map[uint32]RefItem)
	for rows.Next() {
		var item RefItem
		if err := scanReferenceDataItem(rows, &item); err != nil {
			return nil, referenceDataError("items", "scan", err)
		}
		if _, exists := items[item.ID]; exists {
			return nil, referenceDataError("items", "duplicate id", fmt.Errorf("%w: %d", ErrReferenceDataDuplicateID, item.ID))
		}
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, referenceDataError("items", "iterate", err)
	}

	logrus.Infoln("finished loading ref items")
	return items, nil
}

func (r *SQLReferenceDataRepository) loadCharacters(ctx context.Context) (map[uint32]RefChar, error) {
	ctx, err := r.context("characters", "load", ctx)
	if err != nil {
		return nil, err
	}

	count, err := r.count(ctx, "characters", SelectAllRefCharsCount)
	if err != nil {
		return nil, err
	}
	logrus.Infof("loading %d ref chars from database", count)

	rows, err := r.shard.QueryContext(ctx, SelectAllRefChars)
	if err != nil {
		return nil, referenceDataError("characters", "query", err)
	}
	defer rows.Close()

	characters := make(map[uint32]RefChar)
	for rows.Next() {
		var character RefChar
		if err := scanReferenceDataCharacter(rows, &character); err != nil {
			return nil, referenceDataError("characters", "scan", err)
		}
		if _, exists := characters[character.ID]; exists {
			return nil, referenceDataError("characters", "duplicate id", fmt.Errorf("%w: %d", ErrReferenceDataDuplicateID, character.ID))
		}
		characters[character.ID] = character
	}
	if err := rows.Err(); err != nil {
		return nil, referenceDataError("characters", "iterate", err)
	}

	logrus.Infoln("finished loading ref chars")
	return characters, nil
}

func (r *SQLReferenceDataRepository) count(ctx context.Context, dataset, query string) (int, error) {
	if r == nil || r.shard == nil {
		return 0, referenceDataError(dataset, "query count", ErrReferenceDataRepositoryUnavailable)
	}

	rows, err := r.shard.QueryContext(ctx, query)
	if err != nil {
		return 0, referenceDataError(dataset, "query count", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, referenceDataError(dataset, "iterate count", err)
		}
		return 0, nil
	}

	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, referenceDataError(dataset, "scan count", err)
	}
	for rows.Next() {
		// COUNT(*) should have a single row. Drain any unexpected rows so the
		// driver has a chance to surface a terminal cursor failure.
	}
	if err := rows.Err(); err != nil {
		return 0, referenceDataError(dataset, "iterate count", err)
	}
	return count, nil
}

func (r *SQLReferenceDataRepository) context(dataset, operation string, ctx context.Context) (context.Context, error) {
	if r == nil || r.shard == nil {
		return nil, referenceDataError(dataset, operation, ErrReferenceDataRepositoryUnavailable)
	}
	if ctx == nil {
		return context.Background(), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, referenceDataError(dataset, operation, err)
	}
	return ctx, nil
}

func referenceDataError(dataset, operation string, err error) error {
	return &ReferenceDataRepositoryError{Dataset: dataset, Operation: operation, Err: err}
}

type referenceDataRowScanner interface {
	Scan(dest ...any) error
}

// legacyBoolScanner accepts the boolean encodings present in historical SRO
// reference tables. The MySQL driver deliberately rejects values other than
// 0 and 1 when scanning directly into *bool, while the supplied ITEMDATA and
// CHAR_REF_DATA catalogs also use 255 for true.
type legacyBoolScanner struct {
	target *bool
}

func (s *legacyBoolScanner) Scan(source any) error {
	if s == nil || s.target == nil {
		return errors.New("legacy boolean destination is unavailable")
	}

	var value bool
	switch source := source.(type) {
	case bool:
		value = source
	case int64:
		value = source != 0
	case uint64:
		value = source != 0
	case []byte:
		parsed, err := strconv.ParseInt(strings.TrimSpace(string(source)), 10, 64)
		if err != nil {
			return fmt.Errorf("parse legacy boolean %q: %w", source, err)
		}
		value = parsed != 0
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(source), 10, 64)
		if err != nil {
			return fmt.Errorf("parse legacy boolean %q: %w", source, err)
		}
		value = parsed != 0
	default:
		return fmt.Errorf("unsupported legacy boolean source %T", source)
	}

	*s.target = value
	return nil
}

func scanReferenceDataRow(scanner referenceDataRowScanner, destinations ...any) error {
	normalized := make([]any, len(destinations))
	for index, destination := range destinations {
		if target, ok := destination.(*bool); ok {
			normalized[index] = &legacyBoolScanner{target: target}
			continue
		}
		normalized[index] = destination
	}
	return scanner.Scan(normalized...)
}

// scanReferenceDataItem preserves the legacy ITEMDATA SELECT * destination
// order. It is deliberately not a named-column mapping: the current schema
// was not supplied as verified source material for a projection rewrite.
func scanReferenceDataItem(scanner referenceDataRowScanner, item *RefItem) error {
	return scanReferenceDataRow(scanner,
		&item.ID,
		&item.CodeName,
		&item.ObjName,
		&item.OrgObjCodeName,
		&item.NameStrID,
		&item.DescStrID,
		&item.IsCashItem,
		&item.IsBionic,
		&item.TypeID1,
		&item.TypeID2,
		&item.TypeID3,
		&item.TypeID4,
		&item.DecayTime,
		&item.Country,
		&item.Rarity,
		&item.IsTradable,
		&item.IsSellable,
		&item.IsBuyable,
		&item.IsBorrowable,
		&item.IsDropable,
		&item.IsPickable,
		&item.IsRepairable,
		&item.IsRevivable,
		&item.IsUseable,
		&item.IsThrowable,
		&item.Price,
		&item.RepairCost,
		&item.ReviveCost,
		&item.BorrowCost,
		&item.KeepingFee,
		&item.SellPrice,
		&item.ReqLevelType1,
		&item.ReqLevel1,
		&item.ReqLevelType2,
		&item.ReqLevel2,
		&item.ReqLevelType3,
		&item.ReqLevel3,
		&item.ReqLevelType4,
		&item.ReqLevel4,
		&item.MaxContain,
		&item.RegionID,
		&item.Dir,
		&item.OffsetX,
		&item.OffsetY,
		&item.OffsetZ,
		&item.Speed1,
		&item.Speed2,
		&item.Scale,
		&item.BCHeight,
		&item.BCRadius,
		&item.EventID,
		&item.AssocFileObj,
		&item.AssocFileDrop,
		&item.AssocFileIcon,
		&item.AssocFile1,
		&item.AssocFile2,
		&item.StackSize,
		&item.RequiredGender,
		&item.RequiredStr,
		&item.RequiredInt,
		&item.ItemClass,
		&item.SetID,
		&item.DurabilityLower,
		&item.DurabilityUpper,
		&item.PhyDefLower,
		&item.PhyDefUpper,
		&item.PhyDefIncrease,
		&item.EvasionRateLower,
		&item.EvasionRateUpper,
		&item.EvasionRateIncrease,
		&item.PhyAbsRateLower,
		&item.PhyAbsRateUpper,
		&item.PhyAbsRateIncrease,
		&item.BlockingRateLower,
		&item.BlockingRateUpper,
		&item.MagDefLower,
		&item.MagDefUpper,
		&item.MagDefIncrease,
		&item.MagAbsRateLower,
		&item.MagAbsRateUpper,
		&item.MagAbsRateIncrease,
		&item.PhyDefReinforceLower,
		&item.PhyDefReinforceUpper,
		&item.MagDefReinforceLower,
		&item.MagDefReinforceUpper,
		&item.Quivered,
		&item.Ammo1TypeID4,
		&item.Ammo2TypeID4,
		&item.Ammo3TypeID4,
		&item.Ammo4TypeID4,
		&item.Ammo5TypeID4,
		&item.SpeedClass,
		&item.IsTwoHanded,
		&item.Range,
		&item.PhyAttackMinLower,
		&item.PhyAttackMinUpper,
		&item.PhyAttackMaxLower,
		&item.PhyAttackMaxUpper,
		&item.PhyAttackIncrease,
		&item.MagAttackMinLower,
		&item.MagAttackMinUpper,
		&item.MagAttackMaxLower,
		&item.MagAttackMaxUpper,
		&item.MagAttackIncrease,
		&item.PhyAttackReinforceMinLower,
		&item.PhyAttackReinforceMinUpper,
		&item.PhyAttackReinforceMaxLower,
		&item.PhyAttackReinforceMaxUpper,
		&item.MagAttackReinforceMinLower,
		&item.MagAttackReinforceMinUpper,
		&item.MagAttackReinforceMaxLower,
		&item.MagAttackReinforceMaxUpper,
		&item.HitRateLower,
		&item.HitRateUpper,
		&item.HitRateIncrease,
		&item.CritRateLower,
		&item.CritRateUpper,
		&item.Param1,
		&item.Param1Desc,
		&item.Param2,
		&item.Param2Desc,
		&item.Param3,
		&item.Param3Desc,
		&item.Param4,
		&item.Param4Desc,
		&item.Param5,
		&item.Param5Desc,
		&item.Param6,
		&item.Param6Desc,
		&item.Param7,
		&item.Param7Desc,
		&item.Param8,
		&item.Param8Desc,
		&item.Param9,
		&item.Param9Desc,
		&item.Param10,
		&item.Param10Desc,
		&item.Param11,
		&item.Param11Desc,
		&item.Param12,
		&item.Param12Desc,
		&item.Param13,
		&item.Param13Desc,
		&item.Param14,
		&item.Param14Desc,
		&item.Param15,
		&item.Param15Desc,
		&item.Param16,
		&item.Param16Desc,
		&item.Param17,
		&item.Param17Desc,
		&item.Param18,
		&item.Param18Desc,
		&item.Param19,
		&item.Param19Desc,
		&item.Param20,
		&item.Param20Desc,
		&item.MaxMagicOptCount,
		&item.ChildItemCount,
	)
}

// scanReferenceDataCharacter preserves the legacy CHAR_REF_DATA SELECT *
// destination order for the same evidence-bound reason as the item scanner.
func scanReferenceDataCharacter(scanner referenceDataRowScanner, character *RefChar) error {
	return scanReferenceDataRow(scanner,
		&character.ID,
		&character.CodeName,
		&character.ObjName,
		&character.OrgObjCodeName,
		&character.NameStrID,
		&character.DescStrID,
		&character.IsCashItem,
		&character.IsBionic,
		&character.TypeID1,
		&character.TypeID2,
		&character.TypeID3,
		&character.TypeID4,
		&character.DecayTime,
		&character.Country,
		&character.Rarity,
		&character.IsTradable,
		&character.IsSellable,
		&character.IsBuyable,
		&character.IsBorrowable,
		&character.IsDropable,
		&character.IsPickable,
		&character.IsRepairable,
		&character.IsRevivable,
		&character.IsUseable,
		&character.IsThrowable,
		&character.Price,
		&character.RepairCost,
		&character.ReviveCost,
		&character.BorrowCost,
		&character.KeepingFee,
		&character.SellPrice,
		&character.ReqLevelType1,
		&character.ReqLevel1,
		&character.ReqLevelType2,
		&character.ReqLevel2,
		&character.ReqLevelType3,
		&character.ReqLevel3,
		&character.ReqLevelType4,
		&character.ReqLevel4,
		&character.MaxContain,
		&character.RegionID,
		&character.Dir,
		&character.OffsetX,
		&character.OffsetY,
		&character.OffsetZ,
		&character.Speed1,
		&character.Speed2,
		&character.Scale,
		&character.BCHeight,
		&character.BCRadius,
		&character.EventID,
		&character.AssocFileObj,
		&character.AssocFileDrop,
		&character.AssocFileIcon,
		&character.AssocFile1,
		&character.AssocFile2,
		&character.Level,
		&character.CharGender,
		&character.MaxHP,
		&character.MaxMP,
		&character.InventorySize,
		&character.CanStoreTID1,
		&character.CanStoreTID2,
		&character.CanStoreTID3,
		&character.CanStoreTID4,
		&character.CanBeVehicle,
		&character.CanControl,
		&character.DamagePortion,
		&character.MaxPassenger,
		&character.AssocTactics,
		&character.PhyDef,
		&character.MagDef,
		&character.PhyAbsorbRate,
		&character.MagAbsorbRate,
		&character.EvasionRate,
		&character.BlockRate,
		&character.HitRate,
		&character.CriticalHitRate,
		&character.ExpToGive,
		&character.CreepType,
		&character.Knockdown,
		&character.KORecoveryTime,
		&character.DefaultSkill1,
		&character.DefaultSkill2,
		&character.DefaultSkill3,
		&character.DefaultSkill4,
		&character.DefaultSkill5,
		&character.DefaultSkill6,
		&character.DefaultSkill7,
		&character.DefaultSkill8,
		&character.DefaultSkill9,
		&character.DefaultSkill10,
		&character.TextureType,
		&character.Except1,
		&character.Except2,
		&character.Except3,
		&character.Except4,
		&character.Except5,
		&character.Except6,
		&character.Except7,
		&character.Except8,
		&character.Except9,
		&character.Except10,
	)
}
