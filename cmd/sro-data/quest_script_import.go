package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Harvi8/AegisCore/agent-server/questscript"
	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errQuestScriptImportUsage = errors.New("usage: sro-data import-vsro-quest-script --script <Quest.sct>")

func importVSROQuestScript(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-vsro-quest-script", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scriptPath := flags.String("script", "", "Lua 5.1 quest chunk")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *scriptPath == "" {
		return errQuestScriptImportUsage
	}
	contents, err := os.ReadFile(*scriptPath)
	if err != nil {
		return err
	}
	chunk, err := questscript.Decode(bytes.NewReader(contents))
	if err != nil {
		return err
	}
	definitions, err := questscript.Definitions(chunk)
	if err != nil {
		return err
	}
	killObjectives, err := questscript.KillMonsterObjectives(definitions)
	if err != nil {
		return err
	}
	deliverObjectives, err := questscript.DeliverItemEXObjectives(definitions)
	if err != nil {
		return err
	}
	standardDeliverObjectives, err := questscript.DeliverItemObjectives(definitions)
	if err != nil {
		return err
	}
	dialogObjectives, err := questscript.DialogObjectives(definitions)
	if err != nil {
		return err
	}
	killPlayerObjectives, err := questscript.KillPlayerObjectives(definitions)
	if err != nil {
		return err
	}
	collectionObjectives, err := questscript.CollectionItemObjectives(definitions)
	if err != nil {
		return err
	}
	extendedKillObjectives, err := questscript.ExtendedKillMonsterObjectives(definitions)
	if err != nil {
		return err
	}
	talismanObjectives, err := questscript.TalismanObjectives(definitions)
	if err != nil {
		return err
	}
	rewards, err := questscript.RewardDeclarations(definitions)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(contents)
	scriptHash := hex.EncodeToString(digest[:])
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_SCRIPT_REWARD_ITEM`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_SCRIPT_REWARD`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_DELIVER_ITEM_ENTRY`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_DELIVER_ITEM`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_DIALOG_OBJECTIVE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_KILL_PLAYER_JOB_STATE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_KILL_PLAYER_OBJECTIVE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_COLLECTION_ITEM_SOURCE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_COLLECTION_ITEM_OBJECTIVE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_EXTENDED_KILL_TARGET`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_EXTENDED_KILL_OBJECTIVE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_TALISMAN_OBJECTIVE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_DELIVER_ITEM_EX`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_KILL_OBJECTIVE`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_SCRIPT_DECLARATION`"); err != nil {
		return err
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_SCRIPT_DECLARATION
		(QUEST_ID,VARIANT_INDEX,CODE_NAME,SOURCE_NAME,SCRIPT_SHA256,DECLARATIONS)
		SELECT ID,?,CODE_NAME,?,?,CAST(? AS JSON) FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, definition := range definitions {
		declarations, marshalErr := json.Marshal(definition.Declarations)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := statement.ExecContext(ctx, definition.Variant, definition.Source, scriptHash, declarations, definition.CodeName)
		if execErr != nil {
			return fmt.Errorf("import quest %s: %w", definition.CodeName, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("quest %s does not resolve exactly once in REF_QUEST", definition.CodeName)
		}
	}
	killStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_KILL_OBJECTIVE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,OBJECTIVE_KEY,PARAMETER_4,TARGET_REF_OBJECT_ID,TARGET_CODE_NAME,
		 PARAMETER_6,TARGET_CLASS,REQUIRED_COUNT,PARAMETER_9,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT quest.ID,?,?,?,?,target.ID,target.CODE_NAME,?,?,?,?,?,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO target ON target.CODE_NAME=?
		WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer killStatement.Close()
	for _, objective := range killObjectives {
		enabled := objective.QuestCodeName == "QNO_CH_SMITH_2" && objective.Variant == 0 && objective.MissionIndex == 0
		result, execErr := killStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex, objective.ObjectiveKey,
			objective.Parameter4, objective.Parameter6, objective.TargetClass, objective.RequiredCount, objective.Parameter9,
			enabled, scriptHash, objective.TargetCode, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import kill objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("kill objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
	}
	deliverStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_DELIVER_ITEM_EX
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,OBJECTIVE_KEY,TARGET_NPC_REF_OBJECT_ID,TARGET_NPC_CODE_NAME,
		 ITEM_REF_OBJECT_ID,ITEM_CODE_NAME,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT quest.ID,?,?,?,npc.ID,npc.CODE_NAME,item.ID,item.CODE_NAME,CAST(? AS JSON),?,?
		FROM REF_QUEST quest
		JOIN REF_OBJ_COMMON_VSRO npc ON npc.CODE_NAME=?
		JOIN REF_OBJ_COMMON_VSRO item ON item.CODE_NAME=?
		WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer deliverStatement.Close()
	for _, objective := range deliverObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		enabled := objective.QuestCodeName == "QNO_CH_SMITH_1" && objective.Variant == 0 && objective.MissionIndex == 0
		result, execErr := deliverStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex, objective.ObjectiveKey,
			parameters, enabled, scriptHash, objective.TargetNPCCode, objective.ItemCode, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import deliver objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("deliver objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
	}
	standardDeliverStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_DELIVER_ITEM
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,OBJECTIVE_KEY,TARGET_NPC_REF_OBJECT_ID,TARGET_NPC_CODE_NAME,
		 ITEM_COUNT,TRAILING_PARAMETER,DIALOGUE_KEY,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT quest.ID,?,?,?,npc.ID,npc.CODE_NAME,?,?,?,CAST(? AS JSON),0,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO npc ON npc.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer standardDeliverStatement.Close()
	standardDeliverItemStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_DELIVER_ITEM_ENTRY
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,ITEM_INDEX,ITEM_REF_OBJECT_ID,ITEM_CODE_NAME,QUANTITY)
		SELECT quest.ID,?,?,?,item.ID,item.CODE_NAME,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO item ON item.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer standardDeliverItemStatement.Close()
	for _, objective := range standardDeliverObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := standardDeliverStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.ObjectiveKey, len(objective.Items), objective.TrailingParameter, objective.DialogueKey,
			parameters, scriptHash, objective.TargetNPCCode, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import deliver objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("deliver objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
		for index, item := range objective.Items {
			result, execErr = standardDeliverItemStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
				index, item.Quantity, item.ItemCode, objective.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import deliver item %s/%d/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("deliver item %s/%d/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index)
			}
		}
	}
	dialogStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_DIALOG_OBJECTIVE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,OBJECTIVE_KEY,PARAMETER_4,DIALOGUE_KEY,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer dialogStatement.Close()
	for _, objective := range dialogObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := dialogStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.ObjectiveKey, objective.Parameter4, objective.DialogueKey, parameters, scriptHash, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import dialog objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("dialog objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
	}
	killPlayerStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_KILL_PLAYER_OBJECTIVE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,OBJECTIVE_KEY,PARAMETER_4,TARGET_NPC_REF_OBJECT_ID,TARGET_NPC_CODE_NAME,
		 PARAMETER_6,FILTER_COUNT,LEVEL_FILTER_KEY,JOB_FILTER_KEY,REGION_FILTER_KEY,REGION_VALUE,PARAMETER_12,
		 JOB_STATE_COUNT,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT quest.ID,?,?,?,?,npc.ID,npc.CODE_NAME,?,?,?,?,?,?,?,?,CAST(? AS JSON),0,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO npc ON npc.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer killPlayerStatement.Close()
	killPlayerStateStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_KILL_PLAYER_JOB_STATE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,JOB_STATE_INDEX,JOB_STATE_KEY)
		SELECT ID,?,?,?,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer killPlayerStateStatement.Close()
	for _, objective := range killPlayerObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := killPlayerStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.ObjectiveKey, objective.Parameter4, objective.Parameter6, objective.FilterCount,
			objective.LevelFilterKey, objective.JobFilterKey, objective.RegionFilterKey, objective.RegionValue,
			objective.Parameter12, len(objective.JobStates), parameters, scriptHash, objective.TargetNPCCode, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import kill-player objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("kill-player objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
		for index, jobState := range objective.JobStates {
			result, execErr = killPlayerStateStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex, index, jobState, objective.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import kill-player job state %s/%d/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("kill-player job state %s/%d/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index)
			}
		}
	}
	collectionStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_COLLECTION_ITEM_OBJECTIVE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,DECLARATION_KIND,OBJECTIVE_KEY,TARGET_NPC_REF_OBJECT_ID,TARGET_NPC_CODE_NAME,
		 SOURCE_COUNT,PARAMETER_6,REQUIRED_COUNT,ITEM_REF_OBJECT_ID,ITEM_CODE_NAME,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT quest.ID,?,?,?,?,npc.ID,npc.CODE_NAME,?,?,?,item.ID,item.CODE_NAME,CAST(? AS JSON),0,?
		FROM REF_QUEST quest
		JOIN REF_OBJ_COMMON_VSRO npc ON npc.CODE_NAME=?
		JOIN REF_OBJ_COMMON_VSRO item ON item.CODE_NAME=?
		WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer collectionStatement.Close()
	collectionSourceStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_COLLECTION_ITEM_SOURCE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,SOURCE_INDEX,MONSTER_REF_OBJECT_ID,MONSTER_CODE_NAME,SOURCE_PARAMETER)
		SELECT quest.ID,?,?,?,monster.ID,monster.CODE_NAME,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO monster ON monster.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer collectionSourceStatement.Close()
	for _, objective := range collectionObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := collectionStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.Kind, objective.ObjectiveKey, len(objective.Sources), objective.Parameter6, objective.RequiredCount,
			parameters, scriptHash, objective.TargetNPCCode, objective.ItemCode, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import collection-item objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("collection-item objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
		for index, source := range objective.Sources {
			result, execErr = collectionSourceStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
				index, source.Parameter, source.MonsterCode, objective.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import collection-item source %s/%d/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("collection-item source %s/%d/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index)
			}
		}
	}
	extendedKillStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_EXTENDED_KILL_OBJECTIVE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,DECLARATION_KIND,OBJECTIVE_KEY,TARGET_COUNT,TARGET_CLASS,
		 PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer extendedKillStatement.Close()
	extendedKillTargetStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_EXTENDED_KILL_TARGET
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,TARGET_INDEX,TARGET_REF_OBJECT_ID,TARGET_CODE_NAME)
		SELECT quest.ID,?,?,?,target.ID,target.CODE_NAME
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO target ON target.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer extendedKillTargetStatement.Close()
	for _, objective := range extendedKillObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := extendedKillStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.Kind, objective.ObjectiveKey, len(objective.Targets), objective.TargetClass, parameters,
			scriptHash, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import extended kill objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("extended kill objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
		for index, target := range objective.Targets {
			result, execErr = extendedKillTargetStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex, index, target, objective.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import extended kill target %s/%d/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("extended kill target %s/%d/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex, index)
			}
		}
	}
	talismanStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_TALISMAN_OBJECTIVE
		(QUEST_ID,VARIANT_INDEX,MISSION_INDEX,OBJECTIVE_KEY,TARGET_NPC_REF_OBJECT_ID,TARGET_NPC_CODE_NAME,
		 THEME_CODE,PARAMETER_6,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT quest.ID,?,?,?,npc.ID,npc.CODE_NAME,?,?,CAST(? AS JSON),0,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO npc ON npc.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer talismanStatement.Close()
	for _, objective := range talismanObjectives {
		parameters, marshalErr := json.Marshal(objective.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := talismanStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.ObjectiveKey, objective.ThemeCode, objective.Parameter6, parameters, scriptHash,
			objective.TargetNPCCode, objective.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import talisman objective %s/%d/%d: %w", objective.QuestCodeName, objective.Variant, objective.MissionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("talisman objective %s/%d/%d does not resolve exactly once", objective.QuestCodeName, objective.Variant, objective.MissionIndex)
		}
	}
	rewardStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_SCRIPT_REWARD
		(QUEST_ID,VARIANT_INDEX,EXPERIENCE,SKILL_EXPERIENCE,GOLD,ITEM_COUNT,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,CAST(? AS UNSIGNED),CAST(? AS JSON),?,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer rewardStatement.Close()
	rewardItemStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_SCRIPT_REWARD_ITEM
		(QUEST_ID,VARIANT_INDEX,ITEM_INDEX,REWARD_TYPE,ITEM_REF_OBJECT_ID,ITEM_CODE_NAME,QUANTITY)
		SELECT quest.ID,?,?,?,item.ID,item.CODE_NAME,?
		FROM REF_QUEST quest JOIN REF_OBJ_COMMON_VSRO item ON item.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer rewardItemStatement.Close()
	for _, reward := range rewards {
		parameters, marshalErr := json.Marshal(reward.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		enabled := (reward.QuestCodeName == "QNO_CH_SMITH_1" || reward.QuestCodeName == "QNO_CH_SMITH_2") && reward.Variant == 0
		result, execErr := rewardStatement.ExecContext(ctx, reward.Variant, reward.Experience, reward.SkillExperience,
			reward.Gold, len(reward.Items), parameters, enabled, scriptHash, reward.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import reward %s/%d: %w", reward.QuestCodeName, reward.Variant, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("reward %s/%d does not resolve exactly once", reward.QuestCodeName, reward.Variant)
		}
		for index, item := range reward.Items {
			result, execErr = rewardItemStatement.ExecContext(ctx, reward.Variant, index, item.RewardType, item.Quantity, item.ItemCode, reward.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import reward item %s/%d/%d: %w", reward.QuestCodeName, reward.Variant, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("reward item %s/%d/%d does not resolve exactly once", reward.QuestCodeName, reward.Variant, index)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d declarations, %d kill objectives, %d deliver-item EX objectives, %d standard deliver-item objectives, %d dialog objectives, %d kill-player objectives, %d collection-item objectives, %d extended kill objectives, %d talisman objectives, and %d rewards (sha256 %s)\n", len(definitions), len(killObjectives), len(deliverObjectives), len(standardDeliverObjectives), len(dialogObjectives), len(killPlayerObjectives), len(collectionObjectives), len(extendedKillObjectives), len(talismanObjectives), len(rewards), scriptHash)
	return err
}
