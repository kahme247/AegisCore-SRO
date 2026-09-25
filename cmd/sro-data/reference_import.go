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
	"sort"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var errImportUsage = errors.New("usage: sro-data import-reference --media <Media directory>")

type levelReferenceRow struct {
	Level, SkillPoints int
	Experience         uint64
	DropGoldMin        uint64
	DropGoldMax        uint64
	Unknown1, Unknown2 int64
	JobActivityFactor  uint64
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

type magicOptionReferenceRow struct {
	ID            uint64
	CodeName      string
	Level         uint64
	DropRatio     float64
	ParamType     uint64
	ParamValue    uint64
	Applicability []magicOptionApplicabilityRow
}

type magicOptionApplicabilityRow struct {
	Slot, MinItemClass uint64
	ItemFamily         string
}

type dropMagicOptionAssignmentRow struct {
	Service, Country, TypeID3, TypeID4, Slot uint64
	CodeName                                 string
}

type gameWorldReferenceRow struct {
	WorldID, WorldType, MaxInstanceCount, MaxUserCount uint64
	WorldEntryType, WorldEntranceType, WorldLeaveType  uint64
	WorldDurationMS, WorldEmptyRemainMS                uint64
	CodeName, ConfigGroupCodeName                      string
}

type gameWorldRegionReferenceRow struct {
	WorldID  uint16
	RegionID int16
}

type instanceWorldStartPositionReferenceRow struct {
	WorldID, StartIndex uint16
	RegionID            int16
	X, Y, Z, Param      int32
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
	dropGold, err := loadDropGoldReferenceData(filepath.Join(textDataDirectory, "levelgold.txt"))
	if err != nil {
		return err
	}
	if err := mergeDropGoldReferenceData(levels, dropGold); err != nil {
		return err
	}
	skills, err := loadSkillReferenceData(textDataDirectory)
	if err != nil {
		return err
	}
	magicOptions, err := loadMagicOptionReferenceData(filepath.Join(textDataDirectory, "magicoption.txt"))
	if err != nil {
		return err
	}
	dropMagicAssignments, err := loadDropMagicOptionAssignments(filepath.Join(textDataDirectory, "magicoptionassign.txt"))
	if err != nil {
		return err
	}
	gameWorlds, err := loadGameWorldReferenceData(filepath.Join(textDataDirectory, "gameworlddata.txt"))
	if err != nil {
		return err
	}
	gameWorldRegions, err := loadDimensionHoleWorldRegionReferenceData(filepath.Join(textDataDirectory, "refregion.txt"))
	if err != nil {
		return err
	}
	eventRewards, eventRewardItems, err := loadEventRewardReferenceData(textDataDirectory)
	if err != nil {
		return err
	}

	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	dimensionHoleRepository, err := model.NewDimensionHoleCatalogRepository(pool)
	if err != nil {
		return fmt.Errorf("configure Dimension Hole catalog repository: %w", err)
	}
	if _, err = dimensionHoleRepository.LoadDimensionHoleCatalog(ctx); err != nil {
		return fmt.Errorf("load Dimension Hole catalog: %w", err)
	}
	startPositions := dimensionHoleStockStartPositions()
	if err := importReferenceRows(ctx, pool, levels, skills, magicOptions, dropMagicAssignments, gameWorlds, gameWorldRegions, startPositions); err != nil {
		return err
	}
	if err := importEventRewardRows(ctx, pool, eventRewards, eventRewardItems); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d level rows, %d enabled skill rows, %d magic-option rows, %d drop magic-option assignments, %d game-world rows, %d Dimension Hole world-region rows, %d Dimension Hole start positions, %d event rewards, and %d event reward items\n", len(levels), len(skills), len(magicOptions), len(dropMagicAssignments), len(gameWorlds), len(gameWorldRegions), len(startPositions), len(eventRewards), len(eventRewardItems))
	return err
}

// dimensionHoleStockStartPositions is the exact vSRO 1.188
// _RefInstance_World_Start_Pos subset for worlds 63-84. The stock server
// selects uniformly from all rows for a world; this profile contains one each.
func dimensionHoleStockStartPositions() []instanceWorldStartPositionReferenceRow {
	rows := make([]instanceWorldStartPositionReferenceRow, 0, len(model.DimensionHoleCatalog()))
	for _, definition := range model.DimensionHoleCatalog() {
		row := instanceWorldStartPositionReferenceRow{WorldID: definition.WorldID}
		switch definition.WorldID {
		case 63, 64, 65, 66, 67, 68:
			row.RegionID, row.X, row.Y, row.Z = 32484, 1063, 693, 1125
		case 69, 70, 71, 72, 73, 74, 75, 76:
			row.RegionID, row.X, row.Y, row.Z = -32750, 10897, 165, -6110
		case 77, 78, 81, 82:
			row.RegionID, row.X, row.Y, row.Z = 29427, 264, -770, 1745
		case 79, 80, 83, 84:
			row.RegionID, row.X, row.Y, row.Z = 28644, 1257, -247, 1771
		}
		rows = append(rows, row)
	}
	return rows
}

func loadGameWorldReferenceData(path string) ([]gameWorldReferenceRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("open game-world data %q: %w", path, err)
	}
	var rows []gameWorldReferenceRow
	seenIDs := make(map[uint64]bool)
	seenCodes := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 6 {
			return nil, fmt.Errorf("game-world line %d: want 6 fields, got %d", lineNumber, len(fields))
		}
		worldID, idErr := positiveUint(fields[0])
		worldType, typeErr := positiveUint(fields[2])
		maxInstances, instancesErr := positiveUint(fields[3])
		maxUsers, usersErr := positiveUint(fields[4])
		if err := errors.Join(idErr, typeErr, instancesErr, usersErr); err != nil || worldID == 0 || worldID >= 0xffff || maxInstances == 0 || maxInstances >= 0xffff || seenIDs[worldID] || seenCodes[fields[1]] {
			return nil, fmt.Errorf("game-world line %d has invalid or duplicate identity: %w", lineNumber, err)
		}
		seenIDs[worldID], seenCodes[fields[1]] = true, true
		row := gameWorldReferenceRow{WorldID: worldID, CodeName: fields[1], WorldType: worldType, MaxInstanceCount: maxInstances, MaxUserCount: maxUsers, ConfigGroupCodeName: fields[5]}
		if worldID <= uint64(^uint16(0)) {
			if _, dimensionHole := model.DimensionHoleDefinitionForWorld(uint16(worldID)); dimensionHole {
				row.WorldEntryType = uint64(model.DimensionHoleWorldEntryType)
				row.WorldEntranceType = uint64(model.DimensionHoleWorldEntranceType)
				row.WorldLeaveType = uint64(model.DimensionHoleWorldLeaveType)
				row.WorldDurationMS = uint64(model.DimensionHoleWorldDurationMS)
				row.WorldEmptyRemainMS = uint64(model.DimensionHoleWorldEmptyRemainMS)
			}
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read game-world data %q: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, errors.New("game-world data contains no rows")
	}
	return rows, nil
}

func loadDimensionHoleWorldRegionReferenceData(path string) ([]gameWorldRegionReferenceRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("open region data %q: %w", path, err)
	}
	const (
		toguiRegions = 48
		flameRegions = 1
		wreckRegions = 224
	)
	wantedCounts := map[string]int{
		"GOD_TOGUI":     toguiRegions,
		"GOD_FLAME":     flameRegions,
		"GOD_WRECK_IN":  wreckRegions,
		"GOD_WRECK_OUT": wreckRegions,
	}
	regions := make(map[string][]int16, len(wantedCounts))
	seen := make(map[string]map[int16]bool, len(wantedCounts))
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 6 {
			continue
		}
		for index := range fields {
			fields[index] = strings.TrimSpace(fields[index])
		}
		family := strings.ToUpper(fields[3])
		if _, wanted := wantedCounts[family]; !wanted {
			continue
		}
		service, serviceErr := positiveUint(fields[5])
		region, regionErr := strconv.ParseInt(fields[0], 10, 16)
		if serviceErr != nil || regionErr != nil || service != 1 || region == 0 {
			return nil, fmt.Errorf("region line %d has invalid Dimension Hole row: %w", lineNumber, errors.Join(serviceErr, regionErr))
		}
		regionID := int16(region)
		if seen[family] == nil {
			seen[family] = make(map[int16]bool)
		}
		if seen[family][regionID] {
			return nil, fmt.Errorf("region line %d duplicates Dimension Hole family %s region %d", lineNumber, family, regionID)
		}
		seen[family][regionID] = true
		regions[family] = append(regions[family], regionID)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read region data %q: %w", path, err)
	}
	for family, want := range wantedCounts {
		if got := len(regions[family]); got != want {
			return nil, fmt.Errorf("region data family %s has %d rows, want %d", family, got, want)
		}
		sort.Slice(regions[family], func(i, j int) bool { return regions[family][i] < regions[family][j] })
	}

	definitions := model.DimensionHoleCatalog()
	capacity := 0
	for _, definition := range definitions {
		family, ok := dimensionHoleRegionFamily(definition.WorldCodeName)
		if !ok {
			return nil, fmt.Errorf("Dimension Hole world %d has unsupported region family %q", definition.WorldID, definition.WorldCodeName)
		}
		capacity += len(regions[family])
	}
	rows := make([]gameWorldRegionReferenceRow, 0, capacity)
	for _, definition := range definitions {
		family, ok := dimensionHoleRegionFamily(definition.WorldCodeName)
		if !ok {
			return nil, fmt.Errorf("Dimension Hole world %d has unsupported region family %q", definition.WorldID, definition.WorldCodeName)
		}
		for _, regionID := range regions[family] {
			rows = append(rows, gameWorldRegionReferenceRow{WorldID: definition.WorldID, RegionID: regionID})
		}
	}
	return rows, nil
}

func dimensionHoleRegionFamily(worldCodeName string) (string, bool) {
	for _, family := range []string{"GOD_TOGUI", "GOD_FLAME", "GOD_WRECK_IN", "GOD_WRECK_OUT"} {
		if strings.HasPrefix(worldCodeName, "INS_"+family+"_") {
			return family, true
		}
	}
	return "", false
}

func loadMagicOptionReferenceData(path string) ([]magicOptionReferenceRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open magic option data %q: %w", path, err)
	}
	defer file.Close()
	buffered := bufio.NewReader(file)
	reader := io.Reader(buffered)
	if prefix, peekErr := buffered.Peek(4); peekErr == nil && (bytes.Contains(prefix, []byte{0}) || (prefix[0] == 0xFF && prefix[1] == 0xFE)) {
		reader = transform.NewReader(buffered, unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder())
	}
	var rows []magicOptionReferenceRow
	seen := make(map[uint64]bool)
	scanner := bufio.NewScanner(reader)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] == "0" {
			continue
		}
		if len(fields) < 49 {
			return nil, fmt.Errorf("magic option line %d: want at least 49 fields, got %d", lineNumber, len(fields))
		}
		id, idErr := positiveUint(fields[1])
		level, levelErr := positiveUint(fields[4])
		paramType, typeErr := positiveUint(fields[7])
		paramValue, valueErr := positiveUint(fields[8])
		dropRatio, ratioErr := strconv.ParseFloat(fields[5], 64)
		if err := errors.Join(idErr, levelErr, typeErr, valueErr, ratioErr); err != nil || id == 0 || level == 0 || dropRatio < 0 || seen[id] {
			return nil, fmt.Errorf("magic option line %d has invalid or duplicate identity: %w", lineNumber, err)
		}
		seen[id] = true
		row := magicOptionReferenceRow{ID: id, CodeName: fields[2], Level: level, DropRatio: dropRatio, ParamType: paramType, ParamValue: paramValue}
		for slot, index := 0, 29; index+1 < len(fields) && slot < 10; slot, index = slot+1, index+2 {
			if strings.EqualFold(fields[index], "xxx") {
				continue
			}
			minimum, minimumErr := positiveUint(fields[index+1])
			if minimumErr != nil {
				return nil, fmt.Errorf("magic option line %d applicability slot %d: %w", lineNumber, slot, minimumErr)
			}
			row.Applicability = append(row.Applicability, magicOptionApplicabilityRow{Slot: uint64(slot), ItemFamily: strings.ToLower(fields[index]), MinItemClass: minimum})
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read magic option data %q: %w", path, err)
	}
	return rows, nil
}

func loadDropMagicOptionAssignments(path string) ([]dropMagicOptionAssignmentRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("open drop magic-option assignments %q: %w", path, err)
	}
	var rows []dropMagicOptionAssignmentRow
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] == "0" {
			continue
		}
		if len(fields) != 29 {
			return nil, fmt.Errorf("drop magic-option assignment line %d: want 29 fields, got %d", lineNumber, len(fields))
		}
		service, serviceErr := positiveUint(fields[0])
		country, countryErr := positiveUint(fields[1])
		typeID3, type3Err := positiveUint(fields[2])
		typeID4, type4Err := positiveUint(fields[3])
		if err := errors.Join(serviceErr, countryErr, type3Err, type4Err); err != nil || service > 1 || country > 3 || typeID3 > 255 || typeID4 > 255 {
			return nil, fmt.Errorf("drop magic-option assignment line %d has invalid key: %w", lineNumber, err)
		}
		for slot, code := range fields[4:] {
			if strings.EqualFold(code, "xxx") {
				continue
			}
			key := fmt.Sprintf("%d/%d/%d/%d", country, typeID3, typeID4, slot)
			if code == "" || seen[key] {
				return nil, fmt.Errorf("drop magic-option assignment line %d has invalid or duplicate slot %d", lineNumber, slot)
			}
			seen[key] = true
			rows = append(rows, dropMagicOptionAssignmentRow{Service: service, Country: country, TypeID3: typeID3, TypeID4: typeID4, Slot: uint64(slot), CodeName: code})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read drop magic-option assignments %q: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, errors.New("drop magic-option assignments contain no enabled rows")
	}
	return rows, nil
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
		activityFactor, err := positiveUint(fields[5])
		if err != nil {
			return nil, fmt.Errorf("level data line %d: job activity factor: %w", lineNumber, err)
		}
		var job [3]int64
		for i := range job {
			job[i], err = strconv.ParseInt(fields[i+6], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("level data line %d: job experience: %w", lineNumber, err)
			}
		}
		raw, _ := json.Marshal(fields)
		rows = append(rows, levelReferenceRow{
			Level: level, SkillPoints: sp, Experience: experience,
			Unknown1: unknown1, Unknown2: unknown2, JobActivityFactor: activityFactor, JobExperience: job, Raw: string(raw),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read level data: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("level data contains no rows")
	}
	return rows, nil
}

type dropGoldReferenceRow struct {
	Level int
	Min   uint64
	Max   uint64
}

func loadDropGoldReferenceData(path string) (map[int]dropGoldReferenceRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("open level gold data %q: %w", path, err)
	}
	rows := make(map[int]dropGoldReferenceRow)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("level gold line %d: want 3 fields, got %d", lineNumber, len(fields))
		}
		level, levelErr := positiveInt(fields[0])
		minimum, minimumErr := positiveUint(fields[1])
		maximum, maximumErr := positiveUint(fields[2])
		if err := errors.Join(levelErr, minimumErr, maximumErr); err != nil || level == 0 || minimum == 0 || maximum < minimum || rows[level].Level != 0 {
			return nil, fmt.Errorf("level gold line %d is invalid or duplicated: %w", lineNumber, err)
		}
		rows[level] = dropGoldReferenceRow{Level: level, Min: minimum, Max: maximum}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read level gold data %q: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, errors.New("level gold data contains no rows")
	}
	return rows, nil
}

func mergeDropGoldReferenceData(levels []levelReferenceRow, dropGold map[int]dropGoldReferenceRow) error {
	if len(levels) != len(dropGold) {
		return fmt.Errorf("level/drop-gold row count mismatch: %d/%d", len(levels), len(dropGold))
	}
	for index := range levels {
		gold, exists := dropGold[levels[index].Level]
		if !exists {
			return fmt.Errorf("level %d has no drop-gold row", levels[index].Level)
		}
		levels[index].DropGoldMin = gold.Min
		levels[index].DropGoldMax = gold.Max
	}
	return nil
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

func importReferenceRows(ctx context.Context, pool *sql.DB, levels []levelReferenceRow, skills []skillReferenceRow, magicOptions []magicOptionReferenceRow, dropMagicAssignments []dropMagicOptionAssignmentRow, gameWorlds []gameWorldReferenceRow, gameWorldRegions []gameWorldRegionReferenceRow, startPositions []instanceWorldStartPositionReferenceRow) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reference import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	levelStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_LEVEL_DATA` (`LEVEL`,`REQUIRED_EXPERIENCE`,`REQUIRED_SKILL_POINTS`,`DROP_GOLD_MIN`,`DROP_GOLD_MAX`,`UNKNOWN_1`,`UNKNOWN_2`,`JOB_ACTIVITY_FACTOR`,`REQUIRED_JOB_EXPERIENCE`,`REQUIRED_JOB_EXPERIENCE_2`,`REQUIRED_JOB_EXPERIENCE_3`,`RAW_FIELDS`) VALUES (?,?,?,?,?,?,?,?,?,?,?,CAST(? AS JSON)) ON DUPLICATE KEY UPDATE `REQUIRED_EXPERIENCE`=VALUES(`REQUIRED_EXPERIENCE`),`REQUIRED_SKILL_POINTS`=VALUES(`REQUIRED_SKILL_POINTS`),`DROP_GOLD_MIN`=VALUES(`DROP_GOLD_MIN`),`DROP_GOLD_MAX`=VALUES(`DROP_GOLD_MAX`),`UNKNOWN_1`=VALUES(`UNKNOWN_1`),`UNKNOWN_2`=VALUES(`UNKNOWN_2`),`JOB_ACTIVITY_FACTOR`=VALUES(`JOB_ACTIVITY_FACTOR`),`REQUIRED_JOB_EXPERIENCE`=VALUES(`REQUIRED_JOB_EXPERIENCE`),`REQUIRED_JOB_EXPERIENCE_2`=VALUES(`REQUIRED_JOB_EXPERIENCE_2`),`REQUIRED_JOB_EXPERIENCE_3`=VALUES(`REQUIRED_JOB_EXPERIENCE_3`),`RAW_FIELDS`=VALUES(`RAW_FIELDS`)")
	if err != nil {
		return fmt.Errorf("prepare level import: %w", err)
	}
	defer levelStatement.Close()
	for _, row := range levels {
		if _, err := levelStatement.ExecContext(ctx, row.Level, row.Experience, row.SkillPoints, row.DropGoldMin, row.DropGoldMax, row.Unknown1, row.Unknown2, row.JobActivityFactor, row.JobExperience[0], row.JobExperience[1], row.JobExperience[2], row.Raw); err != nil {
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
	magicOptionStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_MAGIC_OPTION` (`ID`,`CODE_NAME`,`OPTION_LEVEL`,`DROP_RATIO`,`PARAM_TYPE`,`PARAM_VALUE`) VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `CODE_NAME`=VALUES(`CODE_NAME`),`OPTION_LEVEL`=VALUES(`OPTION_LEVEL`),`DROP_RATIO`=VALUES(`DROP_RATIO`),`PARAM_TYPE`=VALUES(`PARAM_TYPE`),`PARAM_VALUE`=VALUES(`PARAM_VALUE`)")
	if err != nil {
		return fmt.Errorf("prepare magic option import: %w", err)
	}
	defer magicOptionStatement.Close()
	for _, option := range magicOptions {
		if _, err := magicOptionStatement.ExecContext(ctx, option.ID, option.CodeName, option.Level, option.DropRatio, option.ParamType, option.ParamValue); err != nil {
			return fmt.Errorf("import magic option %d: %w", option.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM `REF_MAGIC_OPTION_APPLICABILITY`"); err != nil {
		return fmt.Errorf("clear magic-option applicability: %w", err)
	}
	applicabilityStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_MAGIC_OPTION_APPLICABILITY` (`REF_MAGIC_OPTION_ID`,`SLOT`,`ITEM_FAMILY`,`MIN_ITEM_CLASS`) VALUES (?,?,?,?)")
	if err != nil {
		return fmt.Errorf("prepare magic-option applicability import: %w", err)
	}
	defer applicabilityStatement.Close()
	for _, option := range magicOptions {
		for _, applicability := range option.Applicability {
			if _, err := applicabilityStatement.ExecContext(ctx, option.ID, applicability.Slot, applicability.ItemFamily, applicability.MinItemClass); err != nil {
				return fmt.Errorf("import magic-option %d applicability slot %d: %w", option.ID, applicability.Slot, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM `REF_DROP_MAGIC_OPTION_ASSIGN`"); err != nil {
		return fmt.Errorf("clear drop magic-option assignments: %w", err)
	}
	dropMagicStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_DROP_MAGIC_OPTION_ASSIGN` (`SERVICE`,`COUNTRY`,`TYPE_ID3`,`TYPE_ID4`,`SLOT`,`OPTION_CODE_NAME`) VALUES (?,?,?,?,?,?)")
	if err != nil {
		return fmt.Errorf("prepare drop magic-option assignment import: %w", err)
	}
	defer dropMagicStatement.Close()
	for _, assignment := range dropMagicAssignments {
		if _, err := dropMagicStatement.ExecContext(ctx, assignment.Service, assignment.Country, assignment.TypeID3, assignment.TypeID4, assignment.Slot, assignment.CodeName); err != nil {
			return fmt.Errorf("import drop magic-option assignment %d/%d/%d/%d: %w", assignment.Country, assignment.TypeID3, assignment.TypeID4, assignment.Slot, err)
		}
	}
	gameWorldStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_GAME_WORLD` (`WORLD_ID`,`CODE_NAME`,`WORLD_TYPE`,`MAX_INSTANCE_COUNT`,`MAX_USER_COUNT`,`WORLD_ENTRY_TYPE`,`WORLD_ENTRANCE_TYPE`,`WORLD_LEAVE_TYPE`,`WORLD_DURATION_MS`,`WORLD_EMPTY_REMAIN_MS`,`CONFIG_GROUP_CODE_NAME`) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `CODE_NAME`=VALUES(`CODE_NAME`),`WORLD_TYPE`=VALUES(`WORLD_TYPE`),`MAX_INSTANCE_COUNT`=VALUES(`MAX_INSTANCE_COUNT`),`MAX_USER_COUNT`=VALUES(`MAX_USER_COUNT`),`WORLD_ENTRY_TYPE`=VALUES(`WORLD_ENTRY_TYPE`),`WORLD_ENTRANCE_TYPE`=VALUES(`WORLD_ENTRANCE_TYPE`),`WORLD_LEAVE_TYPE`=VALUES(`WORLD_LEAVE_TYPE`),`WORLD_DURATION_MS`=VALUES(`WORLD_DURATION_MS`),`WORLD_EMPTY_REMAIN_MS`=VALUES(`WORLD_EMPTY_REMAIN_MS`),`CONFIG_GROUP_CODE_NAME`=VALUES(`CONFIG_GROUP_CODE_NAME`)")
	if err != nil {
		return fmt.Errorf("prepare game-world import: %w", err)
	}
	defer gameWorldStatement.Close()
	for _, world := range gameWorlds {
		if _, err := gameWorldStatement.ExecContext(ctx, world.WorldID, world.CodeName, world.WorldType, world.MaxInstanceCount, world.MaxUserCount, world.WorldEntryType, world.WorldEntranceType, world.WorldLeaveType, world.WorldDurationMS, world.WorldEmptyRemainMS, world.ConfigGroupCodeName); err != nil {
			return fmt.Errorf("import game world %d: %w", world.WorldID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM `REF_GAME_WORLD_REGION` WHERE `WORLD_ID` BETWEEN ? AND ?", model.DimensionHoleFirstWorldID, model.DimensionHoleLastWorldID); err != nil {
		return fmt.Errorf("clear Dimension Hole world regions: %w", err)
	}
	gameWorldRegionStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_GAME_WORLD_REGION` (`WORLD_ID`,`REGION_ID`) VALUES (?,?)")
	if err != nil {
		return fmt.Errorf("prepare Dimension Hole world-region import: %w", err)
	}
	defer gameWorldRegionStatement.Close()
	for _, row := range gameWorldRegions {
		if _, err := gameWorldRegionStatement.ExecContext(ctx, row.WorldID, row.RegionID); err != nil {
			return fmt.Errorf("import Dimension Hole world %d region %d: %w", row.WorldID, row.RegionID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM `REF_INSTANCE_WORLD_START_POSITION` WHERE `WORLD_ID` BETWEEN ? AND ?", model.DimensionHoleFirstWorldID, model.DimensionHoleLastWorldID); err != nil {
		return fmt.Errorf("clear Dimension Hole start positions: %w", err)
	}
	startPositionStatement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_INSTANCE_WORLD_START_POSITION` (`WORLD_ID`,`START_INDEX`,`REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`PARAM_VALUE`) VALUES (?,?,?,?,?,?,?)")
	if err != nil {
		return fmt.Errorf("prepare Dimension Hole start-position import: %w", err)
	}
	defer startPositionStatement.Close()
	for _, row := range startPositions {
		if _, err := startPositionStatement.ExecContext(ctx, row.WorldID, row.StartIndex, row.RegionID, row.X, row.Y, row.Z, row.Param); err != nil {
			return fmt.Errorf("import Dimension Hole world %d start %d: %w", row.WorldID, row.StartIndex, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reference import: %w", err)
	}
	return nil
}
