package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrCharacterSkillRepositoryUnavailable = errors.New("character skill repository is unavailable")
	ErrCharacterSkillCharacterIDRequired   = errors.New("character skill character ID is required")
	ErrCharacterSkillIDRequired            = errors.New("character skill ID is required")
	ErrCharacterSkillEnabledInvalid        = errors.New("character skill enabled state is invalid")
	ErrSkillInvalid                        = errors.New("skill is invalid for this character")
	ErrSkillRaceMismatch                   = errors.New("skill race requirement is not met")
	ErrSkillLevelChainInvalid              = errors.New("skill level chain is invalid")
	ErrSkillAlreadyLearned                 = errors.New("skill is already learned")
	ErrSkillMasteryRequired                = errors.New("skill mastery requirement is not met")
	ErrSkillStrengthRequired               = errors.New("skill strength requirement is not met")
	ErrSkillIntellectRequired              = errors.New("skill intellect requirement is not met")
	ErrSkillPrerequisiteRequired           = errors.New("skill prerequisite is not met")
	ErrSkillInsufficientSP                 = errors.New("insufficient skill points")
	ErrSkillConflict                       = errors.New("skill state conflicts with the request")
	ErrSkillOutcomeUnknown                 = errors.New("skill transaction outcome is unknown")
)

const selectCharacterSkills = "SELECT `SKILL_ID`,`ENABLED` FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? ORDER BY `SKILL_ID`"

// CharacterSkill preserves the original vSRO _CharSkill row contract. Skill
// upgrades are represented by replacing one SkillID with its successor; there
// is deliberately no inferred level column.
type CharacterSkill struct {
	ID      uint32
	Enabled byte
}

type CharacterSkillRepository interface {
	LoadSkills(context.Context, int) ([]CharacterSkill, error)
}

type SkillLearnCommand struct {
	Skill                 RefSkill
	ReplacedSkillID       uint32
	RequiredOwnedSkillIDs [3]uint32
}

type SkillLearnResult struct {
	Skill           CharacterSkill
	ReplacedSkillID uint32
	SkillPoints     uint32
}

type SkillLearningRepository interface {
	LearnSkill(context.Context, int, SkillLearnCommand) (SkillLearnResult, error)
}

type SQLCharacterSkillRepository struct{ shard *sql.DB }

func NewCharacterSkillRepository(shard *sql.DB) (*SQLCharacterSkillRepository, error) {
	if shard == nil {
		return nil, ErrCharacterSkillRepositoryUnavailable
	}
	return &SQLCharacterSkillRepository{shard: shard}, nil
}

func (r *SQLCharacterSkillRepository) LoadSkills(ctx context.Context, characterID int) ([]CharacterSkill, error) {
	if r == nil || r.shard == nil {
		return nil, ErrCharacterSkillRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrCharacterSkillCharacterIDRequired, characterID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.shard.QueryContext(ctx, selectCharacterSkills, characterID)
	if err != nil {
		return nil, fmt.Errorf("load character skills: %w", err)
	}
	defer rows.Close()

	skills := make([]CharacterSkill, 0)
	for rows.Next() {
		var skill CharacterSkill
		if err := rows.Scan(&skill.ID, &skill.Enabled); err != nil {
			return nil, fmt.Errorf("scan character skill: %w", err)
		}
		if err := validateCharacterSkill(skill); err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read character skills: %w", err)
	}
	return skills, nil
}

func validateCharacterSkill(skill CharacterSkill) error {
	if skill.ID == 0 {
		return ErrCharacterSkillIDRequired
	}
	if skill.Enabled > 1 {
		return fmt.Errorf("%w: %d", ErrCharacterSkillEnabledInvalid, skill.Enabled)
	}
	return nil
}

func (r *SQLCharacterSkillRepository) LearnSkill(ctx context.Context, characterID int, command SkillLearnCommand) (result SkillLearnResult, resultErr error) {
	if r == nil || r.shard == nil {
		return result, ErrCharacterSkillRepositoryUnavailable
	}
	if characterID <= 0 || command.Skill.ID == 0 || command.Skill.BasicLevel == 0 {
		return result, ErrSkillConflict
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin skill transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, rollbackErr)
		}
	}()

	var strength, intellect, country int
	var skillPoints uint64
	if err := tx.QueryRowContext(ctx, "SELECT c.`STRENGTH`,c.`INTELLECT`,c.`SKILL_POINTS`,r.`Country` FROM `SRO_SHARD`.`CHAR` c JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` WHERE c.`ID`=? FOR UPDATE", characterID).Scan(&strength, &intellect, &skillPoints, &country); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrSkillConflict
		}
		return result, fmt.Errorf("lock skill character: %w", err)
	}
	if command.Skill.RequiredRace != 3 && int(command.Skill.RequiredRace) != country {
		return result, ErrSkillRaceMismatch
	}
	if strength < command.Skill.RequiredStr {
		return result, ErrSkillStrengthRequired
	}
	if intellect < command.Skill.RequiredInt {
		return result, ErrSkillIntellectRequired
	}
	if skillPoints < uint64(command.Skill.RequiredSP) {
		return result, ErrSkillInsufficientSP
	}

	masteries := make(map[uint32]byte)
	rows, err := tx.QueryContext(ctx, "SELECT `MASTERY_ID`,`LEVEL` FROM `SRO_SHARD`.`CHAR_MASTERY` WHERE `CHAR_ID`=? FOR UPDATE", characterID)
	if err != nil {
		return result, fmt.Errorf("lock skill masteries: %w", err)
	}
	for rows.Next() {
		var id uint32
		var level byte
		if err := rows.Scan(&id, &level); err != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan skill mastery: %w", err)
		}
		masteries[id] = level
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, fmt.Errorf("read skill masteries: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("close skill masteries: %w", err)
	}
	if !masteryRequirementMet(masteries, command.Skill.Mastery1, command.Skill.MasteryLevel1) || !masteryRequirementMet(masteries, command.Skill.Mastery2, command.Skill.MasteryLevel2) {
		return result, ErrSkillMasteryRequired
	}

	owned := make(map[uint32]bool)
	rows, err = tx.QueryContext(ctx, "SELECT `SKILL_ID` FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? FOR UPDATE", characterID)
	if err != nil {
		return result, fmt.Errorf("lock character skills: %w", err)
	}
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan locked skill: %w", err)
		}
		owned[id] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, fmt.Errorf("read locked skills: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("close locked skills: %w", err)
	}
	if owned[command.Skill.ID] {
		return result, ErrSkillAlreadyLearned
	}
	if command.ReplacedSkillID != 0 && !owned[command.ReplacedSkillID] {
		return result, ErrSkillLevelChainInvalid
	}
	for _, requirement := range command.RequiredOwnedSkillIDs {
		if requirement != 0 && !owned[requirement] && requirement != command.ReplacedSkillID {
			return result, ErrSkillPrerequisiteRequired
		}
	}

	if command.ReplacedSkillID != 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? AND `SKILL_ID`=?", characterID, command.ReplacedSkillID); err != nil {
			return result, fmt.Errorf("replace previous skill: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO `SRO_SHARD`.`CHAR_SKILL` (`CHAR_ID`,`SKILL_ID`,`ENABLED`) VALUES (?,?,1)", characterID, command.Skill.ID); err != nil {
		return result, fmt.Errorf("save learned skill: %w", err)
	}
	skillPoints -= uint64(command.Skill.RequiredSP)
	if _, err := tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `SKILL_POINTS`=? WHERE `ID`=?", skillPoints, characterID); err != nil {
		return result, fmt.Errorf("debit skill points: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, errors.Join(ErrSkillOutcomeUnknown, err)
	}
	committed = true
	if skillPoints > uint64(^uint32(0)) {
		return result, ErrSkillOutcomeUnknown
	}
	return SkillLearnResult{Skill: CharacterSkill{ID: command.Skill.ID, Enabled: 1}, ReplacedSkillID: command.ReplacedSkillID, SkillPoints: uint32(skillPoints)}, nil
}

func masteryRequirementMet(masteries map[uint32]byte, id uint32, level byte) bool {
	return id == 0 || masteries[id] >= level
}
