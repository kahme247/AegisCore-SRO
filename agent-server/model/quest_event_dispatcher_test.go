package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

const questEventDispatcherTestDriverName = "aegiscore-quest-event-dispatcher-test"

var questEventDispatcherTestState struct {
	sync.Mutex
	status    QuestStatus
	noRow     bool
	commitErr error
	begins    int
	commits   int
	rollbacks int
	query     string
	args      []driver.NamedValue
}

func init() {
	sql.Register(questEventDispatcherTestDriverName, questEventDispatcherTestDriver{})
}

type questEventDispatcherTestDriver struct{}
type questEventDispatcherTestConn struct{}
type questEventDispatcherTestTx struct{}

func (questEventDispatcherTestDriver) Open(string) (driver.Conn, error) {
	return questEventDispatcherTestConn{}, nil
}

func (questEventDispatcherTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not used")
}
func (questEventDispatcherTestConn) Close() error { return nil }
func (questEventDispatcherTestConn) Begin() (driver.Tx, error) {
	questEventDispatcherTestState.Lock()
	questEventDispatcherTestState.begins++
	questEventDispatcherTestState.Unlock()
	return questEventDispatcherTestTx{}, nil
}
func (questEventDispatcherTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	questEventDispatcherTestState.Lock()
	defer questEventDispatcherTestState.Unlock()
	questEventDispatcherTestState.query = query
	questEventDispatcherTestState.args = append([]driver.NamedValue(nil), args...)
	rows := &questEventDispatcherTestRows{}
	if !questEventDispatcherTestState.noRow {
		rows.values = []driver.Value{int64(questEventDispatcherTestState.status), int64(1), time.Now(), nil, int64(4), int64(0)}
	}
	return rows, nil
}

func (questEventDispatcherTestTx) Commit() error {
	questEventDispatcherTestState.Lock()
	defer questEventDispatcherTestState.Unlock()
	questEventDispatcherTestState.commits++
	return questEventDispatcherTestState.commitErr
}
func (questEventDispatcherTestTx) Rollback() error {
	questEventDispatcherTestState.Lock()
	questEventDispatcherTestState.rollbacks++
	questEventDispatcherTestState.Unlock()
	return nil
}

type questEventDispatcherTestRows struct {
	values []driver.Value
	done   bool
}

func (r *questEventDispatcherTestRows) Columns() []string {
	return []string{"STATUS", "ACHIEVEMENT_COUNT", "STARTED_AT", "COMPLETED_AT", "QUEST_DATA_1", "QUEST_DATA_2"}
}
func (r *questEventDispatcherTestRows) Close() error { return nil }
func (r *questEventDispatcherTestRows) Next(destination []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	copy(destination, r.values)
	r.done = true
	return nil
}

func openQuestEventDispatcherTestDB(t *testing.T, status QuestStatus) *sql.DB {
	t.Helper()
	questEventDispatcherTestState.Lock()
	questEventDispatcherTestState.status = status
	questEventDispatcherTestState.noRow = false
	questEventDispatcherTestState.commitErr = nil
	questEventDispatcherTestState.begins = 0
	questEventDispatcherTestState.commits = 0
	questEventDispatcherTestState.rollbacks = 0
	questEventDispatcherTestState.query = ""
	questEventDispatcherTestState.args = nil
	questEventDispatcherTestState.Unlock()
	database, err := sql.Open(questEventDispatcherTestDriverName, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestQuestEventDispatcherCommitsOnlyAfterRegisteredMutation(t *testing.T) {
	database := openQuestEventDispatcherTestDB(t, QuestTransient2)
	dispatcher, err := NewQuestEventDispatcher(database)
	if err != nil {
		t.Fatal(err)
	}
	mutated := false
	if err = dispatcher.Register(101, QuestAuthoredInteraction, func(_ context.Context, tx *sql.Tx, quest CharacterQuest) error {
		mutated = tx != nil && quest.QuestID == 101 && quest.Status == QuestTransient2 && quest.Data1 == 4
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = dispatcher.DispatchQuestEvent(context.Background(), 77, 101, QuestAuthoredInteraction); err != nil {
		t.Fatal(err)
	}
	questEventDispatcherTestState.Lock()
	defer questEventDispatcherTestState.Unlock()
	if !mutated || questEventDispatcherTestState.begins != 1 || questEventDispatcherTestState.commits != 1 || questEventDispatcherTestState.rollbacks != 0 {
		t.Fatalf("mutated=%t begins=%d commits=%d rollbacks=%d", mutated, questEventDispatcherTestState.begins, questEventDispatcherTestState.commits, questEventDispatcherTestState.rollbacks)
	}
	if !strings.Contains(questEventDispatcherTestState.query, "FOR UPDATE") || len(questEventDispatcherTestState.args) != 2 || questEventDispatcherTestState.args[0].Value != int64(77) || questEventDispatcherTestState.args[1].Value != int64(101) {
		t.Fatalf("query=%q args=%#v", questEventDispatcherTestState.query, questEventDispatcherTestState.args)
	}
}

func TestQuestEventDispatcherLocalActionAcceptsOnlyClientActiveStates(t *testing.T) {
	for _, status := range []QuestStatus{QuestActive, QuestProgressed, QuestRewardReady} {
		database := openQuestEventDispatcherTestDB(t, status)
		dispatcher, err := NewQuestEventDispatcher(database)
		if err != nil {
			t.Fatal(err)
		}
		if err = dispatcher.Register(101, QuestAuthoredLocalAction, func(context.Context, *sql.Tx, CharacterQuest) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if err = dispatcher.DispatchQuestEvent(context.Background(), 77, 101, QuestAuthoredLocalAction); err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		database.Close()
	}
}

func TestQuestEventDispatcherRollsBackRejectedOrFailedMutation(t *testing.T) {
	tests := []struct {
		name     string
		status   QuestStatus
		mutation QuestEventMutation
	}{
		{"invalid state", QuestClosed, func(context.Context, *sql.Tx, CharacterQuest) error { return nil }},
		{"mutation failure", QuestActive, func(context.Context, *sql.Tx, CharacterQuest) error { return errors.New("mutation failed") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openQuestEventDispatcherTestDB(t, test.status)
			dispatcher, err := NewQuestEventDispatcher(database)
			if err != nil {
				t.Fatal(err)
			}
			if err = dispatcher.Register(101, QuestAuthoredLocalAction, test.mutation); err != nil {
				t.Fatal(err)
			}
			if err = dispatcher.DispatchQuestEvent(context.Background(), 77, 101, QuestAuthoredLocalAction); err == nil {
				t.Fatal("dispatch succeeded")
			}
			questEventDispatcherTestState.Lock()
			defer questEventDispatcherTestState.Unlock()
			if questEventDispatcherTestState.commits != 0 || questEventDispatcherTestState.rollbacks != 1 {
				t.Fatalf("commits=%d rollbacks=%d", questEventDispatcherTestState.commits, questEventDispatcherTestState.rollbacks)
			}
		})
	}
}

func TestQuestEventDispatcherRejectsMissingAndDuplicateHandlers(t *testing.T) {
	database := openQuestEventDispatcherTestDB(t, QuestActive)
	dispatcher, err := NewQuestEventDispatcher(database)
	if err != nil {
		t.Fatal(err)
	}
	if err = dispatcher.DispatchQuestEvent(context.Background(), 77, 101, QuestAuthoredLocalAction); !errors.Is(err, ErrQuestEventNotFound) {
		t.Fatalf("missing dispatch error = %v", err)
	}
	mutation := func(context.Context, *sql.Tx, CharacterQuest) error { return nil }
	if err = dispatcher.Register(101, QuestAuthoredLocalAction, mutation); err != nil {
		t.Fatal(err)
	}
	if err = dispatcher.Register(101, QuestAuthoredLocalAction, mutation); !errors.Is(err, ErrQuestEventAlreadyRegistered) {
		t.Fatalf("duplicate register error = %v", err)
	}
}

func TestQuestEventDispatcherClassifiesUnknownCommit(t *testing.T) {
	database := openQuestEventDispatcherTestDB(t, QuestActive)
	want := errors.New("commit failed")
	questEventDispatcherTestState.Lock()
	questEventDispatcherTestState.commitErr = want
	questEventDispatcherTestState.Unlock()
	dispatcher, err := NewQuestEventDispatcher(database)
	if err != nil {
		t.Fatal(err)
	}
	if err = dispatcher.Register(101, QuestAuthoredLocalAction, func(context.Context, *sql.Tx, CharacterQuest) error { return nil }); err != nil {
		t.Fatal(err)
	}
	err = dispatcher.DispatchQuestEvent(context.Background(), 77, 101, QuestAuthoredLocalAction)
	if !errors.Is(err, ErrQuestOutcomeUnknown) || !errors.Is(err, want) {
		t.Fatalf("dispatch error = %v", err)
	}
}
