package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errMediaMapUsage = errors.New("usage: sro-data media-map [--file name] [--table name]")

const selectMediaTableMappings = `SELECT MEDIA_PATTERN,DATABASE_TABLES,AUTHORITY,DATABASE_UPDATE,MEDIA_SYNC,RECONCILE_MODE,NOTES
FROM SRO_SHARD.MEDIA_CATALOG_MAPPING
WHERE SERVICE=1
ORDER BY MEDIA_PATTERN`

type mediaTableMapping struct {
	File           string
	Tables         string
	Authority      string
	DatabaseUpdate string
	MediaSync      string
	ReconcileMode  string
	Notes          string
}

type mediaMappingQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func mediaMap(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	mappings, err := loadMediaTableMappings(ctx, pool)
	if err != nil {
		return err
	}
	return writeMediaMap(arguments, output, mappings)
}

func loadMediaTableMappings(ctx context.Context, queryer mediaMappingQueryer) ([]mediaTableMapping, error) {
	if queryer == nil {
		return nil, errors.New("Media mapping registry is unavailable")
	}
	rows, err := queryer.QueryContext(ctx, selectMediaTableMappings)
	if err != nil {
		return nil, fmt.Errorf("query Media mapping registry: %w", err)
	}
	defer rows.Close()
	mappings := make([]mediaTableMapping, 0)
	for rows.Next() {
		var mapping mediaTableMapping
		if err := rows.Scan(&mapping.File, &mapping.Tables, &mapping.Authority, &mapping.DatabaseUpdate, &mapping.MediaSync, &mapping.ReconcileMode, &mapping.Notes); err != nil {
			return nil, fmt.Errorf("scan Media mapping registry: %w", err)
		}
		mappings = append(mappings, mapping)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Media mapping registry: %w", err)
	}
	if len(mappings) == 0 {
		return nil, errors.New("Media mapping registry contains no enabled rows")
	}
	return mappings, nil
}

func writeMediaMap(arguments []string, output io.Writer, mappings []mediaTableMapping) error {
	flags := flag.NewFlagSet("sro-data media-map", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fileFilter := flags.String("file", "", "Media filename or substring")
	tableFilter := flags.String("table", "", "database table or substring")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errMediaMapUsage
	}
	if output == nil {
		output = io.Discard
	}
	fmt.Fprintln(output, "MEDIA_FILE\tDATABASE_TABLES\tAUTHORITY\tDATABASE_UPDATE\tMEDIA_SYNC\tRECONCILE\tNOTES")
	matched := 0
	for _, mapping := range mappings {
		if !mediaMapMatches(mapping.File, *fileFilter) || !containsFold(mapping.Tables, *tableFilter) {
			continue
		}
		matched++
		fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", mapping.File, mapping.Tables, mapping.Authority, mapping.DatabaseUpdate, mapping.MediaSync, mapping.ReconcileMode, mapping.Notes)
	}
	if matched == 0 && strings.TrimSpace(*fileFilter) != "" && strings.TrimSpace(*tableFilter) == "" {
		fmt.Fprintf(output, "%s\tnone\tunmapped\tnone\tnot-supported\tnone\trequires schema and runtime review\n", filepath.Base(*fileFilter))
	}
	return nil
}

func mediaMapMatches(pattern, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" || containsFold(pattern, filter) {
		return true
	}
	matched, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(filepath.Base(filter)))
	return err == nil && matched
}

func containsFold(value, filter string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(strings.TrimSpace(filter)))
}
