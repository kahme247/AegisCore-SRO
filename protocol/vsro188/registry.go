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
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.TeleportReadyResponse, "TeleportReadyResponse", "agent-loading", "agent-server/handler/character/teleport.go")
	addUnpaired(protocolregistry.ServiceAgent, protocolregistry.AgentCharacterLoading, opcode.GameReadyRequest, "GameReadyRequest", "agent-loading", "agent-server/handler/character/game_ready.go")

	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityMovementRequest, opcode.EntityMovementResponse, "EntityMovementRequest", "EntityMovementResponse", "agent-movement", protocolregistry.MassiveForbidden, "protocol/vsro188/movement_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.MovementPositionUpdate, "EntityPositionUpdate", "agent-movement", "protocol/vsro188/movement_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntitySelectRequest, opcode.EntitySelectResponse, "EntitySelectRequest", "EntitySelectResponse", "agent-interaction", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityDeselectRequest, opcode.EntityDeselectResponse, "EntityDeselectRequest", "EntityDeselectResponse", "agent-interaction", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CharacterIncreaseStrengthRequest, opcode.CharacterIncreaseStrengthResponse, "CharacterIncreaseStrengthRequest", "CharacterIncreaseStrengthResponse", "agent-character", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.CharacterIncreaseIntellectRequest, opcode.CharacterIncreaseIntellectResponse, "CharacterIncreaseIntellectRequest", "CharacterIncreaseIntellectResponse", "agent-character", protocolregistry.MassiveForbidden, "protocol/vsro188/inworld_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntityActionRequest, opcode.EntityActionResponse, "EntityActionRequest", "EntityActionStateResponse", "agent-combat", protocolregistry.MassiveForbidden, "protocol/vsro188/combat_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntitySkillCastBegin, "EntitySkillCastBegin", "agent-combat", "protocol/vsro188/combat_codec.go")
	addOutbound(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.EntitySkillCastEnd, "EntitySkillCastEnd", "agent-combat", "protocol/vsro188/combat_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSCommandRequest, opcode.COSCommandResponse, "COSCommandRequest", "COSCommandResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSTerminationRequest, opcode.COSTerminationResponse, "COSTerminationRequest", "COSTerminationResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSUpdateMountStateRequest, opcode.COSUpdateMountStateResponse, "COSMountStateRequest", "COSMountStateResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
	addPair(protocolregistry.ServiceAgent, protocolregistry.AgentInWorld, opcode.COSUnsummonRequest, opcode.COSUnsummonResponse, "COSUnsummonRequest", "COSUnsummonResponse", "agent-cos", protocolregistry.MassiveForbidden, "protocol/vsro188/cos_codec.go")
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
