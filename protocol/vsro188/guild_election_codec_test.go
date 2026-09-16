package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestGuildElectionRequestsMatchClientLayouts(t *testing.T) {
	start := network.EmptyClientPacket()
	start.WriteUInt32(0x11223344)
	if id, err := DecodeGuildElectionRequest(&start); err != nil || id != 0x11223344 {
		t.Fatalf("start id = %#x, error = %v", id, err)
	}
	participate := network.EmptyClientPacket()
	participate.WriteUInt32(0x55667788)
	if id, err := DecodeGuildElectionParticipateRequest(&participate); err != nil || id != 0x55667788 {
		t.Fatalf("candidate id = %#x, error = %v", id, err)
	}
	votePacket := network.EmptyClientPacket()
	votePacket.WriteUInt32(7)
	votePacket.WriteUInt32(9)
	votePacket.WriteByte(1)
	vote, err := DecodeGuildElectionVoteRequest(&votePacket)
	if err != nil || vote.ElectionID != 7 || vote.CandidateID != 9 || vote.Selection != 1 {
		t.Fatalf("vote = %#v, error = %v", vote, err)
	}
}

func TestGuildElectionResultsMatchClientLayouts(t *testing.T) {
	for _, packet := range []network.Packet{BuildGuildElectionResult(0), BuildGuildElectionParticipateResult(0), BuildGuildElectionVoteResult(0)} {
		if got := hex.EncodeToString(packet.Payload()); got != "01" {
			t.Fatalf("success %04x = %s", packet.MessageID, got)
		}
	}
	failure := BuildGuildElectionVoteResult(0x1234)
	if got := hex.EncodeToString(failure.Payload()); got != "023412" {
		t.Fatalf("failure = %s", got)
	}
}

func TestGuildElectionUpdatesMatchClientParser(t *testing.T) {
	tests := []struct {
		packet network.Packet
		want   string
	}{
		{BuildGuildElectionOpened(0x11223344, 2, 0x55667788), "01443322110288776655"},
		{BuildGuildElectionCandidateResult(7, 1, 9), "03070000000109000000"},
		{BuildGuildElectionCandidateResult(7, 2, 0), "030700000002"},
		{BuildGuildElectionCandidateState(7, 1, 2, 3), "0407000000010203"},
		{BuildGuildElectionWinner(7, 9), "050700000009000000"},
	}
	for _, test := range tests {
		if test.packet.MessageID != opcode.GuildElectionUpdate || hex.EncodeToString(test.packet.Payload()) != test.want {
			t.Fatalf("packet = %04x:%s, want %s", test.packet.MessageID, hex.EncodeToString(test.packet.Payload()), test.want)
		}
	}
}

func TestGuildElectionOpenResultMatchesNestedClientParser(t *testing.T) {
	packet, err := BuildGuildElectionOpenResult(7, 9, []GuildElectionCandidate{{CharacterID: 9, Votes: 2}, {CharacterID: 11, Votes: 1}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(packet.Payload()); got != "0107000000090000000209000000020b00000001" {
		t.Fatalf("open result = %s", got)
	}
}
