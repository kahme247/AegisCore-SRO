package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/scheduler"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type schedulerNoticeRecorder struct {
	message     string
	recipients  int
	err         error
	globalName  string
	globalValue uint32
	vietnam     vsro188.VietnamMRUpdate
	serverData  []vsro188.ServerEventData
	packet      network.Packet
}

func (recorder *schedulerNoticeRecorder) Broadcast(packet network.Packet) { recorder.packet = packet }

type schedulerFortressRecorder struct {
	fortressID int
	period     model.FortressPeriod
	startsAt   time.Time
	endsAt     time.Time
	finished   bool
	structures []model.FortressStructure
}

func (r *schedulerFortressRecorder) RegisterFortressStructure(_ context.Context, structure model.FortressStructure) error {
	r.structures = append(r.structures, structure)
	return nil
}

func (r *schedulerFortressRecorder) SetFortressPeriod(_ context.Context, fortressID int, period model.FortressPeriod, startsAt, endsAt time.Time) (model.FortressState, error) {
	r.fortressID, r.period, r.startsAt, r.endsAt = fortressID, period, startsAt, endsAt
	return model.FortressState{FortressID: fortressID, Period: period}, nil
}

func (r *schedulerFortressRecorder) FinishFortressBattle(_ context.Context, fortressID int, at time.Time) (model.FortressState, error) {
	r.fortressID, r.startsAt, r.finished = fortressID, at, true
	return model.FortressState{FortressID: fortressID}, nil
}

func TestScheduledFortressLifecyclePersistsBeforeBroadcast(t *testing.T) {
	controller := &schedulerFortressRecorder{}
	broadcaster := &schedulerNoticeRecorder{}
	run := scheduler.Run{Payload: []byte(`{"fortressId":3,"action":"battle-start","startsAt":"2026-07-26T10:00:00Z","endsAt":"2026-07-26T12:00:00Z","structures":[{"RefEventID":10,"UniqueID":20,"RefObjectID":30,"Heart":true,"CurrentHP":1000,"MaximumHP":1000}]}`)}
	if err := handleScheduledFortress(context.Background(), run, controller, broadcaster); err != nil {
		t.Fatalf("battle start: %v", err)
	}
	if controller.fortressID != 3 || controller.period != model.FortressPeriodBattle || controller.endsAt.Hour() != 12 {
		t.Fatalf("transition = %+v", controller)
	}
	if len(controller.structures) != 1 || controller.structures[0].FortressID != 3 || !controller.structures[0].Heart {
		t.Fatalf("structures = %+v", controller.structures)
	}
	if broadcaster.packet.MessageID != 0x385F || len(broadcaster.packet.Payload()) != 1 || broadcaster.packet.Payload()[0] != vsro188.SiegeUpdateBattleBegin {
		t.Fatalf("broadcast = %04x/%x", broadcaster.packet.MessageID, broadcaster.packet.Payload())
	}

	run.Payload = []byte(`{"fortressId":3,"action":"battle-finish","startsAt":"2026-07-26T12:00:00Z"}`)
	if err := handleScheduledFortress(context.Background(), run, controller, broadcaster); err != nil || !controller.finished {
		t.Fatalf("battle finish = finished:%t err:%v", controller.finished, err)
	}
	if broadcaster.packet.Payload()[0] != vsro188.SiegeUpdateBattleEnded {
		t.Fatalf("finish broadcast = %x", broadcaster.packet.Payload())
	}
}

type schedulerUniqueSpawnerRecorder struct {
	codeName string
	regionID int16
	radius   float32
	err      error
}

type schedulerRateControllerRecorder struct {
	eventID  string
	rates    model.GameplayRateOverride
	mobRates model.GameplayMobRateOverride
	ended    bool
}

type schedulerServerEventRecorder struct {
	command service.ServerEventProgressCommand
	result  service.ServerEventProgressResult
	err     error
}

func (recorder *schedulerServerEventRecorder) Advance(_ context.Context, command service.ServerEventProgressCommand) (service.ServerEventProgressResult, error) {
	recorder.command = command
	return recorder.result, recorder.err
}

func (recorder *schedulerRateControllerRecorder) ActivateOverride(_ context.Context, eventID string, rates model.GameplayRateOverride) error {
	recorder.eventID, recorder.rates = eventID, rates
	return nil
}

func (recorder *schedulerRateControllerRecorder) DeactivateOverride(_ context.Context, eventID string) error {
	recorder.eventID, recorder.ended = eventID, true
	return nil
}
func (recorder *schedulerRateControllerRecorder) ActivateMobOverride(_ context.Context, eventID string, rates model.GameplayMobRateOverride) error {
	recorder.eventID, recorder.mobRates = eventID, rates
	return nil
}

func TestScheduledMobRateHandlersActivateAndFinishSameEvent(t *testing.T) {
	recorder := &schedulerRateControllerRecorder{}
	err := handleScheduledMobRatesStart(context.Background(), scheduler.Run{Payload: []byte(`{"eventId":"double-drops","mobCountRatePercent":150,"mobRespawnRatePercent":200,"lootDropRatePercent":300}`)}, recorder)
	if err != nil || recorder.eventID != "double-drops" || recorder.mobRates.LootDropRatePercent != 300 {
		t.Fatalf("start result event=%q rates=%#v err=%v", recorder.eventID, recorder.mobRates, err)
	}
	if err := handleScheduledMobRatesFinish(context.Background(), scheduler.Run{Payload: []byte(`{"eventId":"double-drops"}`)}, recorder); err != nil || !recorder.ended {
		t.Fatalf("finish ended=%v err=%v", recorder.ended, err)
	}
}

func TestScheduledMobRateStartRejectsZeroRatePermanently(t *testing.T) {
	err := handleScheduledMobRatesStart(context.Background(), scheduler.Run{Payload: []byte(`{"eventId":"bad","mobCountRatePercent":100,"mobRespawnRatePercent":0,"lootDropRatePercent":100}`)}, &schedulerRateControllerRecorder{})
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("error = %v, want permanent", err)
	}
}
func (recorder *schedulerRateControllerRecorder) DeactivateMobOverride(_ context.Context, eventID string) error {
	recorder.eventID, recorder.ended = eventID, true
	return nil
}

func (spawner *schedulerUniqueSpawnerRecorder) SpawnScheduledUnique(codeName string, regionID int16, radius float32) (*model.NPC, error) {
	spawner.codeName, spawner.regionID, spawner.radius = codeName, regionID, radius
	if spawner.err != nil {
		return nil, spawner.err
	}
	npc := &model.NPC{SRObject: model.NewSRObject()}
	return npc, nil
}

func (recorder *schedulerNoticeRecorder) BroadcastNotice(message string) (int, error) {
	recorder.message = message
	return recorder.recipients, recorder.err
}

func (recorder *schedulerNoticeRecorder) BroadcastGlobalEvent(name string, value uint32) (int, error) {
	recorder.globalName, recorder.globalValue = name, value
	return recorder.recipients, recorder.err
}

func (recorder *schedulerNoticeRecorder) BroadcastVietnamMR(update vsro188.VietnamMRUpdate) (int, error) {
	recorder.vietnam = update
	return recorder.recipients, recorder.err
}

func (recorder *schedulerNoticeRecorder) BroadcastServerEventData(records []vsro188.ServerEventData) (int, error) {
	recorder.serverData = append([]vsro188.ServerEventData(nil), records...)
	return recorder.recipients, recorder.err
}

func TestScheduledSeasonalEventHandlersBroadcastTypedPackets(t *testing.T) {
	recorder := &schedulerNoticeRecorder{recipients: 2}
	if err := handleScheduledGlobalEvent(context.Background(), scheduler.Run{Payload: []byte(`{"name":" summer ","value":7}`)}, recorder); err != nil || recorder.globalName != "summer" || recorder.globalValue != 7 {
		t.Fatalf("global event = %q/%d, %v", recorder.globalName, recorder.globalValue, err)
	}
	if err := handleScheduledVietnamMR(context.Background(), scheduler.Run{Payload: []byte(`{"elapsedSeconds":3600,"startHour":1,"endHour":2}`)}, recorder); err != nil || recorder.vietnam.ElapsedSeconds != 3600 {
		t.Fatalf("Vietnam MR = %+v, %v", recorder.vietnam, err)
	}
	if err := handleScheduledServerEventNotify(context.Background(), scheduler.Run{Payload: []byte(`{"records":[{"ServerEventID":1,"Value":2,"Target":3}]}`)}, recorder); err != nil || len(recorder.serverData) != 1 || recorder.serverData[0].Target != 3 {
		t.Fatalf("server event = %+v, %v", recorder.serverData, err)
	}
}

func TestScheduledServerEventAdvanceBroadcastsCommittedProgress(t *testing.T) {
	controller := &schedulerServerEventRecorder{result: service.ServerEventProgressResult{InstanceID: 9, Progress: 7, Target: 10}}
	broadcaster := &schedulerNoticeRecorder{recipients: 2}
	run := scheduler.Run{ScheduleID: "summer-progress", ScheduledFor: time.Unix(100, 5).UTC(), Payload: []byte(`{"instanceKey":"summer-2026","serverEventId":12,"characterId":77,"delta":2,"target":10}`)}
	if err := handleScheduledServerEventAdvance(context.Background(), run, controller, broadcaster); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if controller.command.InstanceKey != "summer-2026" || controller.command.CommandKey != "schedule:summer-progress:100000000005" {
		t.Fatalf("command = %#v", controller.command)
	}
	if broadcaster.globalName != "summer-2026" || broadcaster.globalValue != 7 {
		t.Fatalf("global progress = %q/%d", broadcaster.globalName, broadcaster.globalValue)
	}
	if len(broadcaster.serverData) != 1 || broadcaster.serverData[0] != (vsro188.ServerEventData{ServerEventID: 12, Value: 7, Target: 10}) {
		t.Fatalf("server progress = %#v", broadcaster.serverData)
	}
}

func TestHandleSchedulerLogAcceptsMessage(t *testing.T) {
	if err := handleSchedulerLog(context.Background(), scheduler.Run{ScheduleID: "test", Payload: []byte(`{"message":"maintenance starts soon"}`)}); err != nil {
		t.Fatalf("handleSchedulerLog() error = %v", err)
	}
}

func TestScheduledExperienceRateHandlersActivateAndFinishSameEvent(t *testing.T) {
	recorder := &schedulerRateControllerRecorder{}
	err := handleScheduledExperienceRatesStart(context.Background(), scheduler.Run{Payload: []byte(`{"eventId":"double-exp","experienceRatePercent":200,"partyExperienceRatePercent":250,"skillExperienceRatePercent":300}`)}, recorder)
	if err != nil || recorder.eventID != "double-exp" || recorder.rates.SkillExperienceRatePercent != 300 {
		t.Fatalf("start result event=%q rates=%#v err=%v", recorder.eventID, recorder.rates, err)
	}
	if err := handleScheduledExperienceRatesFinish(context.Background(), scheduler.Run{Payload: []byte(`{"eventId":"double-exp"}`)}, recorder); err != nil || !recorder.ended {
		t.Fatalf("finish ended=%v err=%v", recorder.ended, err)
	}
}

func TestScheduledExperienceRateStartRejectsZeroRatePermanently(t *testing.T) {
	err := handleScheduledExperienceRatesStart(context.Background(), scheduler.Run{Payload: []byte(`{"eventId":"bad","experienceRatePercent":0,"partyExperienceRatePercent":100,"skillExperienceRatePercent":100}`)}, &schedulerRateControllerRecorder{})
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestHandleSchedulerLogRejectsInvalidPayloadPermanently(t *testing.T) {
	err := handleSchedulerLog(context.Background(), scheduler.Run{Payload: []byte(`{"message":"  "}`)})
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestHandleSchedulerNoticeBroadcastsValidatedMessage(t *testing.T) {
	recorder := &schedulerNoticeRecorder{recipients: 3}
	err := handleSchedulerNotice(context.Background(), scheduler.Run{
		ScheduleID: "maintenance",
		Payload:    []byte(`{"message":"  Maintenance starts in 10 minutes  "}`),
	}, recorder)
	if err != nil {
		t.Fatalf("handleSchedulerNotice() error = %v", err)
	}
	if recorder.message != "Maintenance starts in 10 minutes" {
		t.Fatalf("broadcast message = %q", recorder.message)
	}
}

func TestHandleSchedulerNoticeRejectsInvalidPayloadPermanently(t *testing.T) {
	err := handleSchedulerNotice(context.Background(), scheduler.Run{Payload: []byte(`{"message":""}`)}, &schedulerNoticeRecorder{})
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestHandleSchedulerNoticeReturnsBroadcastFailureForRetry(t *testing.T) {
	want := errors.New("socket unavailable")
	err := handleSchedulerNotice(context.Background(), scheduler.Run{Payload: []byte(`{"message":"hello"}`)}, &schedulerNoticeRecorder{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped broadcast error", err)
	}
	var permanent interface{ Permanent() bool }
	if errors.As(err, &permanent) && permanent.Permanent() {
		t.Fatal("broadcast failure must remain retryable")
	}
}

func TestHandleScheduledUniqueSpawnValidatesAndDispatches(t *testing.T) {
	spawner := &schedulerUniqueSpawnerRecorder{}
	err := handleScheduledUniqueSpawn(context.Background(), scheduler.Run{ScheduleID: "unique-1", Payload: []byte(`{"codeName":"MOB_CH_TIGERWOMAN","regionId":25000,"radius":25}`)}, spawner)
	if err != nil {
		t.Fatalf("handleScheduledUniqueSpawn() error = %v", err)
	}
	if spawner.codeName != "MOB_CH_TIGERWOMAN" || spawner.regionID != 25000 || spawner.radius != 25 {
		t.Fatalf("spawn request = %q %d %f", spawner.codeName, spawner.regionID, spawner.radius)
	}
}

func TestHandleScheduledUniqueSpawnRejectsInvalidPayloadPermanently(t *testing.T) {
	err := handleScheduledUniqueSpawn(context.Background(), scheduler.Run{Payload: []byte(`{"codeName":"","regionId":0}`)}, &schedulerUniqueSpawnerRecorder{})
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("error = %v, want permanent", err)
	}
}
