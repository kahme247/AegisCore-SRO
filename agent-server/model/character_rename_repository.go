package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrCharacterRenameUnavailable = errors.New("character rename is unavailable")
	ErrCharacterRenameNotFound    = errors.New("character rename target was not found")
	ErrCharacterRenameDuplicate   = errors.New("character rename target already exists")
	ErrCharacterRenameItemMissing = errors.New("character rename item is missing")
)

type CharacterRenameRepository interface {
	RenameCharacter(context.Context, uint32, uint16, string, string) error
}

type SQLCharacterRenameRepository struct{ shard *sql.DB }

func NewCharacterRenameRepository(shard *sql.DB) (*SQLCharacterRenameRepository, error) {
	if shard == nil {
		return nil, ErrCharacterRenameUnavailable
	}
	return &SQLCharacterRenameRepository{shard: shard}, nil
}

func (r *SQLCharacterRenameRepository) RenameCharacter(ctx context.Context, userID uint32, shardID uint16, currentName, newName string) (err error) {
	if r == nil || r.shard == nil || userID == 0 || shardID == 0 || currentName == "" || newName == "" {
		return ErrCharacterRenameUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin character rename: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var characterID int64
	if err = tx.QueryRowContext(ctx, "SELECT `ID` FROM `CHAR` WHERE LOWER(TRIM(`CHAR_NAME`))=LOWER(TRIM(?)) AND `FK_USER`=? AND `FK_SHARD`=? FOR UPDATE", currentName, userID, shardID).Scan(&characterID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCharacterRenameNotFound
		}
		return fmt.Errorf("lock character rename target: %w", err)
	}
	var duplicate int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM `CHAR` WHERE LOWER(TRIM(`CHAR_NAME`))=LOWER(TRIM(?)) LIMIT 1", newName).Scan(&duplicate)
	if err == nil {
		return ErrCharacterRenameDuplicate
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check character rename duplicate: %w", err)
	}
	var slot byte
	var itemID int64
	var stack int
	err = tx.QueryRowContext(ctx, "SELECT inv.`SLOT`,item.`id`,item.`STACK_COUNT` FROM `INVENTORY` inv JOIN `ITEM` item ON item.`id`=inv.`FK_ITEM` JOIN `ITEMDATA` ref ON ref.`ItemID`=item.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? AND ref.`TypeID1`=3 AND ref.`TypeID2`=3 AND ref.`TypeID3`=12 AND ref.`TypeID4`=6 ORDER BY inv.`SLOT` LIMIT 1 FOR UPDATE", characterID).Scan(&slot, &itemID, &stack)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCharacterRenameItemMissing
	}
	if err != nil {
		return fmt.Errorf("lock character rename item: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `CHAR_NAME`=? WHERE `ID`=?", newName, characterID); err != nil {
		return fmt.Errorf("persist character rename: %w", err)
	}
	if stack > 1 {
		if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1 WHERE `id`=? AND `STACK_COUNT`=?", itemID, stack); err != nil {
			return fmt.Errorf("consume character rename item: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, itemID); err != nil {
			return fmt.Errorf("unlink character rename item: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", itemID); err != nil {
			return fmt.Errorf("delete character rename item: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit character rename: %w", err)
	}
	return nil
}
