package vsro188

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestFortressExpenditureHistoryMatchesBinaryLayout(t *testing.T) {
	request := network.EmptyClientPacket()
	if err := DecodeFortressExpenditureHistoryRequest(&request); err != nil {
		t.Fatal(err)
	}
	response, err := BuildFortressExpenditureHistory([]FortressExpenditureEntry{{
		Timestamp: time.Unix(0x11223344, 0).UTC(), CharacterName: "Aegis", Amount: 500000, Reason: 6,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if response.MessageID != opcode.GuildGPHistoryResponse || hex.EncodeToString(response.Payload()) != "0101443322110500416567697320a1070006" {
		t.Fatalf("response = %04x:%s", response.MessageID, hex.EncodeToString(response.Payload()))
	}
	failure := BuildFortressExpenditureHistoryFailure(0x4c0d)
	if hex.EncodeToString(failure.Payload()) != "020d4c" {
		t.Fatalf("failure = %s", hex.EncodeToString(failure.Payload()))
	}
}

func TestFortressExpenditureHistoryRejectsInvalidEntries(t *testing.T) {
	if _, err := BuildFortressExpenditureHistory([]FortressExpenditureEntry{{Timestamp: time.Now(), CharacterName: "A", Amount: 1, Reason: 9}}); err == nil {
		t.Fatal("expected invalid reason rejection")
	}
	request := network.EmptyClientPacket()
	request.WriteByte(1)
	if err := DecodeFortressExpenditureHistoryRequest(&request); err == nil {
		t.Fatal("expected non-empty request rejection")
	}
}
