package network

import (
	"errors"

	"github.com/ferdoran/go-sro/framework/security/blowfish"
)

var ErrNilPacket = errors.New("cannot encode a nil packet")

// FrameEncoder is the session-facing counterpart to FrameDecoder. It converts
// one logical packet into an owned TCP wire frame; a sender supplies its
// connection-specific cipher when encryption is enabled.
type FrameEncoder struct {
	cipher *blowfish.Cipher
}

func NewFrameEncoder(cipher *blowfish.Cipher) *FrameEncoder {
	return &FrameEncoder{cipher: cipher}
}

func (e *FrameEncoder) Encode(packet *Packet) ([]byte, error) {
	if packet == nil {
		return nil, ErrNilPacket
	}
	return packet.MarshalWire(e.cipher)
}
