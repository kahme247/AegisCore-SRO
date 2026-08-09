package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errVSROSkillImportUsage = errors.New("usage: sro-data import-vsro-skills --export-dir <SRO_VT_SHARD JSON export directory>")

type vsroSkillTable struct{ file, table string }

var vsroSkillTables = []vsroSkillTable{
	{"ref_skill_group.json", "REF_SKILL_GROUP"},
	{"ref_skill_mastery.json", "REF_SKILL_MASTERY"},
	{"ref_skill.json", "REF_SKILL"},
	{"ref_skill_by_item_opt_level.json", "REF_SKILL_BY_ITEM_OPT_LEVEL"},
	{"ref_char_default_skill.json", "REF_CHAR_DEFAULT_SKILL_VSRO"},
	{"ref_char_default_skill_mastery.json", "REF_CHAR_DEFAULT_SKILL_MASTERY_VSRO"},
	{"ref_obj_char_extra_skill.json", "REF_OBJ_CHAR_EXTRA_SKILL"},
}

func importVSROSkillReference(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-vsro-skills", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	exportDirectory := flags.String("export-dir", "", "directory containing SQL Server JSON exports")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *exportDirectory == "" {
		return errVSROSkillImportUsage
	}
	data := make([][]map[string]any, len(vsroSkillTables))
	for i, spec := range vsroSkillTables {
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
	if err := importVSROSkillRows(ctx, pool, data); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d SRO_VT_SHARD skill rows, %d groups, and %d masteries\n", len(data[2]), len(data[0]), len(data[1]))
	return err
}

func loadVSROJSONRows(path string) ([]map[string]any, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// sqlcmd and bcp wrap a single FOR JSON result around their packet width.
	// SQL Server emits embedded newlines in JSON strings as escaped `\\n`, so
	// removing transport CR/LF here faithfully restores the one JSON document.
	text := strings.NewReplacer("\r", "", "\n", "").Replace(string(contents))
	start := strings.IndexByte(text, '[')
	if start < 0 {
		return nil, errors.New("SQL Server export contains no JSON array")
	}
	decoder := json.NewDecoder(strings.NewReader(text[start:]))
	decoder.UseNumber()
	var rows []map[string]any
	if err := decoder.Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func importVSROSkillRows(ctx context.Context, pool *sql.DB, data [][]map[string]any) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vSRO skill import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"REF_OBJ_CHAR_EXTRA_SKILL", "REF_CHAR_DEFAULT_SKILL_MASTERY_VSRO", "REF_CHAR_DEFAULT_SKILL_VSRO", "REF_SKILL_BY_ITEM_OPT_LEVEL", "REF_SKILL", "REF_SKILL_MASTERY", "REF_SKILL_GROUP"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `"+table+"`"); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	for index, spec := range vsroSkillTables {
		if err := insertVSROJSONRows(ctx, tx, spec.table, data[index]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertVSROJSONRows(ctx context.Context, tx *sql.Tx, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	columns := make([]string, 0, len(rows[0]))
	for source := range rows[0] {
		columns = append(columns, normalizeVSROColumn(source))
	}
	sort.Strings(columns)
	placeholders := make([]string, len(columns))
	quoted := make([]string, len(columns))
	for i, column := range columns {
		placeholders[i] = "?"
		quoted[i] = "`" + column + "`"
	}
	statement, err := tx.PrepareContext(ctx, "INSERT INTO `"+table+"` ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(placeholders, ",")+")")
	if err != nil {
		return fmt.Errorf("prepare %s import: %w", table, err)
	}
	defer statement.Close()
	for rowNumber, row := range rows {
		args := make([]any, len(columns))
		for i, column := range columns {
			args[i] = vsroJSONValue(row, column)
		}
		if _, err := statement.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("insert %s row %d: %w", table, rowNumber+1, err)
		}
	}
	return nil
}

func normalizeVSROColumn(source string) string {
	if strings.HasPrefix(source, "ExtraSkill_") {
		return "EXTRA_SKILL_" + strings.TrimPrefix(source, "ExtraSkill_")
	}
	switch source {
	case "CodeName128":
		return "CODE_NAME"
	case "TypeID1":
		return "TYPE_ID_1"
	case "TypeID2":
		return "TYPE_ID_2"
	case "TypeID3":
		return "TYPE_ID_3"
	case "TypeID4":
		return "TYPE_ID_4"
	case "SetID":
		return "SET_ID"
	case "ItemClass":
		return "ITEM_CLASS"
	case "RefPackageItemCodeName":
		return "PACKAGE_CODE_NAME"
	case "PaymentDevice":
		return "PAYMENT_DEVICE"
	case "RefItemID":
		return "REF_ITEM_ID"
	case "ItemOptLevel":
		return "ITEM_OPT_LEVEL"
	case "RefMagicOptID":
		return "REF_MAGIC_OPT_ID"
	case "Action_Interruptable":
		return "ACTION_INTERRUPTIBLE"
	case "RefSkillID":
		return "REF_SKILL_ID"
	case "SkillID":
		return "SKILL_ID"
	case "MasteryID":
		return "MASTERY_ID"
	case "GroupID":
		return "GROUP_ID"
	case "CharID":
		return "CHAR_ID"
	default:
		return strings.ToUpper(source)
	}
}

func vsroJSONValue(row map[string]any, normalizedColumn string) any {
	for source, value := range row {
		if normalizeVSROColumn(source) != normalizedColumn {
			continue
		}
		if number, ok := value.(json.Number); ok {
			return number.String()
		}
		return value
	}
	return nil
}
