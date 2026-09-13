package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type SQLQuestInstanceScopeAllocator struct{ shard *sql.DB }

func NewQuestInstanceScopeAllocator(shard *sql.DB) (*SQLQuestInstanceScopeAllocator, error) {
	if shard == nil {
		return nil, ErrQuestInstanceInvalid
	}
	return &SQLQuestInstanceScopeAllocator{shard: shard}, nil
}

func (a *SQLQuestInstanceScopeAllocator) AllocateQuestInstanceScope(ctx context.Context, questID int, dungeonKey uint32) (GameWorldScope, error) {
	if a == nil || a.shard == nil || questID <= 0 || dungeonKey == 0 {
		return 0, ErrQuestInstanceInvalid
	}
	connection, err := a.shard.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	lockName := fmt.Sprintf("aegiscore:quest-instance:%d", questID)
	var acquired sql.NullInt64
	if err = connection.QueryRowContext(ctx, "SELECT GET_LOCK(?, 10)", lockName).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return 0, errors.Join(ErrQuestInstanceInvalid, err)
	}
	defer func() { _, _ = connection.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName) }()
	var worldID, maxInstances uint16
	err = connection.QueryRowContext(ctx, `SELECT m.WORLD_ID,w.MAX_INSTANCE_COUNT
		FROM SRO_SHARD.REF_QUEST_INSTANCE_WORLD_MAP m
		JOIN SRO_SHARD.REF_GAME_WORLD w ON w.WORLD_ID=m.WORLD_ID
		WHERE m.QUEST_ID=? ORDER BY m.STAGE_INDEX LIMIT 1`, questID).Scan(&worldID, &maxInstances)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrQuestInstanceMissing
	}
	if err != nil {
		return 0, err
	}
	rows, err := connection.QueryContext(ctx, `SELECT LAYER_ID FROM SRO_SHARD.QUEST_INSTANCE_SCOPE_RESERVATION
		WHERE WORLD_ID=? ORDER BY LAYER_ID`, worldID)
	if err != nil {
		return 0, err
	}
	used := make(map[uint16]struct{})
	for rows.Next() {
		var layer uint16
		if scanErr := rows.Scan(&layer); scanErr != nil {
			_ = rows.Close()
			return 0, scanErr
		}
		used[layer] = struct{}{}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	layerID, ok := firstAvailableQuestInstanceLayer(maxInstances, used)
	if !ok {
		return 0, ErrQuestInstanceExists
	}
	if _, err = connection.ExecContext(ctx, `INSERT INTO SRO_SHARD.QUEST_INSTANCE_SCOPE_RESERVATION
		(WORLD_ID,LAYER_ID,QUEST_ID,DUNGEON_KEY_ID) VALUES (?,?,?,?)`, worldID, layerID, questID, dungeonKey); err != nil {
		return 0, err
	}
	return NewGameWorldScope(worldID, layerID)
}

func (a *SQLQuestInstanceScopeAllocator) ReleaseQuestInstanceScope(ctx context.Context, scope GameWorldScope) error {
	if a == nil || a.shard == nil || !scope.ValidInstance() {
		return ErrQuestInstanceInvalid
	}
	result, err := a.shard.ExecContext(ctx, `DELETE FROM SRO_SHARD.QUEST_INSTANCE_SCOPE_RESERVATION WHERE WORLD_ID=? AND LAYER_ID=?`, scope.WorldID(), scope.LayerID())
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

func firstAvailableQuestInstanceLayer(maxInstances uint16, used map[uint16]struct{}) (uint16, bool) {
	for layer := uint32(1); layer <= uint32(maxInstances) && layer < 0xffff; layer++ {
		if _, exists := used[uint16(layer)]; !exists {
			return uint16(layer), true
		}
	}
	return 0, false
}
