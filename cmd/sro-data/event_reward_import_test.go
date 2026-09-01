package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEventRewardReferenceRows(t *testing.T) {
	directory := t.TempDir()
	reward := "1\t10001\tEVENT_TEST\t1\t1\t1\t0\t0\t0\t0\t500\t600\t700\t0\t0\t0\t0\t2\t11\tx\t12\ty\t13\tz\n"
	items := "1\t10001\tEVENT_TEST\tITEM_TEST_A\t2\t0.5\txxx\t0\tx\t0\ty\n1\t10001\tEVENT_TEST\tITEM_TEST_B\t1\t1\txxx\t0\tx\t0\ty\n"
	if err := os.WriteFile(filepath.Join(directory, "refeventreward.txt"), []byte(reward), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "refeventrewarditems.txt"), []byte(items), 0o600); err != nil {
		t.Fatal(err)
	}
	rewards, rewardItems, err := loadEventRewardReferenceData(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 1 || rewards[0].EventID != 10001 || rewards[0].Gold != 500 || rewards[0].SelectionCount != 2 {
		t.Fatalf("rewards = %#v", rewards)
	}
	if len(rewardItems) != 2 || rewardItems[0].ItemCodeName != "ITEM_TEST_A" || rewardItems[0].PayCount != 2 || rewardItems[0].AchieveRatio != 0.5 {
		t.Fatalf("items = %#v", rewardItems)
	}
}

func TestParseEventRewardReferenceRowsRejectsOrphansAndMalformedRows(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "refeventreward.txt"), []byte("1\t10001\tEVENT_TEST\t1\t1\t1\t0\t0\t0\t0\t0\t0\t0\t0\t0\t0\t0\t1\t0\tx\t0\ty\t0\tz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "refeventrewarditems.txt"), []byte("1\t10002\tEVENT_OTHER\tITEM_TEST\t1\t1\txxx\t0\tx\t0\ty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadEventRewardReferenceData(directory); err == nil || !strings.Contains(err.Error(), "missing event") {
		t.Fatalf("orphan error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "refeventrewarditems.txt"), []byte("1\t10001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadEventRewardReferenceData(directory); err == nil || !strings.Contains(err.Error(), "want at least 11 fields") {
		t.Fatalf("malformed error = %v", err)
	}
}

func TestEmptyMediaEventRewardFilesProduceNoRows(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"refeventreward.txt", "refeventrewarditems.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte{0xff, 0xfe}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rewards, items, err := loadEventRewardReferenceData(directory)
	if err != nil || len(rewards) != 0 || len(items) != 0 {
		t.Fatalf("empty event catalog = rewards=%d items=%d err=%v", len(rewards), len(items), err)
	}
}
