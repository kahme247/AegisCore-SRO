package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"
)

type progressExecer struct {
	query string
	args  []any
	rows  int64
	err   error
}

func (e *progressExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.query = query
	e.args = append([]any(nil), args...)
	return driver.RowsAffected(e.rows), e.err
}

func TestCharacterProgressRepositoryPersistsAuthoritativeSnapshot(t *testing.T) {
	database := &progressExecer{rows: 1}
	repository, err := NewCharacterProgressRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterProgressRepository: %v", err)
	}
	progress := ProgressionResult{Level: 2, Experience: 15, SkillExperience: 9, SkillPoints: 3, StatPoints: 4}
	if err := repository.SaveProgress(context.Background(), 77, progress); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}
	if database.query != UpdateCharacterProgress {
		t.Fatalf("query = %q", database.query)
	}
	want := []any{2, uint64(15), uint64(9), 3, 4, 2, 77}
	if !reflect.DeepEqual(database.args, want) {
		t.Fatalf("args = %#v, want %#v", database.args, want)
	}
}

func TestCharacterProgressRepositoryRejectsMissingRow(t *testing.T) {
	repository, err := NewCharacterProgressRepository(&progressExecer{})
	if err != nil {
		t.Fatalf("NewCharacterProgressRepository: %v", err)
	}
	if err := repository.SaveProgress(context.Background(), 77, ProgressionResult{Level: 2}); err == nil {
		t.Fatal("SaveProgress accepted an update that affected no character")
	}
}
