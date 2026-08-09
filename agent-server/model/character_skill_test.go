package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
)

const characterSkillTestDriverName = "aegiscore-character-skill-test"

var characterSkillTestState struct {
	sync.Mutex
	rows  [][]driver.Value
	err   error
	query string
	args  []driver.NamedValue
	ctx   context.Context
}

func init() {
	sql.Register(characterSkillTestDriverName, characterSkillTestDriver{})
}

type characterSkillTestDriver struct{}

func (characterSkillTestDriver) Open(string) (driver.Conn, error) {
	return characterSkillTestConn{}, nil
}

type characterSkillTestConn struct{}

func (characterSkillTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}
func (characterSkillTestConn) Close() error { return nil }
func (characterSkillTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}
func (characterSkillTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	characterSkillTestState.Lock()
	defer characterSkillTestState.Unlock()
	characterSkillTestState.ctx = ctx
	characterSkillTestState.query = query
	characterSkillTestState.args = append([]driver.NamedValue(nil), args...)
	if characterSkillTestState.err != nil {
		return nil, characterSkillTestState.err
	}
	rows := make([][]driver.Value, len(characterSkillTestState.rows))
	for index := range characterSkillTestState.rows {
		rows[index] = append([]driver.Value(nil), characterSkillTestState.rows[index]...)
	}
	return &characterSkillTestRows{rows: rows}, nil
}

type characterSkillTestRows struct {
	rows  [][]driver.Value
	index int
}

func (*characterSkillTestRows) Columns() []string { return []string{"SKILL_ID", "ENABLED"} }
func (*characterSkillTestRows) Close() error      { return nil }
func (r *characterSkillTestRows) Next(destination []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(destination, r.rows[r.index])
	r.index++
	return nil
}

func newCharacterSkillRepositoryForTest(t *testing.T, rows [][]driver.Value, queryErr error) *SQLCharacterSkillRepository {
	t.Helper()
	characterSkillTestState.Lock()
	characterSkillTestState.rows = rows
	characterSkillTestState.err = queryErr
	characterSkillTestState.query = ""
	characterSkillTestState.args = nil
	characterSkillTestState.ctx = nil
	characterSkillTestState.Unlock()
	database, err := sql.Open(characterSkillTestDriverName, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	repository, err := NewCharacterSkillRepository(database)
	if err != nil {
		t.Fatalf("NewCharacterSkillRepository: %v", err)
	}
	return repository
}

func TestCharacterSkillRepositoryLoadsOrderedRowsWithContext(t *testing.T) {
	repository := newCharacterSkillRepositoryForTest(t, [][]driver.Value{{int64(1), int64(1)}, {int64(40), int64(0)}}, nil)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "skill-request")
	skills, err := repository.LoadSkills(ctx, 77)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	want := []CharacterSkill{{ID: 1, Enabled: 1}, {ID: 40, Enabled: 0}}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("skills = %#v, want %#v", skills, want)
	}
	characterSkillTestState.Lock()
	query := characterSkillTestState.query
	args := append([]driver.NamedValue(nil), characterSkillTestState.args...)
	receivedContext := characterSkillTestState.ctx
	characterSkillTestState.Unlock()
	if query != selectCharacterSkills || len(args) != 1 || args[0].Value != int64(77) {
		t.Fatalf("query = %q args=%#v", query, args)
	}
	if receivedContext.Value(contextKey{}) != "skill-request" {
		t.Fatal("repository did not propagate request context")
	}
}

func TestCharacterSkillRepositoryRejectsMalformedEnabledState(t *testing.T) {
	repository := newCharacterSkillRepositoryForTest(t, [][]driver.Value{{int64(1), int64(2)}}, nil)
	_, err := repository.LoadSkills(context.Background(), 77)
	if !errors.Is(err, ErrCharacterSkillEnabledInvalid) {
		t.Fatalf("LoadSkills error = %v, want ErrCharacterSkillEnabledInvalid", err)
	}
}

func TestCharacterSkillRepositoryHonorsCancellation(t *testing.T) {
	repository := newCharacterSkillRepositoryForTest(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repository.LoadSkills(ctx, 77)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadSkills error = %v, want context.Canceled", err)
	}
}

func TestCharacterSkillRepositoryWrapsQueryFailure(t *testing.T) {
	failure := errors.New("query failed")
	repository := newCharacterSkillRepositoryForTest(t, nil, failure)
	_, err := repository.LoadSkills(context.Background(), 77)
	if !errors.Is(err, failure) || err == failure {
		t.Fatalf("LoadSkills error = %v, want wrapped failure", err)
	}
	if got := fmt.Sprint(err); got == "" {
		t.Fatal("LoadSkills returned an empty error")
	}
}
