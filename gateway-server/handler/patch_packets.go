package handler

import (
	"errors"
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	patchcatalog "github.com/Harvi8/AegisCore/patching/catalog"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func buildPatchAvailableResponse(plan patchcatalog.PatchPlan) (network.Packet, error) {
	if plan.Server.Host == "" || plan.Server.Port == 0 || plan.TargetVersion == 0 || len(plan.Files) == 0 {
		return network.Packet{}, errors.New("patch plan is incomplete")
	}
	if !legacyASCII(plan.Server.Host) {
		return network.Packet{}, errors.New("DownloadServer host is not legacy-ASCII serializable")
	}
	response := network.EmptyPacket()
	response.MessageID = opcode.PatchResponse
	if err := response.WriteByte(patchResultFailure); err != nil {
		return network.Packet{}, err
	}
	if err := response.WriteByte(patchFailureAvailable); err != nil {
		return network.Packet{}, err
	}
	if err := response.WriteString(plan.Server.Host); err != nil {
		return network.Packet{}, fmt.Errorf("write DownloadServer host: %w", err)
	}
	if err := response.WriteUInt16(plan.Server.Port); err != nil {
		return network.Packet{}, err
	}
	if err := response.WriteUInt32(plan.TargetVersion); err != nil {
		return network.Packet{}, err
	}
	for _, file := range plan.Files {
		if file.ID == 0 || file.PreparedSize > math.MaxUint32 {
			return network.Packet{}, fmt.Errorf("patch file %d cannot be represented on the legacy wire", file.ID)
		}
		if !legacyASCII(file.Filename) || !legacyASCII(file.Path) {
			return network.Packet{}, fmt.Errorf("patch file %d metadata is not legacy-ASCII serializable", file.ID)
		}
		if err := response.WriteByte(1); err != nil {
			return network.Packet{}, err
		}
		if err := response.WriteUInt32(file.ID); err != nil {
			return network.Packet{}, err
		}
		if err := response.WriteString(file.Filename); err != nil {
			return network.Packet{}, fmt.Errorf("write patch filename: %w", err)
		}
		if err := response.WriteString(file.Path); err != nil {
			return network.Packet{}, fmt.Errorf("write patch path: %w", err)
		}
		if err := response.WriteUInt32(uint32(file.PreparedSize)); err != nil {
			return network.Packet{}, err
		}
		packing := byte(0)
		if file.ImportToPK2 {
			packing = 1
		}
		if err := response.WriteByte(packing); err != nil {
			return network.Packet{}, err
		}
	}
	if err := response.WriteByte(0); err != nil {
		return network.Packet{}, err
	}
	return response, nil
}

func legacyASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7E {
			return false
		}
	}
	return true
}

// parsePatchRequest follows the documented 0x6100 layout exactly:
// ContentID, module string, then uint32 version. Trailing bytes are a protocol
// violation rather than an extension point because every accepted profile uses
// this exact layout.
func parsePatchRequest(packet network.Packet) (PatchRequest, error) {
	request, err := (vsro188.GatewayPatchRequestCodec{}).Decode(packet)
	if err != nil {
		return PatchRequest{}, fmt.Errorf("malformed patch request: %w", err)
	}
	return PatchRequest{ContentID: request.ContentID, ModuleName: request.ModuleName, Version: request.Version}, nil
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
