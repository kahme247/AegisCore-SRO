package vsro188

import (
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestJobRequestCodecsMatchStockSenderWidths(t *testing.T) {
	join := network.EmptyClientPacket()
	join.WriteUInt32(100000)
	join.WriteByte(JobHunter)
	decodedJoin, err := (JobJoinRequestCodec{}).Decode(join)
	if err != nil || decodedJoin.NPCUID != 100000 || decodedJoin.JobType != JobHunter {
		t.Fatalf("join decode = %+v, %v", decodedJoin, err)
	}

	ranking := network.EmptyClientPacket()
	ranking.WriteUInt32(100000)
	ranking.WriteByte(JobTrader)
	ranking.WriteByte(1)
	decodedRanking, err := (JobRankingRequestCodec{}).Decode(ranking)
	if err != nil || decodedRanking.JobType != JobTrader || decodedRanking.Kind != 1 {
		t.Fatalf("ranking decode = %+v, %v", decodedRanking, err)
	}

	for _, size := range []int{0, 4, 6} {
		packet := network.EmptyClientPacket()
		for index := 0; index < size; index++ {
			packet.WriteByte(0)
		}
		if _, err := (JobJoinRequestCodec{}).Decode(packet); err == nil {
			t.Fatalf("join accepted %d bytes", size)
		}
	}
}

func TestJobUpdateCodecMatchesBinaryReadOrder(t *testing.T) {
	packet, err := (JobUpdateCodec{}).Encode(JobState{Type: JobThief, Level: 3, Experience: 0x12345678})
	if err != nil {
		t.Fatal(err)
	}
	if packet.MessageID != opcode.JobUpdateExperience {
		t.Fatalf("opcode = 0x%04X", packet.MessageID)
	}
	if got, want := packet.Payload(), []byte{JobThief, 3, 0x78, 0x56, 0x34, 0x12}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestJobResponseCodecsMatchBinaryConditionalBranches(t *testing.T) {
	aliasCheck := network.EmptyPacket()
	aliasCheck.WriteByte(1)
	if err := (JobAliasResponseCodec{}).AppendSuccess(&aliasCheck, 0, "Alias"); err != nil {
		t.Fatal(err)
	}
	if got, want := aliasCheck.Payload(), []byte{1, 0, 5, 0, 'A', 'l', 'i', 'a', 's'}; !reflect.DeepEqual(got, want) {
		t.Fatalf("alias check = %x, want %x", got, want)
	}

	active := network.EmptyPacket()
	active.WriteByte(1)
	request := JobRankingRequest{JobType: JobHunter, Kind: 0}
	entries := []JobRankingEntry{{Rank: 1, Alias: "Aegis", Level: 4, Experience: 900, CompatibilityByte: 7}}
	if err := (JobRankingResponseCodec{}).AppendSuccess(&active, request, entries); err != nil {
		t.Fatal(err)
	}
	if got := active.Payload(); len(got) != 18 || got[len(got)-1] != 7 {
		t.Fatalf("active ranking payload = %x", got)
	}

	contribution := network.EmptyPacket()
	contribution.WriteByte(1)
	request.Kind = 1
	if err := (JobRankingResponseCodec{}).AppendSuccess(&contribution, request, entries); err != nil {
		t.Fatal(err)
	}
	if got := contribution.Payload(); len(got) != 17 {
		t.Fatalf("contribution ranking payload = %x", got)
	}
}

func TestJobFailureCodecsAppendStockContextByte(t *testing.T) {
	alias := network.EmptyPacket()
	alias.WriteByte(2)
	alias.WriteUInt16(0x4A05)
	if err := (JobAliasResponseCodec{}).AppendFailure(&alias, 1, "Alias"); err != nil {
		t.Fatal(err)
	}
	if got, want := alias.Payload(), []byte{2, 0x05, 0x4A, 1, 5, 0, 'A', 'l', 'i', 'a', 's'}; !reflect.DeepEqual(got, want) {
		t.Fatalf("alias failure = %x, want %x", got, want)
	}

	outcome := network.EmptyPacket()
	outcome.WriteByte(2)
	outcome.WriteUInt16(0x4A06)
	if err := (JobOutcomeResponseCodec{}).AppendFailure(&outcome, 7); err != nil {
		t.Fatal(err)
	}
	if got, want := outcome.Payload(), []byte{2, 0x06, 0x4A, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("outcome failure = %x, want %x", got, want)
	}
}

func TestJobPreviousInfoUsesRecoveredThirteenByteTail(t *testing.T) {
	packet := network.EmptyPacket()
	packet.WriteByte(1)
	err := (JobPreviousInfoResponseCodec{}).AppendSuccess(&packet, JobPreviousInfo{
		Current:              JobState{Type: JobTrader, Level: 2, Experience: 100},
		PreviousType:         JobThief,
		PreviousExperience:   200,
		PreviousContribution: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := packet.Payload(); len(got) != 16 {
		t.Fatalf("result plus previous-info tail length = %d, want 16: %x", len(got), got)
	}
}

func TestJobPreviousInfoAllowsWithdrawnCharacter(t *testing.T) {
	packet := network.EmptyPacket()
	packet.WriteByte(1)
	err := (JobPreviousInfoResponseCodec{}).AppendSuccess(&packet, JobPreviousInfo{
		Current:              JobState{Type: JobNone},
		PreviousType:         JobTrader,
		PreviousExperience:   1234,
		PreviousContribution: 5678,
	})
	if err != nil {
		t.Fatalf("encode withdrawn previous info: %v", err)
	}
	if got, want := packet.Payload(), []byte{1, JobNone, 0, 0, 0, 0, 0, 0xd2, 0x04, 0, 0, JobTrader, 0x2e, 0x16, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("withdrawn previous info payload = %x, want %x", got, want)
	}
}
