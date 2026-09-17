package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	log "github.com/sirupsen/logrus"
)

const ServerEventRewardReconcileInterval = 5 * time.Second

type serverEventRateController interface {
	Refresh(context.Context) error
}

type serverEventMonsterSpawner interface {
	SpawnServerEventMonster(uint32, int16, float32, float32, float32) (*model.NPC, error)
}

type ServerEventRewardRuntime struct {
	database *sql.DB
	rates    serverEventRateController
	spawner  serverEventMonsterSpawner
	world    *WorldService
	interval time.Duration
	now      func() time.Time
	done     chan struct{}
	once     sync.Once
}

func NewServerEventRewardRuntime(database *sql.DB, rates serverEventRateController, spawner serverEventMonsterSpawner, world *WorldService) (*ServerEventRewardRuntime, error) {
	if database == nil || rates == nil || spawner == nil || world == nil {
		return nil, errors.New("server event reward runtime dependencies are required")
	}
	return &ServerEventRewardRuntime{database: database, rates: rates, spawner: spawner, world: world, interval: ServerEventRewardReconcileInterval, now: time.Now, done: make(chan struct{})}, nil
}

func (runtime *ServerEventRewardRuntime) Start(ctx context.Context) {
	if runtime == nil {
		return
	}
	runtime.once.Do(func() { go runtime.run(ctx) })
}

func (runtime *ServerEventRewardRuntime) run(ctx context.Context) {
	defer close(runtime.done)
	if err := runtime.Reconcile(ctx); err != nil && ctx.Err() == nil {
		log.WithError(err).Error("reconcile server event rewards")
	}
	ticker := time.NewTicker(runtime.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runtime.Reconcile(ctx); err != nil && ctx.Err() == nil {
				log.WithError(err).Error("reconcile server event rewards")
			}
		}
	}
}

func (runtime *ServerEventRewardRuntime) Reconcile(ctx context.Context) error {
	if runtime == nil || runtime.database == nil {
		return errors.New("server event reward runtime is unavailable")
	}
	now := runtime.now().UTC()
	if err := runtime.reconcileTimedRates(ctx, now); err != nil {
		return err
	}
	rows, err := runtime.database.QueryContext(ctx, "SELECT g.ID,g.CHAR_ID,g.REWARD_ID,g.REWARD_CLASS,i.SERVER_EVENT_ID,r.REF_SKILL_ID,r.REF_VALUE_1,r.REF_VALUE_2,r.REF_VALUE_3,s.REF_OBJ_ID,s.REGION_ID,s.POS_X,s.POS_Y,s.POS_Z FROM `SERVER_EVENT_REWARD_GRANT` g JOIN `SERVER_EVENT_INSTANCE` i ON i.ID=g.INSTANCE_ID JOIN `REF_SERVER_EVENT_REWARD` r ON r.REWARD_ID=g.REWARD_ID LEFT JOIN `SERVER_EVENT_MONSTER_SPAWN` s ON s.GRANT_ID=g.ID WHERE g.STATUS=1 ORDER BY g.CREATED_AT,g.ID LIMIT 100")
	if err != nil {
		return fmt.Errorf("query pending server event grants: %w", err)
	}
	defer rows.Close()
	var grants []pendingServerEventGrant
	for rows.Next() {
		var grant pendingServerEventGrant
		var refObj, region sql.NullInt64
		var x, y, z sql.NullFloat64
		if err = rows.Scan(&grant.ID, &grant.CharacterID, &grant.RewardID, &grant.Class, &grant.ServerEventID, &grant.SkillID, &grant.ReferenceValue1, &grant.ReferenceValue2, &grant.ReferenceValue3, &refObj, &region, &x, &y, &z); err != nil {
			return err
		}
		if refObj.Valid {
			grant.RefObjectID = uint32(refObj.Int64)
		}
		if region.Valid {
			grant.RegionID = int16(region.Int64)
		}
		grant.X, grant.Y, grant.Z = float32(x.Float64), float32(y.Float64), float32(z.Float64)
		grants = append(grants, grant)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, grant := range grants {
		if err := runtime.applyGrant(ctx, grant, now); err != nil {
			_ = runtime.recordGrantError(ctx, grant.ID, err)
			if !errors.Is(err, errServerEventTargetOffline) {
				return err
			}
		}
	}
	return nil
}

type pendingServerEventGrant struct {
	ID              uint64
	CharacterID     int
	RewardID        uint32
	Class           byte
	ServerEventID   uint32
	SkillID         uint32
	ReferenceValue1 uint64
	ReferenceValue2 uint64
	ReferenceValue3 uint64
	RefObjectID     uint32
	RegionID        int16
	X, Y, Z         float32
}

var errServerEventTargetOffline = errors.New("server event skill target is offline")

func (runtime *ServerEventRewardRuntime) applyGrant(ctx context.Context, grant pendingServerEventGrant, now time.Time) error {
	switch grant.Class {
	case 1:
		var key string
		var exp, skill uint32
		var ends time.Time
		if err := runtime.database.QueryRowContext(ctx, "SELECT EFFECT_KEY,EXP_RATE_PERCENT,SKILL_EXP_RATE_PERCENT,ENDS_AT FROM SERVER_EVENT_TIMED_EFFECT WHERE GRANT_ID=?", grant.ID).Scan(&key, &exp, &skill, &ends); err != nil {
			return err
		}
		if !ends.After(now) {
			return runtime.rollbackTimedRate(ctx, grant.ID, key, now)
		}
		if grant.ReferenceValue1 > math.MaxUint32 || grant.ReferenceValue2 > math.MaxUint32 || grant.ReferenceValue3 > math.MaxUint32 {
			return ErrServerEventConflict
		}
		remaining := uint64(math.Ceil(ends.Sub(now).Seconds()))
		if remaining > math.MaxUint32 {
			return ErrServerEventConflict
		}
		packet, err := vsro188.BuildServerEventTimedRewards([]vsro188.ServerEventTimedReward{{
			RewardID: grant.RewardID, ServerEventID: grant.ServerEventID, RewardClass: grant.Class,
			ReferenceValue1: uint32(grant.ReferenceValue1), ReferenceValue2: uint32(grant.ReferenceValue2), ReferenceValue3: uint32(grant.ReferenceValue3), Remaining: uint32(remaining),
		}})
		if err != nil {
			return err
		}
		if err = runtime.sendGrantPacket(grant.CharacterID, packet); err != nil {
			return err
		}
		return runtime.markGrantApplied(ctx, grant.ID, now)
	case 2:
		player, err := runtime.world.GetPlayerByCharacterID(grant.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			return errServerEventTargetOffline
		}
		skill, exists := model.RefSkills[grant.SkillID]
		if !exists || grant.ID > math.MaxUint32 || skill.BuffDuration < 0 {
			return ErrServerEventConflict
		}
		token := uint32(grant.ID)
		duration := time.Duration(skill.BuffDuration) * time.Millisecond
		replaced, ok := player.CommitActiveBuffAt(skill.ID, token, player.GetUniqueID(), duration, skill.BuffModifiers, now)
		if !ok {
			return ErrServerEventConflict
		}
		if replaced != 0 {
			_ = player.Session.Send(model.BuildBuffRemove(replaced))
		}
		if err := player.Session.Send(model.BuildBuffAdd(player.GetUniqueID(), skill.ID, token)); err != nil {
			player.RemoveActiveBuff(token)
			return err
		}
		if err := player.Session.Send(player.BuildStatsUpdatePacket()); err != nil {
			return err
		}
		completed, err := vsro188.BuildServerEventRewardCompleted(grant.ServerEventID, grant.RewardID)
		if err != nil {
			return err
		}
		if err = runtime.sendGrantPacket(grant.CharacterID, completed); err != nil {
			return err
		}
		return runtime.markGrantApplied(ctx, grant.ID, now)
	case 3:
		npc, err := runtime.spawner.SpawnServerEventMonster(grant.RefObjectID, grant.RegionID, grant.X, grant.Y, grant.Z)
		if err != nil {
			return err
		}
		if npc == nil {
			return errors.New("server event monster spawner returned nil")
		}
		tx, err := runtime.database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(ctx, "UPDATE SERVER_EVENT_MONSTER_SPAWN SET RUNTIME_UID=?,SPAWNED_AT=? WHERE GRANT_ID=? AND SPAWNED_AT IS NULL", npc.GetUniqueID(), now, grant.ID)
		if err != nil {
			return err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ErrServerEventConflict
		}
		if err = markServerEventGrantApplied(ctx, tx, grant.ID, now); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		completed, err := vsro188.BuildServerEventRewardCompleted(grant.ServerEventID, grant.RewardID)
		if err != nil {
			return err
		}
		return runtime.sendGrantPacket(grant.CharacterID, completed)
	default:
		return ErrServerEventConflict
	}
}

func (runtime *ServerEventRewardRuntime) sendGrantPacket(characterID int, packet network.Packet) error {
	if characterID <= 0 {
		runtime.world.Broadcast(packet)
		return nil
	}
	player, err := runtime.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil || player.GetSession() == nil {
		return errServerEventTargetOffline
	}
	return player.GetSession().Send(packet)
}

func (runtime *ServerEventRewardRuntime) reconcileTimedRates(ctx context.Context, now time.Time) error {
	rows, err := runtime.database.QueryContext(ctx, "SELECT e.GRANT_ID,e.EFFECT_KEY,e.EXP_RATE_PERCENT,e.SKILL_EXP_RATE_PERCENT,e.ENDS_AT FROM SERVER_EVENT_TIMED_EFFECT e JOIN SERVER_EVENT_REWARD_GRANT g ON g.ID=e.GRANT_ID WHERE e.ROLLED_BACK_AT IS NULL ORDER BY e.ENDS_AT,e.GRANT_ID")
	if err != nil {
		return err
	}
	defer rows.Close()
	type effect struct {
		grantID    uint64
		key        string
		exp, skill uint32
		ends       time.Time
	}
	var effects []effect
	for rows.Next() {
		var current effect
		if err = rows.Scan(&current.grantID, &current.key, &current.exp, &current.skill, &current.ends); err != nil {
			return err
		}
		effects = append(effects, current)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, current := range effects {
		if current.ends.After(now) {
			continue
		}
		if err := runtime.rollbackTimedRate(ctx, current.grantID, current.key, now); err != nil {
			return err
		}
	}
	return runtime.rates.Refresh(ctx)
}

func (runtime *ServerEventRewardRuntime) rollbackTimedRate(ctx context.Context, grantID uint64, key string, now time.Time) error {
	_ = key
	tx, err := runtime.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE SERVER_EVENT_TIMED_EFFECT SET ROLLED_BACK_AT=? WHERE GRANT_ID=? AND ROLLED_BACK_AT IS NULL", now, grantID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE SERVER_EVENT_REWARD_GRANT SET STATUS=3,ROLLED_BACK_AT=?,LAST_ERROR=NULL WHERE ID=? AND STATUS IN (1,2)", now, grantID); err != nil {
		return err
	}
	return tx.Commit()
}

func (runtime *ServerEventRewardRuntime) markGrantApplied(ctx context.Context, grantID uint64, now time.Time) error {
	return markServerEventGrantApplied(ctx, runtime.database, grantID, now)
}

type serverEventGrantExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func markServerEventGrantApplied(ctx context.Context, database serverEventGrantExecer, grantID uint64, now time.Time) error {
	result, err := database.ExecContext(ctx, "UPDATE SERVER_EVENT_REWARD_GRANT SET STATUS=2,APPLIED_AT=?,LAST_ERROR=NULL WHERE ID=? AND STATUS=1", now, grantID)
	if err != nil {
		return err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrServerEventConflict
	}
	return nil
}

func (runtime *ServerEventRewardRuntime) recordGrantError(ctx context.Context, grantID uint64, grantErr error) error {
	message := grantErr.Error()
	if len(message) > 1024 {
		message = message[:1024]
	}
	_, err := runtime.database.ExecContext(ctx, "UPDATE SERVER_EVENT_REWARD_GRANT SET LAST_ERROR=? WHERE ID=? AND STATUS=1", message, grantID)
	return err
}

func (runtime *ServerEventRewardRuntime) Wait(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	select {
	case <-runtime.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
