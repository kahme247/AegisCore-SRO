package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

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
