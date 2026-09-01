package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errOptionalTeleportImportUsage = errors.New("usage: sro-data import-optional-teleports --file <refoptionalteleport.txt>")

type optionalTeleportRow struct {
	id                 uint32
	zone               string
	region             int16
	x, y, z            float32
	world              uint16
	group              int
	mapPoint           byte
	minLevel, maxLevel uint16
}

func importOptionalTeleports(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-optional-teleports", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("file", "", "refoptionalteleport.txt path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *path == "" {
		return errOptionalTeleportImportUsage
	}
	rows, err := readOptionalTeleports(*path)
	if err != nil {
		return err
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	if err = replaceOptionalTeleports(ctx, pool, rows); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d optional teleport locations\n", len(rows))
	return err
}

func readOptionalTeleports(path string) ([]optionalTeleportRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open optional teleport catalog: %w", err)
	}
	text, err := decodeOptionalTeleportText(data)
	if err != nil {
		return nil, err
	}
	var rows []optionalTeleportRow
	scanner := bufio.NewScanner(strings.NewReader(text))
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Split(strings.TrimSuffix(scanner.Text(), "\r"), "\t")
		if len(fields) < 13 {
			return nil, fmt.Errorf("optional teleport line %d has %d fields, want at least 13", line, len(fields))
		}
		service, e := strconv.Atoi(fields[0])
		if e != nil {
			return nil, fmt.Errorf("optional teleport line %d service: %w", line, e)
		}
		if service == 0 {
			continue
		}
		ints := make([]int64, 0, 10)
		for _, index := range []int{1, 4, 5, 6, 7, 8, 9, 10, 11, 12} {
			value, parseErr := strconv.ParseInt(fields[index], 10, 32)
			if parseErr != nil {
				return nil, fmt.Errorf("optional teleport line %d field %d: %w", line, index, parseErr)
			}
			ints = append(ints, value)
		}
		rows = append(rows, optionalTeleportRow{id: uint32(ints[0]), zone: fields[3], region: int16(ints[1]), x: float32(ints[2]), y: float32(ints[3]), z: float32(ints[4]), world: uint16(ints[5]), group: int(ints[6]), mapPoint: byte(ints[7]), minLevel: uint16(ints[8]), maxLevel: uint16(ints[9])})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func decodeOptionalTeleportText(data []byte) (string, error) {
	if len(data) >= 2 && (data[0] == 0xff && data[1] == 0xfe || data[0] == 0xfe && data[1] == 0xff) {
		if (len(data)-2)%2 != 0 {
			return "", fmt.Errorf("optional teleport UTF-16 input has an odd byte count")
		}
		littleEndian := data[0] == 0xff
		units := make([]uint16, 0, (len(data)-2)/2)
		for offset := 2; offset < len(data); offset += 2 {
			if littleEndian {
				units = append(units, uint16(data[offset])|uint16(data[offset+1])<<8)
			} else {
				units = append(units, uint16(data[offset])<<8|uint16(data[offset+1]))
			}
		}
		return string(utf16.Decode(units)), nil
	}
	return strings.TrimPrefix(string(data), "\ufeff"), nil
}

func replaceOptionalTeleports(ctx context.Context, pool *sql.DB, rows []optionalTeleportRow) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM `REF_OPTIONAL_TELEPORT`"); err != nil {
		return fmt.Errorf("clear optional teleports: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, "INSERT INTO `REF_OPTIONAL_TELEPORT` (`ID`,`ZONE_NAME`,`REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`,`WORLD_ID`,`REGION_GROUP`,`MAP_POINT`,`MIN_LEVEL`,`MAX_LEVEL`) VALUES (?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, row := range rows {
		if _, err = statement.ExecContext(ctx, row.id, row.zone, row.region, row.x, row.y, row.z, row.world, row.group, row.mapPoint, row.minLevel, row.maxLevel); err != nil {
			return fmt.Errorf("insert optional teleport %d: %w", row.id, err)
		}
	}
	return tx.Commit()
}
