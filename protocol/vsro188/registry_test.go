package vsro188

import (
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
	protocolregistry "github.com/Harvi8/AegisCore/protocol/registry"
)

func TestProtocolRegistryDrivesGatewayAdmissionSurface(t *testing.T) {
	want := []uint16{
		opcode.PatchRequest,
		opcode.ShardlistRequest,
		opcode.LoginRequest,
		opcode.NoticeRequest,
		opcode.ShardlistPing,
	}
	got := ProtocolRegistry().Opcodes(protocolregistry.ServiceGateway, protocolregistry.ClientToServer, protocolregistry.GatewayPreLogin)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gateway request opcodes = %#v, want %#v", got, want)
	}
}

func TestProtocolRegistryDrivesAgentLifecycleSurfaces(t *testing.T) {
	tests := []struct {
		name  string
		phase protocolregistry.Phase
		want  []uint16
	}{
		{"authentication", protocolregistry.AgentAuthentication, []uint16{opcode.AuthRequest}},
		{"lobby", protocolregistry.AgentLobby, []uint16{opcode.JoinLobbyRequest, opcode.LobbyActionRequest, opcode.LobbyRenameRequest, opcode.AgentPingRequest}},
		{"loading", protocolregistry.AgentCharacterLoading, []uint16{opcode.GameReadyRequest, opcode.TeleportReadyResponse, opcode.AgentPingRequest}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ProtocolRegistry().Opcodes(protocolregistry.ServiceAgent, protocolregistry.ClientToServer, test.phase)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("opcodes = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestImplementedRegistryEntriesDoNotClaimCaptureVerification(t *testing.T) {
	for _, entry := range ProtocolRegistry().Entries() {
		if entry.Status == protocolregistry.StatusVerified {
			t.Fatalf("%s claims verification before a sanitized stock-client corpus exists", entry.CanonicalName)
		}
		if len(entry.Evidence) < 2 {
			t.Fatalf("%s lacks implementation and catalog evidence", entry.CanonicalName)
		}
	}
}

func TestProtocolRegistryContainsTypedInWorldSurface(t *testing.T) {
	wantRequests := []uint16{
		opcode.EntityMovementRequest,
		opcode.EntitySelectRequest,
		opcode.EntityDeselectRequest,
		opcode.CharacterIncreaseStrengthRequest,
		opcode.CharacterIncreaseIntellectRequest,
		opcode.EntityActionRequest,
		opcode.COSCommandRequest,
		opcode.COSTerminationRequest,
		opcode.COSUpdateMountStateRequest,
		opcode.COSUnsummonRequest,
		opcode.AgentPingRequest,
	}
	gotRequests := ProtocolRegistry().Opcodes(protocolregistry.ServiceAgent, protocolregistry.ClientToServer, protocolregistry.AgentInWorld)
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("in-world request opcodes = %#v, want %#v", gotRequests, wantRequests)
	}
	for _, packetOpcode := range []uint16{opcode.MovementPositionUpdate, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd} {
		key := protocolregistry.Key{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: packetOpcode}
		entry, exists := ProtocolRegistry().Lookup(key)
		if !exists || entry.Pair != nil {
			t.Fatalf("outbound notification 0x%04X = %+v, exists=%t", packetOpcode, entry, exists)
		}
	}
}
