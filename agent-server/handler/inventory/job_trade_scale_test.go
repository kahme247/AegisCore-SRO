package inventory

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestSpecialtyCOSSellPublishesPostSaleTradeScale(t *testing.T) {
	request := vsro188.COSSellRequest{Source: 3, Amount: 2}
	packets := buildCOSSellResultPackets(request, model.EconomyResult{Gold: 99, BuybackSlot: 4, Specialty: true, TradeScale: 3, SafeTradeReset: true, JobAdvanced: true, Job: model.JobSnapshot{Type: model.JobTrader, Level: 2, Experience: 123}})
	if len(packets) != 5 {
		t.Fatalf("specialty COS sale packets = %d, want 5", len(packets))
	}
	if packets[2].MessageID != opcode.JobUpdateTradeScale {
		t.Fatalf("third opcode = 0x%04X, want 0x%04X", packets[2].MessageID, opcode.JobUpdateTradeScale)
	}
	if packets[3].MessageID != opcode.JobUpdateSafeTrade || !bytes.Equal(packets[3].Payload(), []byte{0, 0x40}) {
		t.Fatalf("safe-trade reset packet = %#v", packets)
	}
	if packets[4].MessageID != opcode.JobUpdateExperience || !bytes.Equal(packets[4].Payload(), []byte{1, 2, 123, 0, 0, 0}) {
		t.Fatalf("job progression packet = %#v", packets[4])
	}
	if got := packets[2].Payload(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("trade-scale payload = %x, want 03", got)
	}

	ordinary := buildCOSSellResultPackets(request, model.EconomyResult{Gold: 99, BuybackSlot: 4})
	if len(ordinary) != 2 {
		t.Fatalf("ordinary COS sale packets = %d, want 2", len(ordinary))
	}
}
