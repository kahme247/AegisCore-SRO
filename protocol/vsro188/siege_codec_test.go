package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestSiegeApplicationLayoutsMatchGameServer(t *testing.T) {
	applyPacket := siegeClientPacket(SiegeActionApplicationApply, SiegeRequestAttack)
	apply, err := DecodeSiegeApplicationRequest(&applyPacket)
	if err != nil || apply.Action != SiegeActionApplicationApply || apply.RequestType != SiegeRequestAttack {
		t.Fatalf("DecodeSiegeApplicationRequest(apply) = %+v, %v", apply, err)
	}
	cancelPacket := siegeClientPacket(SiegeActionApplicationCancel)
	cancel, err := DecodeSiegeApplicationRequest(&cancelPacket)
	if err != nil || cancel.Action != SiegeActionApplicationCancel || cancel.RequestType != 0 {
		t.Fatalf("DecodeSiegeApplicationRequest(cancel) = %+v, %v", cancel, err)
	}

	failure := BuildSiegeActionFailure(SiegeActionApplicationApply, 10255)
	if failure.MessageID != opcode.SiegeActionResponse || hex.EncodeToString(failure.Payload()) != "02070f28" {
		t.Fatalf("failure = %04X %x", failure.MessageID, failure.Payload())
	}
	update, err := BuildSiegeApplicationUpdate(0x11223344, SiegeRequestDefend, false)
	if err != nil || update.MessageID != opcode.SiegeUpdate || hex.EncodeToString(update.Payload()) != "0c4433221102" {
		t.Fatalf("update = %04X %x, %v", update.MessageID, update.Payload(), err)
	}
	cancelUpdate, err := BuildSiegeApplicationUpdate(0x11223344, SiegeRequestAttack, true)
	if err != nil || hex.EncodeToString(cancelUpdate.Payload()) != "0d4433221101" {
		t.Fatalf("cancel update = %x, %v", cancelUpdate.Payload(), err)
	}
}

func TestSiegeApplicationDecoderFailsClosed(t *testing.T) {
	for _, payload := range [][]byte{{}, {6}, {7}, {7, 0}, {7, 1, 0}, {8, 0}} {
		packet := siegeClientPacket(payload...)
		if _, err := DecodeSiegeApplicationRequest(&packet); err == nil {
			t.Fatalf("accepted payload %x", payload)
		}
	}
	if _, err := BuildSiegeApplicationUpdate(0, SiegeRequestAttack, false); err == nil {
		t.Fatal("accepted zero fortress ID")
	}
}

func TestSiegeBattleUpdateLayouts(t *testing.T) {
	period, err := BuildSiegePeriodUpdate(SiegeUpdateBattleBegin)
	if err != nil || hex.EncodeToString(period.Payload()) != "02" {
		t.Fatalf("period = %x, %v", period.Payload(), err)
	}
	structure, err := BuildSiegeStructureStateUpdate(1, 2, 3, 1, true, "Guild")
	if err != nil || hex.EncodeToString(structure.Payload()) != "0b010000000200000003000000010005004775696c64" {
		t.Fatalf("structure = %x, %v", structure.Payload(), err)
	}
	record, err := BuildSiegeBattleRecordUpdate(1, 2, 3)
	if err != nil || hex.EncodeToString(record.Payload()) != "11010000000200000003000000" {
		t.Fatalf("record = %x, %v", record.Payload(), err)
	}
	occupied, err := BuildSiegeOccupiedUpdate(SiegeOccupation{FortressID: 1, GuildName: "G", GuildMasterName: "M", Introduction: "I", GuildID: 2, GuildCrest: 3, UnionID: 4, UnionCrest: 5})
	if err != nil || hex.EncodeToString(occupied.Payload()) != "080100000001004701004d01004902000000030000000400000005000000" {
		t.Fatalf("occupied = %x, %v", occupied.Payload(), err)
	}
}

func siegeClientPacket(payload ...byte) network.Packet {
	packet := network.EmptyClientPacket()
	for _, value := range payload {
		packet.WriteByte(value)
	}
	return packet
}
