package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestSQLLevelTableRepositoryLoadsProgressionRowsFromShard(t *testing.T) {
	resetReferenceDataRepositoryTestState()
	setReferenceDataRepositoryTestResponse(selectAllLevelData, referenceDataRepositoryTestResponse{
		columns: []string{"LEVEL", "REQUIRED_EXPERIENCE", "REQUIRED_SKILL_POINTS", "DROP_GOLD_MIN", "DROP_GOLD_MAX", "UNKNOWN_1"},
		rows: [][]driver.Value{
			{int64(1), int64(118), int64(1), int64(28), int64(59), int64(24)},
			{int64(2), int64(470), int64(1), int64(32), int64(66), int64(47)},
		},
	})

	repository, err := NewLevelTableRepository(openReferenceDataRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewLevelTableRepository() error = %v", err)
	}
	table, err := repository.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if row, ok := table.Level(2); !ok || row.RequiredExperience != 470 || row.RequiredSkillPoints != 1 || row.DropGoldMin != 32 || row.DropGoldMax != 66 || row.JobActivityFactor != 47 {
		t.Fatalf("level 2 row = %#v, present = %t; want 470 XP and 1 SP", row, ok)
	}
	if got := table.MaxLevel(); got != 2 {
		t.Fatalf("MaxLevel() = %d, want 2", got)
	}
	assertReferenceDataRepositoryQueries(t, []string{selectAllLevelData})
}

func TestSQLLevelTableRepositoryRejectsUnavailableOrEmptyData(t *testing.T) {
	if repository, err := NewLevelTableRepository(nil); repository != nil || !errors.Is(err, ErrLevelDataRepositoryUnavailable) {
		t.Fatalf("NewLevelTableRepository(nil) = repository:%v error:%v, want unavailable error", repository, err)
	}

	resetReferenceDataRepositoryTestState()
	setReferenceDataRepositoryTestResponse(selectAllLevelData, referenceDataRepositoryTestResponse{
		columns: []string{"LEVEL", "REQUIRED_EXPERIENCE", "REQUIRED_SKILL_POINTS", "DROP_GOLD_MIN", "DROP_GOLD_MAX", "UNKNOWN_1"},
	})
	repository, err := NewLevelTableRepository(openReferenceDataRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(context.Background()); !errors.Is(err, ErrLevelDataEmpty) {
		t.Fatalf("Load(empty) error = %v, want ErrLevelDataEmpty", err)
	}
}

func TestSQLLevelTableRepositoryRejectsLevelGaps(t *testing.T) {
	resetReferenceDataRepositoryTestState()
	setReferenceDataRepositoryTestResponse(selectAllLevelData, referenceDataRepositoryTestResponse{
		columns: []string{"LEVEL", "REQUIRED_EXPERIENCE", "REQUIRED_SKILL_POINTS", "DROP_GOLD_MIN", "DROP_GOLD_MAX", "UNKNOWN_1"},
		rows: [][]driver.Value{
			{int64(1), int64(118), int64(1), int64(28), int64(59), int64(24)},
			{int64(3), int64(1058), int64(1), int64(35), int64(74), int64(71)},
		},
	})
	repository, err := NewLevelTableRepository(openReferenceDataRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load(context.Background()); !errors.Is(err, ErrLevelDataMalformed) {
		t.Fatalf("Load(gapped levels) error = %v, want ErrLevelDataMalformed", err)
	}
}
