package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Harvi8/AegisCore/patching/builder"
)

type AdminRepository struct {
	db *sql.DB
}

func NewAdminRepository(db *sql.DB) (*AdminRepository, error) {
	if db == nil {
		return nil, errors.New("patch admin database is unavailable")
	}
	return &AdminRepository{db: db}, nil
}

func (r *AdminRepository) CreateDraft(ctx context.Context, build builder.BuildResult) (Release, error) {
	if err := builder.ValidateResult(build); err != nil {
		return Release{}, fmt.Errorf("validate patch draft: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Release{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO PATCH_MODULE_STATE
        (DIVISION_ID, CONTENT_ID, MODULE_ID, MODULE_NAME, ACTIVE_VERSION, MIN_PATCH_VERSION)
        VALUES (?, ?, ?, ?, ?, ?)`, build.Release.DivisionID, build.Release.ContentID, build.Release.ModuleID,
		build.Release.ModuleName, build.Release.BaseVersion, build.Release.BaseVersion); err != nil {
		return Release{}, fmt.Errorf("initialize patch module state: %w", err)
	}
	var activeVersion uint32
	if err := tx.QueryRowContext(ctx, `SELECT ACTIVE_VERSION FROM PATCH_MODULE_STATE
        WHERE DIVISION_ID = ? AND CONTENT_ID = ? AND MODULE_ID = ? AND MODULE_NAME = ? FOR UPDATE`,
		build.Release.DivisionID, build.Release.ContentID, build.Release.ModuleID, build.Release.ModuleName).Scan(&activeVersion); err != nil {
		return Release{}, fmt.Errorf("lock patch module state: %w", err)
	}
	if activeVersion != build.Release.BaseVersion {
		return Release{}, fmt.Errorf("active database version is %d, draft base is %d", activeVersion, build.Release.BaseVersion)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO PATCH_RELEASE
        (DIVISION_ID, CONTENT_ID, MODULE_ID, MODULE_NAME, BASE_VERSION, VERSION, DISPLAY_VERSION, DESCRIPTION, STATUS)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'draft')`,
		build.Release.DivisionID, build.Release.ContentID, build.Release.ModuleID, build.Release.ModuleName,
		build.Release.BaseVersion, build.Release.TargetVersion, build.Release.DisplayVersion, build.Release.Description)
	if err != nil {
		return Release{}, fmt.Errorf("insert patch draft: %w", err)
	}
	releaseID, err := result.LastInsertId()
	if err != nil || releaseID <= 0 {
		return Release{}, fmt.Errorf("read patch draft ID: %w", err)
	}
	release := Release{
		ID: uint64(releaseID), DivisionID: build.Release.DivisionID, ContentID: build.Release.ContentID,
		ModuleID: build.Release.ModuleID, ModuleName: build.Release.ModuleName, BaseVersion: build.Release.BaseVersion,
		Version: build.Release.TargetVersion, DisplayVersion: build.Release.DisplayVersion, Description: build.Release.Description,
		Status: StatusDraft, Files: make([]File, 0, len(build.Files)),
	}
	for index, prepared := range build.Files {
		fileResult, err := tx.ExecContext(ctx, `INSERT INTO PATCH_RELEASE_FILE
            (FK_RELEASE, ORDINAL, FILENAME, PATH, PREPARED_SIZE, IMPORT_TO_PK2, STORAGE_KEY, PREPARED_SHA256, TARGET_SIZE, TARGET_SHA256, VALID)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
			releaseID, index, prepared.Filename, prepared.Path, prepared.PreparedSize, prepared.ImportToPK2,
			prepared.StorageKey, prepared.PreparedSHA256, prepared.TargetSize, prepared.TargetSHA256)
		if err != nil {
			return Release{}, fmt.Errorf("insert patch draft file %d: %w", index, err)
		}
		fileID, err := fileResult.LastInsertId()
		if err != nil || fileID <= 0 || uint64(fileID) > uint64(^uint32(0)) {
			return Release{}, fmt.Errorf("read patch file ID %d: %w", index, err)
		}
		release.Files = append(release.Files, File{
			ID: uint32(fileID), Ordinal: uint16(index), Filename: prepared.Filename, Path: prepared.Path,
			PreparedSize: prepared.PreparedSize, ImportToPK2: prepared.ImportToPK2, StorageKey: prepared.StorageKey,
			PreparedSHA256: prepared.PreparedSHA256, TargetSize: prepared.TargetSize, TargetSHA256: prepared.TargetSHA256,
		})
	}
	if err := tx.Commit(); err != nil {
		return Release{}, fmt.Errorf("commit patch draft: %w", err)
	}
	return release, nil
}

func (r *AdminRepository) MarkTesting(ctx context.Context, releaseID uint64, expectedCurrent uint32) error {
	if expectedCurrent == 0 {
		return errors.New("expected current version must be explicit")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE PATCH_RELEASE r
        JOIN PATCH_MODULE_STATE s ON s.DIVISION_ID = r.DIVISION_ID AND s.CONTENT_ID = r.CONTENT_ID
          AND s.MODULE_ID = r.MODULE_ID AND s.MODULE_NAME = r.MODULE_NAME
        SET r.STATUS = 'testing', r.VALIDATED_AT = CURRENT_TIMESTAMP(6)
        WHERE r.ID = ? AND r.STATUS = 'draft' AND r.BASE_VERSION = ? AND s.ACTIVE_VERSION = ?`,
		releaseID, expectedCurrent, expectedCurrent)
	if err != nil {
		return fmt.Errorf("mark patch release testing: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("patch release was not a matching draft or its base version differs from expected current")
	}
	return nil
}

func (r *AdminRepository) UpsertDownloadServer(ctx context.Context, host string, port uint16, enabled bool, weight uint16) error {
	if host == "" || port == 0 || weight == 0 {
		return errors.New("DownloadServer host, port, and weight must be non-zero")
	}
	if strings.TrimSpace(host) != host || strings.ContainsAny(host, "\\/\x00") {
		return errors.New("DownloadServer host must be a trimmed hostname or address")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO PATCH_DOWNLOAD_SERVER (PUBLIC_HOST, PUBLIC_PORT, ENABLED, WEIGHT)
        VALUES (?, ?, ?, ?) AS new
        ON DUPLICATE KEY UPDATE ENABLED = new.ENABLED, WEIGHT = new.WEIGHT`, host, port, enabled, weight)
	if err != nil {
		return fmt.Errorf("upsert DownloadServer endpoint: %w", err)
	}
	return nil
}

func (r *AdminRepository) LoadRelease(ctx context.Context, releaseID uint64) (Release, error) {
	var release Release
	if err := r.db.QueryRowContext(ctx, `SELECT ID, DIVISION_ID, CONTENT_ID, MODULE_ID, MODULE_NAME, BASE_VERSION,
        VERSION, DISPLAY_VERSION, DESCRIPTION, STATUS, CREATED_AT FROM PATCH_RELEASE WHERE ID = ?`, releaseID).Scan(
		&release.ID, &release.DivisionID, &release.ContentID, &release.ModuleID, &release.ModuleName, &release.BaseVersion,
		&release.Version, &release.DisplayVersion, &release.Description, &release.Status, &release.CreatedAt,
	); err != nil {
		return Release{}, fmt.Errorf("load patch release: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT ID, ORDINAL, FILENAME, PATH, PREPARED_SIZE, IMPORT_TO_PK2,
        STORAGE_KEY, PREPARED_SHA256, TARGET_SIZE, TARGET_SHA256 FROM PATCH_RELEASE_FILE
        WHERE FK_RELEASE = ? AND VALID = 1 ORDER BY ORDINAL ASC`, releaseID)
	if err != nil {
		return Release{}, fmt.Errorf("load patch release files: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.Ordinal, &file.Filename, &file.Path, &file.PreparedSize, &file.ImportToPK2,
			&file.StorageKey, &file.PreparedSHA256, &file.TargetSize, &file.TargetSHA256); err != nil {
			return Release{}, err
		}
		release.Files = append(release.Files, file)
	}
	if err := rows.Err(); err != nil {
		return Release{}, err
	}
	return release, nil
}

func (r *AdminRepository) Promote(ctx context.Context, releaseID uint64, expectedCurrent uint32) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var divisionID, contentID, moduleID uint8
	var moduleName, status string
	var baseVersion, version uint32
	if err := tx.QueryRowContext(ctx, `SELECT DIVISION_ID, CONTENT_ID, MODULE_ID, MODULE_NAME, BASE_VERSION, VERSION, STATUS
        FROM PATCH_RELEASE WHERE ID = ? FOR UPDATE`, releaseID).Scan(
		&divisionID, &contentID, &moduleID, &moduleName, &baseVersion, &version, &status,
	); err != nil {
		return fmt.Errorf("lock patch release: %w", err)
	}
	if status != StatusTesting {
		return fmt.Errorf("patch release status is %q, want testing", status)
	}
	if version != baseVersion+1 {
		return errors.New("patch release is not a one-version increment")
	}
	if expectedCurrent == 0 || baseVersion != expectedCurrent {
		return fmt.Errorf("draft base version is %d, explicit expected current version is %d", baseVersion, expectedCurrent)
	}
	var activeVersion uint32
	if err := tx.QueryRowContext(ctx, `SELECT ACTIVE_VERSION FROM PATCH_MODULE_STATE
        WHERE DIVISION_ID = ? AND CONTENT_ID = ? AND MODULE_ID = ? AND MODULE_NAME = ? FOR UPDATE`,
		divisionID, contentID, moduleID, moduleName).Scan(&activeVersion); err != nil {
		return fmt.Errorf("lock patch module state: %w", err)
	}
	if activeVersion != expectedCurrent {
		return fmt.Errorf("active database version is %d, explicit expected current version is %d", activeVersion, expectedCurrent)
	}
	var current uint32
	err = tx.QueryRowContext(ctx, `SELECT VERSION FROM PATCH_RELEASE
		WHERE DIVISION_ID = ? AND CONTENT_ID = ? AND MODULE_ID = ? AND MODULE_NAME = ? AND STATUS = 'published'
		ORDER BY VERSION DESC LIMIT 1 FOR UPDATE`, divisionID, contentID, moduleID, moduleName).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lock published patch release: %w", err)
	}
	if err == nil && current != baseVersion {
		return fmt.Errorf("published version is %d, draft base is %d", current, baseVersion)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE PATCH_RELEASE SET STATUS = 'retired'
        WHERE DIVISION_ID = ? AND CONTENT_ID = ? AND MODULE_ID = ? AND MODULE_NAME = ? AND STATUS = 'published'`,
		divisionID, contentID, moduleID, moduleName); err != nil {
		return fmt.Errorf("retire previous patch release: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE PATCH_RELEASE SET STATUS = 'published', PUBLISHED_AT = CURRENT_TIMESTAMP(6)
        WHERE ID = ? AND STATUS = 'testing'`, releaseID)
	if err != nil {
		return fmt.Errorf("publish patch release: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return fmt.Errorf("publish patch release changed %d rows: %w", changed, err)
	}
	stateResult, err := tx.ExecContext(ctx, `UPDATE PATCH_MODULE_STATE SET ACTIVE_VERSION = ?
        WHERE DIVISION_ID = ? AND CONTENT_ID = ? AND MODULE_ID = ? AND MODULE_NAME = ? AND ACTIVE_VERSION = ?`,
		version, divisionID, contentID, moduleID, moduleName, expectedCurrent)
	if err != nil {
		return fmt.Errorf("advance active patch version: %w", err)
	}
	stateChanged, err := stateResult.RowsAffected()
	if err != nil || stateChanged != 1 {
		return fmt.Errorf("advance active patch version changed %d rows: %w", stateChanged, err)
	}
	return tx.Commit()
}
