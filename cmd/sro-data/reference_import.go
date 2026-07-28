package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ferdoran/go-sro/framework/config"
	frameworkdb "github.com/ferdoran/go-sro/framework/db"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var errImportUsage = errors.New("usage: sro-data import-reference --media <Media directory>")

type levelReferenceRow struct {
	Level, SkillPoints int
	Experience         uint64
	Unknown1, Unknown2 int64
	JobExperience      [3]int64
	Raw                string
}

type skillReferenceRow struct {
	ID, Service, GroupID, BaseSkillID, Level, Type, NextSkillID                      uint64
	InitTime, NextTime, Cooldown, MasteryID, RequiredMasteryLevel                    uint64
	DelayTime                                                                        int64
	RequiredSkill1ID, RequiredSkill2ID, RequiredSkill1Level, RequiredSkill2Level     uint64
	RequiredSkillPoints, RequiredRace, RequiredWeapon1, RequiredWeapon2              uint64
	RequiredMP, MinHPRatio, MaxHPRatio                                               uint64
	CodeName, Name, GroupCodeName, Icon, ResourceName, Description, StudyDescription string
	Effects, Raw                                                                     string
}

func importReferenceData(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-reference", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mediaDirectory := flags.String("media", "", "path to the extracted Media directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *mediaDirectory == "" {
		return errImportUsage
	}

	textDataDirectory := filepath.Join(*mediaDirectory, "server_dep", "silkroad", "textdata")
	levels, err := loadLevelReferenceData(filepath.Join(textDataDirectory, "leveldata.txt"))
	if err != nil {
		return err
	}
	skills, err := loadSkillReferenceData(textDataDirectory)
	if err != nil {
		return err
	}

	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	if err := importReferenceRows(ctx, pool, levels, skills); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d level rows and %d enabled skill rows\n", len(levels), len(skills))
	return err
}

func loadLevelReferenceData(path string) ([]levelReferenceRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open level data %q: %w", path, err)
	}
	defer file.Close()
	buffered := bufio.NewReader(file)
	reader := io.Reader(buffered)
	if bom, err := buffered.Peek(2); err == nil && len(bom) == 2 && bom[0] == 0xFF && bom[1] == 0xFE {
		reader = transform.NewReader(buffered, unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder())
	}
	var rows []levelReferenceRow
	seen := map[int]bool{}
	scanner := bufio.NewScanner(reader)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 9 {
			return nil, fmt.Errorf("level data line %d: want at least 9 fields, got %d", lineNumber, len(fields))
		}
		level, err := positiveInt(fields[0])
		if err != nil || seen[level] {
			return nil, fmt.Errorf("level data line %d: invalid or duplicate level %q", lineNumber, fields[0])
		}
		seen[level] = true
		experience, err := positiveUint(fields[1])
		if err != nil {
			return nil, fmt.Errorf("level data line %d: experience: %w", lineNumber, err)
		}
		sp, err := positiveInt(fields[2])
		if err != nil {
			return nil, fmt.Errorf("level data line %d: skill points: %w", lineNumber, err)
		}
		unknown1, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("level data line %d: unknown_1: %w", lineNumber, err)
		}
		unknown2, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("level data line %d: unknown_2: %w", lineNumber, err)
		}
		var job [3]int64
		for i := range job {
			job[i], err = strconv.ParseInt(fields[i+5], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("level data line %d: job experience: %w", lineNumber, err)
			}
		}
		raw, _ := json.Marshal(fields)
		rows = append(rows, levelReferenceRow{level, sp, experience, unknown1, unknown2, job, string(raw)})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read level data: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("level data contains no rows")
	}
	return rows, nil
}

func loadSkillReferenceData(directory string) ([]skillReferenceRow, error) {
	list, err := readClientTextFile(filepath.Join(directory, "skilldata.txt"))
	if err != nil {
		return nil, fmt.Errorf("open skill data list: %w", err)
	}
	var rows []skillReferenceRow
	seen := map[uint64]bool{}
	for _, name := range strings.Fields(list) {
		path := filepath.Join(directory, name)
		chunk, err := readClientTextFile(path)
		if err != nil {
			return nil, fmt.Errorf("open skill data chunk %q: %w", name, err)
		}
		scanner := bufio.NewScanner(strings.NewReader(chunk))
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		for lineNumber := 1; scanner.Scan(); lineNumber++ {
			fields := strings.Split(strings.TrimSuffix(scanner.Text(), "\r"), "\t")
			if len(fields) == 1 && fields[0] == "" {
				continue
			}
			row, err := parseSkillReferenceRow(fields)
			if err != nil {
				return nil, fmt.Errorf("skill data %s line %d: %w", name, lineNumber, err)
			}
			if row.Service == 0 {
				continue
			}
			if seen[row.ID] {
				return nil, fmt.Errorf("skill data %s line %d: duplicate skill ID %d", name, lineNumber, row.ID)
			}
			seen[row.ID] = true
			rows = append(rows, row)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read skill data %q: %w", name, err)
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("skill data contains no enabled rows")
	}
	return rows, nil
}

func readClientTextFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	reader := io.Reader(bytes.NewReader(contents))
	if len(contents) >= 2 && contents[0] == 0xFF && contents[1] == 0xFE {
		reader = transform.NewReader(reader, unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder())
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func parseSkillReferenceRow(fields []string) (skillReferenceRow, error) {
	if len(fields) < 117 {
		return skillReferenceRow{}, fmt.Errorf("want at least 117 fields, got %d", len(fields))
	}
	value := func(index int) (uint64, error) { return strconv.ParseUint(strings.TrimSpace(fields[index]), 10, 64) }
	indices := []int{0, 1, 2, 6, 7, 8, 9, 11, 12, 14, 34, 36, 40, 41, 43, 44, 46, 47, 50, 51, 53, 54, 66}
	values := make([]uint64, len(indices))
	for i, index := range indices {
		var err error
		values[i], err = value(index)
		if err != nil {
			return skillReferenceRow{}, fmt.Errorf("field %d: %w", index, err)
		}
	}
	delayTime, err := strconv.ParseInt(strings.TrimSpace(fields[13]), 10, 64)
	if err != nil {
		return skillReferenceRow{}, fmt.Errorf("field 13: %w", err)
	}
	effects, _ := json.Marshal(fields[69:117])
	raw, _ := json.Marshal(fields)
	return skillReferenceRow{ID: values[1], Service: values[0], GroupID: values[2], BaseSkillID: values[3], Level: values[4], Type: values[5], NextSkillID: values[6], InitTime: values[7], DelayTime: delayTime, NextTime: values[8], Cooldown: values[9], MasteryID: values[10], RequiredMasteryLevel: values[11], RequiredSkill1ID: values[12], RequiredSkill2ID: values[13], RequiredSkill1Level: values[14], RequiredSkill2Level: values[15], RequiredSkillPoints: values[16], RequiredRace: values[17], RequiredWeapon1: values[18], RequiredWeapon2: values[19], RequiredMP: values[20], MinHPRatio: values[21], MaxHPRatio: values[22], CodeName: fields[3], Name: fields[4], GroupCodeName: fields[5], Icon: fields[61], ResourceName: fields[62], Description: fields[64], StudyDescription: fields[65], Effects: string(effects), Raw: string(raw)}, nil
}

func positiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, errors.New("must be a non-negative integer")
	}
	return parsed, nil
}
func positiveUint(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func importReferenceRows(ctx context.Context, pool *sql.DB, levels []levelReferenceRow, skills []skillReferenceRow) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reference import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	levelStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_LEVEL_DATA` (`LEVEL`,`REQUIRED_EXPERIENCE`,`REQUIRED_SKILL_POINTS`,`UNKNOWN_1`,`UNKNOWN_2`,`REQUIRED_JOB_EXPERIENCE`,`REQUIRED_JOB_EXPERIENCE_2`,`REQUIRED_JOB_EXPERIENCE_3`,`RAW_FIELDS`) VALUES (?,?,?,?,?,?,?,?,CAST(? AS JSON)) ON DUPLICATE KEY UPDATE `REQUIRED_EXPERIENCE`=VALUES(`REQUIRED_EXPERIENCE`),`REQUIRED_SKILL_POINTS`=VALUES(`REQUIRED_SKILL_POINTS`),`UNKNOWN_1`=VALUES(`UNKNOWN_1`),`UNKNOWN_2`=VALUES(`UNKNOWN_2`),`REQUIRED_JOB_EXPERIENCE`=VALUES(`REQUIRED_JOB_EXPERIENCE`),`REQUIRED_JOB_EXPERIENCE_2`=VALUES(`REQUIRED_JOB_EXPERIENCE_2`),`REQUIRED_JOB_EXPERIENCE_3`=VALUES(`REQUIRED_JOB_EXPERIENCE_3`),`RAW_FIELDS`=VALUES(`RAW_FIELDS`)")
	if err != nil {
		return fmt.Errorf("prepare level import: %w", err)
	}
	defer levelStatement.Close()
	for _, row := range levels {
		if _, err := levelStatement.ExecContext(ctx, row.Level, row.Experience, row.SkillPoints, row.Unknown1, row.Unknown2, row.JobExperience[0], row.JobExperience[1], row.JobExperience[2], row.Raw); err != nil {
			return fmt.Errorf("import level %d: %w", row.Level, err)
		}
	}
	skillStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_SKILL_DATA` (`ID`,`SERVICE`,`GROUP_ID`,`CODE_NAME`,`NAME`,`GROUP_CODE_NAME`,`BASE_SKILL_ID`,`LEVEL`,`TYPE`,`NEXT_SKILL_ID`,`INIT_TIME`,`DELAY_TIME`,`NEXT_TIME`,`COOLDOWN`,`MASTERY_ID`,`REQUIRED_MASTERY_LEVEL`,`REQUIRED_SKILL_1_ID`,`REQUIRED_SKILL_2_ID`,`REQUIRED_SKILL_1_LEVEL`,`REQUIRED_SKILL_2_LEVEL`,`REQUIRED_SKILL_POINTS`,`REQUIRED_RACE`,`REQUIRED_WEAPON_1`,`REQUIRED_WEAPON_2`,`REQUIRED_MP`,`MIN_HP_RATIO`,`ICON`,`RESOURCE_NAME`,`DESCRIPTION`,`STUDY_DESCRIPTION`,`MAX_HP_RATIO`,`EFFECTS`,`RAW_FIELDS`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CAST(? AS JSON),CAST(? AS JSON)) ON DUPLICATE KEY UPDATE `SERVICE`=VALUES(`SERVICE`),`GROUP_ID`=VALUES(`GROUP_ID`),`CODE_NAME`=VALUES(`CODE_NAME`),`NAME`=VALUES(`NAME`),`GROUP_CODE_NAME`=VALUES(`GROUP_CODE_NAME`),`BASE_SKILL_ID`=VALUES(`BASE_SKILL_ID`),`LEVEL`=VALUES(`LEVEL`),`TYPE`=VALUES(`TYPE`),`NEXT_SKILL_ID`=VALUES(`NEXT_SKILL_ID`),`INIT_TIME`=VALUES(`INIT_TIME`),`DELAY_TIME`=VALUES(`DELAY_TIME`),`NEXT_TIME`=VALUES(`NEXT_TIME`),`COOLDOWN`=VALUES(`COOLDOWN`),`MASTERY_ID`=VALUES(`MASTERY_ID`),`REQUIRED_MASTERY_LEVEL`=VALUES(`REQUIRED_MASTERY_LEVEL`),`REQUIRED_SKILL_1_ID`=VALUES(`REQUIRED_SKILL_1_ID`),`REQUIRED_SKILL_2_ID`=VALUES(`REQUIRED_SKILL_2_ID`),`REQUIRED_SKILL_1_LEVEL`=VALUES(`REQUIRED_SKILL_1_LEVEL`),`REQUIRED_SKILL_2_LEVEL`=VALUES(`REQUIRED_SKILL_2_LEVEL`),`REQUIRED_SKILL_POINTS`=VALUES(`REQUIRED_SKILL_POINTS`),`REQUIRED_RACE`=VALUES(`REQUIRED_RACE`),`REQUIRED_WEAPON_1`=VALUES(`REQUIRED_WEAPON_1`),`REQUIRED_WEAPON_2`=VALUES(`REQUIRED_WEAPON_2`),`REQUIRED_MP`=VALUES(`REQUIRED_MP`),`MIN_HP_RATIO`=VALUES(`MIN_HP_RATIO`),`ICON`=VALUES(`ICON`),`RESOURCE_NAME`=VALUES(`RESOURCE_NAME`),`DESCRIPTION`=VALUES(`DESCRIPTION`),`STUDY_DESCRIPTION`=VALUES(`STUDY_DESCRIPTION`),`MAX_HP_RATIO`=VALUES(`MAX_HP_RATIO`),`EFFECTS`=VALUES(`EFFECTS`),`RAW_FIELDS`=VALUES(`RAW_FIELDS`)")
	if err != nil {
		return fmt.Errorf("prepare skill import: %w", err)
	}
	defer skillStatement.Close()
	for _, r := range skills {
		_, err := skillStatement.ExecContext(ctx, r.ID, r.Service, r.GroupID, r.CodeName, r.Name, r.GroupCodeName, r.BaseSkillID, r.Level, r.Type, r.NextSkillID, r.InitTime, r.DelayTime, r.NextTime, r.Cooldown, r.MasteryID, r.RequiredMasteryLevel, r.RequiredSkill1ID, r.RequiredSkill2ID, r.RequiredSkill1Level, r.RequiredSkill2Level, r.RequiredSkillPoints, r.RequiredRace, r.RequiredWeapon1, r.RequiredWeapon2, r.RequiredMP, r.MinHPRatio, r.Icon, r.ResourceName, r.Description, r.StudyDescription, r.MaxHPRatio, r.Effects, r.Raw)
		if err != nil {
			return fmt.Errorf("import skill %d: %w", r.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reference import: %w", err)
	}
	return nil
}
