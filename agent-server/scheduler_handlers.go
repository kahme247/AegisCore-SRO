package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/scheduler"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	log "github.com/sirupsen/logrus"
)

const schedulerLogHandler = "agent.log"
const schedulerNoticeHandler = "server.notice"
const schedulerUniqueSpawnHandler = "unique.spawn"
const schedulerExperienceRatesStartHandler = "rates.experience.start"
const schedulerExperienceRatesFinishHandler = "rates.experience.finish"
const schedulerMobRatesStartHandler = "rates.mob.start"
const schedulerMobRatesFinishHandler = "rates.mob.finish"
const schedulerServerEventAdvanceHandler = "server-event.advance"
const schedulerCompetitiveEventHandler = "competitive.run"
const schedulerGlobalEventHandler = "event.global"
const schedulerVietnamMRHandler = "event.vietnam-mr"
const schedulerServerEventNotifyHandler = "event.server-data"
const schedulerFortressHandler = "fortress.run"

type schedulerLogPayload struct {
	Message string `json:"message"`
}

type noticeBroadcaster interface {
	BroadcastNotice(string) (int, error)
}

type seasonalEventBroadcaster interface {
	BroadcastGlobalEvent(string, uint32) (int, error)
	BroadcastVietnamMR(vsro188.VietnamMRUpdate) (int, error)
	BroadcastServerEventData([]vsro188.ServerEventData) (int, error)
}

type scheduledUniqueSpawner interface {
	SpawnScheduledUnique(string, int16, float32) (*model.NPC, error)
}

type scheduledGameplayRateController interface {
	ActivateOverride(context.Context, string, model.GameplayRateOverride) error
	DeactivateOverride(context.Context, string) error
	ActivateMobOverride(context.Context, string, model.GameplayMobRateOverride) error
	DeactivateMobOverride(context.Context, string) error
}

type scheduledServerEventController interface {
	Advance(context.Context, service.ServerEventProgressCommand) (service.ServerEventProgressResult, error)
}

type scheduledFortressController interface {
	SetFortressPeriod(context.Context, int, model.FortressPeriod, time.Time, time.Time) (model.FortressState, error)
	FinishFortressBattle(context.Context, int, time.Time) (model.FortressState, error)
	RegisterFortressStructure(context.Context, model.FortressStructure) error
}

type packetBroadcaster interface {
	Broadcast(network.Packet)
}

type agentSchedulerAdministration struct {
	store   *scheduler.SQLStore
	runtime *scheduler.Runtime
}

func (source agentSchedulerAdministration) Create(ctx context.Context, definition scheduler.Definition) (scheduler.Definition, error) {
	return source.store.Create(ctx, definition)
}

func (source agentSchedulerAdministration) List(ctx context.Context, limit int) ([]scheduler.Definition, error) {
	return source.store.List(ctx, limit)
}

func (source agentSchedulerAdministration) Get(ctx context.Context, id string) (scheduler.Definition, error) {
	return source.store.Get(ctx, id)
}

func (source agentSchedulerAdministration) HasHandler(name string) bool {
	return source.runtime.HasHandler(name)
}

func (source agentSchedulerAdministration) SetEnabled(ctx context.Context, id string, expectedVersion uint64, enabled bool) (scheduler.Definition, error) {
	return source.store.SetEnabled(ctx, id, expectedVersion, enabled)
}

func (source agentSchedulerAdministration) ListRuns(ctx context.Context, id string, limit int) ([]scheduler.RunRecord, error) {
	return source.store.ListRuns(ctx, id, limit)
}

func agentSchedulerHandlers(notices noticeBroadcaster, uniques scheduledUniqueSpawner, rates scheduledGameplayRateController, events ...scheduledServerEventController) map[string]scheduler.Handler {
	var eventController scheduledServerEventController
	seasonalBroadcaster, _ := notices.(seasonalEventBroadcaster)
	if len(events) > 0 {
		eventController = events[0]
	}
	return map[string]scheduler.Handler{
		schedulerLogHandler: handleSchedulerLog,
		schedulerNoticeHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleSchedulerNotice(ctx, run, notices)
		},
		schedulerUniqueSpawnHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledUniqueSpawn(ctx, run, uniques)
		},
		schedulerExperienceRatesStartHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledExperienceRatesStart(ctx, run, rates)
		},
		schedulerExperienceRatesFinishHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledExperienceRatesFinish(ctx, run, rates)
		},
		schedulerMobRatesStartHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledMobRatesStart(ctx, run, rates)
		},
		schedulerMobRatesFinishHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledMobRatesFinish(ctx, run, rates)
		},
		schedulerServerEventAdvanceHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledServerEventAdvance(ctx, run, eventController, seasonalBroadcaster)
		},
		schedulerGlobalEventHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledGlobalEvent(ctx, run, seasonalBroadcaster)
		},
		schedulerVietnamMRHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledVietnamMR(ctx, run, seasonalBroadcaster)
		},
		schedulerServerEventNotifyHandler: func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledServerEventNotify(ctx, run, seasonalBroadcaster)
		},
	}
}

func agentSchedulerHandlersWithCompetitive(notices noticeBroadcaster, uniques scheduledUniqueSpawner, rates scheduledGameplayRateController, events scheduledServerEventController, competitive *service.CompetitiveRuntime, fortress ...scheduledFortressController) map[string]scheduler.Handler {
	handlers := agentSchedulerHandlers(notices, uniques, rates, events)
	handlers[schedulerCompetitiveEventHandler] = func(ctx context.Context, run scheduler.Run) error {
		return handleScheduledCompetitiveEvent(ctx, run, competitive)
	}
	if len(fortress) > 0 && fortress[0] != nil {
		broadcaster, _ := notices.(packetBroadcaster)
		handlers[schedulerFortressHandler] = func(ctx context.Context, run scheduler.Run) error {
			return handleScheduledFortress(ctx, run, fortress[0], broadcaster)
		}
	}
	return handlers
}

type scheduledFortressPayload struct {
	FortressID int                       `json:"fortressId"`
	Action     string                    `json:"action"`
	StartsAt   time.Time                 `json:"startsAt,omitempty"`
	EndsAt     time.Time                 `json:"endsAt,omitempty"`
	Structures []model.FortressStructure `json:"structures,omitempty"`
}

func handleScheduledFortress(ctx context.Context, run scheduler.Run, controller scheduledFortressController, broadcaster packetBroadcaster) error {
	if controller == nil {
		return errors.New("fortress controller is unavailable")
	}
	var payload scheduledFortressPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil || payload.FortressID <= 0 {
		return permanentSchedulerHandlerError{err: errors.New("fortress.run payload is invalid")}
	}
	startsAt := payload.StartsAt.UTC()
	if startsAt.IsZero() {
		startsAt = time.Now().UTC()
	}
	var period model.FortressPeriod
	var updateKind byte
	setPeriod := true
	switch strings.ToLower(strings.TrimSpace(payload.Action)) {
	case "application-open":
		period, updateKind = model.FortressPeriodApplication, vsro188.SiegeUpdateApplyPeriodBegin
	case "application-close":
		period, updateKind = model.FortressPeriodDefault, vsro188.SiegeUpdateApplyPeriodEnd
	case "battle-start":
		hearts := 0
		for _, structure := range payload.Structures {
			if structure.Heart {
				hearts++
			}
		}
		if len(payload.Structures) == 0 || hearts != 1 {
			return permanentSchedulerHandlerError{err: errors.New("fortress.run battle-start requires structures with exactly one heart")}
		}
		for index := range payload.Structures {
			structure := payload.Structures[index]
			structure.FortressID = payload.FortressID
			structure.UpdatedAt = startsAt
			if err := controller.RegisterFortressStructure(ctx, structure); err != nil {
				return err
			}
		}
		period, updateKind = model.FortressPeriodBattle, vsro188.SiegeUpdateBattleBegin
	case "battle-finish":
		if _, err := controller.FinishFortressBattle(ctx, payload.FortressID, startsAt); err != nil {
			return err
		}
		updateKind = vsro188.SiegeUpdateBattleEnded
		setPeriod = false
	case "tax-open":
		period, updateKind = model.FortressPeriodTax, vsro188.SiegeUpdateTaxPeriodBegin
	case "tax-close":
		period, updateKind = model.FortressPeriodDefault, vsro188.SiegeUpdateTaxPeriodEnd
	default:
		return permanentSchedulerHandlerError{err: errors.New("fortress.run action is invalid")}
	}
	if setPeriod {
		if _, err := controller.SetFortressPeriod(ctx, payload.FortressID, period, startsAt, payload.EndsAt); err != nil {
			return err
		}
	}
	packet, err := vsro188.BuildSiegePeriodUpdate(updateKind)
	if err != nil {
		return err
	}
	if broadcaster != nil {
		broadcaster.Broadcast(packet)
	}
	return nil
}

type scheduledCompetitivePayload struct {
	Event   string `json:"event"`
	Action  string `json:"action"`
	MatchID uint64 `json:"matchId,omitempty"`
}

func handleScheduledCompetitiveEvent(ctx context.Context, run scheduler.Run, runtime *service.CompetitiveRuntime) error {
	if runtime == nil {
		return errors.New("competitive runtime is unavailable")
	}
	var payload scheduledCompetitivePayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("competitive.run payload is invalid")}
	}
	var event service.CompetitiveEvent
	switch strings.ToLower(strings.TrimSpace(payload.Event)) {
	case "flag-war":
		event = service.CompetitiveFlagWar
	case "battle-arena":
		event = service.CompetitiveBattleArena
	default:
		return permanentSchedulerHandlerError{err: errors.New("competitive.run event must be flag-war or battle-arena")}
	}
	switch strings.ToLower(strings.TrimSpace(payload.Action)) {
	case "open":
		return runtime.Open(event)
	case "start":
		_, err := runtime.Start(event)
		return err
	case "finish":
		if payload.MatchID == 0 {
			return permanentSchedulerHandlerError{err: errors.New("competitive.run finish requires matchId")}
		}
		_, err := runtime.Finish(ctx, payload.MatchID)
		return err
	default:
		return permanentSchedulerHandlerError{err: errors.New("competitive.run action must be open, start, or finish")}
	}
}

type scheduledGlobalEventPayload struct {
	Name   string `json:"name"`
	Value  uint32 `json:"value"`
	Notice string `json:"notice,omitempty"`
}

func handleScheduledGlobalEvent(ctx context.Context, run scheduler.Run, broadcaster seasonalEventBroadcaster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if broadcaster == nil {
		return errors.New("seasonal event broadcaster is unavailable")
	}
	var payload scheduledGlobalEventPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("event.global payload is invalid")}
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" || len([]rune(payload.Name)) > 128 {
		return permanentSchedulerHandlerError{err: errors.New("event.global requires a 1-128 character name")}
	}
	if _, err := broadcaster.BroadcastGlobalEvent(payload.Name, payload.Value); err != nil {
		return err
	}
	return broadcastScheduledEventNotice(broadcaster, run.Phase, payload.Name, payload.Notice)
}

func broadcastScheduledEventNotice(broadcaster seasonalEventBroadcaster, phase scheduler.Phase, eventName, explicit string) error {
	notices, ok := broadcaster.(noticeBroadcaster)
	if !ok {
		return nil
	}
	message := strings.TrimSpace(explicit)
	if message == "" {
		switch phase {
		case scheduler.Warning:
			message = fmt.Sprintf("%s event will start soon.", eventName)
		case scheduler.Start:
			message = fmt.Sprintf("%s event has started.", eventName)
		case scheduler.Finish:
			message = fmt.Sprintf("%s event has ended.", eventName)
		default:
			return nil
		}
	}
	if _, err := notices.BroadcastNotice(message); err != nil {
		return fmt.Errorf("broadcast scheduled event notice: %w", err)
	}
	return nil
}

type scheduledVietnamMRPayload struct {
	ElapsedSeconds uint16 `json:"elapsedSeconds"`
	StartHour      byte   `json:"startHour"`
	EndHour        byte   `json:"endHour"`
}

func handleScheduledVietnamMR(ctx context.Context, run scheduler.Run, broadcaster seasonalEventBroadcaster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if broadcaster == nil {
		return errors.New("seasonal event broadcaster is unavailable")
	}
	var payload scheduledVietnamMRPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("event.vietnam-mr payload is invalid")}
	}
	_, err := broadcaster.BroadcastVietnamMR(vsro188.VietnamMRUpdate{ElapsedSeconds: payload.ElapsedSeconds, StartHour: payload.StartHour, EndHour: payload.EndHour})
	if err != nil {
		return permanentSchedulerHandlerError{err: err}
	}
	return nil
}

type scheduledServerEventNotifyPayload struct {
	Records []vsro188.ServerEventData `json:"records"`
}

func handleScheduledServerEventNotify(ctx context.Context, run scheduler.Run, broadcaster seasonalEventBroadcaster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if broadcaster == nil {
		return errors.New("seasonal event broadcaster is unavailable")
	}
	var payload scheduledServerEventNotifyPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("event.server-data payload is invalid")}
	}
	if len(payload.Records) == 0 || len(payload.Records) > 10000 {
		return permanentSchedulerHandlerError{err: errors.New("event.server-data requires 1-10000 records")}
	}
	_, err := broadcaster.BroadcastServerEventData(payload.Records)
	return err
}

type scheduledServerEventPayload struct {
	InstanceKey   string `json:"instanceKey"`
	ServerEventID int    `json:"serverEventId"`
	CharacterID   int    `json:"characterId"`
	Delta         uint64 `json:"delta"`
	Target        uint64 `json:"target"`
	Notice        string `json:"notice,omitempty"`
}

func handleScheduledServerEventAdvance(ctx context.Context, run scheduler.Run, controller scheduledServerEventController, broadcaster seasonalEventBroadcaster) error {
	if controller == nil {
		return errors.New("server event controller is unavailable")
	}
	if broadcaster == nil {
		return errors.New("seasonal event broadcaster is unavailable")
	}
	var payload scheduledServerEventPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("server-event.advance payload is invalid")}
	}
	payload.InstanceKey = strings.TrimSpace(payload.InstanceKey)
	if !validScheduledEventID(payload.InstanceKey) || payload.ServerEventID <= 0 || payload.CharacterID <= 0 || payload.Delta == 0 || payload.Target == 0 || payload.Target > math.MaxUint32 {
		return permanentSchedulerHandlerError{err: errors.New("server-event.advance requires instanceKey, event/character IDs, delta, and target")}
	}
	commandKey := fmt.Sprintf("schedule:%s:%d", run.ScheduleID, run.ScheduledFor.UTC().UnixNano())
	result, err := controller.Advance(ctx, service.ServerEventProgressCommand{InstanceKey: payload.InstanceKey, ServerEventID: payload.ServerEventID, CharacterID: payload.CharacterID, Delta: payload.Delta, Target: payload.Target, CommandKey: commandKey})
	if err != nil {
		return err
	}
	if result.Progress > math.MaxUint32 || result.Target > math.MaxUint32 {
		return errors.New("server event progress exceeds the vSRO packet range")
	}
	if _, err = broadcaster.BroadcastGlobalEvent(payload.InstanceKey, uint32(result.Progress)); err != nil {
		return fmt.Errorf("broadcast global event progress: %w", err)
	}
	_, err = broadcaster.BroadcastServerEventData([]vsro188.ServerEventData{{
		ServerEventID: uint32(payload.ServerEventID),
		Value:         uint32(result.Progress),
		Target:        uint32(result.Target),
	}})
	if err != nil {
		return fmt.Errorf("broadcast server event progress: %w", err)
	}
	return broadcastScheduledEventNotice(broadcaster, run.Phase, payload.InstanceKey, payload.Notice)
}

type scheduledMobRatesPayload struct {
	EventID               string `json:"eventId"`
	MobCountRatePercent   uint32 `json:"mobCountRatePercent"`
	MobRespawnRatePercent uint32 `json:"mobRespawnRatePercent"`
	LootDropRatePercent   uint32 `json:"lootDropRatePercent"`
}

func handleScheduledMobRatesStart(ctx context.Context, run scheduler.Run, controller scheduledGameplayRateController) error {
	if controller == nil {
		return errors.New("gameplay rate controller is unavailable")
	}
	var payload scheduledMobRatesPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("rates.mob.start payload is invalid")}
	}
	payload.EventID = strings.TrimSpace(payload.EventID)
	if !validScheduledEventID(payload.EventID) || !validScheduledRate(payload.MobCountRatePercent) || !validScheduledRate(payload.MobRespawnRatePercent) || !validScheduledRate(payload.LootDropRatePercent) {
		return permanentSchedulerHandlerError{err: errors.New("rates.mob.start requires eventId and rates between 1 and 1000000 percent")}
	}
	return controller.ActivateMobOverride(ctx, payload.EventID, model.GameplayMobRateOverride{MobCountRatePercent: payload.MobCountRatePercent, MobRespawnRatePercent: payload.MobRespawnRatePercent, LootDropRatePercent: payload.LootDropRatePercent})
}

func handleScheduledMobRatesFinish(ctx context.Context, run scheduler.Run, controller scheduledGameplayRateController) error {
	if controller == nil {
		return errors.New("gameplay rate controller is unavailable")
	}
	var payload scheduledExperienceRatesFinishPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("rates.mob.finish payload is invalid")}
	}
	payload.EventID = strings.TrimSpace(payload.EventID)
	if !validScheduledEventID(payload.EventID) {
		return permanentSchedulerHandlerError{err: errors.New("rates.mob.finish requires eventId")}
	}
	return controller.DeactivateMobOverride(ctx, payload.EventID)
}

type scheduledExperienceRatesPayload struct {
	EventID                    string `json:"eventId"`
	ExperienceRatePercent      uint32 `json:"experienceRatePercent"`
	PartyExperienceRatePercent uint32 `json:"partyExperienceRatePercent"`
	SkillExperienceRatePercent uint32 `json:"skillExperienceRatePercent"`
}

type scheduledExperienceRatesFinishPayload struct {
	EventID string `json:"eventId"`
}

func validScheduledEventID(value string) bool { return value != "" && len(value) <= 128 }
func validScheduledRate(value uint32) bool    { return value > 0 && value <= 1000000 }

func handleScheduledExperienceRatesStart(ctx context.Context, run scheduler.Run, controller scheduledGameplayRateController) error {
	if controller == nil {
		return errors.New("gameplay rate controller is unavailable")
	}
	var payload scheduledExperienceRatesPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("rates.experience.start payload is invalid")}
	}
	payload.EventID = strings.TrimSpace(payload.EventID)
	if !validScheduledEventID(payload.EventID) || !validScheduledRate(payload.ExperienceRatePercent) || !validScheduledRate(payload.PartyExperienceRatePercent) || !validScheduledRate(payload.SkillExperienceRatePercent) {
		return permanentSchedulerHandlerError{err: errors.New("rates.experience.start requires eventId and rates between 1 and 1000000 percent")}
	}
	return controller.ActivateOverride(ctx, payload.EventID, model.GameplayRateOverride{ExperienceRatePercent: payload.ExperienceRatePercent, PartyExperienceRatePercent: payload.PartyExperienceRatePercent, SkillExperienceRatePercent: payload.SkillExperienceRatePercent})
}

func handleScheduledExperienceRatesFinish(ctx context.Context, run scheduler.Run, controller scheduledGameplayRateController) error {
	if controller == nil {
		return errors.New("gameplay rate controller is unavailable")
	}
	var payload scheduledExperienceRatesFinishPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("rates.experience.finish payload is invalid")}
	}
	payload.EventID = strings.TrimSpace(payload.EventID)
	if !validScheduledEventID(payload.EventID) {
		return permanentSchedulerHandlerError{err: errors.New("rates.experience.finish requires eventId")}
	}
	return controller.DeactivateOverride(ctx, payload.EventID)
}

type scheduledUniqueSpawnPayload struct {
	CodeName string  `json:"codeName"`
	RegionID int16   `json:"regionId"`
	Radius   float32 `json:"radius,omitempty"`
}

func handleScheduledUniqueSpawn(ctx context.Context, run scheduler.Run, spawner scheduledUniqueSpawner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if spawner == nil {
		return errors.New("scheduled unique spawner is unavailable")
	}
	var payload scheduledUniqueSpawnPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("unique.spawn payload is invalid")}
	}
	payload.CodeName = strings.TrimSpace(payload.CodeName)
	if payload.CodeName == "" || len(payload.CodeName) > 128 || payload.RegionID == 0 || payload.Radius < 0 || payload.Radius > 1000 {
		return permanentSchedulerHandlerError{err: errors.New("unique.spawn requires codeName, non-zero regionId, and radius between 0 and 1000")}
	}
	npc, err := spawner.SpawnScheduledUnique(payload.CodeName, payload.RegionID, payload.Radius)
	if err != nil {
		return permanentSchedulerHandlerError{err: fmt.Errorf("spawn scheduled unique: %w", err)}
	}
	if npc == nil {
		return errors.New("scheduled unique spawn returned no NPC")
	}
	log.WithFields(log.Fields{"schedule_id": run.ScheduleID, "code_name": payload.CodeName, "region_id": payload.RegionID, "unique_id": npc.GetUniqueID()}).Info("scheduled unique spawned")
	return nil
}

func handleSchedulerLog(ctx context.Context, run scheduler.Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var payload schedulerLogPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("agent.log payload must be an object containing message")}
	}
	payload.Message = strings.TrimSpace(payload.Message)
	if payload.Message == "" || len(payload.Message) > 512 {
		return permanentSchedulerHandlerError{err: errors.New("agent.log message must contain 1-512 characters")}
	}
	log.WithFields(log.Fields{
		"schedule_id":   run.ScheduleID,
		"scheduled_for": run.ScheduledFor.UTC(),
		"attempt":       run.Attempt,
	}).Info(payload.Message)
	return nil
}

func handleSchedulerNotice(ctx context.Context, run scheduler.Run, notices noticeBroadcaster) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if notices == nil {
		return errors.New("server notice broadcaster is unavailable")
	}
	var payload schedulerLogPayload
	if err := json.Unmarshal(run.Payload, &payload); err != nil {
		return permanentSchedulerHandlerError{err: errors.New("server.notice payload must be an object containing message")}
	}
	payload.Message = strings.TrimSpace(payload.Message)
	if payload.Message == "" || len([]rune(payload.Message)) > 200 {
		return permanentSchedulerHandlerError{err: errors.New("server.notice message must contain 1-200 characters")}
	}
	recipients, err := notices.BroadcastNotice(payload.Message)
	if err != nil {
		return fmt.Errorf("broadcast scheduled server notice: %w", err)
	}
	log.WithFields(log.Fields{
		"schedule_id": run.ScheduleID,
		"recipients":  recipients,
	}).Info("scheduled server notice broadcast")
	return nil
}

type permanentSchedulerHandlerError struct{ err error }

func (err permanentSchedulerHandlerError) Error() string { return err.err.Error() }
func (err permanentSchedulerHandlerError) Unwrap() error { return err.err }
func (permanentSchedulerHandlerError) Permanent() bool   { return true }
