package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	frameworkdb "github.com/ferdoran/go-sro/framework/db"
)

var (
	ErrCharacterCreationPolicyUnavailable = errors.New("character creation policy is unavailable")
	ErrCharacterCreationDisabled          = errors.New("character creation is disabled")
	ErrCharacterCreationSelectionInvalid  = errors.New("character creation selection is invalid")
)

type CharacterCreationDefaults struct {
	Scale            byte
	Level            int
	Exp              int64
	SkillExp         int64
	Strength         int
	Intellect        int
	StatPoints       int
	HP               int
	MP               int
	PosX             float32
	PosY             float32
	PosZ             float32
	Region           int16
	ReturnTeleportID uint32
	InventorySize    byte
}

type CharacterCreationSelection struct {
	ModelID        uint32
	RequestedScale byte
	Items          map[byte]uint32
}

type CharacterCreationPolicyRepository interface {
	Resolve(context.Context, CharacterCreationSelection) (CharacterCreationDefaults, []CharacterStarterItem, error)
}

type SQLCharacterCreationPolicyRepository struct {
	shard frameworkdb.DBTX
}

func NewCharacterCreationPolicyRepository(shard frameworkdb.DBTX) (*SQLCharacterCreationPolicyRepository, error) {
	if shard == nil {
		return nil, ErrCharacterCreationPolicyUnavailable
	}
	return &SQLCharacterCreationPolicyRepository{shard: shard}, nil
}

const selectCharacterCreationDefaults = "SELECT s.`ENABLED`,s.`ALLOW_CUSTOM_SCALE`,s.`DEFAULT_SCALE`,m.`INITIAL_LEVEL`,m.`INITIAL_EXP`,m.`INITIAL_SKILL_EXP`,m.`INITIAL_STRENGTH`,m.`INITIAL_INTELLECT`,m.`INITIAL_STAT_POINTS`,m.`INITIAL_HP`,m.`INITIAL_MP`,m.`INITIAL_POS_X`,m.`INITIAL_POS_Y`,m.`INITIAL_POS_Z`,m.`INITIAL_REGION_ID`,m.`INITIAL_RETURN_TELEPORT_ID`,m.`INITIAL_INVENTORY_SIZE` FROM `SRO_SHARD`.`CHARACTER_CREATION_SETTING` s JOIN `SRO_SHARD`.`CHARACTER_CREATION_MODEL` m ON m.`MODEL_ID`=? WHERE s.`SETTING_ID`=1"

const selectCharacterCreationStarterItems = "SELECT `SLOT`,`REF_ITEM_ID` FROM `SRO_SHARD`.`CHARACTER_CREATION_STARTER_ITEM` WHERE `MODEL_ID`=? ORDER BY `SLOT`,`REF_ITEM_ID`"

func (r *SQLCharacterCreationPolicyRepository) Resolve(ctx context.Context, selection CharacterCreationSelection) (CharacterCreationDefaults, []CharacterStarterItem, error) {
	if r == nil || r.shard == nil {
		return CharacterCreationDefaults{}, nil, ErrCharacterCreationPolicyUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var defaults CharacterCreationDefaults
	var enabled, customScale bool
	var defaultScale byte
	err := r.shard.QueryRowContext(ctx, selectCharacterCreationDefaults, selection.ModelID).Scan(
		&enabled, &customScale, &defaultScale, &defaults.Level, &defaults.Exp, &defaults.SkillExp,
		&defaults.Strength, &defaults.Intellect, &defaults.StatPoints, &defaults.HP, &defaults.MP,
		&defaults.PosX, &defaults.PosY, &defaults.PosZ, &defaults.Region, &defaults.ReturnTeleportID, &defaults.InventorySize,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CharacterCreationDefaults{}, nil, ErrCharacterCreationSelectionInvalid
	}
	if err != nil {
		return CharacterCreationDefaults{}, nil, fmt.Errorf("load character creation defaults: %w", err)
	}
	if !enabled {
		return CharacterCreationDefaults{}, nil, ErrCharacterCreationDisabled
	}
	defaults.Scale = defaultScale
	if customScale {
		if selection.RequestedScale == 0 {
			return CharacterCreationDefaults{}, nil, ErrCharacterCreationSelectionInvalid
		}
		defaults.Scale = selection.RequestedScale
	}

	rows, err := r.shard.QueryContext(ctx, selectCharacterCreationStarterItems, selection.ModelID)
	if err != nil {
		return CharacterCreationDefaults{}, nil, fmt.Errorf("load character creation starter items: %w", err)
	}
	defer rows.Close()
	allowed := make(map[byte]map[uint32]struct{}, len(selection.Items))
	for rows.Next() {
		var slot byte
		var itemID uint32
		if err := rows.Scan(&slot, &itemID); err != nil {
			return CharacterCreationDefaults{}, nil, fmt.Errorf("scan character creation starter item: %w", err)
		}
		if allowed[slot] == nil {
			allowed[slot] = make(map[uint32]struct{})
		}
		allowed[slot][itemID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return CharacterCreationDefaults{}, nil, fmt.Errorf("iterate character creation starter items: %w", err)
	}

	items := make([]CharacterStarterItem, 0, 4)
	for _, slot := range []byte{SlotPrimaryWeapon, SlotChest, SlotBoots, SlotPants} {
		itemID := selection.Items[slot]
		if _, ok := allowed[slot][itemID]; !ok {
			return CharacterCreationDefaults{}, nil, ErrCharacterCreationSelectionInvalid
		}
		items = append(items, CharacterStarterItem{ReferenceItemID: itemID, Slot: slot})
	}
	return defaults, items, nil
}
