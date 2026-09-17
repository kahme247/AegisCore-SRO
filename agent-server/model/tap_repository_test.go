package model

import (
	"testing"
	"time"
)

func TestTAPRecordActiveHandlesAnnualAndYearWrappingPeriods(t *testing.T) {
	summer := TAPRecord{ID: 1, EntryType: 2, From: TAPClock{Month: 7, Day: 1}, To: TAPClock{Month: 8, Day: 1}}
	if !TAPRecordActive(summer, time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)) || TAPRecordActive(summer, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("summer TAP period active-state mismatch")
	}
	winter := TAPRecord{ID: 2, EntryType: 3, From: TAPClock{Month: 12, Day: 20}, To: TAPClock{Month: 1, Day: 10}}
	if !TAPRecordActive(winter, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) || !TAPRecordActive(winter, time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("year-wrapping TAP period active-state mismatch")
	}
}

func TestTAPRecordActiveRejectsImpossibleCalendarDate(t *testing.T) {
	record := TAPRecord{ID: 1, EntryType: 2, From: TAPClock{Month: 2, Day: 31}, To: TAPClock{Month: 3, Day: 1}}
	if TAPRecordActive(record, time.Now()) {
		t.Fatal("impossible TAP date accepted")
	}
}
