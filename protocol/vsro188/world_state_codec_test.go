package vsro188

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestWorldStateLayoutsMatchClientParsers(t *testing.T) {
	state := BuildCharacterStateUpdate(7)
	if state.MessageID != opcode.CharacterStateUpdate || hex.EncodeToString(state.Payload()) != "07" {
		t.Fatalf("state = %04x/%x", state.MessageID, state.Payload())
	}
	celestial, err := BuildEnvironmentCelestialUpdate(CelestialUpdate{MoonPhase: 15, Hour: 23, Minute: 59})
	if err != nil || hex.EncodeToString(celestial.Payload()) != "0f00173b" {
		t.Fatalf("celestial = %x, %v", celestial.Payload(), err)
	}
	serverTime := BuildGameServerTime(time.Date(2025, 7, 12, 13, 14, 15, 0, time.Local))
	if hex.EncodeToString(serverTime.Payload()) != "d9b1e63c" {
		t.Fatalf("server time = %x", serverTime.Payload())
	}
	notification, err := BuildGameNotification(0x0c05, []byte{1, 2})
	if err != nil || hex.EncodeToString(notification.Payload()) != "050c0102" {
		t.Fatalf("notification = %x, %v", notification.Payload(), err)
	}
	if reset := BuildClientReset(); reset.MessageID != opcode.ClientReset || len(reset.Payload()) != 0 {
		t.Fatalf("reset = %04x/%x", reset.MessageID, reset.Payload())
	}
	success := BuildCharacterMoveStateSuccess()
	failure := BuildCharacterMoveStateFailure(0x1234)
	if hex.EncodeToString(success.Payload()) != "01" || hex.EncodeToString(failure.Payload()) != "023412" {
		t.Fatalf("move state = %x/%x", success.Payload(), failure.Payload())
	}
}

func TestCharacterMoveStateRequestMatchesClientBuilder(t *testing.T) {
	for _, state := range []byte{CharacterMoveStateWalk, CharacterMoveStateRun, CharacterMoveStateSit} {
		packet := network.EmptyClientPacket()
		packet.WriteByte(state)
		if got, err := DecodeCharacterMoveStateRequest(&packet); err != nil || got != state {
			t.Fatalf("state %d = %d, %v", state, got, err)
		}
	}
	bad := network.EmptyClientPacket()
	bad.WriteByte(1)
	if _, err := DecodeCharacterMoveStateRequest(&bad); err == nil {
		t.Fatal("expected invalid move state")
	}
}

func TestCelestialUpdateRejectsInvalidClock(t *testing.T) {
	if _, err := BuildEnvironmentCelestialUpdate(CelestialUpdate{MoonPhase: 31}); err == nil {
		t.Fatal("expected invalid moon phase")
	}
	if _, err := BuildEnvironmentCelestialUpdate(CelestialUpdate{Hour: 24}); err == nil {
		t.Fatal("expected invalid hour")
	}
}
