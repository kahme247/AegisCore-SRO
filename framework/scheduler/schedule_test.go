package scheduler

import (
	"testing"
	"time"
)

func TestDailyScheduleHonorsDateRangeIntervalAndTimezone(t *testing.T) {
	from := mustDate(t, 2026, time.July, 19)
	until := mustDate(t, 2026, time.July, 25)
	definition := recurringDefinition(Daily, from, until)
	definition.Interval = 2
	definition.SecondOfDay = 14 * 3600
	next, ok, err := definition.NextAfter(time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC))
	if err != nil || !ok {
		t.Fatalf("NextAfter() next=%v ok=%t error=%v", next, ok, err)
	}
	// Cairo is UTC+3 in July 2026, so 14:00 local is 11:00 UTC.
	want := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
	next, ok, err = definition.NextAfter(next)
	if err != nil || !ok || !next.Equal(time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("second next=%v ok=%t error=%v", next, ok, err)
	}
}

func TestWeeklyScheduleUsesMondayBasedMaskAndInterval(t *testing.T) {
	from := mustDate(t, 2026, time.July, 20) // Monday.
	until := mustDate(t, 2026, time.August, 20)
	mask, err := WeekdayMask(time.Monday, time.Wednesday)
	if err != nil {
		t.Fatal(err)
	}
	definition := recurringDefinition(Weekly, from, until)
	definition.Timezone = "UTC"
	definition.Interval = 2
	definition.WeekdayMask = mask
	definition.SecondOfDay = 20 * 3600
	next, ok, err := definition.NextAfter(time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC))
	if err != nil || !ok {
		t.Fatalf("NextAfter() next=%v ok=%t error=%v", next, ok, err)
	}
	if want := time.Date(2026, 7, 22, 20, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
	next, ok, err = definition.NextAfter(next)
	if err != nil || !ok {
		t.Fatalf("NextAfter(second) next=%v ok=%t error=%v", next, ok, err)
	}
	if want := time.Date(2026, 8, 3, 20, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestOneTimeScheduleExhaustsAfterItsInstant(t *testing.T) {
	runAt := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	definition := baseDefinition()
	definition.Recurrence = OneTime
	definition.RunAt = runAt
	next, ok, err := definition.NextAfter(runAt.Add(-time.Nanosecond))
	if err != nil || !ok || !next.Equal(runAt) {
		t.Fatalf("next=%v ok=%t error=%v", next, ok, err)
	}
	if _, ok, err := definition.NextAfter(runAt); err != nil || ok {
		t.Fatalf("exhausted schedule ok=%t error=%v", ok, err)
	}
}

func TestScheduleRejectsImplicitTimezoneAndInvalidScope(t *testing.T) {
	definition := baseDefinition()
	definition.Recurrence = OneTime
	definition.RunAt = time.Now()
	definition.Timezone = "Local"
	if err := definition.Validate(); err == nil {
		t.Fatal("implicit Local timezone accepted")
	}
	definition.Timezone = "UTC"
	definition.Scope = AllShards
	definition.ShardID = 1
	if err := definition.Validate(); err == nil {
		t.Fatal("all-shards definition with shard ID accepted")
	}
}

func baseDefinition() Definition {
	return Definition{
		ID: "schedule-1", Name: "Schedule", Handler: "event.start", Phase: Start,
		Scope: SingleShard, ShardID: 1, Timezone: "Africa/Cairo", MisfirePolicy: RunOnce, Enabled: true,
		Payload: []byte(`{}`),
	}
}

func recurringDefinition(recurrence Recurrence, from, until Date) Definition {
	definition := baseDefinition()
	definition.Recurrence = recurrence
	definition.ActiveFrom = &from
	definition.ActiveUntil = &until
	definition.Interval = 1
	return definition
}

func mustDate(t *testing.T, year int, month time.Month, day int) Date {
	t.Helper()
	date, err := NewDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
