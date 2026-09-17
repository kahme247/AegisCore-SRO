package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const CharacterDeletionRetention = 7 * 24 * time.Hour

var (
	ErrCharacterDeletionNotEligible    = errors.New("character is not eligible for deletion")
	ErrCharacterDeletionGuildMaster    = errors.New("guild master cannot be permanently deleted")
	ErrCharacterDeletionOutcomeUnknown = errors.New("character deletion transaction outcome is unknown")
)

// PurgeExpiredCharacters permanently removes due temporary deletions. The
// legacy ShardManager procedure performs one transaction per character; doing
// the same bounds lock duration and lets a later sweep retry only failed rows.
func (r *SQLCharacterRepository) PurgeExpiredCharacters(ctx context.Context, now time.Time, limit int) (int, error) {
	database, err := r.characterDeletionDatabase()
	if err != nil {
		return 0, err
	}
	if now.IsZero() || limit < 1 || limit > 1000 {
		return 0, ErrCharacterDeletionNotEligible
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := database.QueryContext(ctx, "SELECT c.`ID` FROM `SRO_SHARD`.`CHAR` c WHERE c.`DELETING`=TRUE AND c.`DELETION_REQUESTED_AT` IS NOT NULL AND c.`DELETION_REQUESTED_AT`<=? AND NOT EXISTS (SELECT 1 FROM `SRO_SHARD`.`GUILD_MEMBER` gm WHERE gm.`FK_CHAR`=c.`ID` AND gm.`MEMBER_CLASS`=0) ORDER BY c.`DELETION_REQUESTED_AT`,c.`ID` LIMIT ?", now.UTC().Add(-RuntimeCharacterDeletionRetention()), limit)
	if err != nil {
		return 0, fmt.Errorf("query expired character deletions: %w", err)
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan expired character deletion: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterate expired character deletions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired character deletions: %w", err)
	}

	deleted := 0
	for _, id := range ids {
		if err := deleteCharacterTransaction(ctx, database, id, now.UTC(), true); err != nil {
			if errors.Is(err, ErrCharacterDeletionNotEligible) || errors.Is(err, ErrCharacterDeletionGuildMaster) {
				continue
			}
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// ForceDeleteCharacter reproduces _ForceDeleteCharacter's administrative
// behavior: resolve by normalized name, reject a guild master, then remove the
// complete character aggregate without waiting for the retention period.
func (r *SQLCharacterRepository) ForceDeleteCharacter(ctx context.Context, name string) error {
	database, err := r.characterDeletionDatabase()
	if err != nil {
		return err
	}
	if name == "" {
		return ErrCharacterNameRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var id int64
	err = database.QueryRowContext(ctx, "SELECT `ID` FROM `SRO_SHARD`.`CHAR` WHERE LOWER(TRIM(`CHAR_NAME`))=LOWER(TRIM(?)) LIMIT 1", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCharacterNotFound
	}
	if err != nil {
		return fmt.Errorf("resolve forced character deletion: %w", err)
	}
	return deleteCharacterTransaction(ctx, database, id, time.Time{}, false)
}

func (r *SQLCharacterRepository) characterDeletionDatabase() (*sql.DB, error) {
	if r == nil {
		return nil, ErrCharacterRepositoryUnavailable
	}
	database, ok := r.shard.(*sql.DB)
	if !ok || database == nil {
		return nil, ErrCharacterRepositoryUnavailable
	}
	return database, nil
}

func deleteCharacterTransaction(ctx context.Context, database *sql.DB, characterID int64, now time.Time, requireExpired bool) (err error) {
	if database == nil || characterID <= 0 {
		return ErrCharacterDeletionNotEligible
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin character deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var deleting bool
	var requestedAt sql.NullTime
	err = tx.QueryRowContext(ctx, "SELECT `DELETING`,`DELETION_REQUESTED_AT` FROM `SRO_SHARD`.`CHAR` WHERE `ID`=? FOR UPDATE", characterID).Scan(&deleting, &requestedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCharacterNotFound
	}
	if err != nil {
		return fmt.Errorf("lock character deletion target: %w", err)
	}
	if requireExpired && (!deleting || !requestedAt.Valid || requestedAt.Time.UTC().Add(RuntimeCharacterDeletionRetention()).After(now.UTC())) {
		return ErrCharacterDeletionNotEligible
	}

	var guildMaster int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM `SRO_SHARD`.`GUILD_MEMBER` WHERE `FK_CHAR`=? AND `MEMBER_CLASS`=0 LIMIT 1 FOR UPDATE", characterID).Scan(&guildMaster)
	if err == nil {
		return ErrCharacterDeletionGuildMaster
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check character guild-master deletion: %w", err)
	}

	itemIDs, err := lockedCharacterItemIDs(ctx, tx, characterID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=?", characterID); err != nil {
		return fmt.Errorf("delete character inventory links: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`AVATAR_INVENTORY` WHERE `FK_CHAR`=?", characterID); err != nil {
		return fmt.Errorf("delete character avatar links: %w", err)
	}
	for _, itemID := range itemIDs {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`ITEM` WHERE `ID`=?", itemID); err != nil {
			return fmt.Errorf("delete character item %d: %w", itemID, err)
		}
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`CHAR` WHERE `ID`=?", characterID)
	if err != nil {
		return fmt.Errorf("delete character aggregate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read character deletion result: %w", err)
	}
	if affected != 1 {
		return ErrCharacterNotFound
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: %v", ErrCharacterDeletionOutcomeUnknown, err)
	}
	return nil
}

func lockedCharacterItemIDs(ctx context.Context, tx *sql.Tx, characterID int64) ([]int64, error) {
	ids := make([]int64, 0)
	for _, query := range []string{
		"SELECT `FK_ITEM` FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? ORDER BY `SLOT` FOR UPDATE",
		"SELECT `FK_ITEM` FROM `SRO_SHARD`.`AVATAR_INVENTORY` WHERE `FK_CHAR`=? ORDER BY `SLOT` FOR UPDATE",
	} {
		rows, err := tx.QueryContext(ctx, query, characterID)
		if err != nil {
			return nil, fmt.Errorf("lock character items: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan character item: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate character items: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close character items: %w", err)
		}
	}
	return ids, nil
}
