package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestDimensionHoleStockStartPositionsMatchPinnedSQLReference(t *testing.T) {
	startPositions := dimensionHoleStockStartPositions()
	if got, want := len(startPositions), len(model.DimensionHoleCatalog()); got != want {
		t.Fatalf("Dimension Hole start-position rows = %d, want %d", got, want)
	}
	startsByWorld := make(map[uint16]instanceWorldStartPositionReferenceRow, len(startPositions))
	for _, row := range startPositions {
		if row.RegionID == 0 || row.Param != 0 {
			t.Fatalf("invalid Dimension Hole start position %+v", row)
		}
		if _, duplicate := startsByWorld[row.WorldID]; duplicate {
			t.Fatalf("duplicate Dimension Hole start position for world %d", row.WorldID)
		}
		startsByWorld[row.WorldID] = row
	}
	for _, want := range []instanceWorldStartPositionReferenceRow{
		{WorldID: 63, RegionID: 32484, X: 1063, Y: 693, Z: 1125},
		{WorldID: 69, RegionID: -32750, X: 10897, Y: 165, Z: -6110},
		{WorldID: 77, RegionID: 29427, X: 264, Y: -770, Z: 1745},
		{WorldID: 84, RegionID: 28644, X: 1257, Y: -247, Z: 1771},
	} {
		if got := startsByWorld[want.WorldID]; got != want {
			t.Fatalf("Dimension Hole start position world %d = %+v, want %+v", want.WorldID, got, want)
		}
	}
}

func TestLoadReferenceDataFromLocalMedia(t *testing.T) {
	textData := filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata")
	if _, err := os.Stat(textData); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Skip("local Media reference tree is unavailable")
		}
		t.Fatalf("stat local Media reference tree: %v", err)
	}
	levels, err := loadLevelReferenceData(filepath.Join(textData, "leveldata.txt"))
	if err != nil {
		t.Fatalf("loadLevelReferenceData() error = %v", err)
	}
	if got, want := len(levels), 140; got != want {
		t.Fatalf("level rows = %d, want %d", got, want)
	}
	if levels[0].Level != 1 || levels[0].Experience != 118 || levels[0].SkillPoints != 1 {
		t.Fatalf("first level row = %+v, want level 1 / XP 118 / SP 1", levels[0])
	}
	if levels[0].JobActivityFactor != 24 || levels[0].JobExperience != [3]int64{70875, 70875, 70875} {
		t.Fatalf("first level job fields = %+v", levels[0])
	}
	gameWorlds, err := loadGameWorldReferenceData(filepath.Join(textData, "gameworlddata.txt"))
	if err != nil {
		t.Fatalf("loadGameWorldReferenceData() error = %v", err)
	}
	if len(gameWorlds) < 27 || gameWorlds[9] != (gameWorldReferenceRow{WorldID: 10, CodeName: "INS_QNO_RM_OLDWOMAN_6", WorldType: 1, MaxInstanceCount: 100, MaxUserCount: 8, ConfigGroupCodeName: "xxx"}) {
		t.Fatalf("game-world rows=%d old-woman=%+v", len(gameWorlds), gameWorlds[9])
	}
	gameWorldsByID := make(map[uint64]gameWorldReferenceRow, len(gameWorlds))
	for _, world := range gameWorlds {
		gameWorldsByID[world.WorldID] = world
	}
	for _, definition := range model.DimensionHoleCatalog() {
		world, ok := gameWorldsByID[uint64(definition.WorldID)]
		if !ok {
			t.Fatalf("Dimension Hole world %d (%s) is absent from gameworlddata.txt", definition.WorldID, definition.WorldCodeName)
		}
		wantGroup := "GROUP_" + strings.TrimPrefix(definition.WorldCodeName, "INS_")
		if world.CodeName != definition.WorldCodeName || world.WorldType != 1 || world.MaxInstanceCount != 50 ||
			world.MaxUserCount != uint64(definition.Capacity) || world.WorldEntryType != uint64(model.DimensionHoleWorldEntryType) ||
			world.WorldEntranceType != uint64(model.DimensionHoleWorldEntranceType) || world.WorldLeaveType != uint64(model.DimensionHoleWorldLeaveType) ||
			world.WorldDurationMS != uint64(model.DimensionHoleWorldDurationMS) || world.WorldEmptyRemainMS != uint64(model.DimensionHoleWorldEmptyRemainMS) ||
			world.ConfigGroupCodeName != wantGroup {
			t.Fatalf("Dimension Hole world %d = %+v, want code=%s type=1 max-instances=50 capacity=%d group=%s", definition.WorldID, world, definition.WorldCodeName, definition.Capacity, wantGroup)
		}
	}
	worldRegions, err := loadDimensionHoleWorldRegionReferenceData(filepath.Join(textData, "refregion.txt"))
	if err != nil {
		t.Fatalf("loadDimensionHoleWorldRegionReferenceData() error = %v", err)
	}
	if got, want := len(worldRegions), 2088; got != want {
		t.Fatalf("Dimension Hole world-region rows = %d, want %d", got, want)
	}
	regionsByWorld := make(map[uint16]map[int16]bool)
	for _, row := range worldRegions {
		if regionsByWorld[row.WorldID] == nil {
			regionsByWorld[row.WorldID] = make(map[int16]bool)
		}
		if regionsByWorld[row.WorldID][row.RegionID] {
			t.Fatalf("duplicate Dimension Hole world %d region %d", row.WorldID, row.RegionID)
		}
		regionsByWorld[row.WorldID][row.RegionID] = true
	}
	for _, definition := range model.DimensionHoleCatalog() {
		family, _ := dimensionHoleRegionFamily(definition.WorldCodeName)
		want := map[string]int{"GOD_TOGUI": 48, "GOD_FLAME": 1, "GOD_WRECK_IN": 224, "GOD_WRECK_OUT": 224}[family]
		if got := len(regionsByWorld[definition.WorldID]); got != want {
			t.Fatalf("Dimension Hole world %d family %s regions = %d, want %d", definition.WorldID, family, got, want)
		}
	}
	startPositions := dimensionHoleStockStartPositions()
	if got, want := len(startPositions), len(model.DimensionHoleCatalog()); got != want {
		t.Fatalf("Dimension Hole start-position rows = %d, want %d", got, want)
	}
	startsByWorld := make(map[uint16]instanceWorldStartPositionReferenceRow, len(startPositions))
	for _, row := range startPositions {
		if row.RegionID == 0 || row.Param != 0 || regionsByWorld[row.WorldID] == nil || !regionsByWorld[row.WorldID][row.RegionID] {
			t.Fatalf("invalid Dimension Hole start position %+v", row)
		}
		if _, duplicate := startsByWorld[row.WorldID]; duplicate {
			t.Fatalf("duplicate Dimension Hole start position for world %d", row.WorldID)
		}
		startsByWorld[row.WorldID] = row
	}
	for _, want := range []instanceWorldStartPositionReferenceRow{
		{WorldID: 63, RegionID: 32484, X: 1063, Y: 693, Z: 1125},
		{WorldID: 69, RegionID: -32750, X: 10897, Y: 165, Z: -6110},
		{WorldID: 77, RegionID: 29427, X: 264, Y: -770, Z: 1745},
		{WorldID: 84, RegionID: 28644, X: 1257, Y: -247, Z: 1771},
	} {
		if got := startsByWorld[want.WorldID]; got != want {
			t.Fatalf("Dimension Hole start position world %d = %+v, want %+v", want.WorldID, got, want)
		}
	}
	dropGold, err := loadDropGoldReferenceData(filepath.Join(textData, "levelgold.txt"))
	if err != nil {
		t.Fatalf("loadDropGoldReferenceData() error = %v", err)
	}
	if err := mergeDropGoldReferenceData(levels, dropGold); err != nil {
		t.Fatalf("mergeDropGoldReferenceData() error = %v", err)
	}
	if levels[0].DropGoldMin != 28 || levels[0].DropGoldMax != 59 || levels[139].DropGoldMin == 0 {
		t.Fatalf("drop-gold rows are not populated: first=%+v last=%+v", levels[0], levels[139])
	}

	skills, err := loadSkillReferenceData(textData)
	if err != nil {
		t.Fatalf("loadSkillReferenceData() error = %v", err)
	}
	if len(skills) < 1000 {
		t.Fatalf("skill rows = %d, want a populated client skill catalog", len(skills))
	}
	if skills[0].ID != 1 || skills[0].CodeName != "SKILL_PUNCH_01" || skills[0].MasteryID != 0 {
		t.Fatalf("first skill row = %+v, want SKILL_PUNCH_01", skills[0])
	}
	magicOptions, err := loadMagicOptionReferenceData(filepath.Join(textData, "magicoption.txt"))
	if err != nil {
		t.Fatalf("loadMagicOptionReferenceData() error = %v", err)
	}
	if len(magicOptions) < 100 || magicOptions[0].ID == 0 || magicOptions[0].CodeName == "" {
		t.Fatalf("magic option rows are not populated: first=%+v count=%d", magicOptions[0], len(magicOptions))
	}
	if magicOptions[0].DropRatio != 0.5 || len(magicOptions[0].Applicability) != 3 || magicOptions[0].Applicability[0] != (magicOptionApplicabilityRow{Slot: 0, ItemFamily: "weapon", MinItemClass: 1}) {
		t.Fatalf("first magic-option drop metadata = %+v", magicOptions[0])
	}
	dropMagicAssignments, err := loadDropMagicOptionAssignments(filepath.Join(textData, "magicoptionassign.txt"))
	if err != nil {
		t.Fatalf("loadDropMagicOptionAssignments() error = %v", err)
	}
	if len(dropMagicAssignments) < 400 || dropMagicAssignments[0] != (dropMagicOptionAssignmentRow{Service: 1, Country: 0, TypeID3: 6, TypeID4: 2, Slot: 0, CodeName: "MATTR_STR"}) {
		t.Fatalf("drop magic-option assignments are not populated: first=%+v count=%d", dropMagicAssignments[0], len(dropMagicAssignments))
	}
	eventRewards, eventRewardItems, err := loadEventRewardReferenceData(textData)
	if err != nil {
		t.Fatalf("loadEventRewardReferenceData() error = %v", err)
	}
	if len(eventRewards) != 0 || len(eventRewardItems) != 0 {
		t.Fatalf("local Media event catalog unexpectedly contains rows: rewards=%d items=%d", len(eventRewards), len(eventRewardItems))
	}
}
