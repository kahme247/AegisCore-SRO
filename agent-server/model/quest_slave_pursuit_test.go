package model

import (
	"errors"
	"testing"
)

func TestAdvanceSlavePursuitPromotesAndConsumesFirstTick(t *testing.T) {
	original := uint64(0x1122330000000004)
	transition, err := AdvanceSlavePursuit(original, QuestActive, true)
	if err != nil || transition.Status != QuestProgressed || transition.Spawn || byte(transition.Data1>>32) != 59 {
		t.Fatalf("transition = %+v err=%v", transition, err)
	}
	if transition.Data1&^(uint64(0xff)<<32) != original&^(uint64(0xff)<<32) {
		t.Fatalf("opaque data changed: %016X -> %016X", original, transition.Data1)
	}
}

func TestAdvanceSlavePursuitSpawnAndEligibility(t *testing.T) {
	transition, err := AdvanceSlavePursuit(0, QuestProgressed, false)
	if err != nil || transition.Spawn || byte(transition.Data1>>32) != 0 {
		t.Fatalf("ineligible transition = %+v err=%v", transition, err)
	}
	transition, err = AdvanceSlavePursuit(0, QuestProgressed, true)
	if err != nil || !transition.Spawn || byte(transition.Data1>>32) != 59 {
		t.Fatalf("eligible transition = %+v err=%v", transition, err)
	}
}

func TestAdvanceSlavePursuitRejectsInactiveState(t *testing.T) {
	if _, err := AdvanceSlavePursuit(0, QuestInactive, true); !errors.Is(err, ErrSlavePursuitState) {
		t.Fatalf("error = %v", err)
	}
}
