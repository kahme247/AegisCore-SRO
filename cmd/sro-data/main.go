// Command sro-data contains offline data-maintenance entry points.
//
// Verify reads a profile and an archive, produces a provenance document, and
// exits. Import commands are explicitly invoked maintenance operations that
// load client-matched data into SRO_SHARD.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Harvi8/AegisCore/framework/reference"
	"github.com/Harvi8/AegisCore/protocol/profiles"
)

var errUsage = errors.New("usage: sro-data <verify|media-map|media-sync|audit-item-coverage|verify-drop-catalog|verify-dimension-hole|export-media|import-media-catalogs|import-reference|import-drop-catalog|import-event-rewards|import-server-event-rewards|import-item-mall|import-specialty-quotations|distribute-weekly-job-rewards|import-optional-teleports|import-vsro-skills|import-vsro-items|import-vsro-quests|inspect-vsro-quest-script|import-vsro-quest-script> [options]")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "sro-data failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, output io.Writer) error {
	canonical, err := profiles.VSRO188Profile()
	if err != nil {
		return fmt.Errorf("load embedded canonical profile: %w", err)
	}
	return runWithCanonicalProfile(ctx, arguments, output, canonical)
}

func runWithCanonicalProfile(ctx context.Context, arguments []string, output io.Writer, canonical profiles.Profile) error {
	if len(arguments) == 0 {
		return errUsage
	}
	if arguments[0] == "media-map" {
		return mediaMap(ctx, arguments[1:], output)
	}
	if arguments[0] == "media-sync" {
		return mediaSync(ctx, arguments[1:], output)
	}
	if arguments[0] == "audit-item-coverage" {
		return auditItemCoverage(ctx, arguments[1:], output)
	}
	if arguments[0] == "verify-drop-catalog" {
		return verifyDropCatalog(ctx, arguments[1:], output)
	}
	if arguments[0] == "verify-dimension-hole" {
		return verifyDimensionHole(ctx, arguments[1:], output)
	}
	if arguments[0] == "export-media" {
		return exportMedia(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-media-catalogs" {
		return importMediaCatalogs(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-reference" {
		return importReferenceData(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-drop-catalog" {
		return importDropCatalog(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-event-rewards" {
		return importEventRewardData(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-server-event-rewards" {
		return importServerEventRewardData(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-item-mall" {
		return importItemMall(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-specialty-quotations" {
		return importSpecialtyQuotations(ctx, arguments[1:], output)
	}
	if arguments[0] == "distribute-weekly-job-rewards" {
		return distributeWeeklyJobRewards(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-optional-teleports" {
		return importOptionalTeleports(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-vsro-skills" {
		return importVSROSkillReference(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-vsro-items" {
		return importVSROItemReference(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-vsro-quests" {
		return importVSROQuestReference(ctx, arguments[1:], output)
	}
	if arguments[0] == "inspect-vsro-quest-script" {
		return inspectVSROQuestScript(arguments[1:], output)
	}
	if arguments[0] == "import-vsro-quest-script" {
		return importVSROQuestScript(ctx, arguments[1:], output)
	}
	if arguments[0] != "verify" {
		return errUsage
	}

	flags := flag.NewFlagSet("sro-data verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	profilePath := flags.String("profile", "", "profile JSON path")
	archivePath := flags.String("archive", "", "Data.pk2 path")
	if err := flags.Parse(arguments[1:]); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 || *profilePath == "" || *archivePath == "" {
		return errUsage
	}

	profile, err := profiles.LoadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("load supplied profile: %w", err)
	}
	manifest, err := reference.VerifyArchive(ctx, reference.VerificationRequest{
		CanonicalProfile: canonical,
		Profile:          profile,
		ArchivePath:      *archivePath,
	})
	if err != nil {
		return err
	}
	encoded, err := manifest.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("encode provenance manifest: %w", err)
	}
	if _, err := fmt.Fprintln(output, string(encoded)); err != nil {
		return fmt.Errorf("write provenance manifest: %w", err)
	}
	return nil
}
