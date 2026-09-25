package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errMediaSyncUsage = errors.New("usage: sro-data media-sync --output <new-directory> [--table name | --supported-only]")

func mediaSync(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data media-sync", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputDirectory := flags.String("output", "", "new extracted Media directory")
	tableFilter := flags.String("table", "", "synchronize the Media files mapped to this table")
	supportedOnly := flags.Bool("supported-only", false, "export every currently supported database-backed catalog")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || strings.TrimSpace(*outputDirectory) == "" || (*supportedOnly && strings.TrimSpace(*tableFilter) != "") {
		return errMediaSyncUsage
	}
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
	selected, blockers := evaluateMediaSyncMappings(mappings, *tableFilter, *supportedOnly)
	if len(selected) == 0 {
		return errors.New("Media sync selection contains no database-backed catalogs")
	}
	if len(blockers) != 0 {
		return fmt.Errorf("Media sync is not implemented for: %s", strings.Join(blockers, ", "))
	}
	return exportMedia(ctx, []string{"--output", *outputDirectory}, output)
}

func evaluateMediaSyncMappings(mappings []mediaTableMapping, tableFilter string, supportedOnly bool) (selected, blockers []string) {
	for _, mapping := range mappings {
		if mapping.Authority != "database" || tableFilter != "" && !containsFold(mapping.Tables, tableFilter) {
			continue
		}
		if supportedOnly && mapping.MediaSync != "export-media" {
			continue
		}
		selected = append(selected, mapping.File)
		if mapping.MediaSync != "export-media" || mapping.ReconcileMode != "snapshot" {
			blockers = append(blockers, mapping.File)
		}
	}
	sort.Strings(selected)
	sort.Strings(blockers)
	return selected, blockers
}
