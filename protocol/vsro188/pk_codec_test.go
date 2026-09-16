package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestPKUpdateLayoutsMatchClientParsers(t *testing.T) {
	tests := []struct {
		name    string
		opcode  uint16
		payload string
	}{
		{name: "penalty", opcode: opcode.PKUpdatePenalty, payload: "c01dfeff"},
		{name: "daily", opcode: opcode.PKUpdateDaily, payload: "07"},
		{name: "level", opcode: opcode.PKUpdateLevel, payload: "85ff"},
	}
	packets := []func() (uint16, string){
		func() (uint16, string) {
			p := BuildPKPenaltyUpdate(-123456)
			return p.MessageID, hex.EncodeToString(p.Payload())
		},
		func() (uint16, string) {
			p := BuildPKDailyUpdate(7)
			return p.MessageID, hex.EncodeToString(p.Payload())
		},
		func() (uint16, string) {
			p := BuildPKLevelUpdate(-123)
			return p.MessageID, hex.EncodeToString(p.Payload())
		},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messageID, payload := packets[i]()
			if messageID != test.opcode || payload != test.payload {
				t.Fatalf("packet = %04x/%s, want %04x/%s", messageID, payload, test.opcode, test.payload)
			}
		})
	}
}
