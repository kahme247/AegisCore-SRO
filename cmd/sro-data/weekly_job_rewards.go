package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Harvi8/AegisCore/agent-server/model"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

func distributeWeeklyJobRewards(ctx context.Context, arguments []string, output io.Writer) error {
	if len(arguments) != 0 {
		return errUsage
	}
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer pool.Close()
	repository, err := model.NewSocialRepository(pool)
	if err != nil {
		return err
	}
	if err = repository.DistributeWeeklyJobRewards(ctx); err != nil {
		return fmt.Errorf("distribute weekly tri-job rewards: %w", err)
	}
	_, err = fmt.Fprintln(output, "distributed weekly tri-job rewards")
	return err
}
