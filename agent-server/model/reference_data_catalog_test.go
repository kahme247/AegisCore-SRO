package model

import "testing"

func TestReferenceDataCatalogIsolatesSnapshotAndNamespacesDuplicateIDs(t *testing.T) {
	snapshot := ReferenceDataSnapshot{
		Items: map[uint32]RefItem{
			77: {RefObject: RefObject{ID: 77, CodeName: "ITEM_ORIGINAL"}},
		},
		Characters: map[uint32]RefChar{
			77: {RefObject: RefObject{ID: 77, CodeName: "CHARACTER_ORIGINAL", TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 2}}},
		},
		Skills: map[uint32]RefSkill{
			77: {ID: 77, ActionDuration: 2400, ActionRange: 10},
		},
	}

	catalog := NewReferenceDataCatalog(snapshot)

	// A numeric overlap is valid across the two source catalogs. The consumer
	// selects the namespace explicitly, so NPC lookup cannot receive the item.
	if item, ok := catalog.Item(77); !ok || item.CodeName != "ITEM_ORIGINAL" {
		t.Fatalf("catalog item = %#v, present = %t; want ITEM_ORIGINAL", item, ok)
	}
	if character, ok := catalog.Character(77); !ok || character.CodeName != "CHARACTER_ORIGINAL" || character.TypeID2 != 2 {
		t.Fatalf("catalog character = %#v, present = %t; want CHARACTER_ORIGINAL", character, ok)
	}
	if skill, ok := catalog.Skill(77); !ok || skill.ActionDuration != 2400 || skill.ActionRange != 10 {
		t.Fatalf("catalog skill = %#v, present = %t; want original skill timing", skill, ok)
	}

	// Mutating the source snapshot after publication must not change the
	// catalog consumed by a long-running service.
	snapshot.Items[77] = RefItem{RefObject: RefObject{ID: 77, CodeName: "ITEM_MUTATED"}}
	snapshot.Characters[77] = RefChar{RefObject: RefObject{ID: 77, CodeName: "CHARACTER_MUTATED"}}
	snapshot.Skills[77] = RefSkill{ID: 77, ActionDuration: 1}
	snapshot.Items[88] = RefItem{RefObject: RefObject{ID: 88, CodeName: "ITEM_LATE"}}

	if item, ok := catalog.Item(77); !ok || item.CodeName != "ITEM_ORIGINAL" {
		t.Fatalf("catalog item after snapshot mutation = %#v, present = %t; want original", item, ok)
	}
	if character, ok := catalog.Character(77); !ok || character.CodeName != "CHARACTER_ORIGINAL" {
		t.Fatalf("catalog character after snapshot mutation = %#v, present = %t; want original", character, ok)
	}
	if skill, ok := catalog.Skill(77); !ok || skill.ActionDuration != 2400 {
		t.Fatalf("catalog skill after snapshot mutation = %#v, present = %t; want original", skill, ok)
	}
	if _, ok := catalog.Item(88); ok {
		t.Fatal("catalog exposed an item added to the snapshot after construction")
	}
}

func TestReferenceDataCatalogNilIsEmpty(t *testing.T) {
	var catalog *ReferenceDataCatalog
	if _, ok := catalog.Item(1); ok {
		t.Fatal("nil catalog returned an item")
	}
	if _, ok := catalog.Character(1); ok {
		t.Fatal("nil catalog returned a character")
	}
	if _, ok := catalog.Skill(1); ok {
		t.Fatal("nil catalog returned a skill")
	}
}
