package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

type QuestInstanceMemberState struct {
	CharacterID int
	DungeonKey  uint32
	Scope       GameWorldScope
	OpenedAt    time.Time
	RegionID    int16
	X           float32
	Y           float32
	Z           float32
	Activated   bool
	EnterCount  uint32
	LastEnterAt *time.Time
}

type QuestInstanceMemberRepository interface {
	SaveQuestInstanceMember(context.Context, QuestInstanceMemberState) error
	LoadQuestInstanceMember(context.Context, int, uint32) (QuestInstanceMemberState, error)
	DeleteQuestInstanceMember(context.Context, int, uint16) error
}

type questInstanceDB interface {
	frameworkdb.Execer
	frameworkdb.RowQueryer
}

type SQLQuestInstanceMemberRepository struct {
	shard questInstanceDB
}

func NewQuestInstanceMemberRepository(shard questInstanceDB) (*SQLQuestInstanceMemberRepository, error) {
	if shard == nil {
		return nil, ErrQuestInstanceInvalid
	}
	return &SQLQuestInstanceMemberRepository{shard: shard}, nil
}

const saveQuestInstanceMember = `INSERT INTO SRO_SHARD.CHAR_QUEST_INSTANCE_WORLD
	(CHAR_ID,DUNGEON_KEY_ID,WORLD_ID,LAYER_ID,OPENED_AT,REGION_ID,POS_X,POS_Y,POS_Z,IS_ACTIVATED,ENTER_COUNT,LAST_ENTER_AT)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
	ON DUPLICATE KEY UPDATE DUNGEON_KEY_ID=VALUES(DUNGEON_KEY_ID),LAYER_ID=VALUES(LAYER_ID),
	OPENED_AT=VALUES(OPENED_AT),REGION_ID=VALUES(REGION_ID),POS_X=VALUES(POS_X),POS_Y=VALUES(POS_Y),
	POS_Z=VALUES(POS_Z),IS_ACTIVATED=VALUES(IS_ACTIVATED),ENTER_COUNT=VALUES(ENTER_COUNT),
	LAST_ENTER_AT=VALUES(LAST_ENTER_AT)`

func (r *SQLQuestInstanceMemberRepository) SaveQuestInstanceMember(ctx context.Context, state QuestInstanceMemberState) error {
	if r == nil || r.shard == nil || state.CharacterID <= 0 || state.DungeonKey == 0 ||
		!state.Scope.ValidInstance() || state.OpenedAt.IsZero() || state.RegionID == 0 {
		return ErrQuestInstanceInvalid
	}
	result, err := r.shard.ExecContext(ctx, saveQuestInstanceMember,
		state.CharacterID, state.DungeonKey, state.Scope.WorldID(), state.Scope.LayerID(), state.OpenedAt,
		state.RegionID, state.X, state.Y, state.Z, state.Activated, state.EnterCount, state.LastEnterAt)
	if err != nil {
		return fmt.Errorf("save quest instance member: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected < 0 || affected > 2 {
		return fmt.Errorf("save quest instance member affected %d rows", affected)
	}
	return nil
}

func (r *SQLQuestInstanceMemberRepository) LoadQuestInstanceMember(ctx context.Context, characterID int, dungeonKey uint32) (QuestInstanceMemberState, error) {
	if r == nil || r.shard == nil || characterID <= 0 || dungeonKey == 0 {
		return QuestInstanceMemberState{}, ErrQuestInstanceInvalid
	}
	var state QuestInstanceMemberState
	var worldID, layerID uint16
	err := r.shard.QueryRowContext(ctx, `SELECT CHAR_ID,DUNGEON_KEY_ID,WORLD_ID,LAYER_ID,OPENED_AT,
		REGION_ID,POS_X,POS_Y,POS_Z,IS_ACTIVATED,ENTER_COUNT,LAST_ENTER_AT
		FROM SRO_SHARD.CHAR_QUEST_INSTANCE_WORLD WHERE CHAR_ID=? AND DUNGEON_KEY_ID=?`,
		characterID, dungeonKey).Scan(&state.CharacterID, &state.DungeonKey, &worldID, &layerID,
		&state.OpenedAt, &state.RegionID, &state.X, &state.Y, &state.Z, &state.Activated,
		&state.EnterCount, &state.LastEnterAt)
	if errors.Is(err, sql.ErrNoRows) {
		return QuestInstanceMemberState{}, ErrQuestInstanceMissing
	}
	if err != nil {
		return QuestInstanceMemberState{}, err
	}
	state.Scope, err = NewGameWorldScope(worldID, layerID)
	if err != nil {
		return QuestInstanceMemberState{}, errors.Join(ErrQuestInstanceInvalid, err)
	}
	return state, nil
}

func (r *SQLQuestInstanceMemberRepository) LoadQuestInstanceMemberByWorld(ctx context.Context, characterID int, worldID uint16) (QuestInstanceMemberState, error) {
	if r == nil || r.shard == nil || characterID <= 0 || worldID == 0 || worldID == 0xffff {
		return QuestInstanceMemberState{}, ErrQuestInstanceInvalid
	}
	var state QuestInstanceMemberState
	var loadedWorldID, layerID uint16
	err := r.shard.QueryRowContext(ctx, `SELECT CHAR_ID,DUNGEON_KEY_ID,WORLD_ID,LAYER_ID,OPENED_AT,
		REGION_ID,POS_X,POS_Y,POS_Z,IS_ACTIVATED,ENTER_COUNT,LAST_ENTER_AT
		FROM SRO_SHARD.CHAR_QUEST_INSTANCE_WORLD WHERE CHAR_ID=? AND WORLD_ID=?`,
		characterID, worldID).Scan(&state.CharacterID, &state.DungeonKey, &loadedWorldID, &layerID,
		&state.OpenedAt, &state.RegionID, &state.X, &state.Y, &state.Z, &state.Activated,
		&state.EnterCount, &state.LastEnterAt)
	if errors.Is(err, sql.ErrNoRows) {
		return QuestInstanceMemberState{}, ErrQuestInstanceMissing
	}
	if err != nil {
		return QuestInstanceMemberState{}, err
	}
	state.Scope, err = NewGameWorldScope(loadedWorldID, layerID)
	if err != nil {
		return QuestInstanceMemberState{}, errors.Join(ErrQuestInstanceInvalid, err)
	}
	return state, nil
}

func (r *SQLQuestInstanceMemberRepository) DeleteQuestInstanceMember(ctx context.Context, characterID int, worldID uint16) error {
	if r == nil || r.shard == nil || characterID <= 0 || worldID == 0 || worldID == 0xffff {
		return ErrQuestInstanceInvalid
	}
	result, err := r.shard.ExecContext(ctx, `DELETE FROM SRO_SHARD.CHAR_QUEST_INSTANCE_WORLD
		WHERE CHAR_ID=? AND WORLD_ID=?`, characterID, worldID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrQuestInstanceMissing
	}
	return nil
}
