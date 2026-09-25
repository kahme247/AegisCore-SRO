package main

import (
	"context"
	"errors"
	"testing"
)

func TestClearVSROQuestScriptTablesPreservesDependencyOrder(t *testing.T) {
	var calls []string
	err := clearVSROQuestScriptTables(context.Background(), func(_ context.Context, statement string) error {
		calls = append(calls, statement)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 24 {
		t.Fatalf("delete calls = %d, want 24", len(calls))
	}
	if calls[0] != "DELETE FROM `REF_QUEST_SCRIPT_REWARD_ITEM`" ||
		calls[1] != "DELETE FROM `REF_QUEST_SCRIPT_REWARD`" ||
		calls[12] != "DELETE FROM `REF_QUEST_DEPENDENCY_ENTRY`" ||
		calls[13] != "DELETE FROM `REF_QUEST_DEPENDENCY_GROUP`" ||
		calls[23] != "DELETE FROM `REF_QUEST_SCRIPT_DECLARATION`" {
		t.Fatalf("delete order anchors = %#v", calls)
	}
}

func TestClearVSROQuestScriptTablesStopsAtFirstError(t *testing.T) {
	stop := errors.New("stop delete")
	var calls []string
	err := clearVSROQuestScriptTables(context.Background(), func(_ context.Context, statement string) error {
		calls = append(calls, statement)
		if len(calls) == 8 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("delete error = %v, want stop error", err)
	}
	if len(calls) != 8 || calls[7] != "DELETE FROM `REF_QUEST_COLLECTION_ITEM_SOURCE`" {
		t.Fatalf("delete calls after failure = %#v", calls)
	}
}

type questScriptRowsAffectedResult struct {
	affected int64
	err      error
	calls    int
}

func (r *questScriptRowsAffectedResult) RowsAffected() (int64, error) {
	r.calls++
	return r.affected, r.err
}

func TestQuestScriptRowsAffectedExactlyOnce(t *testing.T) {
	rowsErr := errors.New("rows affected failed")
	for _, test := range []struct {
		name     string
		affected int64
		err      error
		want     bool
	}{
		{name: "one row", affected: 1, want: true},
		{name: "zero rows", affected: 0},
		{name: "multiple rows", affected: 2},
		{name: "rows error", affected: 1, err: rowsErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := &questScriptRowsAffectedResult{affected: test.affected, err: test.err}
			if got := questScriptRowsAffectedExactlyOnce(result); got != test.want || result.calls != 1 {
				t.Fatalf("exactly once = %t after %d calls, want %t after 1", got, result.calls, test.want)
			}
		})
	}
}
