package main

import (
	"path/filepath"
	"testing"
)

func TestLoadReferenceDataFromCheckedInMedia(t *testing.T) {
	textData := filepath.Join("..", "..", "Media", "Media", "server_dep", "silkroad", "textdata")
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
}
