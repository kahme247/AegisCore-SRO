package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestEntityActionRequestCodecsMatchRecovered7074Forms(t *testing.T) {
	tests := []struct {
		name   string
		encode func() ([]byte, uint16, error)
		want   []byte
	}{
		{
			name: "stop",
			encode: func() ([]byte, uint16, error) {
				packet, err := (EntityActionStopRequestCodec{}).Encode(EntityActionStopRequest{})
				return packet.Payload(), packet.MessageID, err
			},
			want: []byte{1},
		},
		{
			name: "basic/entity target",
			encode: func() ([]byte, uint16, error) {
				packet, err := (EntityActionTargetRequestCodec{}).Encode(EntityActionTargetRequest{TargetUID: 0x12345678})
				return packet.Payload(), packet.MessageID, err
			},
			want: []byte{1, 4, 0x78, 0x56, 0x34, 0x12, 0},
		},
		{
			name: "entity skill",
			encode: func() ([]byte, uint16, error) {
				packet, err := (EntityActionSkillTargetRequestCodec{}).Encode(EntityActionSkillTargetRequest{SkillID: 0x11223344, TargetUID: 0xAABBCCDD})
				return packet.Payload(), packet.MessageID, err
			},
			want: []byte{1, 4, 0x44, 0x33, 0x22, 0x11, 1, 0xDD, 0xCC, 0xBB, 0xAA},
		},
		{
			name: "position skill",
			encode: func() ([]byte, uint16, error) {
				packet, err := (EntityActionPositionSkillRequestCodec{}).Encode(EntityActionPositionSkillRequest{
					SkillID: 0x11223344, Region: -2, X: 0x01020304, Z: -5, Y: -1599029040,
				})
				return packet.Payload(), packet.MessageID, err
			},
			want: []byte{1, 4, 0x44, 0x33, 0x22, 0x11, 2, 0xFE, 0xFF, 0x04, 0x03, 0x02, 0x01, 0xFB, 0xFF, 0xFF, 0xFF, 0xD0, 0xC0, 0xB0, 0xA0},
		},
		{
			name: "remove own buff",
			encode: func() ([]byte, uint16, error) {
				packet, err := (EntityActionRemoveBuffRequestCodec{}).Encode(EntityActionRemoveBuffRequest{SkillID: 0x11223344})
				return packet.Payload(), packet.MessageID, err
			},
			want: []byte{1, 5, 0x44, 0x33, 0x22, 0x11, 0},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, messageID, err := test.encode()
			if err != nil {
				t.Fatal(err)
			}
			if messageID != opcode.EntityActionRequest {
				t.Fatalf("message ID = 0x%04X, want 0x%04X", messageID, opcode.EntityActionRequest)
			}
			if !bytes.Equal(got, test.want) {
				t.Fatalf("payload = %x, want %x", got, test.want)
			}
		})
	}
}
