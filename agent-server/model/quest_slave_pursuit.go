package model

import (
	"context"
	"errors"
)

const SlavePursuitQuestID = 162
const SlavePursuitHandler uint32 = 0x008A5000
const SlavePursuitMobCodeName = "MOB_QT_01_WINGTRIBE"
const SlavePursuitInterval byte = 60
const SlavePursuitSpawnRadius = float32(20)

var ErrSlavePursuitState = errors.New("invalid QNO_RM_SLAVE2_2 pursuit state")

type SlavePursuitTransition struct {
	Data1  uint64
	Status QuestStatus
	Spawn  bool
}

// AdvanceSlavePursuit reproduces SR_GameServer 0x008A5000. A newly active
// quest is promoted to progressed, initialized to sixty, and decremented in
// the same player update. At zero, an eligible non-job player spawns one pursuer,
// resets to sixty, and again decrements to fifty-nine.
func AdvanceSlavePursuit(data1 uint64, status QuestStatus, spawnEligible bool) (SlavePursuitTransition, error) {
	if status != QuestActive && status != QuestProgressed && status != QuestRewardReady {
		return SlavePursuitTransition{Data1: data1, Status: status}, ErrSlavePursuitState
	}
	transition := SlavePursuitTransition{Data1: data1, Status: status}
	countdown := byte(data1 >> 32)
	if status == QuestActive {
		transition.Status = QuestProgressed
		countdown = SlavePursuitInterval
	}
	if countdown == 0 && spawnEligible {
		transition.Spawn = true
		countdown = SlavePursuitInterval
	}
	if countdown != 0 {
		countdown--
	}
	transition.Data1 = data1&^(uint64(0xff)<<32) | uint64(countdown)<<32
	return transition, nil
}

type SlavePursuitTickResult struct {
	Quest CharacterQuest
	Spawn bool
}

func (r *SQLQuestRuntimeRepository) TickSlavePursuit(ctx context.Context, characterID int, spawnEligible bool) (SlavePursuitTickResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return SlavePursuitTickResult{}, ErrSlavePursuitState
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return SlavePursuitTickResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	quest, err := lockCharacterQuest(ctx, tx, characterID, SlavePursuitQuestID)
	if err != nil || !quest.Status.ClientActive() {
		return SlavePursuitTickResult{}, ErrSlavePursuitState
	}
	transition, err := AdvanceSlavePursuit(quest.Data1, quest.Status, spawnEligible)
	if err != nil {
		return SlavePursuitTickResult{}, err
	}
	// Native 0x008A5000 resolves and creates the pursuer before resetting the
	// byte. Keep zero durable until world admission succeeds so failure retries
	// on the next periodic player update.
	if transition.Spawn {
		if err = tx.Commit(); err != nil {
			return SlavePursuitTickResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
		}
		committed = true
		return SlavePursuitTickResult{Quest: quest, Spawn: true}, nil
	}
	previousData, previousStatus := quest.Data1, quest.Status
	quest.Data1, quest.Status = transition.Data1, transition.Status
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, quest.Status, quest.Data1,
		characterID, quest.QuestID, previousStatus, previousData)
	if err != nil {
		return SlavePursuitTickResult{}, err
	}
	if err = requireQuestRow(updated, "tick QNO_RM_SLAVE2_2 pursuit"); err != nil {
		return SlavePursuitTickResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return SlavePursuitTickResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return SlavePursuitTickResult{Quest: quest, Spawn: transition.Spawn}, nil
}

func (r *SQLQuestRuntimeRepository) ConfirmSlavePursuitSpawn(ctx context.Context, characterID int) (CharacterQuest, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return CharacterQuest{}, ErrSlavePursuitState
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return CharacterQuest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	quest, err := lockCharacterQuest(ctx, tx, characterID, SlavePursuitQuestID)
	if err != nil || !quest.Status.ClientActive() || byte(quest.Data1>>32) != 0 {
		return CharacterQuest{}, ErrSlavePursuitState
	}
	previousData := quest.Data1
	quest.Data1 = quest.Data1&^(uint64(0xff)<<32) | uint64(SlavePursuitInterval-1)<<32
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET QUEST_DATA_1=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, quest.Data1,
		characterID, quest.QuestID, quest.Status, previousData)
	if err != nil {
		return CharacterQuest{}, err
	}
	if err = requireQuestRow(updated, "confirm QNO_RM_SLAVE2_2 pursuer spawn"); err != nil {
		return CharacterQuest{}, err
	}
	if err = tx.Commit(); err != nil {
		return CharacterQuest{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return quest, nil
}
