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
	startConditions, err := questscript.StartConditions(definitions)
	if err != nil {
		return err
	}
	startMethods, err := questscript.StartMethods(definitions)
	if err != nil {
		return err
	}
	dependencies, err := questscript.Dependencies(definitions)
	if err != nil {
		return err
	}
	completionPolicies, err := questscript.CompletionPolicies(definitions)
	if err != nil {
		return err
	}
	jobStartConditions, err := questscript.JobStartConditions(definitions)
	if err != nil {
		return err
	}
	raceStartConditions, err := questscript.RaceStartConditions(definitions)
	if err != nil {
		return err
	}
	pointRewards, err := questscript.PointRewards(definitions)
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
	type objectiveKey struct {
		quest   string
		variant int
	}
	objectiveCounts := make(map[objectiveKey]int)
	for _, objective := range killObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range deliverObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range standardDeliverObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range dialogObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range killPlayerObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range collectionObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range extendedKillObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range talismanObjectives {
		objectiveCounts[objectiveKey{objective.QuestCodeName, objective.Variant}]++
	}
	safeKillQuests := make(map[objectiveKey]bool)
	for _, objective := range killObjectives {
		key := objectiveKey{objective.QuestCodeName, objective.Variant}
		if objective.Variant == 0 && objective.MissionIndex == 0 && objectiveCounts[key] == 1 {
			safeKillQuests[key] = true
		}
	}
	safeDeliveryQuests := make(map[objectiveKey]bool)
	for _, objective := range standardDeliverObjectives {
		key := objectiveKey{objective.QuestCodeName, objective.Variant}
		if objective.Variant == 0 && objective.MissionIndex == 0 && objectiveCounts[key] == 1 {
			safeDeliveryQuests[key] = true
		}
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
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_DEPENDENCY_ENTRY`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_DEPENDENCY_GROUP`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_START_NPC`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_START_METHOD`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_START_CONDITION`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_SCRIPT_POINT_REWARD`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_RACE_START_CONDITION`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_JOB_START_CONDITION`"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_QUEST_COMPLETION_POLICY`"); err != nil {
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
	startConditionStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_START_CONDITION
		(QUEST_ID,VARIANT_INDEX,CONDITION_COUNT,LEVEL_LOWER,LEVEL_UPPER,SYSTEM_REQUIRED,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer startConditionStatement.Close()
	for _, condition := range startConditions {
		parameters, marshalErr := json.Marshal(condition.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := startConditionStatement.ExecContext(ctx, condition.Variant, condition.ConditionCount,
			condition.LevelLower, condition.LevelUpper, condition.SystemRequired, parameters, scriptHash, condition.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import start condition %s/%d: %w", condition.QuestCodeName, condition.Variant, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("start condition %s/%d does not resolve exactly once", condition.QuestCodeName, condition.Variant)
		}
	}
	startMethodStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_START_METHOD
		(QUEST_ID,VARIANT_INDEX,METHOD_KEY,NPC_COUNT,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer startMethodStatement.Close()
	startNPCStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_START_NPC
		(QUEST_ID,VARIANT_INDEX,NPC_INDEX,NPC_REF_OBJECT_ID,NPC_CODE_NAME)
		SELECT quest.ID,?,?,npc.ID,npc.CODE_NAME FROM REF_QUEST quest
		JOIN REF_OBJ_COMMON_VSRO npc ON npc.CODE_NAME=? WHERE quest.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer startNPCStatement.Close()
	for _, method := range startMethods {
		parameters, marshalErr := json.Marshal(method.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := startMethodStatement.ExecContext(ctx, method.Variant, method.MethodKey, len(method.NPCs), parameters, scriptHash, method.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import start method %s/%d: %w", method.QuestCodeName, method.Variant, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("start method %s/%d does not resolve exactly once", method.QuestCodeName, method.Variant)
		}
		for index, npc := range method.NPCs {
			result, execErr = startNPCStatement.ExecContext(ctx, method.Variant, index, npc, method.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import start NPC %s/%d/%d: %w", method.QuestCodeName, method.Variant, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("start NPC %s/%d/%d does not resolve exactly once", method.QuestCodeName, method.Variant, index)
			}
		}
	}
	dependencyGroupStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_DEPENDENCY_GROUP
		(QUEST_ID,VARIANT_INDEX,GROUP_INDEX,DEPENDENCY_MODE,DEPENDENCY_COUNT,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer dependencyGroupStatement.Close()
	dependencyEntryStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_DEPENDENCY_ENTRY
		(QUEST_ID,VARIANT_INDEX,GROUP_INDEX,DEPENDENCY_INDEX,DEPENDENCY_QUEST_ID,DEPENDENCY_CODE_NAME)
		SELECT owner.ID,?,?,?,dependency.ID,dependency.CODE_NAME FROM REF_QUEST owner
		JOIN REF_QUEST dependency ON dependency.CODE_NAME=? WHERE owner.CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer dependencyEntryStatement.Close()
	for _, dependency := range dependencies {
		parameters, marshalErr := json.Marshal(dependency.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := dependencyGroupStatement.ExecContext(ctx, dependency.Variant, dependency.GroupIndex,
			dependency.Mode, len(dependency.QuestCodes), parameters, scriptHash, dependency.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import dependency group %s/%d/%d: %w", dependency.QuestCodeName, dependency.Variant, dependency.GroupIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("dependency group %s/%d/%d does not resolve exactly once", dependency.QuestCodeName, dependency.Variant, dependency.GroupIndex)
		}
		for index, dependencyCode := range dependency.QuestCodes {
			result, execErr = dependencyEntryStatement.ExecContext(ctx, dependency.Variant, dependency.GroupIndex,
				index, dependencyCode, dependency.QuestCodeName)
			if execErr != nil {
				return fmt.Errorf("import dependency %s/%d/%d/%d: %w", dependency.QuestCodeName, dependency.Variant, dependency.GroupIndex, index, execErr)
			}
			affected, rowsErr = result.RowsAffected()
			if rowsErr != nil || affected != 1 {
				return fmt.Errorf("dependency %s/%d/%d/%d does not resolve exactly once", dependency.QuestCodeName, dependency.Variant, dependency.GroupIndex, index)
			}
		}
	}
	completionPolicyStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_COMPLETION_POLICY
		(QUEST_ID,VARIANT_INDEX,MISSION_COMPLETE_NUM,ACHIEVEMENT_LIMIT,DIVIDE_TYPE,REAL_TIME,
		 DAY_LIMIT,DAY_OR_NIGHT,LIMIT_TIME,HWAN_LEVEL,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,?,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer completionPolicyStatement.Close()
	for _, policy := range completionPolicies {
		parameters, marshalErr := json.Marshal(policy.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := completionPolicyStatement.ExecContext(ctx, policy.Variant, policy.MissionCompleteNum,
			nullableQuestUint(policy.AchievementLimit), nullableQuestUint(policy.DivideType), policy.RealTime,
			nullableQuestUint(policy.DayLimit), nullableQuestUint(policy.DayOrNight), nullableQuestUint(policy.LimitTime),
			nullableQuestUint(policy.HwanLevel), parameters, scriptHash, policy.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import completion policy %s/%d: %w", policy.QuestCodeName, policy.Variant, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("completion policy %s/%d does not resolve exactly once", policy.QuestCodeName, policy.Variant)
		}
	}
	jobConditionStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_JOB_START_CONDITION
		(QUEST_ID,VARIANT_INDEX,CONDITION_INDEX,PARAMETER_0,JOB_STATE_KEY,PARAMETER_2,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer jobConditionStatement.Close()
	for _, condition := range jobStartConditions {
		parameters, marshalErr := json.Marshal(condition.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := jobConditionStatement.ExecContext(ctx, condition.Variant, condition.ConditionIndex,
			condition.Parameter0, condition.JobStateKey, condition.Parameter2, parameters, scriptHash, condition.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import job condition %s/%d/%d: %w", condition.QuestCodeName, condition.Variant, condition.ConditionIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("job condition %s/%d/%d does not resolve exactly once", condition.QuestCodeName, condition.Variant, condition.ConditionIndex)
		}
	}
	raceConditionStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_RACE_START_CONDITION
		(QUEST_ID,VARIANT_INDEX,CONDITION_KEY,COUNTRY_KEY,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer raceConditionStatement.Close()
	for _, condition := range raceStartConditions {
		parameters, marshalErr := json.Marshal(condition.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := raceConditionStatement.ExecContext(ctx, condition.Variant, condition.ConditionKey,
			condition.CountryKey, parameters, scriptHash, condition.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import race condition %s/%d: %w", condition.QuestCodeName, condition.Variant, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("race condition %s/%d does not resolve exactly once", condition.QuestCodeName, condition.Variant)
		}
	}
	pointRewardStatement, err := tx.PrepareContext(ctx, `INSERT INTO REF_QUEST_SCRIPT_POINT_REWARD
		(QUEST_ID,VARIANT_INDEX,REWARD_INDEX,REWARD_KIND,AMOUNT,POINT_KEY,PARAMETERS,RUNTIME_ENABLED,SCRIPT_SHA256)
		SELECT ID,?,?,?,?,?,CAST(? AS JSON),0,? FROM REF_QUEST WHERE CODE_NAME=?`)
	if err != nil {
		return err
	}
	defer pointRewardStatement.Close()
	for _, reward := range pointRewards {
		parameters, marshalErr := json.Marshal(reward.Parameters)
		if marshalErr != nil {
			return marshalErr
		}
		result, execErr := pointRewardStatement.ExecContext(ctx, reward.Variant, reward.RewardIndex,
			reward.Kind, reward.Amount, reward.PointKey, parameters, scriptHash, reward.QuestCodeName)
		if execErr != nil {
			return fmt.Errorf("import point reward %s/%d/%d: %w", reward.QuestCodeName, reward.Variant, reward.RewardIndex, execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil || affected != 1 {
			return fmt.Errorf("point reward %s/%d/%d does not resolve exactly once", reward.QuestCodeName, reward.Variant, reward.RewardIndex)
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
		enabled := safeKillQuests[objectiveKey{objective.QuestCodeName, objective.Variant}]
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
		SELECT quest.ID,?,?,?,npc.ID,npc.CODE_NAME,?,?,?,CAST(? AS JSON),?,?
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
		enabled := safeDeliveryQuests[objectiveKey{objective.QuestCodeName, objective.Variant}]
		result, execErr := standardDeliverStatement.ExecContext(ctx, objective.Variant, objective.MissionIndex,
			objective.ObjectiveKey, len(objective.Items), objective.TrailingParameter, objective.DialogueKey,
			parameters, enabled, scriptHash, objective.TargetNPCCode, objective.QuestCodeName)
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
		key := objectiveKey{reward.QuestCodeName, reward.Variant}
		enabled := reward.Variant == 0 && (reward.QuestCodeName == "QNO_CH_SMITH_1" || safeKillQuests[key] || safeDeliveryQuests[key])
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
	_, err = fmt.Fprintf(output, "imported %d declarations, %d start conditions, %d start methods, %d dependency groups, %d completion policies, %d job conditions, %d race conditions, %d point rewards, %d kill objectives, %d deliver-item EX objectives, %d standard deliver-item objectives, %d dialog objectives, %d kill-player objectives, %d collection-item objectives, %d extended kill objectives, %d talisman objectives, and %d rewards (sha256 %s)\n", len(definitions), len(startConditions), len(startMethods), len(dependencies), len(completionPolicies), len(jobStartConditions), len(raceStartConditions), len(pointRewards), len(killObjectives), len(deliverObjectives), len(standardDeliverObjectives), len(dialogObjectives), len(killPlayerObjectives), len(collectionObjectives), len(extendedKillObjectives), len(talismanObjectives), len(rewards), scriptHash)
	return err
}

func nullableQuestUint(value *uint32) any {
	if value == nil {
		return nil
	}
	return *value
}
