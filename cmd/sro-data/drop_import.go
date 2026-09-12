package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	_ "github.com/go-sql-driver/mysql"
)

var errDropCatalogImportUsage = errors.New("usage: sro-data import-drop-catalog --data-dir <drop table directory>")

type dropCatalogImport struct {
	gold, groups, random, direct, itemAssign, optLevels [][]string
	classSelections                                     []dropClassSelectionImport
}

type dropClassSelectionImport struct {
	family string
	rows   [][]string
}

func importDropCatalog(ctx context.Context, arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("sro-data import-drop-catalog", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("data-dir", "", "directory containing the client drop CSV files")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || strings.TrimSpace(*directory) == "" {
		return errDropCatalogImportUsage
	}
	data, err := loadDropCatalogImport(*directory)
	if err != nil {
		return err
	}
	config.Initialize()
	database, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer database.Close()
	if err := writeDropCatalogImport(ctx, database, data); err != nil {
		return err
	}
	classRows := 0
	for _, source := range data.classSelections {
		classRows += len(source.rows)
	}
	_, err = fmt.Fprintf(output, "imported %d gold, %d groups, %d random assignments, %d direct assignments, %d class-selection rows, %d item assignments, and %d option levels\n", len(data.gold), len(data.groups), len(data.random), len(data.direct), classRows, len(data.itemAssign), len(data.optLevels))
	return err
}

func loadDropCatalogImport(directory string) (dropCatalogImport, error) {
	var result dropCatalogImport
	var err error
	for name, target := range map[string]*[][]string{
		"_RefDropGold.csv":                    &result.gold,
		"_RefDropItemGroup.csv":               &result.groups,
		"_RefMonster_AssignedItemRndDrop.csv": &result.random,
		"_RefMonster_AssignedItemDrop.csv":    &result.direct,
	} {
		minimum := 4
		switch name {
		case "_RefDropItemGroup.csv":
			minimum = 6
		case "_RefMonster_AssignedItemRndDrop.csv":
			minimum = 10
		case "_RefMonster_AssignedItemDrop.csv":
			minimum = 26
		}
		*target, err = readDropCSV(filepath.Join(directory, name), minimum)
		if err != nil {
			return dropCatalogImport{}, err
		}
	}
	for _, source := range []struct {
		file   string
		family string
	}{
		{"_RefDropClassSel_Equip.csv", "EQUIP"},
		{"_RefDropClassSel_RareEquip.csv", "RAREEQUIP"},
		{"_RefDropClassSel_Recover.csv", "RECOVER"},
		{"_RefDropClassSel_Cure.csv", "CURE"},
		{"_RefDropClassSel_Ammo.csv", "AMMO"},
		{"_RefDropClassSel_Scroll.csv", "SCROLL"},
		{"_RefDropClassSel_Reinforce.csv", "REINFORCE"},
		{"_RefDropClassSel_Alchemy_Tablet.csv", "ALCHEMY_TABLET"},
		{"_RefDropClassSel_Alchemy_MagicStone.csv", "ALCHEMY_MAGICSTONE"},
		{"_RefDropClassSel_Alchemy_ATTRStone.csv", "ALCHEMY_ATTRSTONE"},
	} {
		rows, readErr := readDropCSV(filepath.Join(directory, source.file), 2)
		if readErr != nil {
			return dropCatalogImport{}, readErr
		}
		result.classSelections = append(result.classSelections, dropClassSelectionImport{family: source.family, rows: rows})
	}
	result.itemAssign, err = readDropCSV(filepath.Join(directory, "_RefDropItemAssign.csv"), 6)
	if err != nil {
		return dropCatalogImport{}, err
	}
	result.optLevels, err = readDropCSV(filepath.Join(directory, "_RefDropOptLvlSel.csv"), 3)
	if err != nil {
		return dropCatalogImport{}, err
	}
	return result, nil
}

func readDropCSV(path string, minimumColumns int) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open drop catalog %q: %w", path, err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	var rows [][]string
	for line := 1; ; line++ {
		fields, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read drop catalog %q line %d: %w", path, line, readErr)
		}
		if line == 1 || len(fields) == 0 || strings.TrimSpace(strings.Join(fields, "")) == "" {
			continue
		}
		if len(fields) < minimumColumns {
			return nil, fmt.Errorf("drop catalog %q line %d: want %d columns, got %d", path, line, minimumColumns, len(fields))
		}
		rows = append(rows, fields)
	}
	return rows, nil
}

func writeDropCatalogImport(ctx context.Context, database *sql.DB, data dropCatalogImport) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin drop catalog import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"REF_MONSTER_ASSIGNED_ITEM_DROP", "REF_MONSTER_ASSIGNED_ITEM_RND_DROP", "REF_DROP_ITEM_GROUP", "REF_DROP_GOLD", "REF_DROP_CLASS_SELECTION", "REF_DROP_ITEM_ASSIGN", "REF_DROP_OPT_LEVEL_SELECTION"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`"+table+"`"); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	if err := insertDropGold(ctx, tx, data.gold); err != nil {
		return err
	}
	if err := insertDropGroups(ctx, tx, data.groups); err != nil {
		return err
	}
	if err := insertRandomDrops(ctx, tx, data.random); err != nil {
		return err
	}
	if err := insertDirectDrops(ctx, tx, data.direct); err != nil {
		return err
	}
	if err := insertDropClassSelections(ctx, tx, data.classSelections); err != nil {
		return err
	}
	if err := insertDropItemAssignments(ctx, tx, data.itemAssign); err != nil {
		return err
	}
	if err := insertDropOptLevels(ctx, tx, data.optLevels); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit drop catalog import: %w", err)
	}
	return nil
}

func insertDropClassSelections(ctx context.Context, tx *sql.Tx, sources []dropClassSelectionImport) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_DROP_CLASS_SELECTION` (`FAMILY`,`MON_LEVEL`,`PROB_GROUP`,`PROB`) VALUES (?,?,?,?)"
	for _, source := range sources {
		if source.family == "" {
			return errors.New("drop class-selection family is empty")
		}
		for line, row := range source.rows {
			level, err := dropUint(row[0])
			if err != nil || level == 0 {
				return fmt.Errorf("invalid %s class-selection row %d: %w", source.family, line+2, err)
			}
			for group := 1; group < len(row); group++ {
				probability, probabilityErr := dropFloat(row[group])
				if probabilityErr != nil || probability < 0 {
					return fmt.Errorf("invalid %s class-selection row %d group %d: %w", source.family, line+2, group, probabilityErr)
				}
				if _, err = tx.ExecContext(ctx, query, source.family, level, group, probability); err != nil {
					return fmt.Errorf("insert %s class-selection row %d group %d: %w", source.family, line+2, group, err)
				}
			}
		}
	}
	return nil
}

func insertDropItemAssignments(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_DROP_ITEM_ASSIGN` (`SOURCE_ROW`,`SERVICE`,`REF_ITEM_ID`,`PROB_RELATIVE`,`PROB_ABSOLUTE`,`ASSIGNED_GROUP`,`DROP_COUNT`) VALUES (?,?,?,?,?,?,?)"
	for line, row := range rows {
		service, err := dropUint(row[0])
		itemID, itemErr := dropUint(row[1])
		relative, relativeErr := dropFloat(row[2])
		absolute, absoluteErr := dropFloat(row[3])
		group, groupErr := dropInt(row[4])
		count, countErr := dropUint(row[5])
		if err = errors.Join(err, itemErr, relativeErr, absoluteErr, groupErr, countErr); err != nil || itemID == 0 || relative < 0 || absolute < 0 {
			return fmt.Errorf("invalid drop-item-assignment row %d: %w", line+2, err)
		}
		if _, err = tx.ExecContext(ctx, query, line+2, service, itemID, relative, absolute, group, count); err != nil {
			return fmt.Errorf("insert drop-item-assignment row %d: %w", line+2, err)
		}
	}
	return nil
}

func insertDropOptLevels(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_DROP_OPT_LEVEL_SELECTION` (`OPT_LEVEL`,`PROB`,`REQ_ONLINE_TIME`) VALUES (?,?,?)"
	for line, row := range rows {
		level, err := dropUint(row[0])
		probability, probabilityErr := dropFloat(row[1])
		requiredTime, timeErr := dropUint(row[2])
		if err = errors.Join(err, probabilityErr, timeErr); err != nil || probability < 0 {
			return fmt.Errorf("invalid drop-option-level row %d: %w", line+2, err)
		}
		if _, err = tx.ExecContext(ctx, query, level, probability, requiredTime); err != nil {
			return fmt.Errorf("insert drop-option-level row %d: %w", line+2, err)
		}
	}
	return nil
}

func insertDropGold(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_DROP_GOLD` (`MON_LEVEL`,`SERVICE`,`DROP_PROB`,`GOLD_MIN`,`GOLD_MAX`) VALUES (?,?,?,?,?)"
	for line, row := range rows {
		level, err := dropInt(row[0])
		prob, probErr := dropFloat(row[1])
		minimum, minErr := dropUint(row[2])
		maximum, maxErr := dropUint(row[3])
		if err = errors.Join(err, probErr, minErr, maxErr); err != nil || level <= 0 || maximum < minimum {
			return fmt.Errorf("invalid drop-gold row %d: %w", line+2, err)
		}
		if _, err := tx.ExecContext(ctx, query, level, 1, prob, minimum, maximum); err != nil {
			return fmt.Errorf("insert drop-gold row %d: %w", line+2, err)
		}
	}
	return nil
}

func insertDropGroups(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_DROP_ITEM_GROUP` (`SERVICE`,`REF_ITEM_GROUP_ID`,`REF_ITEM_ID`,`REF_MAGIC_GROUP_ID`,`SELECT_RATIO`) VALUES (?,?,?,?,?)"
	for line, row := range rows {
		service, err := dropUint(row[0])
		group, groupErr := dropUint(row[1])
		item, itemErr := dropUint(row[3])
		magicGroup, magicGroupErr := dropUint(row[5])
		ratio, ratioErr := dropFloat(row[4])
		if err = errors.Join(err, groupErr, itemErr, magicGroupErr, ratioErr); err != nil || group == 0 || item == 0 || ratio < 0 {
			return fmt.Errorf("invalid drop-group row %d: %w", line+2, err)
		}
		if _, err := tx.ExecContext(ctx, query, service, group, item, magicGroup, ratio); err != nil {
			return fmt.Errorf("insert drop-group row %d: %w", line+2, err)
		}
	}
	return nil
}

func insertRandomDrops(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_RND_DROP` (`SERVICE`,`REF_MONSTER_ID`,`REF_ITEM_GROUP_ID`,`OVERLAP`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO`) VALUES (?,?,?,?,?,?,?)"
	for line, row := range rows {
		service, err := dropUint(row[0])
		monster, monsterErr := dropUint(row[1])
		group, groupErr := dropUint(row[2])
		overlap, overlapErr := dropUint(row[4])
		minimum, minErr := dropUint(row[5])
		maximum, maxErr := dropUint(row[6])
		ratio, ratioErr := dropFloat(row[7])
		if err = errors.Join(err, monsterErr, groupErr, overlapErr, minErr, maxErr, ratioErr); err != nil || monster == 0 || group == 0 || maximum < minimum {
			return fmt.Errorf("invalid random-drop row %d: %w", line+2, err)
		}
		if _, err := tx.ExecContext(ctx, query, service, monster, group, overlap, minimum, maximum, ratio); err != nil {
			return fmt.Errorf("insert random-drop row %d: %w", line+2, err)
		}
	}
	return nil
}

func insertDirectDrops(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	const query = "INSERT INTO `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_DROP` (`SERVICE`,`REF_MONSTER_ID`,`REF_ITEM_ID`,`DROP_GROUP_TYPE`,`OPT_LEVEL`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO`,`REF_MAGIC_OPTION_ID1`,`CUSTOM_VALUE1`,`REF_MAGIC_OPTION_ID2`,`CUSTOM_VALUE2`,`REF_MAGIC_OPTION_ID3`,`CUSTOM_VALUE3`,`REF_MAGIC_OPTION_ID4`,`CUSTOM_VALUE4`,`REF_MAGIC_OPTION_ID5`,`CUSTOM_VALUE5`,`REF_MAGIC_OPTION_ID6`,`CUSTOM_VALUE6`,`REF_MAGIC_OPTION_ID7`,`CUSTOM_VALUE7`,`REF_MAGIC_OPTION_ID8`,`CUSTOM_VALUE8`,`REF_MAGIC_OPTION_ID9`,`CUSTOM_VALUE9`,`RENT_CODE_NAME`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)"
	for line, row := range rows {
		values, err := directDropValues(row)
		if err != nil {
			return fmt.Errorf("invalid direct-drop row %d: %w", line+2, err)
		}
		if _, err := tx.ExecContext(ctx, query, values...); err != nil {
			return fmt.Errorf("insert direct-drop row %d: %w", line+2, err)
		}
	}
	return nil
}

func directDropValues(row []string) ([]any, error) {
	if len(row) < 26 {
		return nil, fmt.Errorf("want 26 columns, got %d", len(row))
	}
	// _RefMonster_AssignedItemDrop is the one drop export without a
	// leading Service column. Its first seven fields are
	// RefMonsterID, RefItemID, DropGroupType, OptLevel,
	// DropAmountMin, DropAmountMax, and DropRatio. The normalized runtime
	// table still carries SERVICE, which is enabled for every imported row.
	values := make([]any, 0, 27)
	values = append(values, uint64(1))
	for _, position := range []int{0, 1, 2, 3, 4, 5} {
		value, err := dropUint(row[position])
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	minimum, minErr := dropUint(row[4])
	maximum, maxErr := dropUint(row[5])
	ratio, ratioErr := dropFloat(row[6])
	if err := errors.Join(minErr, maxErr, ratioErr); err != nil {
		return nil, err
	}
	if maximum < minimum {
		return nil, fmt.Errorf("drop amount max %d is below min %d", maximum, minimum)
	}
	values = append(values, ratio)
	for position := 7; position < 25; position += 2 {
		option, err := nullableDropUint(row[position])
		if err != nil {
			return nil, err
		}
		custom, customErr := dropUint(row[position+1])
		if customErr != nil {
			return nil, customErr
		}
		values = append(values, option, custom)
	}
	values = append(values, row[25])
	return values, nil
}

func dropUint(value string) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(value), 10, 64)
}
func dropInt(value string) (int, error) { return strconv.Atoi(strings.TrimSpace(value)) }
func dropFloat(value string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(value), ",", "."), 64)
}
func nullableDropUint(value string) (any, error) {
	if strings.EqualFold(strings.TrimSpace(value), "null") || strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return dropUint(value)
}
