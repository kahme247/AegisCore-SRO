package handler

import (
	"context"
	"fmt"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/ferdoran/go-sro/gateway-server/config"
	log "github.com/sirupsen/logrus"
)

// PatchRequest is the complete 0x6100 request. The request must be parsed in
// full before Gateway decides whether the single pinned profile is supported.
type PatchRequest struct {
	ContentID  byte
	ModuleName string
	Version    uint32
}

const (
	patchResultSuccess            byte = 1
	patchResultFailure            byte = 2
	patchFailureUnknownClient     byte = 1
	patchFailureNoAvailableServer byte = 3
)

// PatchRequestHandler has a profile loader rather than a hard-coded client
// version so an operator can promote exactly one capture-backed vSRO 1.88
// profile without introducing undocumented packet defaults into source.
type PatchRequestHandler struct {
	LoadProfile func() (config.ProtocolProfile, error)
}

func NewPatchRequestHandler(loadProfile func() (config.ProtocolProfile, error)) *PatchRequestHandler {
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	return &PatchRequestHandler{LoadProfile: loadProfile}
}

func RegisterPatchRequestHandler(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewPatchRequestHandler(config.LoadProtocolProfile)
	target.RegisterHandler(opcode.PatchRequest, handler.Handle)
}

func (h *PatchRequestHandler) Handle(_ context.Context, session *server.Session, packet network.Packet) error {
	request, err := parsePatchRequest(packet)
	if err != nil {
		log.Warnf("CLIENT_GATEWAY_PATCH_REQUEST rejected during parsing: %v", err)
		return err
	}
	log.Infof("CLIENT_GATEWAY_PATCH_REQUEST content_id=%d module=%q version=%d", request.ContentID, request.ModuleName, request.Version)
	loadProfile := h.LoadProfile
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	profile, err := loadProfile()
	if err != nil {
		return fmt.Errorf("load gateway patch profile: %w", err)
	}
	response, outcome, err := buildPatchResponse(request, profile)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("send patch response: missing session")
	}
	if err := session.SendMassive(response); err != nil {
		return err
	}
	log.Infof("SERVER_GATEWAY_PATCH_RESPONSE sent %s massive=true", outcome)
	return nil
}

// parsePatchRequest follows the documented 0x6100 layout exactly:
// ContentID, module string, then uint32 version. Trailing bytes are a protocol
// violation rather than an extension point because this server supports one
// pinned client profile only.
func parsePatchRequest(packet network.Packet) (PatchRequest, error) {
	contentID, err := packet.ReadByte()
	if err != nil {
		return PatchRequest{}, fmt.Errorf("malformed patch request content ID: %w", err)
	}
	moduleName, err := packet.ReadString()
	if err != nil {
		return PatchRequest{}, fmt.Errorf("malformed patch request module name: %w", err)
	}
	version, err := packet.ReadUInt32()
	if err != nil {
		return PatchRequest{}, fmt.Errorf("malformed patch request version: %w", err)
	}
	if packet.Remaining() != 0 {
		return PatchRequest{}, fmt.Errorf("malformed patch request: trailing data")
	}
	return PatchRequest{ContentID: contentID, ModuleName: moduleName, Version: version}, nil
}

func (request PatchRequest) Validate(profile config.ProtocolProfile) error {
	if err := config.ValidateProtocolProfile(profile); err != nil {
		return fmt.Errorf("invalid pinned gateway profile: %w", err)
	}
	if request.ContentID != profile.ContentID {
		return fmt.Errorf("unsupported patch content ID %d", request.ContentID)
	}
	if request.ModuleName != profile.ModuleName {
		return fmt.Errorf("unsupported patch module %q", request.ModuleName)
	}
	if request.Version != profile.Version {
		return fmt.Errorf("unsupported patch version %d", request.Version)
	}
	return nil
}

// buildPatchSuccessResponse is the documented fixed-core result: massive
// 0xA100 whose logical payload is exactly [Result=1]. No undocumented patch
// manifest, error branch, or extra padding is invented without a capture.
func buildPatchSuccessResponse() (network.Packet, error) {
	response := network.EmptyPacket()
	response.MessageID = opcode.PatchResponse
	if err := response.WriteByte(patchResultSuccess); err != nil {
		return network.Packet{}, fmt.Errorf("write patch success result: %w", err)
	}
	return response, nil
}

// buildPatchResponse implements only TestIn branches whose response bytes and
// comparisons are confirmed in GatewayServer.exe. Exact versions succeed.
// Unknown clients and higher-than-known versions return [2,1]. Older clients
// return [2,3] because GO-SRO does not yet advertise a verified patch server.
func buildPatchResponse(request PatchRequest, profile config.ProtocolProfile) (network.Packet, string, error) {
	if err := config.ValidateProtocolProfile(profile); err != nil {
		return network.Packet{}, "", fmt.Errorf("invalid pinned gateway profile: %w", err)
	}
	if request.ContentID != profile.ContentID || request.ModuleName != profile.ModuleName {
		response, err := buildPatchFailureResponse(patchFailureUnknownClient)
		return response, "result=2 reason=unknown-client", err
	}
	if request.Version > profile.Version {
		response, err := buildPatchFailureResponse(patchFailureUnknownClient)
		return response, "result=2 reason=higher-than-known-version", err
	}
	if request.Version < profile.Version {
		response, err := buildPatchFailureResponse(patchFailureNoAvailableServer)
		return response, "result=2 reason=no-available-patch-server", err
	}
	response, err := buildPatchSuccessResponse()
	return response, "result=1", err
}

func buildPatchFailureResponse(reason byte) (network.Packet, error) {
	response := network.EmptyPacket()
	response.MessageID = opcode.PatchResponse
	if err := response.WriteByte(patchResultFailure); err != nil {
		return network.Packet{}, fmt.Errorf("write patch failure result: %w", err)
	}
	if err := response.WriteByte(reason); err != nil {
		return network.Packet{}, fmt.Errorf("write patch failure reason: %w", err)
	}
	return response, nil
}
