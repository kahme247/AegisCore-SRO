package model

import (
	"context"
	"reflect"
	"testing"
)

func TestCharacterAttributeRepositorySpendsOnePointAtomically(t *testing.T) {
	database := &progressExecer{rows: 1}
	repository, err := NewCharacterAttributeRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	spent, err := repository.SpendStatPoint(context.Background(), 77, CharacterStrength)
	if err != nil || !spent {
		t.Fatalf("SpendStatPoint = %v, %v", spent, err)
	}
	if database.query != spendCharacterStatPoint {
		t.Fatalf("query = %q", database.query)
	}
	if want := []any{1, 0, 77}; !reflect.DeepEqual(database.args, want) {
		t.Fatalf("args = %#v, want %#v", database.args, want)
	}
}

func TestCharacterAttributeRepositoryRejectsEmptyBalance(t *testing.T) {
	repository, err := NewCharacterAttributeRepository(&progressExecer{rows: 0})
	if err != nil {
		t.Fatal(err)
	}
	spent, err := repository.SpendStatPoint(context.Background(), 77, CharacterIntellect)
	if err != nil || spent {
		t.Fatalf("SpendStatPoint = %v, %v, want false nil", spent, err)
	}
}
