package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	_ "github.com/go-sql-driver/mysql"
)

var errEventRewardImportUsage = errors.New("usage: sro-data import-event-rewards --media <Media directory>")

func importEventRewardData(ctx context.Context, arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("sro-data import-event-rewards", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mediaDirectory := flags.String("media", "", "path to the extracted Media directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *mediaDirectory == "" {
		return errEventRewardImportUsage
	}
	textDataDirectory := filepath.Join(*mediaDirectory, "server_dep", "silkroad", "textdata")
	rewards, items, err := loadEventRewardReferenceData(textDataDirectory)
	if err != nil {
		return err
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer pool.Close()
	if err := importEventRewardRows(ctx, pool, rewards, items); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d event rewards and %d event reward items\n", len(rewards), len(items))
	return err
}

type eventRewardReferenceRow struct {
	Service, EventID                               uint64
	EventCodeName                                  string
	IsView, IsBasicReward, IsItemReward            uint64
	IsCheckCondition, IsCheckCountry, IsCheckClass uint64
	IsCheckGender                                  uint64
	Gold, Experience, SkillExperience, SkillPoints int64
	Hwan, InventorySlots, ItemRewardType           uint64
	SelectionCount                                 uint64
	Param1, Param2, Param3                         int64
}

type eventRewardItemReferenceRow struct {
	Service, EventID                              uint64
	EventCodeName, ItemCodeName, RentItemCodeName string
	PayCount, Param1, Param2                      int
	AchieveRatio                                  float64
}

func loadEventRewardReferenceData(textDataDirectory string) ([]eventRewardReferenceRow, []eventRewardItemReferenceRow, error) {
	rewards, err := parseEventRewardRows(filepath.Join(textDataDirectory, "refeventreward.txt"))
	if err != nil {
		return nil, nil, err
	}
	items, err := parseEventRewardItemRows(filepath.Join(textDataDirectory, "refeventrewarditems.txt"))
	if err != nil {
		return nil, nil, err
	}
	known := make(map[uint64]struct{}, len(rewards))
	for _, reward := range rewards {
		known[reward.EventID] = struct{}{}
	}
	for _, item := range items {
		if _, exists := known[item.EventID]; !exists {
			return nil, nil, fmt.Errorf("event reward item %s references missing event %d", item.ItemCodeName, item.EventID)
		}
	}
	return rewards, items, nil
}

func parseEventRewardRows(path string) ([]eventRewardReferenceRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("open event reward data %q: %w", path, err)
	}
	var rows []eventRewardReferenceRow
	seen := make(map[uint64]struct{})
	for lineNumber, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 24 {
			return nil, fmt.Errorf("event reward line %d: want at least 24 fields, got %d", lineNumber+1, len(fields))
		}
		service, err := parseReferenceUint(fields[0], lineNumber+1, 0)
		if err != nil {
			return nil, err
		}
		eventID, err := parseReferenceUint(fields[1], lineNumber+1, 1)
		if err != nil || eventID < 10000 {
			if err == nil {
				err = fmt.Errorf("must be >= 10000")
			}
			return nil, fmt.Errorf("event reward line %d field 1: %w", lineNumber+1, err)
		}
		if _, exists := seen[eventID]; exists {
			return nil, fmt.Errorf("event reward line %d duplicates event %d", lineNumber+1, eventID)
		}
		seen[eventID] = struct{}{}
		values := make([]uint64, 0, 12)
		for _, index := range []int{3, 4, 5, 6, 7, 8, 9, 13, 14, 15, 16, 17} {
			value, parseErr := parseReferenceUint(fields[index], lineNumber+1, index)
			if parseErr != nil {
				return nil, parseErr
			}
			values = append(values, value)
		}
		gold, err := parseReferenceInt(fields[10], lineNumber+1, 10)
		if err != nil {
			return nil, err
		}
		experience, err := parseReferenceInt(fields[11], lineNumber+1, 11)
		if err != nil {
			return nil, err
		}
		skillExperience, err := parseReferenceInt(fields[12], lineNumber+1, 12)
		if err != nil {
			return nil, err
		}
		params := [3]int64{}
		for offset, index := range []int{18, 20, 22} {
			params[offset], err = parseReferenceInt(fields[index], lineNumber+1, index)
			if err != nil {
				return nil, err
			}
		}
		rows = append(rows, eventRewardReferenceRow{
			Service: service, EventID: eventID, EventCodeName: fields[2],
			IsView: values[0], IsBasicReward: values[1], IsItemReward: values[2],
			IsCheckCondition: values[3], IsCheckCountry: values[4], IsCheckClass: values[5], IsCheckGender: values[6],
			Gold: gold, Experience: experience, SkillExperience: skillExperience, SkillPoints: int64(values[7]),
			Hwan: values[8], InventorySlots: values[9], ItemRewardType: values[10], SelectionCount: values[11],
			Param1: params[0], Param2: params[1], Param3: params[2],
		})
	}
	return rows, nil
}

func parseEventRewardItemRows(path string) ([]eventRewardItemReferenceRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("open event reward item data %q: %w", path, err)
	}
	var rows []eventRewardItemReferenceRow
	for lineNumber, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 11 {
			return nil, fmt.Errorf("event reward item line %d: want at least 11 fields, got %d", lineNumber+1, len(fields))
		}
		service, err := parseReferenceUint(fields[0], lineNumber+1, 0)
		if err != nil {
			return nil, err
		}
		eventID, err := parseReferenceUint(fields[1], lineNumber+1, 1)
		if err != nil || eventID < 10000 {
			if err == nil {
				err = fmt.Errorf("must be >= 10000")
			}
			return nil, fmt.Errorf("event reward item line %d field 1: %w", lineNumber+1, err)
		}
		payCount, err := strconv.Atoi(fields[4])
		if err != nil {
			return nil, fmt.Errorf("event reward item line %d field 4: %w", lineNumber+1, err)
		}
		ratio, err := strconv.ParseFloat(fields[5], 32)
		if err != nil {
			return nil, fmt.Errorf("event reward item line %d field 5: %w", lineNumber+1, err)
		}
		param1, err := strconv.Atoi(fields[7])
		if err != nil {
			return nil, fmt.Errorf("event reward item line %d field 7: %w", lineNumber+1, err)
		}
		param2, err := strconv.Atoi(fields[9])
		if err != nil {
			return nil, fmt.Errorf("event reward item line %d field 9: %w", lineNumber+1, err)
		}
		rows = append(rows, eventRewardItemReferenceRow{Service: service, EventID: eventID, EventCodeName: fields[2], ItemCodeName: fields[3], PayCount: payCount, AchieveRatio: ratio, RentItemCodeName: fields[6], Param1: param1, Param2: param2})
	}
	return rows, nil
}

func parseReferenceUint(value string, line, field int) (uint64, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reference line %d field %d: %w", line, field, err)
	}
	return parsed, nil
}

func parseReferenceInt(value string, line, field int) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reference line %d field %d: %w", line, field, err)
	}
	return parsed, nil
}

func importEventRewardRows(ctx context.Context, pool *sql.DB, rewards []eventRewardReferenceRow, items []eventRewardItemReferenceRow) error {
	if len(rewards) == 0 && len(items) == 0 {
		return nil
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event reward import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rewardStatement, err := tx.PrepareContext(ctx, "INSERT INTO REF_EVENT_REWARD (EVENT_ID,SERVICE,EVENT_CODE_NAME,IS_VIEW,IS_BASIC_REWARD,IS_ITEM_REWARD,IS_CHECK_CONDITION,IS_CHECK_COUNTRY,IS_CHECK_CLASS,IS_CHECK_GENDER,GOLD,EXPERIENCE,SKILL_EXPERIENCE,SKILL_POINTS,HWAN,INVENTORY_SLOTS,ITEM_REWARD_TYPE,SELECTION_COUNT,PARAM_1,PARAM_2,PARAM_3) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE SERVICE=VALUES(SERVICE),EVENT_CODE_NAME=VALUES(EVENT_CODE_NAME),IS_VIEW=VALUES(IS_VIEW),IS_BASIC_REWARD=VALUES(IS_BASIC_REWARD),IS_ITEM_REWARD=VALUES(IS_ITEM_REWARD),IS_CHECK_CONDITION=VALUES(IS_CHECK_CONDITION),IS_CHECK_COUNTRY=VALUES(IS_CHECK_COUNTRY),IS_CHECK_CLASS=VALUES(IS_CHECK_CLASS),IS_CHECK_GENDER=VALUES(IS_CHECK_GENDER),GOLD=VALUES(GOLD),EXPERIENCE=VALUES(EXPERIENCE),SKILL_EXPERIENCE=VALUES(SKILL_EXPERIENCE),SKILL_POINTS=VALUES(SKILL_POINTS),HWAN=VALUES(HWAN),INVENTORY_SLOTS=VALUES(INVENTORY_SLOTS),ITEM_REWARD_TYPE=VALUES(ITEM_REWARD_TYPE),SELECTION_COUNT=VALUES(SELECTION_COUNT),PARAM_1=VALUES(PARAM_1),PARAM_2=VALUES(PARAM_2),PARAM_3=VALUES(PARAM_3)")
	if err != nil {
		return fmt.Errorf("prepare event reward import: %w", err)
	}
	defer rewardStatement.Close()
	for _, reward := range rewards {
		if _, err := rewardStatement.ExecContext(ctx, reward.EventID, reward.Service, reward.EventCodeName, reward.IsView, reward.IsBasicReward, reward.IsItemReward, reward.IsCheckCondition, reward.IsCheckCountry, reward.IsCheckClass, reward.IsCheckGender, reward.Gold, reward.Experience, reward.SkillExperience, reward.SkillPoints, reward.Hwan, reward.InventorySlots, reward.ItemRewardType, reward.SelectionCount, reward.Param1, reward.Param2, reward.Param3); err != nil {
			return fmt.Errorf("import event reward %d: %w", reward.EventID, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM `REF_EVENT_REWARD_ITEM` WHERE `EVENT_ID`=?", reward.EventID); err != nil {
			return fmt.Errorf("replace event reward items %d: %w", reward.EventID, err)
		}
	}
	itemStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_EVENT_REWARD_ITEM` (`EVENT_ID`,`SERVICE`,`EVENT_CODE_NAME`,`ITEM_CODE_NAME`,`PAY_COUNT`,`ACHIEVE_RATIO`,`RENT_ITEM_CODE_NAME`,`PARAM_1`,`PARAM_2`) VALUES (?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return fmt.Errorf("prepare event reward item import: %w", err)
	}
	defer itemStatement.Close()
	for _, item := range items {
		if _, err := itemStatement.ExecContext(ctx, item.EventID, item.Service, item.EventCodeName, item.ItemCodeName, item.PayCount, item.AchieveRatio, item.RentItemCodeName, item.Param1, item.Param2); err != nil {
			return fmt.Errorf("import event reward item %s: %w", item.ItemCodeName, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event reward import: %w", err)
	}
	return nil
}
