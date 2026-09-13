package character

import (
	"context"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

// questTimerWireMetadata contains the two 0x30D5 definition bytes. They are
// authored quest metadata and must be supplied by the quest catalog rather
// than inferred from the database quest ID.
type questTimerWireMetadata struct {
	PackedQuestByte     byte
	QuestDefinitionByte byte
}

// questTimerPublisher owns timer display and callback ordering. The authored
// duration is seconds: GameServer presents duration/60 and duration%60 as the
// minute/second pair and persists one decrement per event-0x0E callback.
type questTimerPublisher struct {
	dispatcher  model.QuestEventDispatcher
	send        func(network.Packet) error
	schedule    func(time.Duration, func())
	tickCadence time.Duration
	onError     func(error)
	isActive    func(*model.Player, int) bool
	checkpoint  func(context.Context, model.QuestTimer) error
}

func (p questTimerPublisher) start(ctx context.Context, player *model.Player, questID int, duration uint32, mode byte, metadata questTimerWireMetadata) (model.QuestTimer, bool, error) {
	if player == nil || player.ID <= 0 || p.send == nil {
		return model.QuestTimer{}, false, fmt.Errorf("quest timer publisher is unavailable")
	}
	if duration > 0 && (p.schedule == nil || p.tickCadence <= 0 || p.dispatcher == nil) {
		return model.QuestTimer{}, false, fmt.Errorf("quest countdown scheduler is unavailable")
	}
	timer, created, err := player.StartQuestTimer(questID, duration, mode)
	if err != nil || !created {
		return timer, created, err
	}

	// Mode 2 only registers authored event 0x18. Unlike positive durations and
	// zero-valued modes 1/4, the recovered setup path does not send a display.
	if timer.Event != model.QuestAuthoredMode2Timer {
		if err = p.publish(timer, timer.PackedValue, metadata); err != nil {
			player.CancelQuestTimer(questID)
			return model.QuestTimer{}, false, err
		}
	}
	if timer.Remaining > 0 {
		callbackContext := context.WithoutCancel(ctx)
		p.schedule(p.tickCadence, func() { p.tick(callbackContext, player, timer, metadata) })
	}
	return timer, true, nil
}

// trigger runs an authored zero-duration event when the quest event manager
// fires it. No wall-clock schedule is assigned to modes 1/2/4 here.
func (p questTimerPublisher) trigger(ctx context.Context, player *model.Player, questID int, generation uint64, metadata questTimerWireMetadata) error {
	return p.processTick(ctx, player, questID, generation, metadata, false)
}

func (p questTimerPublisher) cancel(player *model.Player, questID int, metadata questTimerWireMetadata) error {
	if player == nil || p.send == nil {
		return fmt.Errorf("quest timer publisher is unavailable")
	}
	timer, exists := player.QuestTimerSnapshot(questID)
	if !exists {
		return fmt.Errorf("quest timer %d is not registered", questID)
	}
	if err := p.publish(timer, model.QuestTimerInactive, metadata); err != nil {
		return err
	}
	if _, exists = player.CancelQuestTimer(questID); !exists {
		return fmt.Errorf("quest timer %d disappeared during cancellation", questID)
	}
	return nil
}

func (p questTimerPublisher) tick(ctx context.Context, player *model.Player, timer model.QuestTimer, metadata questTimerWireMetadata) {
	if err := p.processTick(ctx, player, timer.QuestID, timer.Generation, metadata, true); err != nil {
		if p.onError != nil {
			p.onError(err)
		}
		if current, active := player.QuestTimerSnapshot(timer.QuestID); active && current.Generation == timer.Generation && p.schedule != nil {
			p.schedule(p.tickCadence, func() { p.tick(ctx, player, current, metadata) })
		}
	}
}

func (p questTimerPublisher) processTick(ctx context.Context, player *model.Player, questID int, generation uint64, metadata questTimerWireMetadata, reschedule bool) error {
	if player == nil || p.dispatcher == nil {
		return fmt.Errorf("quest timer event dispatcher is unavailable")
	}
	if p.isActive != nil && !p.isActive(player, questID) {
		player.CancelQuestTimer(questID)
		return nil
	}
	tick, exists := player.TickQuestTimer(questID, generation)
	if !exists {
		return nil // stale scheduled callback or an already-cancelled timer
	}
	if tick.DispatchEvent {
		if err := p.dispatcher.DispatchQuestEvent(ctx, player.ID, questID, tick.Timer.Event); err != nil {
			return fmt.Errorf("dispatch quest %d timer event 0x%02X: %w", questID, tick.Timer.Event, err)
		}
	}
	if tick.Timer.Remaining > 0 && p.checkpoint != nil {
		if err := p.checkpoint(ctx, tick.Timer); err != nil {
			return fmt.Errorf("checkpoint quest %d countdown: %w", questID, err)
		}
	}
	if tick.DisplayUpdate {
		if err := p.publish(tick.Timer, tick.Timer.PackedValue, metadata); err != nil {
			return err
		}
	}
	if reschedule {
		if current, active := player.QuestTimerSnapshot(questID); active && current.Generation == generation {
			p.schedule(p.tickCadence, func() { p.tick(ctx, player, current, metadata) })
		}
	}
	return nil
}

func (p questTimerPublisher) publish(timer model.QuestTimer, packed uint32, metadata questTimerWireMetadata) error {
	packet, err := vsro188.BuildQuestUpdate(vsro188.QuestUpdateMessage{
		Subtype:             vsro188.QuestUpdateDelta,
		QuestID:             uint32(timer.QuestID),
		PackedQuestByte:     metadata.PackedQuestByte,
		QuestDefinitionByte: metadata.QuestDefinitionByte,
		Flags:               vsro188.QuestDeltaTimerFlag,
		Timer:               packed,
	})
	if err != nil {
		return err
	}
	return p.send(packet)
}
