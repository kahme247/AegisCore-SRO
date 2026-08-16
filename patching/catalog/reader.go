package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var (
	ErrNoRelease      = errors.New("no patch release is available")
	ErrClientNewer    = errors.New("client version is newer than the release")
	ErrClientTooOld   = errors.New("client version is outside the incremental patch chain")
	ErrNoDownloadHost = errors.New("no enabled DownloadServer is configured")
)

type Reader interface {
	Resolve(ctx context.Context, contentID uint8, moduleName string, requestedVersion uint32, channel string) (PatchPlan, error)
}

type ReaderFunc func(context.Context, uint8, string, uint32, string) (PatchPlan, error)

func (f ReaderFunc) Resolve(ctx context.Context, contentID uint8, moduleName string, requestedVersion uint32, channel string) (PatchPlan, error) {
	return f(ctx, contentID, moduleName, requestedVersion, channel)
}

type Repository struct {
	queryer frameworkdb.Queryer
}

type releaseLink struct {
	base    uint32
	version uint32
}

func NewRepository(queryer frameworkdb.Queryer) (*Repository, error) {
	if queryer == nil {
		return nil, errors.New("patch catalog database is unavailable")
	}
	return &Repository{queryer: queryer}, nil
}

func (r *Repository) Resolve(ctx context.Context, contentID uint8, moduleName string, requestedVersion uint32, channel string) (PatchPlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != StatusPublished && channel != StatusTesting {
		return PatchPlan{}, fmt.Errorf("unsupported patch release channel %q", channel)
	}
	rows, err := r.queryer.QueryContext(ctx, `SELECT ACTIVE_VERSION, MIN_PATCH_VERSION FROM PATCH_MODULE_STATE
		WHERE CONTENT_ID = ? AND MODULE_NAME = ?`, contentID, moduleName)
	if err != nil {
		return PatchPlan{}, fmt.Errorf("query patch module state: %w", err)
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			rows.Close()
			return PatchPlan{}, err
		}
		rows.Close()
		return PatchPlan{}, ErrNoRelease
	}
	var targetVersion, minimumVersion uint32
	if err := rows.Scan(&targetVersion, &minimumVersion); err != nil {
		rows.Close()
		return PatchPlan{}, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PatchPlan{}, err
	}
	rows.Close()
	if channel == StatusTesting {
		testingRows, err := r.queryer.QueryContext(ctx, `SELECT VERSION FROM PATCH_RELEASE
			WHERE CONTENT_ID = ? AND MODULE_NAME = ? AND STATUS = 'testing' ORDER BY VERSION DESC LIMIT 1`, contentID, moduleName)
		if err != nil {
			return PatchPlan{}, fmt.Errorf("query testing patch target: %w", err)
		}
		if !testingRows.Next() {
			testingRows.Close()
			return PatchPlan{}, ErrNoRelease
		}
		if err := testingRows.Scan(&targetVersion); err != nil {
			testingRows.Close()
			return PatchPlan{}, err
		}
		testingRows.Close()
	}
	if requestedVersion > targetVersion {
		return PatchPlan{}, ErrClientNewer
	}
	if requestedVersion < minimumVersion {
		return PatchPlan{}, ErrClientTooOld
	}
	plan := PatchPlan{TargetVersion: targetVersion}
	if requestedVersion == targetVersion {
		return plan, nil
	}

	statuses := []string{StatusPublished, StatusRetired}
	if channel == StatusTesting {
		statuses = append(statuses, StatusTesting)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	arguments := []any{contentID, moduleName, requestedVersion, targetVersion}
	for _, status := range statuses {
		arguments = append(arguments, status)
	}
	chainRows, err := r.queryer.QueryContext(ctx, `SELECT BASE_VERSION, VERSION FROM PATCH_RELEASE
        WHERE CONTENT_ID = ? AND MODULE_NAME = ? AND VERSION > ? AND VERSION <= ? AND STATUS IN (`+placeholders+`)
        ORDER BY VERSION ASC`, arguments...)
	if err != nil {
		return PatchPlan{}, fmt.Errorf("query patch release chain: %w", err)
	}
	links := make([]releaseLink, 0)
	for chainRows.Next() {
		var base, version uint32
		if err := chainRows.Scan(&base, &version); err != nil {
			chainRows.Close()
			return PatchPlan{}, err
		}
		links = append(links, releaseLink{base: base, version: version})
	}
	chainErr := chainRows.Err()
	chainRows.Close()
	if chainErr != nil {
		return PatchPlan{}, chainErr
	}
	if err := validateReleaseChain(requestedVersion, targetVersion, links); err != nil {
		return PatchPlan{}, ErrClientTooOld
	}

	fileRows, err := r.queryer.QueryContext(ctx, `SELECT f.ID, f.ORDINAL, f.FILENAME, f.PATH, f.PREPARED_SIZE,
        f.IMPORT_TO_PK2, f.STORAGE_KEY, f.PREPARED_SHA256, f.TARGET_SIZE, f.TARGET_SHA256
        FROM PATCH_RELEASE_FILE f JOIN PATCH_RELEASE r ON r.ID = f.FK_RELEASE
        WHERE r.CONTENT_ID = ? AND r.MODULE_NAME = ? AND r.VERSION > ? AND r.VERSION <= ?
          AND r.STATUS IN (`+placeholders+`) AND f.VALID = 1
        ORDER BY r.VERSION ASC, f.ORDINAL ASC`, arguments...)
	if err != nil {
		return PatchPlan{}, fmt.Errorf("query patch files: %w", err)
	}
	defer fileRows.Close()
	for fileRows.Next() {
		var file File
		if err := fileRows.Scan(&file.ID, &file.Ordinal, &file.Filename, &file.Path, &file.PreparedSize,
			&file.ImportToPK2, &file.StorageKey, &file.PreparedSHA256, &file.TargetSize, &file.TargetSHA256); err != nil {
			return PatchPlan{}, err
		}
		plan.Files = append(plan.Files, file)
	}
	if err := fileRows.Err(); err != nil {
		return PatchPlan{}, err
	}
	if len(plan.Files) == 0 {
		return PatchPlan{}, ErrClientTooOld
	}

	serverRows, err := r.queryer.QueryContext(ctx, `SELECT PUBLIC_HOST, PUBLIC_PORT FROM PATCH_DOWNLOAD_SERVER
        WHERE ENABLED = 1 ORDER BY WEIGHT DESC, ID ASC LIMIT 1`)
	if err != nil {
		return PatchPlan{}, fmt.Errorf("query DownloadServer endpoint: %w", err)
	}
	defer serverRows.Close()
	if !serverRows.Next() {
		if err := serverRows.Err(); err != nil {
			return PatchPlan{}, err
		}
		return PatchPlan{}, ErrNoDownloadHost
	}
	if err := serverRows.Scan(&plan.Server.Host, &plan.Server.Port); err != nil {
		return PatchPlan{}, err
	}
	return plan, nil
}

func validateReleaseChain(requestedVersion, targetVersion uint32, links []releaseLink) error {
	cursor := requestedVersion
	for _, link := range links {
		if link.base != cursor || link.version != link.base+1 {
			return ErrClientTooOld
		}
		cursor = link.version
	}
	if cursor != targetVersion {
		return ErrClientTooOld
	}
	return nil
}
