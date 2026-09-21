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
		{"loading", protocolregistry.AgentCharacterLoading, []uint16{opcode.GameReadyRequest, opcode.TeleportReadyResponse, opcode.LobbyActionRequest, opcode.TeleportCancelRequest, opcode.GuideRequest, opcode.AgentPingRequest}},
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
		opcode.QuestTalk,
		opcode.CommunityFriendResponse,
		opcode.AcademyMatchingResponse,
		opcode.PremiumPause,
		opcode.OperatorRequest,
		opcode.EntityMovementRequest,
		opcode.EntityMovementForwardRequest,
		opcode.EntityMovementTurnRequest,
		opcode.RecallSolveRequest,
		opcode.ItemOperationRequest,
		opcode.StorageOpenRequest,
		opcode.InventoryUnknownRequest,
		opcode.EntitySelectRequest,
		opcode.EntityDeselectRequest,
		opcode.InventoryItemUseRequest,
		opcode.CharacterIncreaseStrengthRequest,
		opcode.CharacterIncreaseIntellectRequest,
		opcode.TeleportDesignateRequest,
		opcode.TeleportUseRequest,
		opcode.SiegeActionRequest,
		opcode.EntityActionRequest,
		opcode.COSCommandRequest,
		opcode.COSTerminationRequest,
		opcode.COSUnknownRequest,
		opcode.COSUpdateMountStateRequest,
		opcode.QuestAbandonRequest,
		opcode.GuildUpdateSiegeAuthorityRequest,
		opcode.GuildElectionRequest,
		opcode.GuildElectionParticipateRequest,
		opcode.GuildElectionVoteRequest,
		opcode.GuildWarRequest,
		opcode.GuildWarEndRequest,
		opcode.GuildWarCompensationClaimRequest,
		opcode.GuildWarRewardRequest,
		opcode.COSUnsummonRequest,
		opcode.COSNameRequest,
		opcode.SilkHistoryRequest,
		opcode.SilkUnknownRequest,
		opcode.AlchemyElixirRequest,
		opcode.AlchemyStoneRequest,
		opcode.AlchemyMaterialRequest,
		opcode.AlchemyDismantleRequest,
		opcode.PremiumDecoUseRequest,
		opcode.AlchemySocketRequest,
		opcode.SkillWithdrawRequest,
		opcode.MasteryWithdrawRequest,
		opcode.GuildMercenaryAttrRequest,
		opcode.GuildMercenaryTerminateRequest,
		opcode.CommunityFriendAddRequest,
		opcode.CommunityFriendDeleteRequest,
		opcode.CommunityMemoOpenRequest,
		opcode.CommunityMemoSendRequest,
		opcode.CommunityMemoDeleteRequest,
		opcode.CommunityMemoListRequest,
		opcode.CommunityMemoGroupSendRequest,
		opcode.CommunityBlockUpdateRequest,
		opcode.COSSettingsRequest,
		opcode.AcademyInviteRequest,
		opcode.AcademyMemberActionRequest,
		opcode.AcademyUnknownOperationRequest,
		opcode.FlagWarRegisterRequest,
		opcode.BattleArenaRequest,
		opcode.GuildGPHistoryRequest,
		opcode.OpenMarketDetailRequest,
		opcode.OpenMarketCloseRequest,
		opcode.OpenMarketRegisterRequest,
		opcode.OpenMarketUnregisterRequest,
		opcode.OpenMarketBuyRequest,
		opcode.OpenMarketSettleRequest,
		opcode.OpenMarketSearchRequest,
		opcode.OpenMarketListRequest,
		opcode.QuestRewardSelectionRequest,
		opcode.FGWRecallListRequest,
		opcode.FGWRecallRequest,
		opcode.FGWRecallResponse,
		opcode.FGWExitRequest,
		opcode.AgentPingRequest,
	}
	gotRequests := ProtocolRegistry().Opcodes(protocolregistry.ServiceAgent, protocolregistry.ClientToServer, protocolregistry.AgentInWorld)
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("in-world request opcodes = %#v, want %#v", gotRequests, wantRequests)
	}
	legacyKey := protocolregistry.Key{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ClientToServer, Phase: protocolregistry.AgentInWorld, Opcode: opcode.COSUnknownRequest30C4}
	if entry, exists := ProtocolRegistry().Lookup(legacyKey); exists {
		t.Fatalf("unsupported legacy COS request 0x30C4 is registered: %+v", entry)
	}
	for _, packetOpcode := range []uint16{opcode.MovementPositionUpdate, opcode.EntityMovementTurnResponse, opcode.EntitySkillCastBegin, opcode.EntitySkillCastEnd, opcode.ItemMallBalanceUpdate, opcode.ItemMallCashNotification, opcode.EntityItemUsed, opcode.ItemDurabilityUpdate, opcode.ItemRepairResponse, opcode.EntityUpdatePoints, opcode.InventoryItemUpdate, opcode.InventorySizeUpdate, opcode.StorageDataBegin, opcode.StorageData, opcode.StorageDataEnd, opcode.GuildStorageDataBegin, opcode.GuildStorageData, opcode.GuildStorageDataEnd, opcode.FGWUpdate, opcode.GuildWarInfo, opcode.OpenMarketUpdate, opcode.SiegeUpdate} {
		key := protocolregistry.Key{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: packetOpcode}
		entry, exists := ProtocolRegistry().Lookup(key)
		if !exists || entry.Pair != nil {
			t.Fatalf("outbound notification 0x%04X = %+v, exists=%t", packetOpcode, entry, exists)
		}
	}
	for _, key := range []protocolregistry.Key{
		{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ClientToServer, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestTalk},
		{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestTalk},
		{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ClientToServer, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestAbandonRequest},
		{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestAbandonResponse},
		{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ClientToServer, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestRewardSelectionRequest},
		{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestRewardSelectionResponse},
	} {
		entry, exists := ProtocolRegistry().Lookup(key)
		if !exists || entry.Pair == nil {
			t.Fatalf("quest pair %+v = %+v, exists=%t", key, entry, exists)
		}
	}
	updateKey := protocolregistry.Key{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: opcode.QuestUpdate}
	if entry, exists := ProtocolRegistry().Lookup(updateKey); !exists || entry.Pair != nil {
		t.Fatalf("quest update = %+v, exists=%t", entry, exists)
	}
	for _, packetOpcode := range []uint16{opcode.QuestMarkAdd, opcode.QuestMarkRemove} {
		key := protocolregistry.Key{Service: protocolregistry.ServiceAgent, Direction: protocolregistry.ServerToClient, Phase: protocolregistry.AgentInWorld, Opcode: packetOpcode}
		if entry, exists := ProtocolRegistry().Lookup(key); !exists || entry.Pair != nil {
			t.Fatalf("quest mark 0x%04X = %+v, exists=%t", packetOpcode, entry, exists)
		}
	}
}
