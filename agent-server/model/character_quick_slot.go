package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	MaxCharacterQuickSlots = 51

	selectCharacterQuickSlots = "SELECT `SLOT_SEQ`,`CONTENT_TYPE`,`CONTENT_ID` FROM `SRO_SHARD`.`CHAR_QUICK_SLOT` WHERE `CHAR_ID`=? ORDER BY `SLOT_SEQ`"
	upsertCharacterQuickSlot  = "INSERT INTO `SRO_SHARD`.`CHAR_QUICK_SLOT` (`CHAR_ID`,`SLOT_SEQ`,`CONTENT_TYPE`,`CONTENT_ID`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `CONTENT_TYPE`=VALUES(`CONTENT_TYPE`),`CONTENT_ID`=VALUES(`CONTENT_ID`)"
	deleteCharacterQuickSlot  = "DELETE FROM `SRO_SHARD`.`CHAR_QUICK_SLOT` WHERE `CHAR_ID`=? AND `SLOT_SEQ`=?"
)

var (
	ErrCharacterQuickSlotRepositoryUnavailable = errors.New("character quick-slot repository is unavailable")
	ErrCharacterQuickSlotCharacterIDRequired   = errors.New("character quick-slot character ID is required")
	ErrCharacterQuickSlotSequenceInvalid       = errors.New("character quick-slot sequence is invalid")
	ErrCharacterQuickSlotContentTypeRequired   = errors.New("character quick-slot content type is required")
)

type CharacterQuickSlot struct {
	Sequence    byte
	ContentType byte
	ContentID   uint32
}

type CharacterQuickSlotRepository interface {
	LoadQuickSlots(context.Context, int) ([]CharacterQuickSlot, error)
	SaveQuickSlot(context.Context, int, CharacterQuickSlot) error
}

type quickSlotDatabase interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type SQLCharacterQuickSlotRepository struct{ shard quickSlotDatabase }

func NewCharacterQuickSlotRepository(shard quickSlotDatabase) (*SQLCharacterQuickSlotRepository, error) {
	if shard == nil {
		return nil, ErrCharacterQuickSlotRepositoryUnavailable
	}
	return &SQLCharacterQuickSlotRepository{shard: shard}, nil
}

func (r *SQLCharacterQuickSlotRepository) LoadQuickSlots(ctx context.Context, characterID int) ([]CharacterQuickSlot, error) {
	if r == nil || r.shard == nil {
		return nil, ErrCharacterQuickSlotRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrCharacterQuickSlotCharacterIDRequired, characterID)
	}
	rows, err := r.shard.QueryContext(ctx, selectCharacterQuickSlots, characterID)
	if err != nil {
		return nil, fmt.Errorf("load character quick slots: %w", err)
	}
	defer rows.Close()

	slots := make([]CharacterQuickSlot, 0)
	for rows.Next() {
		var slot CharacterQuickSlot
		if err := rows.Scan(&slot.Sequence, &slot.ContentType, &slot.ContentID); err != nil {
			return nil, fmt.Errorf("scan character quick slot: %w", err)
		}
		if err := validateCharacterQuickSlot(slot, false); err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read character quick slots: %w", err)
	}
	return slots, nil
}

func (r *SQLCharacterQuickSlotRepository) SaveQuickSlot(ctx context.Context, characterID int, slot CharacterQuickSlot) error {
	if r == nil || r.shard == nil {
		return ErrCharacterQuickSlotRepositoryUnavailable
	}
	if characterID <= 0 {
		return fmt.Errorf("%w: %d", ErrCharacterQuickSlotCharacterIDRequired, characterID)
	}
	if err := validateCharacterQuickSlot(slot, true); err != nil {
		return err
	}
	var err error
	if slot.ContentType == 0 {
		_, err = r.shard.ExecContext(ctx, deleteCharacterQuickSlot, characterID, slot.Sequence)
	} else {
		_, err = r.shard.ExecContext(ctx, upsertCharacterQuickSlot, characterID, slot.Sequence, slot.ContentType, slot.ContentID)
	}
	if err != nil {
		return fmt.Errorf("save character quick slot: %w", err)
	}
	return nil
}

func validateCharacterQuickSlot(slot CharacterQuickSlot, allowClear bool) error {
	if int(slot.Sequence) >= MaxCharacterQuickSlots {
		return fmt.Errorf("%w: %d", ErrCharacterQuickSlotSequenceInvalid, slot.Sequence)
	}
	if slot.ContentType == 0 && !allowClear {
		return ErrCharacterQuickSlotContentTypeRequired
	}
	return nil
}
