// Command sro-data contains offline data-maintenance entry points.
//
// Verify reads a profile and an archive, produces a provenance document, and
// exits. Import-reference is an explicitly invoked maintenance operation that
// loads client-matched LevelData and SkillData into SRO_SHARD.
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

	"github.com/ferdoran/go-sro/framework/reference"
	"github.com/ferdoran/go-sro/protocol/profiles"
)

var errUsage = errors.New("usage: sro-data verify --profile <profile.json> --archive <Data.pk2> | sro-data import-reference --media <Media directory> | sro-data import-vsro-skills --export-dir <directory>")

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
	if arguments[0] == "import-reference" {
		return importReferenceData(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-vsro-skills" {
		return importVSROSkillReference(ctx, arguments[1:], output)
	}
	if arguments[0] == "import-vsro-items" {
		return importVSROItemReference(ctx, arguments[1:], output)
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
