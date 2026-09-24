package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
)

const (
	JobTrader byte = 1
	JobThief  byte = 2
	JobHunter byte = 3
	JobNone   byte = 4
)

type JobSnapshot struct {
	CharacterID          int
	Type                 byte
	Alias                string
	Level                byte
	Experience           uint64
	Contribution         uint64
	WeeklyContribution   uint64
	PendingRewardGold    uint64
	EnrollmentFee        uint32
	LeaveAvailableAt     sql.NullTime
	PreviousType         byte
	PreviousExperience   uint64
	PreviousContribution uint64
}

type JobRepository interface {
	Job(context.Context, int) (JobSnapshot, error)
	JobPreviousInfo(context.Context, int) (JobSnapshot, error)
	JoinJob(context.Context, int, byte, uint32) (JobSnapshot, uint64, error)
	LeaveJob(context.Context, int, uint32) (uint64, error)
	AdvanceJob(context.Context, int, uint64, uint64) (JobSnapshot, error)
	JobAliasAvailable(context.Context, string) (bool, error)
	SetJobAlias(context.Context, int, string) error
	JobRanking(context.Context, byte, bool, int) ([]JobSnapshot, error)
	ClaimJobReward(context.Context, int) (uint64, uint64, error)
	DistributeWeeklyJobRewards(context.Context) error
}

const jobSnapshotColumns = "FK_CHAR,JOB_TYPE,COALESCE(ALIAS,''),JOB_LEVEL,JOB_EXP,CONTRIBUTION,WEEKLY_CONTRIBUTION,PENDING_REWARD_GOLD,ENROLLMENT_FEE,LEAVE_AVAILABLE_AT,PREVIOUS_JOB_TYPE,PREVIOUS_JOB_EXP,PREVIOUS_CONTRIBUTION"

func scanJob(row interface{ Scan(...any) error }, job *JobSnapshot) error {
	return row.Scan(&job.CharacterID, &job.Type, &job.Alias, &job.Level, &job.Experience, &job.Contribution, &job.WeeklyContribution, &job.PendingRewardGold, &job.EnrollmentFee, &job.LeaveAvailableAt, &job.PreviousType, &job.PreviousExperience, &job.PreviousContribution)
}

func (r *SQLSocialRepository) Job(ctx context.Context, characterID int) (JobSnapshot, error) {
	var job JobSnapshot
	err := scanJob(r.shard.QueryRowContext(socialContext(ctx), "SELECT "+jobSnapshotColumns+" FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID), &job)
	if errors.Is(err, sql.ErrNoRows) {
		return JobSnapshot{}, ErrSocialNotFound
	}
	return job, err
}

// JobPreviousInfo returns the active job, when present, together with the
// latest archived job. Unlike Job, it also succeeds for a character that has
// withdrawn and therefore has no CHARACTER_JOB row.
func (r *SQLSocialRepository) JobPreviousInfo(ctx context.Context, characterID int) (JobSnapshot, error) {
	job, err := r.Job(ctx, characterID)
	if err != nil && !errors.Is(err, ErrSocialNotFound) {
		return JobSnapshot{}, err
	}
	if errors.Is(err, ErrSocialNotFound) {
		job = JobSnapshot{CharacterID: characterID, Type: JobNone}
	}
	err = r.shard.QueryRowContext(socialContext(ctx), "SELECT JOB_TYPE,JOB_EXP,CONTRIBUTION FROM CHARACTER_JOB_HISTORY WHERE FK_CHAR=? ORDER BY LEFT_AT DESC,ID DESC LIMIT 1", characterID).
		Scan(&job.PreviousType, &job.PreviousExperience, &job.PreviousContribution)
	if errors.Is(err, sql.ErrNoRows) {
		return job, nil
	}
	return job, err
}

func jobEnrollmentFee(level int) (uint32, bool) {
	if level < 20 {
		return 0, false
	}
	fee := uint64(level-20) * 5000
	if fee > math.MaxUint32 {
		return 0, false
	}
	return uint32(fee), true
}

func jobLeaveFee(level int) (uint32, bool) {
	if level <= 0 {
		return 0, false
	}
	fee := uint64(level) * 50000
	if fee > math.MaxUint32 {
		return 0, false
	}
	return uint32(fee), true
}

func (r *SQLSocialRepository) JoinJob(ctx context.Context, characterID int, jobType byte, npcUID uint32) (JobSnapshot, uint64, error) {
	if jobType < JobTrader || jobType > JobHunter {
		return JobSnapshot{}, 0, ErrSocialInvalidRequest
	}
	// vSRO 1.188 sends the selected NPC UID in the first 0x70E1 field.
	// Enrollment cost is authoritative server state derived from character level.
	_ = npcUID
	var snapshot JobSnapshot
	var gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var level int
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL,GOLD FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level, &gold); err != nil {
			return ErrSocialNotFound
		}
		cost, eligible := jobEnrollmentFee(level)
		if !eligible {
			return ErrSocialInsufficientLevel
		}
		if gold < uint64(cost) {
			return ErrSocialInsufficientGold
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO CHARACTER_JOB (FK_CHAR,JOB_TYPE,ENROLLMENT_FEE) VALUES (?,?,?)", characterID, jobType, cost); err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		if jobUsesSafeTradeState(jobType) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO CHARACTER_SAFE_TRADE (FK_CHAR,ABLE_COUNT,STATUS,PROGRESS) VALUES (?,0,0,0)", characterID); err != nil {
				return errors.Join(ErrSocialConflict, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, characterID); err != nil {
			return err
		}
		gold -= uint64(cost)
		return scanJob(tx.QueryRowContext(ctx, "SELECT "+jobSnapshotColumns+" FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID), &snapshot)
	})
	return snapshot, gold, err
}

func jobUsesSafeTradeState(jobType byte) bool {
	return jobType == JobTrader
}

func (r *SQLSocialRepository) LeaveJob(ctx context.Context, characterID int, requestedFee uint32) (uint64, error) {
	var remainingGold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var level int
		var gold uint64
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL,GOLD FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level, &gold); err != nil {
			return ErrSocialNotFound
		}
		cost, valid := jobLeaveFee(level)
		if !valid || requestedFee != cost {
			return ErrSocialInvalidRequest
		}
		if gold < uint64(cost) {
			return ErrSocialInsufficientGold
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO CHARACTER_JOB_HISTORY (FK_CHAR,JOB_TYPE,ALIAS,JOB_LEVEL,JOB_EXP,CONTRIBUTION,JOINED_AT) SELECT FK_CHAR,JOB_TYPE,ALIAS,JOB_LEVEL,JOB_EXP,CONTRIBUTION,JOINED_AT FROM CHARACTER_JOB WHERE FK_CHAR=? AND (LEAVE_AVAILABLE_AT IS NULL OR LEAVE_AVAILABLE_AT<=CURRENT_TIMESTAMP(6))", characterID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM CHARACTER_SAFE_TRADE WHERE FK_CHAR=?", characterID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, characterID); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, "DELETE FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		remainingGold = gold - uint64(cost)
		return nil
	})
	return remainingGold, err
}

func (r *SQLSocialRepository) AdvanceJob(ctx context.Context, characterID int, experience, contribution uint64) (JobSnapshot, error) {
	var snapshot JobSnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		snapshot, err = advanceJobInTransaction(ctx, tx, characterID, experience, contribution)
		return err
	})
	return snapshot, err
}

func advanceJobInTransaction(ctx context.Context, tx *sql.Tx, characterID int, experience, contribution uint64) (JobSnapshot, error) {
	var snapshot JobSnapshot
	var jobType, level byte
	var currentExperience uint64
	if err := tx.QueryRowContext(ctx, "SELECT JOB_TYPE,JOB_LEVEL,JOB_EXP FROM CHARACTER_JOB WHERE FK_CHAR=? FOR UPDATE", characterID).Scan(&jobType, &level, &currentExperience); err != nil {
		return snapshot, ErrSocialNotFound
	}
	if jobType < JobTrader || jobType > JobHunter || level == 0 || level > 7 {
		return snapshot, ErrSocialConflict
	}
	currentExperience = saturatingAdd(currentExperience, experience)
	for level < 7 {
		var required uint64
		err := tx.QueryRowContext(ctx, "SELECT CASE ? WHEN 1 THEN `REQUIRED_JOB_EXPERIENCE` WHEN 2 THEN `REQUIRED_JOB_EXPERIENCE_2` WHEN 3 THEN `REQUIRED_JOB_EXPERIENCE_3` END FROM `REF_LEVEL_DATA` WHERE `LEVEL`=?", jobType, level).Scan(&required)
		if errors.Is(err, sql.ErrNoRows) {
			return snapshot, ErrSocialRepositoryUnavailable
		}
		if err != nil || required == 0 || currentExperience < required {
			if err != nil {
				return snapshot, err
			}
			break
		}
		currentExperience -= required
		level++
	}
	updated, err := tx.ExecContext(ctx, "UPDATE CHARACTER_JOB SET JOB_LEVEL=?,JOB_EXP=?,CONTRIBUTION=LEAST(CONTRIBUTION+?,18446744073709551615),WEEKLY_CONTRIBUTION=LEAST(WEEKLY_CONTRIBUTION+?,18446744073709551615) WHERE FK_CHAR=?", level, currentExperience, contribution, contribution, characterID)
	if err != nil {
		return snapshot, err
	}
	if err = requireSocialRow(updated); err != nil {
		return snapshot, err
	}
	return snapshot, scanJob(tx.QueryRowContext(ctx, "SELECT "+jobSnapshotColumns+" FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID), &snapshot)
}

func CombatJobExperience(levels *LevelTable, characterLevel, targetLevel int, jobType byte) uint64 {
	if levels == nil || jobType < JobTrader || jobType > JobHunter {
		return 0
	}
	character, characterOK := levels.Level(characterLevel)
	target, targetOK := levels.Level(targetLevel)
	if !characterOK || !targetOK || character.JobActivityFactor == 0 || target.JobActivityFactor == 0 {
		return 0
	}
	characterFactor := int(float64(character.JobActivityFactor*10) * 0.125)
	targetFactor := int(float64(target.JobActivityFactor*10) * 0.125)
	if characterFactor <= 0 || targetFactor <= 0 {
		return 0
	}
	ratio := float64(targetFactor) / float64(characterFactor)
	ratio = max(0.5, min(ratio, 1.5))
	reward := float64(targetFactor) * ratio
	if jobType == JobTrader {
		reward *= 0.5
	}
	if reward <= 0 {
		return 1
	}
	return uint64(reward)
}

func PartyCombatJobExperience(base uint64, recipientLevel, totalLevel, recipients int) uint64 {
	if base == 0 || recipientLevel <= 0 || totalLevel <= 0 || recipients <= 0 {
		return 0
	}
	bonus := float64(recipients-1)*0.05000000074505806 + 1.0
	return uint64(float64(base) * (float64(recipientLevel) / float64(totalLevel) * bonus))
}

type weeklyJobRewardMember struct {
	characterID  int
	contribution uint64
	pending      uint64
}

func weeklyJobRewardAllocations(pool uint64, members []weeklyJobRewardMember) map[int]uint64 {
	allocations := make(map[int]uint64, len(members))
	var total uint64
	for _, member := range members {
		total = saturatingAdd(total, member.contribution)
	}
	for _, member := range members {
		share := uint64(0)
		if pool != 0 && total != 0 && member.contribution != 0 {
			product := new(big.Int).Mul(new(big.Int).SetUint64(pool), new(big.Int).SetUint64(member.contribution))
			share = new(big.Int).Quo(product, new(big.Int).SetUint64(total)).Uint64()
		}
		pending := saturatingAdd(member.pending, share)
		allocations[member.characterID] = min(pending, uint64(math.MaxInt32))
	}
	return allocations
}

func (r *SQLSocialRepository) DistributeWeeklyJobRewards(ctx context.Context) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, jobType := range []byte{JobThief, JobHunter} {
			var pool uint64
			if err := tx.QueryRowContext(ctx, "SELECT `REWARD` FROM `TRIJOB_REWARDS` WHERE `JOB_TYPE`=? FOR UPDATE", jobType).Scan(&pool); err != nil {
				return err
			}
			rows, err := tx.QueryContext(ctx, "SELECT `FK_CHAR`,`WEEKLY_CONTRIBUTION`,`PENDING_REWARD_GOLD` FROM `CHARACTER_JOB` WHERE `JOB_TYPE`=? ORDER BY `FK_CHAR` FOR UPDATE", jobType)
			if err != nil {
				return err
			}
			members := make([]weeklyJobRewardMember, 0)
			for rows.Next() {
				var member weeklyJobRewardMember
				if err = rows.Scan(&member.characterID, &member.contribution, &member.pending); err != nil {
					rows.Close()
					return err
				}
				members = append(members, member)
			}
			if err = rows.Close(); err != nil {
				return err
			}
			var totalContribution uint64
			for _, member := range members {
				totalContribution = saturatingAdd(totalContribution, member.contribution)
			}
			if pool != 0 && totalContribution == 0 {
				return ErrSocialConflict
			}
			for characterID, pending := range weeklyJobRewardAllocations(pool, members) {
				if _, err = tx.ExecContext(ctx, "UPDATE `CHARACTER_JOB` SET `PENDING_REWARD_GOLD`=? WHERE `FK_CHAR`=?", pending, characterID); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, "UPDATE `TRIJOB_REWARDS` SET `REWARD`=0 WHERE `JOB_TYPE`=?", jobType); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE `CHARACTER_JOB` SET `WEEKLY_CONTRIBUTION`=0")
		return err
	})
}

func (r *SQLSocialRepository) JobAliasAvailable(ctx context.Context, alias string) (bool, error) {
	if err := validateSocialText("job alias", alias, 20, false); err != nil {
		return false, err
	}
	var count int
	err := r.shard.QueryRowContext(socialContext(ctx), "SELECT COUNT(*) FROM CHARACTER_JOB WHERE LOWER(ALIAS)=LOWER(?)", alias).Scan(&count)
	return count == 0, err
}

func (r *SQLSocialRepository) SetJobAlias(ctx context.Context, characterID int, alias string) error {
	if err := validateSocialText("job alias", alias, 20, false); err != nil {
		return err
	}
	result, err := r.shard.ExecContext(socialContext(ctx), "UPDATE CHARACTER_JOB SET ALIAS=? WHERE FK_CHAR=?", alias, characterID)
	if err != nil {
		return errors.Join(ErrSocialConflict, err)
	}
	return requireSocialRow(result)
}

func (r *SQLSocialRepository) JobRanking(ctx context.Context, jobType byte, weekly bool, limit int) ([]JobSnapshot, error) {
	if jobType < JobTrader || jobType > JobHunter {
		return nil, ErrSocialInvalidRequest
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	column := "CONTRIBUTION"
	if weekly {
		column = "WEEKLY_CONTRIBUTION"
	}
	query := fmt.Sprintf("SELECT %s FROM CHARACTER_JOB WHERE JOB_TYPE=? AND ALIAS IS NOT NULL ORDER BY %s DESC,JOB_EXP DESC,FK_CHAR LIMIT ?", jobSnapshotColumns, column)
	rows, err := r.shard.QueryContext(socialContext(ctx), query, jobType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []JobSnapshot
	for rows.Next() {
		var j JobSnapshot
		if err = scanJob(rows, &j); err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

func (r *SQLSocialRepository) ClaimJobReward(ctx context.Context, characterID int) (uint64, uint64, error) {
	var amount, gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT PENDING_REWARD_GOLD FROM CHARACTER_JOB WHERE FK_CHAR=? FOR UPDATE", characterID).Scan(&amount); err != nil {
			return ErrSocialNotFound
		}
		if amount == 0 {
			return ErrSocialConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD+? WHERE id=?", amount, characterID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE CHARACTER_JOB SET PENDING_REWARD_GOLD=0 WHERE FK_CHAR=?", characterID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=?", characterID).Scan(&gold)
	})
	return amount, gold, err
}
