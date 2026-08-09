package main

import (
	"errors"
	"testing"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

func TestSelectMigrationTargetAcceptsExactlyOneKnownTarget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		arguments []string
		want      frameworkdb.MigrationTarget
	}{
		{name: "account equals", arguments: []string{"--target=account"}, want: frameworkdb.MigrationTargetAccount},
		{name: "shard separated", arguments: []string{"--target", "shard"}, want: frameworkdb.MigrationTargetShard},
		{name: "trimmed case insensitive", arguments: []string{"--target= ShArD "}, want: frameworkdb.MigrationTargetShard},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectMigrationTarget(testCase.arguments)
			if err != nil {
				t.Fatalf("selectMigrationTarget(%q): %v", testCase.arguments, err)
			}
			if got != testCase.want {
				t.Fatalf("selectMigrationTarget(%q) = %q, want %q", testCase.arguments, got, testCase.want)
			}
		})
	}
}

func TestSelectMigrationTargetRejectsAmbiguousOrInvalidInput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		arguments []string
	}{
		{name: "missing"},
		{name: "value missing", arguments: []string{"--target"}},
		{name: "duplicate", arguments: []string{"--target=account", "--target=shard"}},
		{name: "unknown target", arguments: []string{"--target=both"}},
		{name: "unknown flag", arguments: []string{"--all"}},
		{name: "positional argument", arguments: []string{"account"}},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := selectMigrationTarget(testCase.arguments)
			if !errors.Is(err, errMigrationTargetSelection) {
				t.Fatalf("selectMigrationTarget(%q) error = %v, want target-selection error", testCase.arguments, err)
			}
		})
	}
}
