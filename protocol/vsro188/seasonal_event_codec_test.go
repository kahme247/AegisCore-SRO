package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestSeasonalEventLayoutsMatchClientParsers(t *testing.T) {
	global, err := BuildGlobalEventUpdate("summer", 7)
	if err != nil || hex.EncodeToString(global.Payload()) != "060073756d6d657207000000" {
		t.Fatalf("global = %x, %v", global.Payload(), err)
	}
	request := network.EmptyClientPacket()
	if err := DecodeChristmasEventRequest(&request); err != nil {
		t.Fatalf("Christmas request: %v", err)
	}
	success, failure := BuildChristmasEventSuccess(), BuildChristmasEventFailure(3)
	if hex.EncodeToString(success.Payload()) != "01" || hex.EncodeToString(failure.Payload()) != "0203" {
		t.Fatalf("Christmas = %x/%x", success.Payload(), failure.Payload())
	}
	vietnam, err := BuildVietnamMRSystemUpdate(VietnamMRUpdate{ElapsedSeconds: 3600, StartHour: 1, EndHour: 2})
	if err != nil || hex.EncodeToString(vietnam.Payload()) != "100e0102" {
		t.Fatalf("Vietnam = %x, %v", vietnam.Payload(), err)
	}
	event := BuildServerEventDataSnapshot([]ServerEventData{{ServerEventID: 1, Value: 2, Target: 3}})
	if hex.EncodeToString(event.Payload()) != "020301000000010000000200000003000000" {
		t.Fatalf("server event = %x", event.Payload())
	}
	rewards, err := BuildServerEventTimedRewards([]ServerEventTimedReward{{RewardID: 4, ServerEventID: 1, RewardClass: 1, ReferenceValue1: 5, ReferenceValue2: 6, ReferenceValue3: 7, Remaining: 8}})
	if err != nil || hex.EncodeToString(rewards.Payload()) != "070100000004000000010000000105000000060000000700000008000000" {
		t.Fatalf("server rewards = %x, %v", rewards.Payload(), err)
	}
	completed, err := BuildServerEventRewardCompleted(1, 4)
	if err != nil || hex.EncodeToString(completed.Payload()) != "080100000004000000" {
		t.Fatalf("server completion = %x, %v", completed.Payload(), err)
	}
}
