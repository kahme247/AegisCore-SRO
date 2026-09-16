package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errVSROItemImportUsage = errors.New("usage: sro-data import-vsro-items --export-dir <SRO_VT_SHARD JSON export directory>")

type vsroItemTable struct {
	file, table string
	columns     []string
}

var vsroItemTables = []vsroItemTable{
	{"ref_obj_common.json", "REF_OBJ_COMMON_VSRO", []string{"ID", "Service", "CodeName128", "TypeID1", "TypeID2", "TypeID3", "TypeID4", "Link"}},
	{"ref_obj_item.json", "REF_OBJ_ITEM_VSRO", []string{"ID", "SetID", "ItemClass"}},
	{"ref_obj_struct.json", "REF_OBJ_STRUCT_VSRO", []string{"ID"}},
	{"ref_package_item.json", "REF_PACKAGE_ITEM_VSRO", []string{"Country", "Service", "ID", "CodeName128"}},
	{"ref_price_policy_item.json", "REF_PRICE_POLICY_ITEM_VSRO", []string{"Country", "Service", "RefPackageItemCodeName", "PaymentDevice", "Cost"}},
	{"ref_scrap_of_package_item.json", "REF_SCRAP_OF_PACKAGE_ITEM_VSRO", []string{"Service", "Country", "RefPackageItemCodeName", "Index", "RefItemCodeName", "OptLevel", "Variance", "Data", "MagParamNum"}},
	{"ref_shop_group.json", "REF_SHOP_GROUP_VSRO", []string{"Service", "Country", "ID", "CodeName128", "RefNPCCodeName"}},
	{"ref_mapping_shop_group.json", "REF_MAPPING_SHOP_GROUP_VSRO", []string{"Service", "Country", "RefShopGroupCodeName", "RefShopCodeName"}},
	{"ref_mapping_shop_with_tab.json", "REF_MAPPING_SHOP_WITH_TAB_VSRO", []string{"Service", "Country", "RefShopCodeName", "RefTabGroupCodeName"}},
	{"ref_shop_tab.json", "REF_SHOP_TAB_VSRO", []string{"Service", "Country", "ID", "CodeName128", "RefTabGroupCodeName"}},
	{"ref_shop_goods.json", "REF_SHOP_GOODS_PACKAGE_VSRO", []string{"Service", "Country", "RefTabCodeName", "SlotIndex", "RefPackageItemCodeName"}},
	{"ref_ability_item_opt_level.json", "REF_ABILITY_ITEM_OPT_LEVEL_VSRO", []string{"ID", "RefItemID", "ItemOptLevel"}},
	{"ref_magic_opt_item_opt_level.json", "REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO", []string{"Link", "RefMagicOptID"}},
	{"ref_set_item_group.json", "REF_SET_ITEM_GROUP_VSRO", []string{"ID", "CodeName128"}},
}

func importVSROItemReference(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-vsro-items", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("export-dir", "", "directory containing SQL Server JSON exports")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *directory == "" {
		return errVSROItemImportUsage
	}
	data := make([][]map[string]any, len(vsroItemTables))
	for i, spec := range vsroItemTables {
		rows, err := loadVSROJSONRows(filepath.Join(*directory, spec.file))
		if err != nil {
			return fmt.Errorf("load %s: %w", spec.file, err)
		}
		data[i] = rows
	}
	if err := validateVSROItemLinks(data); err != nil {
		return fmt.Errorf("validate vSRO item links: %w", err)
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	if err := importVSROItemRows(ctx, pool, data); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d vSRO object rows and %d item rows\n", len(data[0]), len(data[1]))
	return err
}

func importVSROItemRows(ctx context.Context, pool *sql.DB, data [][]map[string]any) error {
	if err := validateVSROItemLinks(data); err != nil {
		return fmt.Errorf("validate vSRO item links: %w", err)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO", "REF_ABILITY_ITEM_OPT_LEVEL_VSRO", "REF_SHOP_GOODS_PACKAGE_VSRO", "REF_SHOP_TAB_VSRO", "REF_MAPPING_SHOP_WITH_TAB_VSRO", "REF_MAPPING_SHOP_GROUP_VSRO", "REF_SHOP_GROUP_VSRO", "REF_SCRAP_OF_PACKAGE_ITEM_VSRO", "REF_PRICE_POLICY_ITEM_VSRO", "REF_PACKAGE_ITEM_VSRO", "REF_OBJ_STRUCT_VSRO", "REF_OBJ_ITEM_VSRO", "REF_SET_ITEM_GROUP_VSRO", "REF_OBJ_COMMON_VSRO"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `"+table+"`"); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	for i, spec := range vsroItemTables {
		if err := insertVSROItemRows(ctx, tx, spec, data[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// validateVSROItemLinks enforces the stock _RefObjCommon.Link -> detail-row ID
// relationships before the replacement transaction starts. Common.ID is the
// public object reference; both item.ID and struct.ID are private detail-row
// identities and are not interchangeable once the source sequences diverge.
func validateVSROItemLinks(data [][]map[string]any) error {
	if len(data) != len(vsroItemTables) {
		return fmt.Errorf("got %d table payloads, want %d", len(data), len(vsroItemTables))
	}
	if err := validateVSRODetailLinks(data[0], data[1], 3, "_RefObjItem"); err != nil {
		return err
	}
	return validateVSRODetailLinks(data[0], data[2], 4, "_RefObjStruct")
}

func validateVSRODetailLinks(commonRows, detailRows []map[string]any, typeID1 int64, detailName string) error {
	detailIDs := make(map[int64]struct{}, len(detailRows))
	for rowNumber, row := range detailRows {
		id, err := vsroItemInteger(row, "ID")
		if err != nil || id <= 0 {
			return fmt.Errorf("%s row %d ID: %w", detailName, rowNumber+1, errInvalidVSROItemInteger(err, id))
		}
		if _, duplicate := detailIDs[id]; duplicate {
			return fmt.Errorf("%s row %d duplicates ID %d", detailName, rowNumber+1, id)
		}
		detailIDs[id] = struct{}{}
	}

	linked := make(map[int64]int64, len(detailIDs))
	for rowNumber, row := range commonRows {
		commonType, err := vsroItemInteger(row, "TypeID1")
		if err != nil {
			return fmt.Errorf("_RefObjCommon row %d TypeID1: %w", rowNumber+1, err)
		}
		if commonType != typeID1 {
			continue
		}
		commonID, err := vsroItemInteger(row, "ID")
		if err != nil || commonID <= 0 {
			return fmt.Errorf("_RefObjCommon row %d ID: %w", rowNumber+1, errInvalidVSROItemInteger(err, commonID))
		}
		link, err := vsroItemInteger(row, "Link")
		if err != nil || link <= 0 {
			return fmt.Errorf("_RefObjCommon row %d Link: %w", rowNumber+1, errInvalidVSROItemInteger(err, link))
		}
		if _, exists := detailIDs[link]; !exists {
			return fmt.Errorf("_RefObjCommon ID %d links to missing %s ID %d", commonID, detailName, link)
		}
		if previous, duplicate := linked[link]; duplicate {
			return fmt.Errorf("_RefObjCommon IDs %d and %d both link to %s ID %d", previous, commonID, detailName, link)
		}
		linked[link] = commonID
	}
	if len(linked) != len(detailIDs) {
		for id := range detailIDs {
			if _, exists := linked[id]; !exists {
				return fmt.Errorf("%s ID %d has no TypeID1=%d _RefObjCommon Link", detailName, id, typeID1)
			}
		}
	}
	return nil
}

func vsroItemInteger(row map[string]any, column string) (int64, error) {
	value := vsroJSONValue(row, normalizeVSROColumn(column))
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if math.Trunc(typed) != typed || typed < float64(-1<<63) || typed >= float64(1<<63) {
			return 0, fmt.Errorf("%v is not an integer", typed)
		}
		return int64(typed), nil
	case nil:
		return 0, errors.New("is missing")
	default:
		return 0, fmt.Errorf("has unsupported value %T", value)
	}
}

func errInvalidVSROItemInteger(err error, value int64) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("must be positive, got %d", value)
}

func insertVSROItemRows(ctx context.Context, tx *sql.Tx, spec vsroItemTable, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	columns := make([]string, 0, len(spec.columns)+1)
	placeholders := make([]string, 0, len(spec.columns)+1)
	for _, column := range spec.columns {
		columns = append(columns, "`"+normalizeVSROColumn(column)+"`")
		placeholders = append(placeholders, "?")
	}
	columns = append(columns, "`RAW_DATA`")
	placeholders = append(placeholders, "CAST(? AS JSON)")
	statement, err := tx.PrepareContext(ctx, "INSERT INTO `"+spec.table+"` ("+join(columns)+") VALUES ("+join(placeholders)+")")
	if err != nil {
		return fmt.Errorf("prepare %s: %w", spec.table, err)
	}
	defer statement.Close()
	for number, row := range rows {
		args := make([]any, 0, len(spec.columns)+1)
		for _, column := range spec.columns {
			args = append(args, vsroJSONValue(row, normalizeVSROColumn(column)))
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return err
		}
		args = append(args, string(raw))
		if _, err := statement.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("insert %s row %d: %w", spec.table, number+1, err)
		}
	}
	return nil
}

func join(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += "," + value
	}
	return result
}
