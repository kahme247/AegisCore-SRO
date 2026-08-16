package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	frameworkconfig "github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/logging"
	"github.com/Harvi8/AegisCore/patching/builder"
	patchcatalog "github.com/Harvi8/AegisCore/patching/catalog"
	log "github.com/sirupsen/logrus"
)

const operationTimeout = 30 * time.Minute

func main() {
	frameworkconfig.Initialize()
	logging.Init()
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return usageError()
	}
	switch arguments[0] {
	case "scan-media":
		return runScanMedia(arguments[1:])
	case "build":
		return runBuild(arguments[1:])
	case "testing":
		return runTesting(arguments[1:])
	case "promote":
		return runPromote(arguments[1:])
	case "register-server":
		return runRegisterServer(arguments[1:])
	default:
		return usageError()
	}
}

func runScanMedia(arguments []string) error {
	flags := flag.NewFlagSet("scan-media", flag.ContinueOnError)
	archive := flags.String("archive", "VSRO_Client/Media.pk2", "deployed baseline Media.pk2")
	targetRoot := flags.String("target-root", "Media/Media", "approved extracted target Media tree")
	output := flags.String("output", "PatchSource/188-to-189.json", "reviewable output build specification")
	divisionID := flags.Uint("division-id", 22, "legacy division ID")
	contentID := flags.Uint("content-id", 22, "client content ID")
	moduleID := flags.Uint("module-id", 9, "legacy module ID")
	moduleName := flags.String("module-name", "SR_Client", "client module name")
	baseVersion := flags.Uint("base-version", 188, "deployed baseline version")
	targetVersion := flags.Uint("target-version", 189, "candidate target version")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	for name, value := range map[string]uint{"division-id": *divisionID, "content-id": *contentID, "module-id": *moduleID} {
		if value == 0 || value > 255 {
			return fmt.Errorf("%s must be 1..255", name)
		}
	}
	if *baseVersion == 0 || *targetVersion == 0 || uint64(*baseVersion) > uint64(^uint32(0)) || uint64(*targetVersion) > uint64(^uint32(0)) {
		return errors.New("base-version and target-version must be uint32 values")
	}
	result, err := builder.ScanPK2(*archive, *targetRoot, "media")
	if err != nil {
		return err
	}
	spec := builder.BuildSpec{
		Release: builder.ReleaseSpec{
			DivisionID: uint8(*divisionID), ContentID: uint8(*contentID), ModuleID: uint8(*moduleID), ModuleName: *moduleName,
			BaseVersion: uint32(*baseVersion), TargetVersion: uint32(*targetVersion), DisplayVersion: fmt.Sprintf("1.%d", *targetVersion),
			Description: fmt.Sprintf("Reviewed %d to %d Media update", *baseVersion, *targetVersion),
		},
		Files: result.Changed,
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
		return err
	}
	if err := builder.WriteSpec(*output, spec); err != nil {
		return err
	}
	fmt.Printf("scanned Media.pk2 changed=%d unchanged=%d skipped=%d spec=%s\n", len(result.Changed), result.Unchanged, len(result.Skipped), *output)
	for index, skipped := range result.Skipped {
		if index == 20 {
			fmt.Printf("... %d additional skipped entries\n", len(result.Skipped)-index)
			break
		}
		fmt.Printf("skipped: %s\n", skipped)
	}
	return nil
}

func runBuild(arguments []string) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	specPath := flags.String("spec", "", "approved patch build specification")
	sourceRoot := flags.String("source-root", "", "controlled extracted client source root")
	storageRoot := flags.String("storage-root", "Patch/files", "prepared payload storage root")
	manifestPath := flags.String("manifest", "Patch/manifest.json", "DownloadServer serving manifest")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *specPath == "" || *sourceRoot == "" {
		return errors.New("build requires --spec and --source-root")
	}
	spec, err := builder.LoadSpec(*specPath)
	if err != nil {
		return err
	}
	result, err := builder.Build(spec, *sourceRoot, *storageRoot)
	if err != nil {
		return err
	}
	return withAdminRepository(func(ctx context.Context, repository *patchcatalog.AdminRepository) error {
		release, err := repository.CreateDraft(ctx, result)
		if err != nil {
			return err
		}
		if err := patchcatalog.WriteServingManifest(*manifestPath, release); err != nil {
			return fmt.Errorf("draft %d was created but serving manifest generation failed: %w", release.ID, err)
		}
		fmt.Printf("created draft release id=%d version=%d files=%d manifest=%s\n", release.ID, release.Version, len(release.Files), *manifestPath)
		return nil
	})
}

func runTesting(arguments []string) error {
	flags := flag.NewFlagSet("testing", flag.ContinueOnError)
	releaseID := flags.Uint64("release-id", 0, "draft release ID")
	expectedCurrent := flags.Uint("expected-current", 0, "explicit currently deployed version")
	storageRoot := flags.String("storage-root", "Patch/files", "prepared payload storage root")
	manifestPath := flags.String("manifest", "Patch/manifest.json", "DownloadServer serving manifest")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *releaseID == 0 || *expectedCurrent == 0 || uint64(*expectedCurrent) > uint64(^uint32(0)) {
		return errors.New("testing requires --release-id and a uint32 --expected-current")
	}
	return withAdminRepository(func(ctx context.Context, repository *patchcatalog.AdminRepository) error {
		release, err := repository.LoadRelease(ctx, *releaseID)
		if err != nil {
			return err
		}
		if err := patchcatalog.VerifyReleaseStorage(release, *manifestPath, *storageRoot); err != nil {
			return err
		}
		if err := repository.MarkTesting(ctx, *releaseID, uint32(*expectedCurrent)); err != nil {
			return err
		}
		fmt.Printf("release id=%d version=%d is ready for clean-client testing\n", release.ID, release.Version)
		return nil
	})
}

func runPromote(arguments []string) error {
	flags := flag.NewFlagSet("promote", flag.ContinueOnError)
	releaseID := flags.Uint64("release-id", 0, "testing release ID")
	expectedCurrent := flags.Uint("expected-current", 0, "explicit currently deployed version")
	storageRoot := flags.String("storage-root", "Patch/files", "prepared payload storage root")
	manifestPath := flags.String("manifest", "Patch/manifest.json", "DownloadServer serving manifest")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *releaseID == 0 || *expectedCurrent == 0 || uint64(*expectedCurrent) > uint64(^uint32(0)) {
		return errors.New("promote requires --release-id and a uint32 --expected-current")
	}
	return withAdminRepository(func(ctx context.Context, repository *patchcatalog.AdminRepository) error {
		release, err := repository.LoadRelease(ctx, *releaseID)
		if err != nil {
			return err
		}
		if err := patchcatalog.VerifyReleaseStorage(release, *manifestPath, *storageRoot); err != nil {
			return err
		}
		if err := repository.Promote(ctx, *releaseID, uint32(*expectedCurrent)); err != nil {
			return err
		}
		fmt.Printf("published release id=%d version=%d\n", release.ID, release.Version)
		return nil
	})
}

func runRegisterServer(arguments []string) error {
	flags := flag.NewFlagSet("register-server", flag.ContinueOnError)
	host := flags.String("host", "", "public DownloadServer hostname or IPv4 address")
	port := flags.Uint("port", 15880, "public DownloadServer port")
	weight := flags.Uint("weight", 100, "positive selection weight")
	enabled := flags.Bool("enabled", false, "allow Gateway to advertise this endpoint")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *host == "" || *port == 0 || *port > 65535 || *weight == 0 || *weight > 65535 {
		return errors.New("register-server requires --host and valid uint16 --port/--weight")
	}
	return withAdminRepository(func(ctx context.Context, repository *patchcatalog.AdminRepository) error {
		if err := repository.UpsertDownloadServer(ctx, *host, uint16(*port), *enabled, uint16(*weight)); err != nil {
			return err
		}
		fmt.Printf("registered DownloadServer %s:%d enabled=%s\n", *host, *port, strconv.FormatBool(*enabled))
		return nil
	})
}

func withAdminRepository(operation func(context.Context, *patchcatalog.AdminRepository) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.AccountPool, frameworkdb.AccountPoolConfig())
	if err != nil {
		return err
	}
	defer pool.Close()
	repository, err := patchcatalog.NewAdminRepository(pool)
	if err != nil {
		return err
	}
	return operation(ctx, repository)
}

func usageError() error {
	return errors.New("usage: patch-admin <scan-media|build|testing|promote|register-server> [options]")
}
