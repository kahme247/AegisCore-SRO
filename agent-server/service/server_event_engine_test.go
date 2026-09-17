package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Harvi8/AegisCore/framework/aggregate"
)

type replayServerEventCoordinator struct {
	command aggregate.Command
	payload json.RawMessage
}

func (coordinator *replayServerEventCoordinator) ExecuteLatest(_ context.Context, command aggregate.Command, _ aggregate.Mutation) (aggregate.Result, error) {
	coordinator.command = command
	return aggregate.Result{Version: 4, Payload: coordinator.payload, Duplicate: true}, nil
}

func TestServerEventAdvanceReplaysAggregateResultWithoutDuplicatingGrants(t *testing.T) {
	payload, _ := json.Marshal(ServerEventProgressResult{InstanceID: 9, Progress: 10, Target: 10, Completed: true, GrantCount: 3})
	coordinator := &replayServerEventCoordinator{payload: payload}
	engine, err := NewServerEventEngine(coordinator)
	if err != nil {
		t.Fatalf("NewServerEventEngine() error = %v", err)
	}
	result, err := engine.Advance(context.Background(), ServerEventProgressCommand{InstanceKey: "summer-2026", ServerEventID: 12, CharacterID: 77, Delta: 5, Target: 10, CommandKey: "run:12:2"})
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if !result.Duplicate || !result.Completed || result.GrantCount != 3 {
		t.Fatalf("result = %+v", result)
	}
	if coordinator.command.Aggregate != (aggregate.ID{Kind: aggregate.Event, Value: "summer-2026"}) || coordinator.command.IdempotencyKey != "run:12:2" {
		t.Fatalf("aggregate command = %+v", coordinator.command)
	}
}

func TestServerEventAdvanceRejectsInvalidIdentityAndProgress(t *testing.T) {
	engine, _ := NewServerEventEngine(&replayServerEventCoordinator{})
	for _, command := range []ServerEventProgressCommand{
		{},
		{InstanceKey: "bad key", ServerEventID: 1, CharacterID: 1, Delta: 1, Target: 1, CommandKey: "valid"},
		{InstanceKey: "valid", ServerEventID: 1, CharacterID: 1, Target: 1, CommandKey: "valid"},
	} {
		if _, err := engine.Advance(context.Background(), command); err != ErrServerEventInvalid {
			t.Fatalf("Advance(%+v) error = %v", command, err)
		}
	}
}
