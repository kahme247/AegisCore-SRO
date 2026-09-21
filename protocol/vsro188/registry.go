package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
	protocolregistry "github.com/Harvi8/AegisCore/protocol/registry"
)

var protocolRegistry = mustProtocolRegistry()

// ProtocolRegistry returns the immutable, evidence-aware catalog for listener
// admission and packet identities whose typed codecs are maintained here. It
// is not a claim that the whole vSRO 1.188 protocol has been recovered.
func ProtocolRegistry() *protocolregistry.Registry {
	return protocolRegistry
}

func mustProtocolRegistry() *protocolregistry.Registry {
	registry, err := protocolregistry.New(protocolEntries())
	if err != nil {
		panic(fmt.Sprintf("build vSRO 1.188 protocol registry: %v", err))
	}
	return registry
}

func protocolEntries() []protocolregistry.Entry {
	var entries []protocolregistry.Entry
	addPair := func(service protocolregistry.Service, phase protocolregistry.Phase, requestOpcode, responseOpcode uint16, requestName, responseName, rateLimit string, responseMassive protocolregistry.MassiveRule, source string) {
		requestKey := protocolregistry.Key{Service: service, Direction: protocolregistry.ClientToServer, Phase: phase, Opcode: requestOpcode}
		responseKey := protocolregistry.Key{Service: service, Direction: protocolregistry.ServerToClient, Phase: phase, Opcode: responseOpcode}
		entries = append(entries,
			implementedEntry(requestKey, requestName, &responseKey, protocolregistry.MassiveForbidden, rateLimit, source),
			implementedEntry(responseKey, responseName, &requestKey, responseMassive, rateLimit, source),
		)
	}
	addUnpaired := func(service protocolregistry.Service, phase protocolregistry.Phase, packetOpcode uint16, name, rateLimit, source string) {
		entries = append(entries, implementedEntry(
			protocolregistry.Key{Service: service, Direction: protocolregistry.ClientToServer, Phase: phase, Opcode: packetOpcode},
			name,
			nil,
			protocolregistry.MassiveForbidden,
			rateLimit,
			source,
		))
	}
	addOutbound := func(service protocolregistry.Service, phase protocolregistry.Phase, packetOpcode uint16, name, rateLimit, source string) {
		entries = append(entries, implementedEntry(
			protocolregistry.Key{Service: service, Direction: protocolregistry.ServerToClient, Phase: phase, Opcode: packetOpcode},
			name,
			nil,
			protocolregistry.MassiveForbidden,
			rateLimit,
			source,
		))
	}

	addPair(protocolregistry.ServiceGateway, protocolregistry.GatewayPreLogin, opcode.PatchRequest, opcode.PatchResponse, "PatchRequest", "PatchResponse", "gateway-patch", protocolregistry.MassiveAllowed, "gateway-server/handler/patch_request_handler.go")
	addPair(protocolregistry.ServiceGateway, protocolregistry.GatewayPreLogin, opcode.ShardlistRequest, opcode.ShardlistResponse, "ShardListRequest", "ShardListResponse", "gateway-query", protocolregistry.MassiveForbidden, "gateway-server/handler/shardlist_request_handler.go")
	addPair(protocolregistry.ServiceGateway, protocolregistry.GatewayPreLogin, opcode.LoginRequest, opcode.LoginResponse, "LoginRequest", "LoginResponse", "gateway-login", protocolregistry.MassiveForbidden, "gateway-server/handler/login_request_handler.go")
	addPair(protocolregistry.ServiceGateway, protocolregistry.GatewayPreLogin, opcode.NoticeRequest, opcode.NoticeResponse, "NoticeRequest", "NoticeResponse", "gateway-query", protocolregistry.MassiveAllowed, "gateway-server/handler/notice_request_handler.go")
	addPair(protocolregistry.ServiceGateway, protocolregistry.GatewayPreLogin, opcode.ShardlistPing, opcode.ShardlistPong, "ShardListPing", "ShardListPong", "gateway-query", protocolregistry.MassiveForbidden, "protocol/vsro188/gateway_codec.go")

	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentAuthentication, opcode.AuthRequest, opcode.AuthResponse, "AuthRequest", "AuthResponse", "agent-auth", protocolregistry.MassiveForbidden, "agent-server/handler/lobby/auth_request_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentLobby, opcode.JoinLobbyRequest, opcode.LobbySelectCharacterResponse, "CharacterSelectionJoinRequest", "CharacterSelectionJoinResponse", "agent-lobby", protocolregistry.MassiveForbidden, "agent-server/handler/lobby/char_selection_join_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentLobby, opcode.LobbyActionRequest, opcode.LobbyActionResponse, "CharacterSelectionActionRequest", "CharacterSelectionActionResponse", "agent-lobby", protocolregistry.MassiveForbidden, "agent-server/handler/lobby/char_selection_action_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentLobby, opcode.LobbyRenameRequest, opcode.LobbyRenameResponse, "CharacterSelectionRenameRequest", "CharacterSelectionRenameResponse", "agent-lobby", protocolregistry.MassiveForbidden, "agent-server/handler/lobby/char_selection_rename_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentLobby, opcode.AgentPingRequest, opcode.AgentPingResponse, "AgentPingRequest", "AgentPingResponse", "agent-keepalive", protocolregistry.MassiveForbidden, "agent-server/handler/ping/ping_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.AgentPingRequest, opcode.AgentPingResponse, "AgentPingRequest", "AgentPingResponse", "agent-keepalive", protocolregistry.MassiveForbidden, "agent-server/handler/ping/ping_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.GuideRequest, opcode.GuideResponse, "GuideRequest", "GuideResponse", "agent-loading", protocolregistry.MassiveForbidden, "agent-server/handler/character/guide.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.LobbyActionRequest, opcode.LobbyActionResponse, "CharacterSelectionListRequest", "CharacterSelectionListResponse", "agent-loading", protocolregistry.MassiveForbidden, "agent-server/handler/lobby/char_selection_action_handler.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.TeleportReadyResponse, "TeleportReadyResponse", "agent-loading", "agent-server/handler/character/teleport.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.TeleportCancelRequest, "TeleportCancelRequest", "agent-loading", "agent-server/handler/character/teleport.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.GameReadyRequest, "GameReadyRequest", "agent-loading", "agent-server/handler/character/game_ready.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.TeleportDesignateRequest, opcode.TeleportDesignateResponse, "TeleportDesignateRequest", "TeleportDesignateResponse", "agent-teleport", protocolregistry.MassiveForbidden, "agent-server/handler/character/teleport.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.TeleportUseRequest, opcode.TeleportUseResponse, "TeleportUseRequest", "TeleportUseResponse", "agent-teleport", protocolregistry.MassiveForbidden, "agent-server/handler/character/teleport.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.RecallChallenge, "RecallChallenge", "agent-recall", "protocol/vsro188/forgotten_world_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FGWUpdate, "ForgottenWorldUpdate", "agent-recall", "protocol/vsro188/forgotten_world_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.RecallResponse, "RecallResponse", "agent-recall", "protocol/vsro188/forgotten_world_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.RecallSolveRequest, opcode.RecallSolveResponse, "RecallSolveRequest", "RecallSolveResponse", "agent-recall", protocolregistry.MassiveForbidden, "protocol/vsro188/forgotten_world_codec.go")

	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityMovementRequest, opcode.EntityMovementResponse, "EntityMovementRequest", "EntityMovementResponse", "agent-movement", protocolregistry.MassiveForbidden, "protocol/vsro188/movement_codec.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityMovementForwardRequest, "EntityMovementForwardRequest", "agent-movement", "protocol/vsro188/movement_codec.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityMovementTurnRequest, "EntityMovementTurnRequest", "agent-movement", "protocol/vsro188/movement_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.MovementPositionUpdate, "EntityPositionUpdate", "agent-movement", "protocol/vsro188/movement_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityMovementTurnResponse, "EntityTurnResponse", "agent-movement", "protocol/vsro188/movement_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntitySelectRequest, opcode.EntitySelectResponse, "EntitySelectRequest", "EntitySelectResponse", "agent-interaction", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityDeselectRequest, opcode.EntityDeselectResponse, "EntityDeselectRequest", "EntityDeselectResponse", "agent-interaction", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CharacterIncreaseStrengthRequest, opcode.CharacterIncreaseStrengthResponse, "CharacterIncreaseStrengthRequest", "CharacterIncreaseStrengthResponse", "agent-character", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CharacterIncreaseIntellectRequest, opcode.CharacterIncreaseIntellectResponse, "CharacterIncreaseIntellectRequest", "CharacterIncreaseIntellectResponse", "agent-character", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.ItemOperationRequest, opcode.ItemOperationResponse, "ItemOperationRequest", "ItemOperationResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/inventory_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.InventoryItemUseRequest, opcode.InventoryItemUseResponse, "InventoryItemUseRequest", "InventoryItemUseResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/item_use_handler.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.StorageOpenRequest, "StorageOpenRequest", "agent-inventory", "agent-server/handler/inventory/storage_handler.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.InventoryStorageOpenResponse, "StorageOpenResponse", "agent-inventory", "protocol/vsro188/inventory_edge_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.InventoryUpdateAmmo, "InventoryAmmoUpdate", "agent-inventory", "protocol/vsro188/inventory_edge_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.InventoryUnknownRequest, opcode.InventoryUnknownResponse, "InventoryUnknownRequest", "InventoryUnknownResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/inventory_handler.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.ItemMallBalanceUpdate, "ItemMallBalanceUpdate", "agent-inventory", "agent-server/handler/inventory/inventory_handler.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.ItemMallCashNotification, "ItemMallCashNotification", "agent-inventory", "protocol/vsro188/item_mall_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.PremiumUnknownUpdate315A, "PremiumUnknownUpdate", "agent-premium", "protocol/vsro188/premium_silk_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.PremiumDeco, "PremiumDecoration", "agent-premium", "protocol/vsro188/premium_silk_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.PremiumDecoReset, "PremiumDecorationReset", "agent-premium", "protocol/vsro188/premium_silk_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.PremiumDailyReset, "PremiumDailyReset", "agent-premium", "protocol/vsro188/premium_silk_codec.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.PremiumPause, "PremiumPauseRequest", "agent-premium", "protocol/vsro188/premium_silk_codec.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyMatchingResponse, "AcademyInvitationResponse", "agent-social", "protocol/vsro188/academy_extension_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyMatchingResponse, "AcademyInvitation", "agent-social", "protocol/vsro188/academy_extension_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyUpdate, "AcademyUpdate", "agent-social", "protocol/vsro188/academy_extension_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyUnknownUpdate3C86, "AcademyTimedUpdate", "agent-social", "protocol/vsro188/academy_extension_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyUnknownUpdate3C87, "AcademyTimedReferenceUpdate", "agent-social", "protocol/vsro188/academy_extension_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityItemUsed, "ItemUsedNotification", "agent-inventory", "protocol/vsro188/item_notification_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.ItemDurabilityUpdate, "DurabilityUpdate", "agent-inventory", "protocol/vsro188/durability_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.ItemRepairResponse, "RepairResponse", "agent-inventory", "protocol/vsro188/repair_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityUpdatePoints, "GoldPointUpdate", "agent-inventory", "protocol/vsro188/point_update_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.InventoryItemUpdate, "InventoryItemUpdate", "agent-inventory", "protocol/vsro188/item_update_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.InventorySizeUpdate, "InventorySizeUpdate", "agent-inventory", "protocol/vsro188/inventory_size_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.StorageDataBegin, "StorageDataBegin", "agent-inventory", "protocol/vsro188/storage_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.StorageData, "StorageData", "agent-inventory", "protocol/vsro188/storage_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.StorageDataEnd, "StorageDataEnd", "agent-inventory", "protocol/vsro188/storage_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildStorageDataBegin, "GuildStorageDataBegin", "agent-inventory", "protocol/vsro188/storage_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildStorageData, "GuildStorageData", "agent-inventory", "protocol/vsro188/storage_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildStorageDataEnd, "GuildStorageDataEnd", "agent-inventory", "protocol/vsro188/storage_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildUpdate, "GuildUpdate", "agent-social", "protocol/vsro188/guild_update_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildEntityUpdate, "GuildEntityUpdate", "agent-social", "protocol/vsro188/guild_update_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityUpdateGuildNickname, "GuildEntityNicknameUpdate", "agent-social", "protocol/vsro188/guild_update_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildWarRequest, opcode.GuildWarStartResponse, "GuildWarProposal", "GuildWarStartResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_war_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildWarRequest, "GuildWarInvitation", "agent-social", "protocol/vsro188/guild_war_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildWarInfo, "GuildWarInfo", "agent-social", "protocol/vsro188/guild_war_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildWarEndRequest, opcode.GuildWarEndResponse, "GuildWarEndRequest", "GuildWarEndResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_war_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildWarCompensationClaimRequest, opcode.GuildWarCompensationClaimResponse, "GuildWarCompensationClaimRequest", "GuildWarCompensationClaimResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_war_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildWarRewardRequest, opcode.GuildWarRewardResponse, "GuildWarRewardRequest", "GuildWarRewardResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_war_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.SilkHistoryRequest, opcode.SilkHistoryResponse, "SilkHistoryRequest", "SilkHistoryResponse", "agent-premium", protocolregistry.MassiveForbidden, "protocol/vsro188/premium_silk_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.SilkUnknownRequest, opcode.SilkUnknownResponse, "SilkUnknownRequest", "SilkUnknownResponse", "agent-premium", protocolregistry.MassiveForbidden, "protocol/vsro188/premium_silk_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.PremiumDecoUseRequest, opcode.PremiumDecoUseResponse, "PremiumDecorationUseRequest", "PremiumDecorationUseResponse", "agent-premium", protocolregistry.MassiveForbidden, "protocol/vsro188/premium_silk_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyInviteRequest, opcode.AcademyInviteResponse, "AcademyInviteRequest", "AcademyInviteResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/academy_extension_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyMemberActionRequest, opcode.AcademyMemberActionResponse, "AcademyMemberActionRequest", "AcademyMemberActionResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/academy_extension_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AcademyUnknownOperationRequest, opcode.AcademyUnknownOperationResponse, "AcademyTimedOperationRequest", "AcademyTimedOperationResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/academy_extension_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildElectionRequest, opcode.GuildElectionResponse, "GuildElectionRequest", "GuildElectionResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_election_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildElectionParticipateRequest, opcode.GuildElectionParticipateResponse, "GuildElectionParticipateRequest", "GuildElectionParticipateResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_election_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildElectionVoteRequest, opcode.GuildElectionVoteResponse, "GuildElectionVoteRequest", "GuildElectionVoteResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_election_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildElectionUpdate, "GuildElectionUpdate", "agent-social", "protocol/vsro188/guild_election_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildUpdateSiegeAuthorityRequest, opcode.GuildUpdateSiegeAuthorityResponse, "GuildSiegeAuthorityRequest", "GuildSiegeAuthorityFailure", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_siege_authority_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityGuildUpdateSiegeAuthority, "GuildSiegeAuthorityUpdate", "agent-social", "protocol/vsro188/guild_siege_authority_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildMercenaryAttrRequest, opcode.GuildMercenaryAttrResponse, "GuildMercenaryAttributeRequest", "GuildMercenaryAttributeFailure", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_mercenary_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildMercenaryTerminateRequest, opcode.GuildMercenaryTerminateResponse, "GuildMercenaryTerminateRequest", "GuildMercenaryTerminateFailure", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/guild_mercenary_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.GuildGPHistoryRequest, opcode.GuildGPHistoryResponse, "FortressExpenditureHistoryRequest", "FortressExpenditureHistory", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/fortress_expenditure_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.SiegeActionRequest, opcode.SiegeActionResponse, "SiegeApplicationRequest", "SiegeActionFailure", "agent-siege", protocolregistry.MassiveForbidden, "protocol/vsro188/siege_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.SiegeUpdate, "SiegeApplicationUpdate", "agent-siege", "protocol/vsro188/siege_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FGWRecallListRequest, opcode.FGWRecallListResponse, "ForgottenWorldRecallListRequest", "ForgottenWorldRecallListResponse", "agent-recall", protocolregistry.MassiveForbidden, "protocol/vsro188/forgotten_world_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FGWRecallRequest, opcode.FGWRecallMemberResponse, "ForgottenWorldRecallMemberRequest", "ForgottenWorldRecallMemberResponse", "agent-recall", protocolregistry.MassiveForbidden, "protocol/vsro188/forgotten_world_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FGWRecallRequest, "ForgottenWorldRecallInvitation", "agent-recall", "protocol/vsro188/forgotten_world_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FGWRecallResponse, opcode.FGWRecallMemberAcceptedResponse, "ForgottenWorldRecallResponse", "ForgottenWorldRecallAcceptedResponse", "agent-recall", protocolregistry.MassiveForbidden, "protocol/vsro188/forgotten_world_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FGWExitRequest, opcode.FGWExitResponse, "ForgottenWorldExitRequest", "ForgottenWorldExitResponse", "agent-recall", protocolregistry.MassiveForbidden, "protocol/vsro188/forgotten_world_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityFriendAddRequest, opcode.CommunityFriendAddResponse, "CommunityFriendAddRequest", "CommunityFriendAddFailure", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_friend_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityFriendRequest, "CommunityFriendInvitation", "agent-social", "protocol/vsro188/community_friend_codec.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityFriendResponse, "CommunityFriendResponse", "agent-social", "protocol/vsro188/community_friend_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityFriendDeleteRequest, opcode.CommunityFriendDeleteResponse, "CommunityFriendDeleteRequest", "CommunityFriendDeleteResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_friend_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityFriendInfo, "CommunityFriendInfo", "agent-social", "protocol/vsro188/community_friend_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityUpdate, "CommunityUpdate", "agent-social", "protocol/vsro188/community_friend_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityMemoOpenRequest, opcode.CommunityMemoOpenResponse, "CommunityMemoOpenRequest", "CommunityMemoOpenResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_memo_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityMemoSendRequest, opcode.CommunityMemoSendResponse, "CommunityMemoSendRequest", "CommunityMemoSendResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_memo_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityMemoDeleteRequest, opcode.CommunityMemoDeleteResponse, "CommunityMemoDeleteRequest", "CommunityMemoDeleteResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_memo_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityMemoListRequest, opcode.CommunityMemoListResponse, "CommunityMemoListRequest", "CommunityMemoListResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_memo_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityMemoGroupSendRequest, opcode.CommunityMemoGroupSendResponse, "CommunityMemoGroupSendRequest", "CommunityMemoGroupSendResponse", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_memo_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CommunityBlockUpdateRequest, opcode.CommunityBlockUpdateResponse, "CommunityBlockUpdateRequest", "CommunityBlockUpdateResult", "agent-social", protocolregistry.MassiveForbidden, "protocol/vsro188/community_block_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AlchemyElixirRequest, opcode.AlchemyElixirResponse, "AlchemyElixirRequest", "AlchemyElixirResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/alchemy_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AlchemyStoneRequest, opcode.AlchemyStoneResponse, "AlchemyStoneRequest", "AlchemyStoneResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/alchemy_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AlchemyMaterialRequest, opcode.AlchemyMaterialResponse, "AlchemyMaterialRequest", "AlchemyMaterialResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/alchemy_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AlchemyDismantleRequest, opcode.AlchemyDismantleResponse, "AlchemyDismantleRequest", "AlchemyDismantleResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/alchemy_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AlchemySocketRequest, opcode.AlchemySocketResponse, "AlchemySocketRequest", "AlchemySocketResponse", "agent-inventory", protocolregistry.MassiveForbidden, "agent-server/handler/inventory/alchemy_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityActionRequest, opcode.EntityActionResponse, "EntityActionRequest", "EntityActionStateResponse", "agent-combat", protocolregistry.MassiveForbidden, "protocol/vsro188/combat_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OperatorRequest, opcode.OperatorResponse, "OperatorRequest", "OperatorResponse", "agent-operator", protocolregistry.MassiveForbidden, "agent-server/handler/character/operator.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.FlagWarRegisterRequest, opcode.FlagWarUpdate, "FlagWarRegisterRequest", "FlagWarUnavailable", "agent-competitive", protocolregistry.MassiveForbidden, "agent-server/handler/character/free_pvp.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.BattleArenaRequest, opcode.BattleArenaOperation, "BattleArenaRequest", "BattleArenaUnavailable", "agent-competitive", protocolregistry.MassiveForbidden, "agent-server/handler/character/free_pvp.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OperatorPunishment, "OperatorPunishment", "agent-operator", "protocol/vsro188/operator_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.SkillWithdrawRequest, opcode.SkillWithdrawResponse, "SkillWithdrawRequest", "SkillWithdrawResponse", "agent-character", protocolregistry.MassiveForbidden, "agent-server/handler/character/skill_restoration.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.MasteryWithdrawRequest, opcode.MasteryWithdrawResponse, "MasteryWithdrawRequest", "MasteryWithdrawResponse", "agent-character", protocolregistry.MassiveForbidden, "agent-server/handler/character/skill_restoration.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.QuestTalk, opcode.QuestTalk, "QuestTalkSelection", "QuestTalkMessage", "agent-quest", protocolregistry.MassiveForbidden, "agent-server/handler/character/npc_talk.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.QuestAbandonRequest, opcode.QuestAbandonResponse, "QuestAbandonRequest", "QuestAbandonResponse", "agent-quest", protocolregistry.MassiveForbidden, "agent-server/handler/character/quest.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.QuestRewardSelectionRequest, opcode.QuestRewardSelectionResponse, "QuestRewardSelectionRequest", "QuestRewardSelectionResponse", "agent-quest", protocolregistry.MassiveForbidden, "agent-server/handler/character/quest_reward_selection.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.QuestUpdate, "QuestUpdate", "agent-quest", "protocol/vsro188/quest_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.QuestMarkAdd, "QuestMarkAdd", "agent-quest", "agent-server/handler/character/quest_marks.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.QuestMarkRemove, "QuestMarkRemove", "agent-quest", "agent-server/handler/character/quest_marks.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntitySkillCastBegin, "EntitySkillCastBegin", "agent-combat", "protocol/vsro188/combat_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntitySkillCastEnd, "EntitySkillCastEnd", "agent-combat", "protocol/vsro188/combat_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSCommandRequest, opcode.COSCommandResponse, "COSCommandRequest", "COSCommandResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSTerminationRequest, opcode.COSTerminationResponse, "COSTerminationRequest", "COSTerminationResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSUpdateMountStateRequest, opcode.COSUpdateMountStateResponse, "COSMountStateRequest", "COSMountStateResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSUnsummonRequest, opcode.COSUnsummonResponse, "COSUnsummonRequest", "COSUnsummonResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSNameRequest, opcode.COSNameResponse, "COSNameRequest", "COSNameResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSSettingsRequest, opcode.COSSettingsResponse, "COSSettingsRequest", "COSSettingsResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSUnknownRequest, opcode.COSUnknownResponse, "COSUnknownRequest", "COSUnknownFailure", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_extension_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketDetailRequest, opcode.OpenMarketDetailResponse, "OpenMarketDetailRequest", "OpenMarketDetailResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketCloseRequest, opcode.OpenMarketCloseResponse, "OpenMarketCloseRequest", "OpenMarketCloseResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketRegisterRequest, opcode.OpenMarketRegisterResponse, "OpenMarketRegisterRequest", "OpenMarketRegisterResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketUnregisterRequest, opcode.OpenMarketUnregisterResponse, "OpenMarketUnregisterRequest", "OpenMarketUnregisterResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketBuyRequest, opcode.OpenMarketBuyResponse, "OpenMarketBuyRequest", "OpenMarketBuyResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketSettleRequest, opcode.OpenMarketSettleResponse, "OpenMarketSettleRequest", "OpenMarketSettleResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketSearchRequest, opcode.OpenMarketSearchResponse, "OpenMarketSearchRequest", "OpenMarketSearchResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketListRequest, opcode.OpenMarketListResponse, "OpenMarketListRequest", "OpenMarketListResponse", "agent-market", protocolregistry.MassiveForbidden, "agent-server/handler/market/market_handler.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.OpenMarketUpdate, "OpenMarketUpdate", "agent-market", "agent-server/handler/market/market_handler.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.AgentPingRequest, opcode.AgentPingResponse, "AgentPingRequest", "AgentPingResponse", "agent-keepalive", protocolregistry.MassiveForbidden, "protocol/vsro188/session_codec.go")

	return entries
}

func implementedEntry(key protocolregistry.Key, name string, pair *protocolregistry.Key, massive protocolregistry.MassiveRule, rateLimit, source string) protocolregistry.Entry {
	return protocolregistry.Entry{
		Key:           key,
		CanonicalName: name,
		Pair:          pair,
		Encryption:    protocolregistry.EncryptionRequired,
		Massive:       massive,
		RateLimit:     rateLimit,
		Evidence: []protocolregistry.Evidence{
			{
				Kind:       protocolregistry.EvidenceImplementation,
				Source:     source,
				Locator:    fmt.Sprintf("%s/0x%04X", name, key.Opcode),
				Confidence: "implemented-not-stock-client-verified",
			},
			{
				Kind:       protocolregistry.EvidenceProtocolCorpus,
				Source:     "../.references/testin-decompilation/opcode-catalog.csv",
				Locator:    fmt.Sprintf("opcode=0x%04X", key.Opcode),
				Confidence: "catalogued-mixed-provenance",
			},
		},
		Status: protocolregistry.StatusImplemented,
	}
}
