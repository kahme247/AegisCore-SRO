package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
)

const (
	COSCommandAttack    byte = 2
	COSCommandStop      byte = 9
	COSCommandCastSkill byte = 13
	cosMountRequestSize      = 5
)

type COSMountStateRequest struct {
	Mounted           bool
	TransportUniqueID uint32
}

type COSUIDRequest struct{ UniqueID uint32 }

type COSCommandRequest struct {
	PetUniqueID    uint32
	Command        byte
	TargetUniqueID uint32
	SkillID        uint32
}

type COSMountStateRequestCodec struct{}
type COSUIDRequestCodec struct{}
type COSCommandRequestCodec struct{}

func (COSMountStateRequestCodec) Decode(packet network.Packet) (COSMountStateRequest, error) {
	if packet.Remaining() != cosMountRequestSize {
		return COSMountStateRequest{}, fmt.Errorf("COS mount-state payload length %d, want %d", packet.Remaining(), cosMountRequestSize)
	}
	action, err := packet.ReadByte()
	if err != nil {
		return COSMountStateRequest{}, fmt.Errorf("read COS mount-state action: %w", err)
	}
	if action != 0 && action != 1 {
		return COSMountStateRequest{}, fmt.Errorf("invalid COS mount-state action %d", action)
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return COSMountStateRequest{}, fmt.Errorf("read COS transport unique ID: %w", err)
	}
	if uid == 0 {
		return COSMountStateRequest{}, fmt.Errorf("COS transport unique ID is required")
	}
	return COSMountStateRequest{Mounted: action == 1, TransportUniqueID: uid}, nil
}

func (COSUIDRequestCodec) Decode(packet network.Packet) (COSUIDRequest, error) {
	uid, err := decodeExactUID(packet, "COS lifecycle")
	if err != nil {
		return COSUIDRequest{}, err
	}
	if uid == 0 {
		return COSUIDRequest{}, fmt.Errorf("COS unique ID is required")
	}
	return COSUIDRequest{UniqueID: uid}, nil
}

// Decode returns a partially populated request on failure so the handler can
// preserve the stock failure packet's command echo.
func (COSCommandRequestCodec) Decode(packet network.Packet) (COSCommandRequest, error) {
	request := COSCommandRequest{}
	if packet.Remaining() < 5 {
		return request, fmt.Errorf("COS command payload length %d, want at least 5", packet.Remaining())
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return request, fmt.Errorf("read COS command pet unique ID: %w", err)
	}
	request.PetUniqueID = uid
	command, err := packet.ReadByte()
	if err != nil {
		return request, fmt.Errorf("read COS command: %w", err)
	}
	request.Command = command
	if uid == 0 {
		return request, fmt.Errorf("COS command pet unique ID is required")
	}
	switch command {
	case COSCommandAttack:
		if packet.Remaining() != 4 {
			return request, fmt.Errorf("COS attack command payload has %d trailing bytes, want 4", packet.Remaining())
		}
		request.TargetUniqueID, err = packet.ReadUInt32()
	case COSCommandStop:
		if packet.Remaining() != 0 {
			return request, fmt.Errorf("COS stop command has %d trailing bytes", packet.Remaining())
		}
	case COSCommandCastSkill:
		if packet.Remaining() != 8 {
			return request, fmt.Errorf("COS skill command payload has %d trailing bytes, want 8", packet.Remaining())
		}
		request.SkillID, err = packet.ReadUInt32()
		if err == nil {
			request.TargetUniqueID, err = packet.ReadUInt32()
		}
	default:
		return request, fmt.Errorf("unsupported COS command %d", command)
	}
	if err != nil {
		return request, fmt.Errorf("read COS command arguments: %w", err)
	}
	return request, nil
}
