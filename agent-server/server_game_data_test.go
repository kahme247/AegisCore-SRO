package main

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestBuildReferenceObjectIndexUsesCurrentSnapshots(t *testing.T) {
	items := map[uint32]model.RefItem{
		100: {RefObject: model.RefObject{ID: 100, CodeName: "ITEM_CURRENT"}},
	}
	characters := map[uint32]model.RefChar{
		200: {RefObject: model.RefObject{ID: 200, CodeName: "CHAR_CURRENT"}},
	}

	// This represents the previous boot's index. A subsequent build must not
	// reuse it when the row is absent from the current snapshots.
	previousIndex := buildReferenceObjectIndex(map[uint32]model.RefItem{
		999: {RefObject: model.RefObject{ID: 999, CodeName: "STALE_OBJECT"}},
	}, nil)
	index := buildReferenceObjectIndex(items, characters)

	if got, ok := index[100]; !ok || got.CodeName != "ITEM_CURRENT" {
		t.Fatalf("item reference object = %#v, present = %t; want current item", got, ok)
	}
	if got, ok := index[200]; !ok || got.CodeName != "CHAR_CURRENT" {
		t.Fatalf("character reference object = %#v, present = %t; want current character", got, ok)
	}
	if _, retained := index[999]; retained {
		t.Fatalf("stale reference object from prior index %+v was retained", previousIndex[999])
	}
}
