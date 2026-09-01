package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	_ "github.com/go-sql-driver/mysql"
)

var errServerEventRewardImportUsage = errors.New("usage: sro-data import-server-event-rewards --media <Media directory>")

type serverEventRewardRow struct {
	RewardID, RefSkillID, RewardClass, Flags, ApplyTime int64
	RefValue1, RefValue2, RefValue3                     int64
	ExpRatio, SkillExpRatio                             float64
	Raw                                                 string
}
type serverEventRewardExpRow struct {
	RewardID                int64
	ExpRatio, SkillExpRatio float64
}
type serverEventRewardSpawnRow struct {
	RewardID, RefObjID, MonsterCount, Level int64
	Param1, Param2                          int64
}
type serverEventRewardEventRow struct{ ServerEventID, RewardID, AQType int64 }

func importServerEventRewardData(ctx context.Context, arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("sro-data import-server-event-rewards", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	media := flags.String("media", "", "path to the extracted Media directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *media == "" {
		return errServerEventRewardImportUsage
	}
	d := filepath.Join(*media, "server_dep", "silkroad", "textdata")
	rewards, exp, spawn, links, err := loadServerEventRewardReferenceData(d)
	if err != nil {
		return err
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer pool.Close()
	if err := importServerEventRewardRows(ctx, pool, rewards, exp, spawn, links); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d server-event rewards, %d experience rewards, %d spawn rewards, and %d event links\n", len(rewards), len(exp), len(spawn), len(links))
	return err
}

func loadServerEventRewardReferenceData(dir string) ([]serverEventRewardRow, []serverEventRewardExpRow, []serverEventRewardSpawnRow, []serverEventRewardEventRow, error) {
	rewards, err := parseServerEventRewardRows(filepath.Join(dir, "refservereventreward.txt"))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	exp, err := parseServerEventRewardExpRows(filepath.Join(dir, "refservereventreward_expupforplayers.txt"))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	spawn, err := parseServerEventRewardSpawnRows(filepath.Join(dir, "refservereventreward_spawnmonster.txt"))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	links, err := parseServerEventRewardEventRows(filepath.Join(dir, "refservereventreward_event.txt"))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	known := map[int64]bool{}
	for _, r := range rewards {
		known[r.RewardID] = true
	}
	for _, id := range func() []int64 {
		out := []int64{}
		for _, r := range exp {
			out = append(out, r.RewardID)
		}
		for _, r := range spawn {
			out = append(out, r.RewardID)
		}
		for _, r := range links {
			out = append(out, r.RewardID)
		}
		return out
	}() {
		if !known[id] {
			return nil, nil, nil, nil, fmt.Errorf("server-event reward %d is not present in base catalog", id)
		}
	}
	return rewards, exp, spawn, links, nil
}

func parseServerEventRewardRows(path string) ([]serverEventRewardRow, error) {
	return parseRows(path, 10, func(f []string, raw string) (serverEventRewardRow, error) {
		v := make([]int64, 8)
		for i := range v {
			n, e := strconv.ParseInt(f[i], 10, 64)
			if e != nil {
				return serverEventRewardRow{}, e
			}
			v[i] = n
		}
		a, e := strconv.ParseFloat(f[8], 64)
		if e != nil {
			return serverEventRewardRow{}, e
		}
		b, e := strconv.ParseFloat(f[9], 64)
		if e != nil {
			return serverEventRewardRow{}, e
		}
		return serverEventRewardRow{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], a, b, raw}, nil
	})
}
func parseServerEventRewardExpRows(path string) ([]serverEventRewardExpRow, error) {
	return parseRows(path, 3, func(f []string, _ string) (serverEventRewardExpRow, error) {
		id, e := strconv.ParseInt(f[0], 10, 64)
		if e != nil {
			return serverEventRewardExpRow{}, e
		}
		a, e := strconv.ParseFloat(f[1], 64)
		if e != nil {
			return serverEventRewardExpRow{}, e
		}
		b, e := strconv.ParseFloat(f[2], 64)
		return serverEventRewardExpRow{id, a, b}, e
	})
}
func parseServerEventRewardSpawnRows(path string) ([]serverEventRewardSpawnRow, error) {
	return parseRows(path, 6, func(f []string, _ string) (serverEventRewardSpawnRow, error) {
		v := make([]int64, 6)
		for i := range v {
			n, e := strconv.ParseInt(f[i], 10, 64)
			if e != nil {
				return serverEventRewardSpawnRow{}, e
			}
			v[i] = n
		}
		return serverEventRewardSpawnRow{v[0], v[1], v[2], v[3], v[4], v[5]}, nil
	})
}
func parseServerEventRewardEventRows(path string) ([]serverEventRewardEventRow, error) {
	return parseRows(path, 3, func(f []string, _ string) (serverEventRewardEventRow, error) {
		v := make([]int64, 3)
		for i := range v {
			n, e := strconv.ParseInt(f[i], 10, 64)
			if e != nil {
				return serverEventRewardEventRow{}, e
			}
			v[i] = n
		}
		return serverEventRewardEventRow{v[0], v[1], v[2]}, nil
	})
}
func parseRows[T any](path string, width int, fn func([]string, string) (T, error)) ([]T, error) {
	s, e := readClientTextFile(path)
	if e != nil {
		if os.IsNotExist(e) {
			return nil, nil
		}
		return nil, fmt.Errorf("open server-event catalog %q: %w", path, e)
	}
	var out []T
	for lineNo, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < width {
			return nil, fmt.Errorf("server-event line %d: want at least %d fields, got %d", lineNo+1, width, len(f))
		}
		r, e := fn(f, line)
		if e != nil {
			return nil, fmt.Errorf("server-event line %d: %w", lineNo+1, e)
		}
		out = append(out, r)
	}
	return out, nil
}

func importServerEventRewardRows(ctx context.Context, db *sql.DB, rewards []serverEventRewardRow, exp []serverEventRewardExpRow, spawn []serverEventRewardSpawnRow, links []serverEventRewardEventRow) error {
	if len(rewards)+len(exp)+len(spawn)+len(links) == 0 {
		return nil
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, r := range rewards {
		_, e = tx.ExecContext(ctx, "INSERT INTO REF_SERVER_EVENT_REWARD (REWARD_ID,REF_SKILL_ID,REWARD_CLASS,FLAGS,APPLY_TIME,REF_VALUE_1,REF_VALUE_2,REF_VALUE_3,EXP_RATIO,SKILL_EXP_RATIO,RAW_FIELDS) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE REF_SKILL_ID=VALUES(REF_SKILL_ID),REWARD_CLASS=VALUES(REWARD_CLASS),FLAGS=VALUES(FLAGS),APPLY_TIME=VALUES(APPLY_TIME),REF_VALUE_1=VALUES(REF_VALUE_1),REF_VALUE_2=VALUES(REF_VALUE_2),REF_VALUE_3=VALUES(REF_VALUE_3),EXP_RATIO=VALUES(EXP_RATIO),SKILL_EXP_RATIO=VALUES(SKILL_EXP_RATIO),RAW_FIELDS=VALUES(RAW_FIELDS)", r.RewardID, r.RefSkillID, r.RewardClass, r.Flags, r.ApplyTime, r.RefValue1, r.RefValue2, r.RefValue3, r.ExpRatio, r.SkillExpRatio, r.Raw)
		if e != nil {
			return e
		}
	}
	for _, r := range exp {
		_, e = tx.ExecContext(ctx, "INSERT INTO REF_SERVER_EVENT_REWARD_EXP_UP_FOR_PLAYERS VALUES (?,?,?) ON DUPLICATE KEY UPDATE EXP_RATIO=VALUES(EXP_RATIO),SKILL_EXP_RATIO=VALUES(SKILL_EXP_RATIO)", r.RewardID, r.ExpRatio, r.SkillExpRatio)
		if e != nil {
			return e
		}
	}
	for _, r := range spawn {
		_, e = tx.ExecContext(ctx, "INSERT INTO REF_SERVER_EVENT_REWARD_SPAWN_MONSTER VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE REF_OBJ_ID=VALUES(REF_OBJ_ID),MONSTER_COUNT=VALUES(MONSTER_COUNT),LEVEL=VALUES(LEVEL),PARAM_1=VALUES(PARAM_1),PARAM_2=VALUES(PARAM_2)", r.RewardID, r.RefObjID, r.MonsterCount, r.Level, r.Param1, r.Param2)
		if e != nil {
			return e
		}
	}
	for _, r := range links {
		_, e = tx.ExecContext(ctx, "INSERT INTO REF_SERVER_EVENT_REWARD_EVENT VALUES (?,?,?) ON DUPLICATE KEY UPDATE AQ_TYPE=VALUES(AQ_TYPE)", r.ServerEventID, r.RewardID, r.AQType)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
