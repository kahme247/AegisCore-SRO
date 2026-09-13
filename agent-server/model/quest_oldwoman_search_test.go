package model

import (
	"errors"
	"testing"
)

func TestOldWomanSearchRecoveredStageTable(t *testing.T) {
	wantCountdowns := [...]byte{5, 10, 15, 20, 25, 30, 40}
	wantChances := [...]byte{25, 30, 40, 50, 60, 75, 100}
	if len(OldWomanSearchStages) != 7 {
		t.Fatalf("stage count = %d", len(OldWomanSearchStages))
	}
	for index, stage := range OldWomanSearchStages {
		if stage.Region <= 0 || stage.Radius <= 0 || stage.Countdown != wantCountdowns[index] || stage.SuccessChance != wantChances[index] {
			t.Fatalf("stage %d = %#v", index, stage)
		}
	}
}

func TestOldWomanSearchProgressCodecPreservesOpaqueBytes(t *testing.T) {
	const original = uint64(0xAABBCCDDEEFF1122)
	progress := OldWomanSearchProgress{Step: 7, Complete: 0x31, Stage: 6, Ready: 0x42, Countdown: 40}
	encoded := EncodeOldWomanSearchProgress(original, progress)
	if got := DecodeOldWomanSearchProgress(encoded); got != progress {
		t.Fatalf("decoded progress = %#v", got)
	}
	const fieldMask = uint64(0xffffffffff)
	if encoded&^fieldMask != original&^fieldMask {
		t.Fatalf("opaque bytes changed: before=%016X after=%016X", original, encoded)
	}
	if cleared := ClearOldWomanSearchCountdown(encoded); DecodeOldWomanSearchProgress(cleared).Countdown != 0 || cleared&^(uint64(0xff)<<32) != encoded&^(uint64(0xff)<<32) {
		t.Fatalf("event-0x17 clear = %016X", cleared)
	}
}

func TestAdvanceOldWomanSearchAction(t *testing.T) {
	tests := []struct {
		name                       string
		progress                   OldWomanSearchProgress
		roll                       byte
		want                       OldWomanSearchProgress
		resolved, succeeded, ready bool
	}{
		{name: "decrement", progress: OldWomanSearchProgress{Step: 1, Stage: 0, Countdown: 2}, roll: 100, want: OldWomanSearchProgress{Step: 1, Stage: 0, Countdown: 1}},
		{name: "inclusive success", progress: OldWomanSearchProgress{Step: 1, Stage: 0, Countdown: 1}, roll: 25, want: OldWomanSearchProgress{Step: 2, Stage: 0}, resolved: true, succeeded: true},
		{name: "failure", progress: OldWomanSearchProgress{Step: 1, Stage: 0, Countdown: 1}, roll: 26, want: OldWomanSearchProgress{Step: 1, Stage: 0}, resolved: true},
		{name: "seventh success", progress: OldWomanSearchProgress{Step: 7, Stage: 6, Countdown: 1}, roll: 100, want: OldWomanSearchProgress{Step: 8, Complete: 1, Stage: 6, Ready: 1}, resolved: true, succeeded: true, ready: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const opaque = uint64(0xA5B6C7) << 40
			initial := EncodeOldWomanSearchProgress(opaque, test.progress)
			next, resolved, succeeded, ready, err := AdvanceOldWomanSearchAction(initial, test.roll)
			if err != nil {
				t.Fatal(err)
			}
			if got := DecodeOldWomanSearchProgress(next); got != test.want || resolved != test.resolved || succeeded != test.succeeded || ready != test.ready {
				t.Fatalf("got (%+v,%v,%v,%v), want (%+v,%v,%v,%v)", got, resolved, succeeded, ready, test.want, test.resolved, test.succeeded, test.ready)
			}
			if next>>40 != opaque>>40 {
				t.Fatalf("opaque bytes changed: %#x", next)
			}
		})
	}
}

func TestAdvanceOldWomanSearchActionRejectsInvalidState(t *testing.T) {
	for _, test := range []struct {
		progress OldWomanSearchProgress
		roll     byte
	}{
		{progress: OldWomanSearchProgress{Stage: 7, Countdown: 1}},
		{progress: OldWomanSearchProgress{Stage: 0, Countdown: 0}},
		{progress: OldWomanSearchProgress{Stage: 0, Countdown: 1}, roll: 101},
	} {
		initial := EncodeOldWomanSearchProgress(0, test.progress)
		if next, _, _, _, err := AdvanceOldWomanSearchAction(initial, test.roll); !errors.Is(err, ErrOldWomanSearchState) || next != initial {
			t.Fatalf("next=%#x err=%v", next, err)
		}
	}
}

func TestOldWomanSearchPositionMatchesRecoveredSphere(t *testing.T) {
	stage := OldWomanSearchStages[0]
	if !OldWomanSearchPositionMatches(stage, OldWomanSearchPosition{Region: stage.Region, X: stage.X + stage.Radius, Y: stage.Y, Z: stage.Z}) {
		t.Fatal("inclusive sphere boundary was rejected")
	}
	if OldWomanSearchPositionMatches(stage, OldWomanSearchPosition{Region: stage.Region, X: stage.X + stage.Radius + 1, Y: stage.Y, Z: stage.Z}) {
		t.Fatal("position outside sphere was admitted")
	}
	if OldWomanSearchPositionMatches(stage, OldWomanSearchPosition{Region: stage.Region + 1, X: stage.X, Y: stage.Y, Z: stage.Z}) {
		t.Fatal("wrong region was admitted")
	}
}
