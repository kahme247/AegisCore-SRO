package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var errExportMediaUsage = errors.New("usage: sro-data export-media --output <new Media directory>")

const mediaTextDataPath = "server_dep/silkroad/textdata"

type mediaExportLevel struct {
	Level                                                              uint64
	Experience, SkillPoints, DropGoldMin, DropGoldMax                  uint64
	Unknown1, Unknown2, JobExperience1, JobExperience2, JobExperience3 int64
	Raw                                                                string
}

type mediaExportSkill struct {
	ID     uint64
	Raw    string
	Values map[int]string
}

type mediaExportFile struct {
	Path   string `json:"path"`
	Rows   int    `json:"rows"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type mediaExportManifest struct {
	SchemaVersion       int               `json:"schema_version"`
	Profile             string            `json:"profile"`
	Files               []mediaExportFile `json:"files"`
	UnsupportedCatalogs []string          `json:"unsupported_catalogs,omitempty"`
}

var unsupportedMediaCatalogs = []string{
	"characterdata_5000.txt", "characterdata_10000.txt", "characterdata_15000.txt", "characterdata_20000.txt",
	"characterdata_25000.txt", "characterdata_30000.txt", "characterdata_35000.txt", "characterdata_40000.txt",
	"characterdata_45000.txt", "eventzonedata.txt", "gachaitemset.txt", "gameworldconfigdata.txt", "gameworlddata.txt",
	"hwanleveldata.txt", "magicoption.txt", "magicoptionassign.txt", "refmappingshopgroup.txt", "refmappingshopwithtab.txt",
	"refoptionalteleport.txt", "refscrapofpackageitem.txt", "refshop.txt", "refshopgoods.txt", "refshopgroup.txt",
	"refshoptab.txt", "refshoptabgroup.txt", "shopgroupdata.txt", "refsiegeblessbuff.txt", "refsiegedungeon.txt",
	"siegefortress.txt", "siegefortressbattlerank.txt", "siegefortressguard.txt", "siegefortressitemforge.txt",
	"siegestructupgradedata.txt", "teleportbuilding.txt", "teleportdata.txt", "teleportlink.txt",
}

func exportMedia(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data export-media", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputDirectory := flags.String("output", "", "new extracted Media directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || strings.TrimSpace(*outputDirectory) == "" {
		return errExportMediaUsage
	}
	if _, err := os.Stat(*outputDirectory); err == nil {
		return fmt.Errorf("output directory %q already exists; use a new directory for each export", *outputDirectory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output directory %q: %w", *outputDirectory, err)
	}

	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()

	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin consistent Media export: %w", err)
	}
	defer tx.Rollback()

	levels, err := loadMediaExportLevels(ctx, tx)
	if err != nil {
		return err
	}
	skills, err := loadMediaExportSkills(ctx, tx)
	if err != nil {
		return err
	}
	catalogs, err := loadMediaExportCatalogs(ctx, tx)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finish consistent Media export read: %w", err)
	}

	cleanOutput := filepath.Clean(*outputDirectory)
	if err := os.MkdirAll(filepath.Dir(cleanOutput), 0o750); err != nil {
		return fmt.Errorf("create Media export parent directory: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(cleanOutput), ".media-export-")
	if err != nil {
		return fmt.Errorf("create Media export staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	manifest, err := writeMediaExport(staging, levels, skills, catalogs)
	if err != nil {
		return err
	}
	if err := os.Rename(staging, cleanOutput); err != nil {
		return fmt.Errorf("publish Media export directory: %w", err)
	}
	_, err = fmt.Fprintf(output, "exported %d Media files to %s\n", len(manifest.Files), cleanOutput)
	return err
}

func loadMediaExportLevels(ctx context.Context, tx *sql.Tx) ([]mediaExportLevel, error) {
	rows, err := tx.QueryContext(ctx, `SELECT LEVEL,REQUIRED_EXPERIENCE,REQUIRED_SKILL_POINTS,
        COALESCE(DROP_GOLD_MIN,0),COALESCE(DROP_GOLD_MAX,0),UNKNOWN_1,UNKNOWN_2,
        REQUIRED_JOB_EXPERIENCE,REQUIRED_JOB_EXPERIENCE_2,REQUIRED_JOB_EXPERIENCE_3,RAW_FIELDS
        FROM SRO_SHARD.REF_LEVEL_DATA ORDER BY LEVEL`)
	if err != nil {
		return nil, fmt.Errorf("query level Media data: %w", err)
	}
	defer rows.Close()
	var result []mediaExportLevel
	for rows.Next() {
		var row mediaExportLevel
		if err := rows.Scan(&row.Level, &row.Experience, &row.SkillPoints, &row.DropGoldMin, &row.DropGoldMax,
			&row.Unknown1, &row.Unknown2, &row.JobExperience1, &row.JobExperience2, &row.JobExperience3, &row.Raw); err != nil {
			return nil, fmt.Errorf("scan level Media data: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read level Media data: %w", err)
	}
	if len(result) == 0 {
		return nil, errors.New("REF_LEVEL_DATA contains no rows")
	}
	return result, nil
}

func loadMediaExportSkills(ctx context.Context, tx *sql.Tx) ([]mediaExportSkill, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ID,RAW_FIELDS,SERVICE,GROUP_ID,CODE_NAME,NAME,GROUP_CODE_NAME,
        BASE_SKILL_ID,LEVEL,TYPE,NEXT_SKILL_ID,INIT_TIME,DELAY_TIME,NEXT_TIME,COOLDOWN,MASTERY_ID,
        REQUIRED_MASTERY_LEVEL,REQUIRED_SKILL_1_ID,REQUIRED_SKILL_2_ID,REQUIRED_SKILL_1_LEVEL,
        REQUIRED_SKILL_2_LEVEL,REQUIRED_SKILL_POINTS,REQUIRED_RACE,REQUIRED_WEAPON_1,REQUIRED_WEAPON_2,
        REQUIRED_MP,MIN_HP_RATIO,ICON,RESOURCE_NAME,DESCRIPTION,STUDY_DESCRIPTION,MAX_HP_RATIO
        FROM SRO_SHARD.REF_SKILL_DATA WHERE SERVICE=1 ORDER BY ID`)
	if err != nil {
		return nil, fmt.Errorf("query skill Media data: %w", err)
	}
	defer rows.Close()
	var result []mediaExportSkill
	for rows.Next() {
		var row mediaExportSkill
		values := make([]any, 30)
		stringsOut := make([]string, len(values))
		for i := range values {
			values[i] = &stringsOut[i]
		}
		destinations := []any{&row.ID, &row.Raw}
		destinations = append(destinations, values...)
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan skill Media data: %w", err)
		}
		indices := []int{0, 2, 3, 4, 5, 6, 7, 8, 9, 11, 13, 12, 14, 34, 36, 40, 41, 43, 44, 46, 47, 50, 51, 53, 54, 61, 62, 64, 65, 66}
		row.Values = make(map[int]string, len(indices)+1)
		row.Values[1] = strconv.FormatUint(row.ID, 10)
		for i, index := range indices {
			row.Values[index] = stringsOut[i]
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read skill Media data: %w", err)
	}
	if len(result) == 0 {
		return nil, errors.New("REF_SKILL_DATA contains no enabled rows")
	}
	return result, nil
}

func writeMediaExport(root string, levels []mediaExportLevel, skills []mediaExportSkill, catalogs []mediaExportCatalog) (mediaExportManifest, error) {
	textData := filepath.Join(root, filepath.FromSlash(mediaTextDataPath))
	if err := os.MkdirAll(textData, 0o750); err != nil {
		return mediaExportManifest{}, fmt.Errorf("create Media export directory: %w", err)
	}
	generatedCatalogs := make(map[string]struct{}, len(catalogs))
	for _, catalog := range catalogs {
		generatedCatalogs[strings.ToLower(catalog.Name)] = struct{}{}
	}
	var unsupported []string
	for _, name := range unsupportedMediaCatalogs {
		if _, generated := generatedCatalogs[name]; !generated {
			unsupported = append(unsupported, name)
		}
	}
	manifest := mediaExportManifest{SchemaVersion: 1, Profile: "vsro188", UnsupportedCatalogs: unsupported}
	addBytes := func(name string, rowCount int, encoded []byte) error {
		if err := os.WriteFile(filepath.Join(textData, name), encoded, 0o640); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		hash := sha256.Sum256(encoded)
		manifest.Files = append(manifest.Files, mediaExportFile{
			Path: filepath.ToSlash(filepath.Join(mediaTextDataPath, name)), Rows: rowCount, Bytes: len(encoded), SHA256: hex.EncodeToString(hash[:]),
		})
		return nil
	}
	add := func(name string, rows [][]string) error {
		encoded, err := encodeMediaRows(rows)
		if err != nil {
			return fmt.Errorf("encode %s: %w", name, err)
		}
		return addBytes(name, len(rows), encoded)
	}

	levelRows := make([][]string, 0, len(levels))
	goldRows := make([][]string, 0, len(levels))
	for _, row := range levels {
		fields, err := rawMediaFields(row.Raw, 9)
		if err != nil {
			return mediaExportManifest{}, fmt.Errorf("level %d: %w", row.Level, err)
		}
		updates := []string{strconv.FormatUint(row.Level, 10), strconv.FormatUint(row.Experience, 10), strconv.FormatUint(row.SkillPoints, 10), strconv.FormatInt(row.Unknown1, 10), strconv.FormatInt(row.Unknown2, 10), strconv.FormatInt(row.JobExperience1, 10), strconv.FormatInt(row.JobExperience2, 10), strconv.FormatInt(row.JobExperience3, 10)}
		copy(fields, updates)
		levelRows = append(levelRows, fields)
		goldRows = append(goldRows, []string{strconv.FormatUint(row.Level, 10), strconv.FormatUint(row.DropGoldMin, 10), strconv.FormatUint(row.DropGoldMax, 10)})
	}
	if err := add("leveldata.txt", levelRows); err != nil {
		return mediaExportManifest{}, err
	}
	if err := add("levelgold.txt", goldRows); err != nil {
		return mediaExportManifest{}, err
	}

	chunks := make(map[uint64][][]string)
	for _, row := range skills {
		fields, err := rawMediaFields(row.Raw, 117)
		if err != nil {
			return mediaExportManifest{}, fmt.Errorf("skill %d: %w", row.ID, err)
		}
		for index, value := range row.Values {
			fields[index] = value
		}
		upperBound := (row.ID/5000 + 1) * 5000
		chunks[upperBound] = append(chunks[upperBound], fields)
	}
	boundaries := make([]uint64, 0, len(chunks))
	for boundary := range chunks {
		boundaries = append(boundaries, boundary)
	}
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i] < boundaries[j] })
	listRows := make([][]string, 0, len(boundaries))
	for _, boundary := range boundaries {
		name := fmt.Sprintf("skilldata_%d.txt", boundary)
		encoded, err := encodeMediaRows(chunks[boundary])
		if err != nil {
			return mediaExportManifest{}, fmt.Errorf("encode %s: %w", name, err)
		}
		if err := addBytes(name, len(chunks[boundary]), encoded); err != nil {
			return mediaExportManifest{}, err
		}
		encryptedName := strings.TrimSuffix(name, ".txt") + "enc.txt"
		if err := addBytes(encryptedName, len(chunks[boundary]), encryptSkillData(encoded)); err != nil {
			return mediaExportManifest{}, err
		}
		listRows = append(listRows, []string{name})
	}
	if err := add("skilldata.txt", listRows); err != nil {
		return mediaExportManifest{}, err
	}
	for _, catalog := range catalogs {
		if err := add(catalog.Name, catalog.Rows); err != nil {
			return mediaExportManifest{}, err
		}
	}

	encodedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return mediaExportManifest{}, fmt.Errorf("encode Media export manifest: %w", err)
	}
	encodedManifest = append(encodedManifest, '\n')
	if err := os.WriteFile(filepath.Join(root, "media-export-manifest.json"), encodedManifest, 0o640); err != nil {
		return mediaExportManifest{}, fmt.Errorf("write Media export manifest: %w", err)
	}
	return manifest, nil
}

func rawMediaFields(raw string, minimum int) ([]string, error) {
	var fields []string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("decode RAW_FIELDS: %w", err)
	}
	if len(fields) < minimum {
		return nil, fmt.Errorf("RAW_FIELDS has %d fields, want at least %d", len(fields), minimum)
	}
	return fields, nil
}

func encodeMediaRows(rows [][]string) ([]byte, error) {
	var plain strings.Builder
	for _, fields := range rows {
		for _, field := range fields {
			if strings.ContainsAny(field, "\t\r\n") {
				return nil, errors.New("field contains a tab or newline")
			}
		}
		plain.WriteString(strings.Join(fields, "\t"))
		plain.WriteString("\r\n")
	}
	encoder := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder()
	encoded, err := io.ReadAll(transform.NewReader(bytes.NewBufferString(plain.String()), encoder))
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
