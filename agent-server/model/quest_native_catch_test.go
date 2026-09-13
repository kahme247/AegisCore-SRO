package model

import (
	"errors"
	"testing"
)

func TestNativeCatchActionRecoveredCatalog(t *testing.T) {
	wantIDs := []int{163, 181, 203, 207, 239, 286, 357, 371}
	if len(NativeCatchActionDefinitions) != len(wantIDs) {
		t.Fatalf("definition count = %d", len(NativeCatchActionDefinitions))
	}
	for index, wantID := range wantIDs {
		definition := NativeCatchActionDefinitions[index]
		if definition.QuestID != wantID || definition.Countdown != 10 || definition.TriggerRefID == 0 || definition.ResultRefID == 0 || len(definition.Spheres) == 0 {
			t.Fatalf("definition[%d] = %+v", index, definition)
		}
		if got, exists := NativeCatchDefinitionForQuest(wantID); !exists || got.QuestID != wantID {
			t.Fatalf("quest lookup %d = %+v exists=%t", wantID, got, exists)
		}
	}
	if definitions := NativeCatchDefinitionsForTrigger(23367); len(definitions) != 2 || definitions[0].QuestID != 357 || definitions[1].QuestID != 371 {
		t.Fatalf("shared Hwan trigger definitions = %+v", definitions)
	}
}

func TestNativeCatchActionExactRollPredicates(t *testing.T) {
	tests := []struct {
		questID int
		roll    byte
		want    bool
	}{
		{163, 50, true}, {163, 51, false},
		{181, 100, true},
		{203, 67, true}, {203, 68, false},
		{207, 49, false}, {207, 50, true},
		{239, 49, false}, {239, 50, true},
		{286, 50, true}, {286, 51, false},
		{357, 10, true}, {357, 11, false},
	}
	for _, test := range tests {
		definition, _ := NativeCatchDefinitionForQuest(test.questID)
		if got := definition.RollSucceeds(test.roll); got != test.want {
			t.Fatalf("quest %d roll %d succeeds=%t", test.questID, test.roll, got)
		}
	}
}

func TestAdvanceNativeCatchAction(t *testing.T) {
	definition, _ := NativeCatchDefinitionForQuest(163)
	original := uint64(0xAABBCC0000000000) | uint64(2)<<32
	transition, err := AdvanceNativeCatchAction(definition, original, 100)
	if err != nil || transition.Resolved || byte(transition.Data1>>32) != 1 || transition.Data1&0xffffff00ffffffff != original&0xffffff00ffffffff {
		t.Fatalf("decrement transition = %+v err=%v", transition, err)
	}
	transition, err = AdvanceNativeCatchAction(definition, transition.Data1, 50)
	if err != nil || !transition.Resolved || !transition.Succeeded || transition.RewardReady || byte(transition.Data1>>32) != 0 || uint16(transition.Data1) != 1 {
		t.Fatalf("success transition = %+v err=%v", transition, err)
	}
	if _, err = AdvanceNativeCatchAction(definition, transition.Data1, 50); !errors.Is(err, ErrNativeCatchActionState) {
		t.Fatalf("zero-countdown error = %v", err)
	}
}

func TestAdvanceNativeCatchActionPromotesAtRecoveredResultCap(t *testing.T) {
	definition, _ := NativeCatchDefinitionForQuest(163)
	original := uint64(4) | uint64(1)<<32
	transition, err := AdvanceNativeCatchAction(definition, original, 50)
	if err != nil || !transition.Succeeded || !transition.RewardReady || uint16(transition.Data1) != 5 {
		t.Fatalf("transition = %+v err=%v", transition, err)
	}
}

func TestAdvanceNativeCatchHwanPromotesCompletionBytes(t *testing.T) {
	definition, _ := NativeCatchDefinitionForQuest(357)
	original := uint64(1) << 32
	transition, err := AdvanceNativeCatchAction(definition, original, 10)
	if err != nil || !transition.Resolved || !transition.Succeeded || !transition.RewardReady {
		t.Fatalf("transition = %+v err=%v", transition, err)
	}
	if byte(transition.Data1>>8) != 1 || byte(transition.Data1>>24) != 1 {
		t.Fatalf("completion data = %016X", transition.Data1)
	}
}

func TestNativeCatchActionPositionAdmissionIncludesOrnamentSpheres(t *testing.T) {
	definition, _ := NativeCatchDefinitionForQuest(286)
	for index, sphere := range definition.Spheres {
		if !definition.PositionMatches(OldWomanSearchPosition{Region: sphere.Region, X: sphere.X, Y: sphere.Y, Z: sphere.Z}) {
			t.Fatalf("sphere %d center rejected", index)
		}
	}
	if definition.PositionMatches(OldWomanSearchPosition{Region: -32763, X: 0, Y: 0, Z: 0}) {
		t.Fatal("unrelated ornament position admitted")
	}
}

func TestStartNativeCatchActionAdmission(t *testing.T) {
	definition, _ := NativeCatchDefinitionForQuest(203)
	sphere := definition.Spheres[0]
	position := OldWomanSearchPosition{Region: sphere.Region, X: sphere.X, Y: sphere.Y, Z: sphere.Z}
	original := uint64(0xAABBCC0000000003)
	started, err := StartNativeCatchAction(definition, original, position, 3, true)
	if err != nil || byte(started>>32) != 10 || started&^(uint64(0xff)<<32) != original&^(uint64(0xff)<<32) {
		t.Fatalf("started = %016X err=%v", started, err)
	}
	for name, test := range map[string]struct {
		data1    uint64
		position OldWomanSearchPosition
		count    int
		free     bool
	}{
		"active countdown": {data1: uint64(1) << 32, position: position, free: true},
		"outside sphere":   {position: OldWomanSearchPosition{Region: sphere.Region + 1}, free: true},
		"result cap":       {position: position, count: definition.ResultCap, free: true},
		"inventory full":   {position: position},
	} {
		if next, startErr := StartNativeCatchAction(definition, test.data1, test.position, test.count, test.free); !errors.Is(startErr, ErrNativeCatchActionState) || next != test.data1 {
			t.Fatalf("%s next=%016X err=%v", name, next, startErr)
		}
	}
}
