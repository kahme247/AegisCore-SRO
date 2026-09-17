package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/framework/aggregate"
)

var (
	ErrServerEventInvalid  = errors.New("server event command is invalid")
	ErrServerEventConflict = errors.New("server event state conflicts with the command")
)

type ServerEventProgressCommand struct {
	InstanceKey   string
	ServerEventID int
	CharacterID   int
	Delta         uint64
	Target        uint64
	CommandKey    string
}

type ServerEventProgressResult struct {
	InstanceID uint64 `json:"instance_id"`
	Progress   uint64 `json:"progress"`
	Target     uint64 `json:"target"`
	Completed  bool   `json:"completed"`
	GrantCount int    `json:"grant_count"`
	Duplicate  bool   `json:"-"`
}

type ServerEventEngine struct {
	coordinator aggregate.LatestCoordinator
	now         func() time.Time
}

func NewServerEventEngine(coordinator aggregate.LatestCoordinator) (*ServerEventEngine, error) {
	if coordinator == nil {
		return nil, errors.New("server event aggregate coordinator is required")
	}
	return &ServerEventEngine{coordinator: coordinator, now: time.Now}, nil
}

func (engine *ServerEventEngine) Advance(ctx context.Context, command ServerEventProgressCommand) (ServerEventProgressResult, error) {
	if engine == nil || engine.coordinator == nil {
		return ServerEventProgressResult{}, ErrServerEventInvalid
	}
	if !validServerEventKey(command.InstanceKey) || !validServerEventKey(command.CommandKey) || command.ServerEventID <= 0 || command.CharacterID <= 0 || command.Delta == 0 || command.Target == 0 {
		return ServerEventProgressResult{}, ErrServerEventInvalid
	}
	aggregateID := aggregate.ID{Kind: aggregate.Event, Value: command.InstanceKey}
	aggregateCommand := aggregate.Command{Aggregate: aggregateID, IdempotencyKey: aggregate.IdempotencyKey(command.CommandKey), Name: "server-event.advance"}
	result, err := engine.coordinator.ExecuteLatest(ctx, aggregateCommand, func(ctx context.Context, transaction aggregate.Transaction) (json.RawMessage, error) {
		progress, mutationErr := advanceServerEvent(ctx, transaction, command, engine.now().UTC())
		if mutationErr != nil {
			return nil, mutationErr
		}
		payload, marshalErr := json.Marshal(progress)
		if marshalErr != nil {
			return nil, marshalErr
		}
		topic := "server-event.progressed"
		if progress.Completed {
			topic = "server-event.completed"
		}
		if enqueueErr := transaction.Enqueue(aggregate.OutboxMessage{ID: command.CommandKey, Aggregate: aggregateID, Topic: topic, Payload: payload, OccurredAt: engine.now().UTC()}); enqueueErr != nil {
			return nil, enqueueErr
		}
		return payload, nil
	})
	if err != nil {
		return ServerEventProgressResult{}, err
	}
	var progress ServerEventProgressResult
	if err := json.Unmarshal(result.Payload, &progress); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("decode server event result: %w", err)
	}
	progress.Duplicate = result.Duplicate
	return progress, nil
}

func validServerEventKey(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("-_.:", character)) {
			return false
		}
	}
	return true
}

func advanceServerEvent(ctx context.Context, transaction aggregate.Transaction, command ServerEventProgressCommand, now time.Time) (ServerEventProgressResult, error) {
	database := transaction.DB()
	if _, err := database.ExecContext(ctx, "INSERT INTO `SERVER_EVENT_INSTANCE` (`INSTANCE_KEY`,`SERVER_EVENT_ID`,`TARGET_PROGRESS`) VALUES (?,?,?) ON DUPLICATE KEY UPDATE `INSTANCE_KEY`=VALUES(`INSTANCE_KEY`)", command.InstanceKey, command.ServerEventID, command.Target); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("ensure server event instance: %w", err)
	}
	var instanceID, progress, target uint64
	var eventID int
	var state byte
	if err := database.QueryRowContext(ctx, "SELECT `ID`,`SERVER_EVENT_ID`,`PROGRESS`,`TARGET_PROGRESS`,`STATE` FROM `SERVER_EVENT_INSTANCE` WHERE `INSTANCE_KEY`=? FOR UPDATE", command.InstanceKey).Scan(&instanceID, &eventID, &progress, &target, &state); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("lock server event instance: %w", err)
	}
	if eventID != command.ServerEventID || target != command.Target || state == 3 {
		return ServerEventProgressResult{}, ErrServerEventConflict
	}
	if state == 2 {
		return ServerEventProgressResult{InstanceID: instanceID, Progress: progress, Target: target, Completed: true}, nil
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO `SERVER_EVENT_PARTICIPANT` (`INSTANCE_ID`,`CHAR_ID`,`PROGRESS`) VALUES (?,?,0) ON DUPLICATE KEY UPDATE `CHAR_ID`=VALUES(`CHAR_ID`)", instanceID, command.CharacterID); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("ensure server event participant: %w", err)
	}
	var participantProgress uint64
	if err := database.QueryRowContext(ctx, "SELECT `PROGRESS` FROM `SERVER_EVENT_PARTICIPANT` WHERE `INSTANCE_ID`=? AND `CHAR_ID`=? FOR UPDATE", instanceID, command.CharacterID).Scan(&participantProgress); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("lock server event participant: %w", err)
	}
	if progress > math.MaxUint64-command.Delta || participantProgress > math.MaxUint64-command.Delta {
		return ServerEventProgressResult{}, ErrServerEventConflict
	}
	progress = min(progress+command.Delta, target)
	participantProgress += command.Delta
	if _, err := database.ExecContext(ctx, "UPDATE `SERVER_EVENT_PARTICIPANT` SET `PROGRESS`=? WHERE `INSTANCE_ID`=? AND `CHAR_ID`=?", participantProgress, instanceID, command.CharacterID); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("update server event participant: %w", err)
	}
	completed := progress >= target
	if completed {
		result, err := database.ExecContext(ctx, "UPDATE `SERVER_EVENT_INSTANCE` SET `PROGRESS`=?,`STATE`=2,`COMPLETED_AT`=? WHERE `ID`=? AND `STATE`=1", progress, now, instanceID)
		if err != nil {
			return ServerEventProgressResult{}, fmt.Errorf("complete server event instance: %w", err)
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ServerEventProgressResult{}, ErrServerEventConflict
		}
		if _, err = database.ExecContext(ctx, "UPDATE `SERVER_EVENT_PARTICIPANT` SET `COMPLETED_AT`=? WHERE `INSTANCE_ID`=?", now, instanceID); err != nil {
			return ServerEventProgressResult{}, fmt.Errorf("complete server event participants: %w", err)
		}
	} else if _, err := database.ExecContext(ctx, "UPDATE `SERVER_EVENT_INSTANCE` SET `PROGRESS`=? WHERE `ID`=? AND `STATE`=1", progress, instanceID); err != nil {
		return ServerEventProgressResult{}, fmt.Errorf("update server event progress: %w", err)
	}
	grantCount := 0
	if completed {
		var err error
		grantCount, err = createServerEventGrants(ctx, transaction, instanceID, command.ServerEventID, now)
		if err != nil {
			return ServerEventProgressResult{}, err
		}
	}
	return ServerEventProgressResult{InstanceID: instanceID, Progress: progress, Target: target, Completed: completed, GrantCount: grantCount}, nil
}

type serverEventRewardReference struct {
	ID           int
	Class        byte
	ApplySeconds int
	SkillID      int
	Experience   float64
	SkillExp     float64
	MonsterRefID int
	RegionID     int16
	X, Y, Z      float32
}

func createServerEventGrants(ctx context.Context, transaction aggregate.Transaction, instanceID uint64, eventID int, now time.Time) (int, error) {
	database := transaction.DB()
	rows, err := database.QueryContext(ctx, "SELECT r.REWARD_ID,r.REWARD_CLASS,r.APPLY_TIME,r.REF_SKILL_ID,COALESCE(e.EXP_RATIO,r.EXP_RATIO),COALESCE(e.SKILL_EXP_RATIO,r.SKILL_EXP_RATIO),s.REF_OBJ_ID,s.REGION_ID,s.POS_X,s.POS_Y,s.POS_Z FROM `REF_SERVER_EVENT_REWARD_EVENT` link JOIN `REF_SERVER_EVENT_REWARD` r ON r.REWARD_ID=link.REWARD_ID LEFT JOIN `REF_SERVER_EVENT_REWARD_EXP_UP_FOR_PLAYERS` e ON e.REWARD_ID=r.REWARD_ID LEFT JOIN `REF_SERVER_EVENT_REWARD_SPAWN_MONSTER` s ON s.REWARD_ID=r.REWARD_ID WHERE link.SERVER_EVENT_ID=? ORDER BY r.REWARD_ID", eventID)
	if err != nil {
		return 0, fmt.Errorf("load server event rewards: %w", err)
	}
	defer rows.Close()
	var rewards []serverEventRewardReference
	for rows.Next() {
		var reward serverEventRewardReference
		var monsterRef, region sql.NullInt64
		var x, y, z sql.NullFloat64
		if err = rows.Scan(&reward.ID, &reward.Class, &reward.ApplySeconds, &reward.SkillID, &reward.Experience, &reward.SkillExp, &monsterRef, &region, &x, &y, &z); err != nil {
			return 0, fmt.Errorf("scan server event reward: %w", err)
		}
		if monsterRef.Valid {
			reward.MonsterRefID = int(monsterRef.Int64)
		}
		if region.Valid {
			reward.RegionID = int16(region.Int64)
		}
		reward.X, reward.Y, reward.Z = float32(x.Float64), float32(y.Float64), float32(z.Float64)
		if reward.ID <= 0 || reward.Class < 1 || reward.Class > 3 {
			return 0, ErrServerEventConflict
		}
		rewards = append(rewards, reward)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	participants, err := serverEventParticipantIDs(ctx, transaction, instanceID)
	if err != nil {
		return 0, err
	}
	grantCount := 0
	for _, reward := range rewards {
		targets := []int{0}
		if reward.Class == 2 {
			targets = participants
		}
		for _, characterID := range targets {
			if reward.Class == 2 && reward.SkillID <= 0 {
				return 0, ErrServerEventConflict
			}
			if reward.Class == 3 && (reward.MonsterRefID <= 0 || reward.RegionID == 0) {
				return 0, ErrServerEventConflict
			}
			key := "event:" + strconv.FormatUint(instanceID, 10) + ":" + strconv.Itoa(characterID) + ":" + strconv.Itoa(reward.ID)
			result, insertErr := database.ExecContext(ctx, "INSERT INTO `SERVER_EVENT_REWARD_GRANT` (`INSTANCE_ID`,`CHAR_ID`,`REWARD_ID`,`REWARD_CLASS`,`COMMAND_KEY`) VALUES (?,?,?,?,?)", instanceID, characterID, reward.ID, reward.Class, key)
			if insertErr != nil {
				return 0, fmt.Errorf("insert server event reward grant: %w", insertErr)
			}
			grantID, insertErr := result.LastInsertId()
			if insertErr != nil {
				return 0, insertErr
			}
			if reward.Class == 1 {
				if reward.ApplySeconds <= 0 || reward.Experience < 0 || reward.SkillExp < 0 || reward.Experience > 1 || reward.SkillExp > 1 || (reward.Experience == 0 && reward.SkillExp == 0) {
					return 0, ErrServerEventConflict
				}
				// SR_GameServer 0x00671680 adds each authored ratio to the
				// normal 1.0 multiplier and clamps the bonus to [0,1].
				expPercent := uint32(100 + math.Round(reward.Experience*100))
				skillPercent := uint32(100 + math.Round(reward.SkillExp*100))
				if _, insertErr = database.ExecContext(ctx, "INSERT INTO `SERVER_EVENT_TIMED_EFFECT` (`GRANT_ID`,`EFFECT_KEY`,`EXP_RATE_PERCENT`,`SKILL_EXP_RATE_PERCENT`,`STARTS_AT`,`ENDS_AT`) VALUES (?,?,?,?,?,?)", grantID, key, expPercent, skillPercent, now, now.Add(time.Duration(reward.ApplySeconds)*time.Second)); insertErr != nil {
					return 0, fmt.Errorf("insert server event timed effect: %w", insertErr)
				}
			} else if reward.Class == 3 {
				if _, insertErr = database.ExecContext(ctx, "INSERT INTO `SERVER_EVENT_MONSTER_SPAWN` (`GRANT_ID`,`REF_OBJ_ID`,`REGION_ID`,`POS_X`,`POS_Y`,`POS_Z`) VALUES (?,?,?,?,?,?)", grantID, reward.MonsterRefID, reward.RegionID, reward.X, reward.Y, reward.Z); insertErr != nil {
					return 0, fmt.Errorf("insert server event monster spawn: %w", insertErr)
				}
			}
			grantCount++
		}
	}
	return grantCount, nil
}

func serverEventParticipantIDs(ctx context.Context, transaction aggregate.Transaction, instanceID uint64) ([]int, error) {
	rows, err := transaction.DB().QueryContext(ctx, "SELECT `CHAR_ID` FROM `SERVER_EVENT_PARTICIPANT` WHERE `INSTANCE_ID`=? ORDER BY `CHAR_ID` FOR UPDATE", instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
