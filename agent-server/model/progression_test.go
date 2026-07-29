package model

import (
	"strings"
	"testing"
)

func testLevelData() *strings.Reader {
	return strings.NewReader("1\t118\t1\t0\t0\t24\t0\n2\t470\t2\t0\t0\t47\t0\n3\t999999\t3\t0\t0\t71\t0\n")
}

func TestApplyExperienceUsesClientLevelThresholds(t *testing.T) {
	table, err := LoadLevelTable(testLevelData())
	if err != nil {
		t.Fatal(err)
	}
	result := ApplyExperience(table, 1, 100, 0, 500, 10, 0, 0)
	if result.Level != 3 || result.Experience != 12 || result.LevelsGained != 2 {
		t.Fatalf("unexpected progression: %+v", result)
	}
}

func TestApplyExperienceDoesNotExceedClientCap(t *testing.T) {
	table, err := LoadLevelTable(testLevelData())
	if err != nil {
		t.Fatal(err)
	}
	result := ApplyExperience(table, 2, 0, 0, 10000, 0, 0, 0)
	if result.Level != 3 || result.Experience != 9530 {
		t.Fatalf("capped progression = %+v", result)
	}
}

func TestApplyExperienceConvertsEveryFourHundredSkillExperience(t *testing.T) {
	table, err := LoadLevelTable(testLevelData())
	if err != nil {
		t.Fatal(err)
	}
	result := ApplyExperience(table, 1, 0, 390, 0, 25, 7, 0)
	if result.SkillExperience != 15 || result.SkillPoints != 8 {
		t.Fatalf("skill progress = remainder %d points %d, want 15 and 8", result.SkillExperience, result.SkillPoints)
	}
}
