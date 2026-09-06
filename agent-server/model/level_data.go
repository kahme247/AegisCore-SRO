package model

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var (
	ErrLevelDataEmpty      = errors.New("level data contains no rows")
	ErrLevelDataMalformed  = errors.New("malformed level data row")
	ErrLevelDataDuplicate  = errors.New("duplicate level data row")
	ErrLevelDataNotPresent = errors.New("level data is unavailable")
)

// LevelDataRow is the portion of the client LevelData.txt contract needed by
// server-authoritative progression. RequiredExperience and
// RequiredSkillPoints are the costs to advance from the level in the row.
type LevelDataRow struct {
	Level               int
	RequiredExperience  uint64
	RequiredSkillPoints uint64
	DropGoldMin         uint64
	DropGoldMax         uint64
}

// LevelTable is loaded from the client-matched reference rows stored in the
// shard database. Its values must never be replaced with a server-side guessed
// curve.
type LevelTable struct {
	levels map[int]LevelDataRow
	max    int
}

func LoadLevelTable(reader io.Reader) (*LevelTable, error) {
	if reader == nil {
		return nil, ErrLevelDataNotPresent
	}

	buffered := bufio.NewReader(reader)
	input := io.Reader(buffered)
	if bom, err := buffered.Peek(2); err == nil && len(bom) == 2 && bom[0] == 0xFF && bom[1] == 0xFE {
		input = transform.NewReader(buffered, unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder())
	}

	var rows []LevelDataRow
	scanner := bufio.NewScanner(input)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			return nil, fmt.Errorf("%w at line %d: want at least 6 columns", ErrLevelDataMalformed, lineNumber)
		}
		level, err := strconv.Atoi(fields[0])
		if err != nil || level <= 0 {
			return nil, fmt.Errorf("%w at line %d: level", ErrLevelDataMalformed, lineNumber)
		}
		requiredExperience, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w at line %d: required experience", ErrLevelDataMalformed, lineNumber)
		}
		requiredSkillPoints, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w at line %d: required skill points", ErrLevelDataMalformed, lineNumber)
		}
		rows = append(rows, LevelDataRow{
			Level:               level,
			RequiredExperience:  requiredExperience,
			RequiredSkillPoints: requiredSkillPoints,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read level data: %w", err)
	}
	return newLevelTable(rows)
}

func newLevelTable(rows []LevelDataRow) (*LevelTable, error) {
	if len(rows) == 0 {
		return nil, ErrLevelDataEmpty
	}
	table := &LevelTable{levels: make(map[int]LevelDataRow, len(rows))}
	for _, row := range rows {
		if row.Level <= 0 {
			return nil, ErrLevelDataMalformed
		}
		if _, exists := table.levels[row.Level]; exists {
			return nil, fmt.Errorf("%w: level %d", ErrLevelDataDuplicate, row.Level)
		}
		table.levels[row.Level] = row
		if row.Level > table.max {
			table.max = row.Level
		}
	}
	for level := 1; level <= table.max; level++ {
		if _, exists := table.levels[level]; !exists {
			return nil, fmt.Errorf("%w: missing level %d", ErrLevelDataMalformed, level)
		}
	}
	return table, nil
}

func (t *LevelTable) Level(level int) (LevelDataRow, bool) {
	if t == nil {
		return LevelDataRow{}, false
	}
	row, ok := t.levels[level]
	return row, ok
}

func (t *LevelTable) MaxLevel() int {
	if t == nil {
		return 0
	}
	return t.max
}
