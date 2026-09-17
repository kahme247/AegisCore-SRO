package model

import (
	"math"
	"testing"
)

func TestNextCharacterPKKillStateIncrementsAndClampsRecoveredWidths(t *testing.T) {
	if got := NextCharacterPKKillState(CharacterPKState{DailyPK: 7, TotalPK: 12, PKPenaltyPoint: 345}); got != (CharacterPKState{DailyPK: 8, TotalPK: 13, PKPenaltyPoint: 10_345}) {
		t.Fatalf("next PK state = %#v", got)
	}
	maximum := CharacterPKState{DailyPK: math.MaxUint8, TotalPK: math.MaxInt16, PKPenaltyPoint: math.MaxInt32 - 1}
	if got := NextCharacterPKKillState(maximum); got != (CharacterPKState{DailyPK: math.MaxUint8, TotalPK: math.MaxInt16, PKPenaltyPoint: math.MaxInt32}) {
		t.Fatalf("clamped PK state = %#v", got)
	}
}
