package main

import (
	"errors"
	"fmt"
	"strings"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errMigrationTargetSelection = errors.New("exactly one migration target is required")

// selectMigrationTarget parses the deliberately small command-line surface of
// the maintenance job. It is kept pure so it can be exhaustively tested and so
// no environment default can accidentally migrate both schemas.
//
// Accepted forms are --target account and --target=account (with shard as the
// only other target). A duplicate target, any positional argument, or an
// unknown flag is rejected rather than silently choosing a last value.
func selectMigrationTarget(arguments []string) (frameworkdb.MigrationTarget, error) {
	values := make([]string, 0, 1)
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--target":
			if index+1 >= len(arguments) {
				return "", fmt.Errorf("%w: --target needs account or shard", errMigrationTargetSelection)
			}
			index++
			values = append(values, arguments[index])
		case strings.HasPrefix(argument, "--target="):
			values = append(values, strings.TrimPrefix(argument, "--target="))
		default:
			return "", fmt.Errorf("%w: unsupported argument %q", errMigrationTargetSelection, argument)
		}
	}

	if len(values) != 1 {
		return "", fmt.Errorf("%w: pass one --target=account or --target=shard", errMigrationTargetSelection)
	}

	switch strings.ToLower(strings.TrimSpace(values[0])) {
	case string(frameworkdb.MigrationTargetAccount):
		return frameworkdb.MigrationTargetAccount, nil
	case string(frameworkdb.MigrationTargetShard):
		return frameworkdb.MigrationTargetShard, nil
	default:
		return "", fmt.Errorf("%w: %q is not account or shard", errMigrationTargetSelection, values[0])
	}
}
