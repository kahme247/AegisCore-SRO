package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errVSROQuestImportUsage = errors.New("usage: sro-data import-vsro-quests --export-dir <SRO_VT_SHARD JSON export directory>")

type vsroQuestTable struct {
	file    string
	table   string
	columns map[string]string
}

var vsroQuestTables = []vsroQuestTable{
	{"ref_quest.json", "REF_QUEST", map[string]string{
		"Service": "SERVICE", "ID": "ID", "CodeName": "CODE_NAME", "Level": "LEVEL",
		"DescName": "DESCRIPTION_NAME", "NameString": "NAME_STRING", "PayString": "PAY_STRING",
		"ContentsString": "CONTENTS_STRING", "PayContents": "PAY_CONTENTS", "NoticeNPC": "NOTICE_NPC",
		"NoticeCondition": "NOTICE_CONDITION",
	}},
	{"ref_quest_reward.json", "REF_QUEST_REWARD", map[string]string{
		"Service": "SERVICE", "QuestID": "QUEST_ID", "QuestCodeName": "QUEST_CODE_NAME",
		"IsView": "IS_VIEW", "IsBasicReward": "IS_BASIC_REWARD", "IsItemReward": "IS_ITEM_REWARD",
		"IsCheckCondition": "IS_CHECK_CONDITION", "IsCheckCountry": "IS_CHECK_COUNTRY",
		"IsCheckClass": "IS_CHECK_CLASS", "IsCheckGender": "IS_CHECK_GENDER", "Gold": "GOLD",
		"Exp": "EXPERIENCE", "SPExp": "SKILL_EXPERIENCE", "SP": "SKILL_POINTS",
		"AP": "ACADEMY_POINTS", "APType": "ACADEMY_POINT_TYPE", "Hwan": "HWAN",
		"Inventory": "INVENTORY_SLOTS", "ItemRewardType": "ITEM_REWARD_TYPE",
		"SelectionCnt": "SELECTION_COUNT", "Param1": "PARAM_1", "Param1_Desc": "PARAM_1_DESCRIPTION",
		"Param2": "PARAM_2", "Param2_Desc": "PARAM_2_DESCRIPTION", "Param3": "PARAM_3",
		"Param3_Desc": "PARAM_3_DESCRIPTION",
	}},
	{"ref_quest_reward_items.json", "REF_QUEST_REWARD_ITEM", map[string]string{
		"Service": "SERVICE", "QuestID": "QUEST_ID", "QuestCodeName": "QUEST_CODE_NAME",
		"RewardType": "REWARD_TYPE", "ItemCodeName": "ITEM_CODE_NAME",
		"OptionalItemCode": "OPTIONAL_ITEM_CODE", "OptionalItemCnt": "OPTIONAL_ITEM_COUNT",
		"AchieveQuantity": "ACHIEVE_QUANTITY", "RentItemCodeName": "RENT_ITEM_CODE_NAME",
		"Param1": "PARAM_1", "Param1_Desc": "PARAM_1_DESCRIPTION", "Param2": "PARAM_2",
		"Param2_Desc": "PARAM_2_DESCRIPTION",
	}},
}

func importVSROQuestReference(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-vsro-quests", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	exportDirectory := flags.String("export-dir", "", "directory containing SQL Server JSON exports")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *exportDirectory == "" {
		return errVSROQuestImportUsage
	}
	data := make([][]map[string]any, len(vsroQuestTables))
	for i, spec := range vsroQuestTables {
		rows, err := loadVSROJSONRows(filepath.Join(*exportDirectory, spec.file))
		if err != nil {
			return fmt.Errorf("load %s: %w", spec.file, err)
		}
		data[i] = rows
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	if err := importVSROQuestRows(ctx, pool, data); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d quests, %d rewards, and %d reward items from SRO_VT_SHARD\n", len(data[0]), len(data[1]), len(data[2]))
	return err
}

func importVSROQuestRows(ctx context.Context, pool *sql.DB, data [][]map[string]any) error {
	if len(data) != len(vsroQuestTables) {
		return fmt.Errorf("vSRO quest import received %d tables, want %d", len(data), len(vsroQuestTables))
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vSRO quest import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"REF_QUEST_REWARD_ITEM", "REF_QUEST_REWARD", "REF_QUEST"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `"+table+"`"); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	for index, spec := range vsroQuestTables {
		if err := insertVSROQuestRows(ctx, tx, spec, data[index]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertVSROQuestRows(ctx context.Context, tx *sql.Tx, spec vsroQuestTable, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	sources := make([]string, 0, len(spec.columns))
	for source := range spec.columns {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return spec.columns[sources[i]] < spec.columns[sources[j]] })
	quoted := make([]string, len(sources))
	placeholders := make([]string, len(sources))
	for i, source := range sources {
		quoted[i] = "`" + spec.columns[source] + "`"
		placeholders[i] = "?"
	}
	statement, err := tx.PrepareContext(ctx, "INSERT INTO `"+spec.table+"` ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(placeholders, ",")+")")
	if err != nil {
		return fmt.Errorf("prepare %s import: %w", spec.table, err)
	}
	defer statement.Close()
	for rowNumber, row := range rows {
		args := make([]any, len(sources))
		for i, source := range sources {
			value, exists := row[source]
			if !exists {
				return fmt.Errorf("%s row %d is missing source column %s", spec.table, rowNumber+1, source)
			}
			if number, ok := value.(json.Number); ok {
				value = number.String()
			}
			args[i] = value
		}
		if _, err := statement.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("insert %s row %d: %w", spec.table, rowNumber+1, err)
		}
	}
	return nil
}
