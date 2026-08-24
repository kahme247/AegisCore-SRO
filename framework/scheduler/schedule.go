// Package scheduler defines deterministic persistent shard schedules. All
// instants stored by repositories are UTC; recurrence is evaluated in the
// definition's explicit IANA timezone.
package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Recurrence string

const (
	OneTime Recurrence = "one_time"
	Daily   Recurrence = "daily"
	Weekly  Recurrence = "weekly"
)

type Phase string

const (
	Warning Phase = "warning"
	Start   Phase = "start"
	Update  Phase = "update"
	Finish  Phase = "finish"
)

type Scope string

const (
	SingleShard Scope = "shard"
	AllShards   Scope = "all_shards"
)

type MisfirePolicy string

const (
	SkipMissed MisfirePolicy = "skip"
	RunOnce    MisfirePolicy = "run_once"
)

type Date struct {
	Year  int
	Month time.Month
	Day   int
}

func NewDate(year int, month time.Month, day int) (Date, error) {
	date := Date{Year: year, Month: month, Day: day}
	if err := date.Validate(); err != nil {
		return Date{}, err
	}
	return date, nil
}

func (date Date) Validate() error {
	if date.Year < 1 || date.Year > 9999 || date.Month < time.January || date.Month > time.December || date.Day < 1 || date.Day > 31 {
		return errors.New("scheduler date is outside the supported calendar")
	}
	validated := time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC)
	if validated.Year() != date.Year || validated.Month() != date.Month || validated.Day() != date.Day {
		return errors.New("scheduler date is not a real calendar day")
	}
	return nil
}

func (date Date) Compare(other Date) int {
	left, right := date.ordinal(), other.ordinal()
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (date Date) ordinal() int64 {
	return time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).Unix() / int64(24*time.Hour/time.Second)
}

func dateFromTime(value time.Time) Date {
	return Date{Year: value.Year(), Month: value.Month(), Day: value.Day()}
}

func (date Date) addDays(days int) Date {
	value := time.Date(date.Year, date.Month, date.Day+days, 0, 0, 0, 0, time.UTC)
	return dateFromTime(value)
}

type Definition struct {
	ID            string
	Name          string
	Handler       string
	Recurrence    Recurrence
	Phase         Phase
	Scope         Scope
	ShardID       uint16
	Timezone      string
	ActiveFrom    *Date
	ActiveUntil   *Date
	RunAt         time.Time
	SecondOfDay   uint32
	Interval      uint16
	WeekdayMask   uint8
	MisfirePolicy MisfirePolicy
	Enabled       bool
	Version       uint64
	NextRunAt     time.Time
	Payload       json.RawMessage
}

func (definition Definition) Validate() error {
	for field, value := range map[string]string{"ID": definition.ID, "name": definition.Name, "handler": definition.Handler} {
		if strings.TrimSpace(value) == "" || len(value) > 128 || strings.TrimSpace(value) != value {
			return fmt.Errorf("scheduler %s must be 1-128 trimmed characters", field)
		}
	}
	if _, err := time.LoadLocation(definition.Timezone); err != nil || definition.Timezone == "Local" {
		return fmt.Errorf("scheduler timezone %q is not an explicit IANA timezone", definition.Timezone)
	}
	switch definition.Phase {
	case Warning, Start, Update, Finish:
	default:
		return fmt.Errorf("invalid scheduler phase %q", definition.Phase)
	}
	switch definition.Scope {
	case SingleShard:
		if definition.ShardID == 0 {
			return errors.New("single-shard schedule requires a shard ID")
		}
	case AllShards:
		if definition.ShardID != 0 {
			return errors.New("all-shards schedule cannot carry a shard ID")
		}
	default:
		return fmt.Errorf("invalid scheduler scope %q", definition.Scope)
	}
	if definition.MisfirePolicy != SkipMissed && definition.MisfirePolicy != RunOnce {
		return fmt.Errorf("invalid scheduler misfire policy %q", definition.MisfirePolicy)
	}
	if definition.SecondOfDay >= 24*60*60 {
		return errors.New("scheduler second-of-day is outside a day")
	}
	if len(definition.Payload) == 0 || !json.Valid(definition.Payload) {
		return errors.New("scheduler payload must be one valid JSON value")
	}
	switch definition.Recurrence {
	case OneTime:
		if definition.RunAt.IsZero() || definition.ActiveFrom != nil || definition.ActiveUntil != nil || definition.Interval != 0 || definition.WeekdayMask != 0 || definition.SecondOfDay != 0 {
			return errors.New("one-time schedule has incompatible recurrence fields")
		}
	case Daily, Weekly:
		if !definition.RunAt.IsZero() || definition.ActiveFrom == nil || definition.ActiveUntil == nil || definition.Interval == 0 {
			return errors.New("recurring schedule requires date range and positive interval")
		}
		if err := definition.ActiveFrom.Validate(); err != nil {
			return err
		}
		if err := definition.ActiveUntil.Validate(); err != nil {
			return err
		}
		if definition.ActiveFrom.Compare(*definition.ActiveUntil) > 0 {
			return errors.New("scheduler active date range is inverted")
		}
		if definition.Recurrence == Daily && definition.WeekdayMask != 0 {
			return errors.New("daily schedule cannot carry a weekday mask")
		}
		if definition.Recurrence == Weekly && (definition.WeekdayMask == 0 || definition.WeekdayMask&0x80 != 0) {
			return errors.New("weekly schedule requires a seven-bit weekday mask")
		}
	default:
		return fmt.Errorf("invalid scheduler recurrence %q", definition.Recurrence)
	}
	return nil
}

// NextAfter returns the first scheduled UTC instant strictly after the input.
// A false result means the finite schedule is exhausted.
func (definition Definition) NextAfter(after time.Time) (time.Time, bool, error) {
	if err := definition.Validate(); err != nil {
		return time.Time{}, false, err
	}
	after = after.UTC()
	if definition.Recurrence == OneTime {
		runAt := definition.RunAt.UTC()
		return runAt, runAt.After(after), nil
	}
	location, _ := time.LoadLocation(definition.Timezone)
	localAfter := after.In(location)
	candidateDate := dateFromTime(localAfter)
	if candidateDate.Compare(*definition.ActiveFrom) < 0 {
		candidateDate = *definition.ActiveFrom
	}
	for candidateDate.Compare(*definition.ActiveUntil) <= 0 {
		days := candidateDate.ordinal() - definition.ActiveFrom.ordinal()
		eligible := definition.Recurrence == Daily && days%int64(definition.Interval) == 0
		if definition.Recurrence == Weekly {
			weekday := time.Date(candidateDate.Year, candidateDate.Month, candidateDate.Day, 12, 0, 0, 0, location).Weekday()
			mondayIndex := (int(weekday) + 6) % 7
			eligible = (days/7)%int64(definition.Interval) == 0 && definition.WeekdayMask&(1<<mondayIndex) != 0
		}
		if eligible {
			hour := int(definition.SecondOfDay / 3600)
			minute := int((definition.SecondOfDay % 3600) / 60)
			second := int(definition.SecondOfDay % 60)
			candidate := time.Date(candidateDate.Year, candidateDate.Month, candidateDate.Day, hour, minute, second, 0, location)
			// Skip nonexistent local wall times during a daylight-saving jump.
			if candidate.Year() == candidateDate.Year && candidate.Month() == candidateDate.Month && candidate.Day() == candidateDate.Day && candidate.Hour() == hour && candidate.Minute() == minute && candidate.Second() == second && candidate.UTC().After(after) {
				return candidate.UTC(), true, nil
			}
		}
		candidateDate = candidateDate.addDays(1)
	}
	return time.Time{}, false, nil
}

func WeekdayMask(days ...time.Weekday) (uint8, error) {
	var mask uint8
	for _, day := range days {
		if day < time.Sunday || day > time.Saturday {
			return 0, fmt.Errorf("invalid weekday %d", day)
		}
		mondayIndex := (int(day) + 6) % 7
		mask |= 1 << mondayIndex
	}
	if mask == 0 {
		return 0, errors.New("weekday mask is empty")
	}
	return mask, nil
}
