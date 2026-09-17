package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrTAPInvalidRecord = errors.New("invalid TAP record")

type TAPClock struct {
	Month  byte
	Day    byte
	Hour   byte
	Minute byte
}

type TAPRecord struct {
	ID                uint64
	NPCRefObjectID    uint32
	EntryType         byte
	From              TAPClock
	To                TAPClock
	TraderUnionPoints uint32
	ThiefUnionPoints  uint32
}

type TAPPoints struct {
	Trader uint32
	Thief  uint32
}

type TAPRepository interface {
	Snapshot(context.Context, uint32) ([]TAPRecord, error)
	Points(context.Context, []uint64) ([]TAPPoints, error)
}

type SQLTAPRepository struct{ shard *sql.DB }

func NewTAPRepository(shard *sql.DB) (TAPRepository, error) {
	if shard == nil {
		return nil, errors.New("TAP shard database is required")
	}
	return &SQLTAPRepository{shard: shard}, nil
}

func (repository *SQLTAPRepository) Snapshot(ctx context.Context, npcRefObjectID uint32) ([]TAPRecord, error) {
	query := "SELECT `ID`,`NPC_REF_OBJECT_ID`,`ENTRY_TYPE`,`FROM_MONTH`,`FROM_DAY`,`FROM_HOUR`,`FROM_MINUTE`,`TO_MONTH`,`TO_DAY`,`TO_HOUR`,`TO_MINUTE`,`TRADER_UNION_POINTS`,`THIEF_UNION_POINTS` FROM `TAP_PERIOD` WHERE `ENABLED`=1"
	arguments := make([]any, 0, 1)
	if npcRefObjectID != 0 {
		query += " AND (`NPC_REF_OBJECT_ID`=0 OR `NPC_REF_OBJECT_ID`=?)"
		arguments = append(arguments, npcRefObjectID)
	}
	query += " ORDER BY `ID` LIMIT 255"
	rows, err := repository.shard.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("load TAP snapshot: %w", err)
	}
	defer rows.Close()
	records := make([]TAPRecord, 0)
	for rows.Next() {
		var record TAPRecord
		if err = rows.Scan(&record.ID, &record.NPCRefObjectID, &record.EntryType, &record.From.Month, &record.From.Day, &record.From.Hour, &record.From.Minute, &record.To.Month, &record.To.Day, &record.To.Hour, &record.To.Minute, &record.TraderUnionPoints, &record.ThiefUnionPoints); err != nil {
			return nil, fmt.Errorf("scan TAP snapshot: %w", err)
		}
		if !validTAPRecord(record) {
			return nil, ErrTAPInvalidRecord
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (repository *SQLTAPRepository) Points(ctx context.Context, ids []uint64) ([]TAPPoints, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	points := make([]TAPPoints, len(ids))
	for index, id := range ids {
		if id == 0 {
			return nil, ErrTAPInvalidRecord
		}
		if err := repository.shard.QueryRowContext(ctx, "SELECT `TRADER_UNION_POINTS`,`THIEF_UNION_POINTS` FROM `TAP_PERIOD` WHERE `ID`=? AND `ENABLED`=1", id).Scan(&points[index].Trader, &points[index].Thief); err != nil {
			return nil, fmt.Errorf("load TAP points for period %d: %w", id, err)
		}
	}
	return points, nil
}

func TAPRecordActive(record TAPRecord, now time.Time) bool {
	if !validTAPRecord(record) {
		return false
	}
	year := now.Year()
	from := time.Date(year, time.Month(record.From.Month), int(record.From.Day), int(record.From.Hour), int(record.From.Minute), 0, 0, now.Location())
	to := time.Date(year, time.Month(record.To.Month), int(record.To.Day), int(record.To.Hour), int(record.To.Minute), 0, 0, now.Location())
	if to.Before(from) {
		if now.Before(from) {
			from = from.AddDate(-1, 0, 0)
		} else {
			to = to.AddDate(1, 0, 0)
		}
	}
	return !now.Before(from) && now.Before(to)
}

func validTAPRecord(record TAPRecord) bool {
	if record.ID == 0 || (record.EntryType != 2 && record.EntryType != 3) {
		return false
	}
	return validTAPClock(record.From) && validTAPClock(record.To)
}

func validTAPClock(clock TAPClock) bool {
	if clock.Month < 1 || clock.Month > 12 || clock.Day < 1 || clock.Day > 31 || clock.Hour > 23 || clock.Minute > 59 {
		return false
	}
	value := time.Date(2000, time.Month(clock.Month), int(clock.Day), int(clock.Hour), int(clock.Minute), 0, 0, time.UTC)
	return int(value.Month()) == int(clock.Month) && value.Day() == int(clock.Day)
}
