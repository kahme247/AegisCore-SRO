package character

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type questTimerDispatchCall struct {
	characterID int
	questID     int
	event       model.QuestAuthoredEvent
}

type questTimerDispatcherStub struct {
	calls []questTimerDispatchCall
	err   error
}

func (d *questTimerDispatcherStub) DispatchQuestEvent(_ context.Context, characterID, questID int, event model.QuestAuthoredEvent) error {
	d.calls = append(d.calls, questTimerDispatchCall{characterID: characterID, questID: questID, event: event})
	return d.err
}

func TestQuestTimerPublisherSchedulesPositiveCountdownAndDispatchesExpiry(t *testing.T) {
	player := &model.Player{ID: 77}
	dispatcher := &questTimerDispatcherStub{}
	var sent []network.Packet
	var callbacks []func()
	publisher := questTimerPublisher{
		dispatcher: dispatcher,
		send: func(packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		schedule: func(delay time.Duration, callback func()) {
			if delay != 25*time.Millisecond {
				t.Fatalf("schedule delay = %v", delay)
			}
			callbacks = append(callbacks, callback)
		},
		tickCadence: 25 * time.Millisecond,
	}
	metadata := questTimerWireMetadata{PackedQuestByte: 0x12, QuestDefinitionByte: 0x34}
	timer, created, err := publisher.start(context.Background(), player, 100, 1, model.QuestTimerModeOne, metadata)
	if err != nil || !created || timer.Event != model.QuestAuthoredCountdown {
		t.Fatalf("start = (%+v, %v, %v)", timer, created, err)
	}
	if len(sent) != 1 || len(callbacks) != 1 {
		t.Fatalf("start sent=%d callbacks=%d", len(sent), len(callbacks))
	}
	wantStart := []byte{2, 100, 0, 0, 0, 0x12, 0x34, 0x04, 0, 0, 0x10, 0}
	if sent[0].MessageID != opcode.QuestUpdate || !reflect.DeepEqual(sent[0].Payload(), wantStart) {
		t.Fatalf("start packet = % X, want % X", sent[0].Payload(), wantStart)
	}

	callbacks[0]()
	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != (questTimerDispatchCall{characterID: 77, questID: 100, event: model.QuestAuthoredCountdown}) {
		t.Fatalf("dispatch calls = %#v", dispatcher.calls)
	}
	if len(sent) != 2 || len(callbacks) != 1 {
		t.Fatalf("expiry sent=%d callbacks=%d", len(sent), len(callbacks))
	}
	wantExpiry := []byte{2, 100, 0, 0, 0, 0x12, 0x34, 0x04, 0, 0, 0, 0}
	if !reflect.DeepEqual(sent[1].Payload(), wantExpiry) {
		t.Fatalf("expiry packet = % X, want % X", sent[1].Payload(), wantExpiry)
	}
}

func TestQuestTimerPublisherKeepsZeroModesEventDriven(t *testing.T) {
	player := &model.Player{ID: 88}
	dispatcher := &questTimerDispatcherStub{}
	var sent []network.Packet
	var scheduled int
	publisher := questTimerPublisher{
		dispatcher: dispatcher,
		send:       func(packet network.Packet) error { sent = append(sent, packet); return nil },
		schedule:   func(time.Duration, func()) { scheduled++ }, tickCadence: time.Second,
	}
	timer, created, err := publisher.start(context.Background(), player, 101, 0, model.QuestTimerModeTwo, questTimerWireMetadata{})
	if err != nil || !created {
		t.Fatalf("start = (%+v, %v, %v)", timer, created, err)
	}
	if len(sent) != 0 || scheduled != 0 {
		t.Fatalf("mode two sent=%d scheduled=%d", len(sent), scheduled)
	}
	if err = publisher.trigger(context.Background(), player, 101, timer.Generation, questTimerWireMetadata{}); err != nil {
		t.Fatal(err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].event != model.QuestAuthoredMode2Timer {
		t.Fatalf("dispatch calls = %#v", dispatcher.calls)
	}
}

func TestQuestTimerPublisherCancellationUsesSentinelAndSendOrdering(t *testing.T) {
	player := &model.Player{ID: 99}
	timer, _, err := player.StartQuestTimer(102, 20, model.QuestTimerModeOne)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("send failed")
	publisher := questTimerPublisher{send: func(network.Packet) error { return want }}
	if err = publisher.cancel(player, 102, questTimerWireMetadata{}); !errors.Is(err, want) {
		t.Fatalf("cancel error = %v", err)
	}
	if current, exists := player.QuestTimerSnapshot(102); !exists || current.Generation != timer.Generation {
		t.Fatalf("timer after failed send = (%+v, %v)", current, exists)
	}

	var sent network.Packet
	publisher.send = func(packet network.Packet) error { sent = packet; return nil }
	if err = publisher.cancel(player, 102, questTimerWireMetadata{}); err != nil {
		t.Fatal(err)
	}
	if _, exists := player.QuestTimerSnapshot(102); exists {
		t.Fatal("timer remains after successful cancellation")
	}
	wantPayload := []byte{2, 102, 0, 0, 0, 0, 0, 0x04, 0xff, 0xff, 0xff, 0xff}
	if !reflect.DeepEqual(sent.Payload(), wantPayload) {
		t.Fatalf("cancel packet = % X, want % X", sent.Payload(), wantPayload)
	}
}

func TestQuestTimerPublisherRollsBackFailedInitialDisplay(t *testing.T) {
	player := &model.Player{ID: 1}
	want := errors.New("send failed")
	publisher := questTimerPublisher{
		dispatcher: &questTimerDispatcherStub{}, send: func(network.Packet) error { return want },
		schedule: func(time.Duration, func()) {}, tickCadence: time.Second,
	}
	if _, created, err := publisher.start(context.Background(), player, 103, 10, model.QuestTimerModeOne, questTimerWireMetadata{}); !errors.Is(err, want) || created {
		t.Fatalf("start = (created=%v, err=%v)", created, err)
	}
	if _, exists := player.QuestTimerSnapshot(103); exists {
		t.Fatal("failed initial display left timer registered")
	}
}
