package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errDimensionHoleVerifyUsage = errors.New("usage: sro-data verify-dimension-hole")

type dimensionHoleReadinessReport struct {
	worlds, regions, starts int
}

func verifyDimensionHole(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data verify-dimension-hole", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errDimensionHoleVerifyUsage
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if output == nil {
		output = io.Discard
	}
	config.Initialize()
	database, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, database.Close()) }()
	catalogRepository, err := model.NewDimensionHoleCatalogRepository(database)
	if err != nil {
		return err
	}
	if _, err = catalogRepository.LoadDimensionHoleCatalog(ctx); err != nil {
		return fmt.Errorf("verify Dimension Hole definitions: %w", err)
	}

	worldRepository, err := model.NewDimensionHoleWorldRepository(database)
	if err != nil {
		return err
	}
	regionRepository, err := model.NewDimensionHoleWorldRegionRepository(database)
	if err != nil {
		return err
	}
	startRepository, err := model.NewDimensionHoleStartPositionRepository(database)
	if err != nil {
		return err
	}
	worlds, err := worldRepository.LoadDimensionHoleWorldCatalog(ctx)
	if err != nil {
		return fmt.Errorf("verify Dimension Hole worlds: %w", err)
	}
	regions, err := regionRepository.LoadDimensionHoleWorldRegionCatalog(ctx)
	if err != nil {
		return fmt.Errorf("verify Dimension Hole world regions: %w", err)
	}
	starts, err := startRepository.LoadDimensionHoleStartPositionCatalog(ctx, regions)
	if err != nil {
		return fmt.Errorf("verify Dimension Hole start positions: %w", err)
	}
	report := summarizeDimensionHoleReadiness(worlds, regions, starts)
	_, err = fmt.Fprintf(output, "dimension-hole status=ok worlds=%d regions=%d starts=%d\n", report.worlds, report.regions, report.starts)
	return err
}

func summarizeDimensionHoleReadiness(worlds *model.DimensionHoleWorldCatalog, regions *model.DimensionHoleWorldRegionCatalog, starts *model.DimensionHoleStartPositionCatalog) dimensionHoleReadinessReport {
	report := dimensionHoleReadinessReport{}
	if worlds != nil {
		report.worlds = len(worlds.Worlds())
	}
	for _, definition := range model.DimensionHoleCatalog() {
		if regions != nil {
			report.regions += len(regions.Regions(definition.WorldID))
		}
		if starts != nil {
			report.starts += len(starts.Positions(definition.WorldID))
		}
	}
	return report
}
