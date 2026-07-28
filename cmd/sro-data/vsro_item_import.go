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

	"github.com/ferdoran/go-sro/framework/config"
	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

var errVSROItemImportUsage = errors.New("usage: sro-data import-vsro-items --export-dir <SRO_VT_SHARD JSON export directory>")

type vsroItemTable struct {
	file, table string
	columns     []string
}

var vsroItemTables = []vsroItemTable{
	{"ref_obj_common.json", "REF_OBJ_COMMON_VSRO", []string{"ID", "Service", "CodeName128", "TypeID1", "TypeID2", "TypeID3", "TypeID4"}},
	{"ref_obj_item.json", "REF_OBJ_ITEM_VSRO", []string{"ID", "SetID", "ItemClass"}},
	{"ref_obj_struct.json", "REF_OBJ_STRUCT_VSRO", []string{"ID"}},
	{"ref_package_item.json", "REF_PACKAGE_ITEM_VSRO", []string{"Country", "ID", "CodeName128"}},
	{"ref_price_policy_item.json", "REF_PRICE_POLICY_ITEM_VSRO", []string{"Country", "RefPackageItemCodeName", "PaymentDevice"}},
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
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO", "REF_ABILITY_ITEM_OPT_LEVEL_VSRO", "REF_PRICE_POLICY_ITEM_VSRO", "REF_PACKAGE_ITEM_VSRO", "REF_OBJ_STRUCT_VSRO", "REF_OBJ_ITEM_VSRO", "REF_SET_ITEM_GROUP_VSRO", "REF_OBJ_COMMON_VSRO"} {
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
