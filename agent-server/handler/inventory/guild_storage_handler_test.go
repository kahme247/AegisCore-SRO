package inventory

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func guildPacketPayload(packet network.Packet) []byte { return packet.Payload() }

func TestGuildStorageLifecyclePacketsMatchVSRO188(t *testing.T) {
	if got := hex.EncodeToString(guildPacketPayload(buildGuildStorageOpenSuccess())); got != "01" {
		t.Fatalf("open success = %s", got)
	}
	if got := hex.EncodeToString(guildPacketPayload(buildGuildStorageOpenFailure(0x48))); got != "02484c" {
		t.Fatalf("open busy = %s", got)
	}
	if got := hex.EncodeToString(guildPacketPayload(buildGuildStorageCloseSuccess())); got != "01" {
		t.Fatalf("close success = %s", got)
	}

	responses := buildGuildStorageResponses(model.StorageSnapshot{Capacity: 150, Gold: 99, Items: map[byte]model.Item{}})
	if len(responses) != 3 {
		t.Fatalf("response count = %d, want 3", len(responses))
	}
	if responses[0].MessageID != opcode.GuildStorageDataBegin || !reflect.DeepEqual(responses[0].Payload(), []byte{99, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("begin = 0x%04X %x", responses[0].MessageID, responses[0].Payload())
	}
	if responses[1].MessageID != opcode.GuildStorageData || !reflect.DeepEqual(responses[1].Payload(), []byte{150, 0}) {
		t.Fatalf("data = 0x%04X %x", responses[1].MessageID, responses[1].Payload())
	}
	if responses[2].MessageID != opcode.GuildStorageDataEnd || len(responses[2].Payload()) != 0 {
		t.Fatalf("end = 0x%04X %x", responses[2].MessageID, responses[2].Payload())
	}
}

func TestGuildStorageOperationSuccessLayouts(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"move", hex.EncodeToString(guildPacketPayload(buildGuildStorageMoveSuccess(GuildStorageMoveOperation, 1, 2, 3))), "011d01020300"},
		{"deposit", hex.EncodeToString(guildPacketPayload(buildGuildStorageTransferSuccess(GuildStorageDepositItemOperation, 13, 0))), "011e0d00"},
		{"withdraw", hex.EncodeToString(guildPacketPayload(buildGuildStorageTransferSuccess(GuildStorageWithdrawItemOperation, 0, 13))), "011f000d"},
		{"gold deposit", hex.EncodeToString(guildPacketPayload(buildGuildStorageGoldSuccess(GuildStorageDepositGoldOperation, 5))), "01200500000000000000"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %s, want %s", test.name, test.got, test.want)
		}
	}
}
