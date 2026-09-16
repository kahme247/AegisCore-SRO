package vsro188

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestGuildWarProposalRoundTripMatchesClientLayout(t *testing.T) {
	want := GuildWarProposal{GuildName: "Aegis", Mode: 0, WarPeriodID: 10 << 20, VictoryPointID: 2, Wager: 50_000_000}
	packet := BuildGuildWarProposal(want)
	if packet.MessageID != opcode.GuildWarRequest {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.GuildWarRequest)
	}
	if got := hex.EncodeToString(packet.Payload()); got != "05004165676973000000a0000280f0fa02" {
		t.Fatalf("payload = %s", got)
	}
	reader := packet
	got, err := DecodeGuildWarProposal(&reader)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decoded = %#v, want %#v", got, want)
	}
}

func TestGuildWarPeriodDecodesClientBitPacking(t *testing.T) {
	// 2 days, 3 hours, 40 minutes: day<<10 | hour<<15 | minute<<20.
	duration, unlimited, err := DecodeGuildWarPeriod(2<<10 | 3<<15 | 40<<20)
	if err != nil || unlimited || duration != 51*time.Hour+40*time.Minute {
		t.Fatalf("duration = %v, unlimited = %v, error = %v", duration, unlimited, err)
	}
	if _, unlimited, err = DecodeGuildWarPeriod(GuildWarUnlimitedPeriod); err != nil || !unlimited {
		t.Fatalf("unlimited = %v, error = %v", unlimited, err)
	}
	for _, invalid := range []uint32{0, 31 << 10, 24 << 15, 60 << 20, 1} {
		if _, _, err = DecodeGuildWarPeriod(invalid); err == nil {
			t.Fatalf("period 0x%08X accepted", invalid)
		}
	}
}

func TestGuildWarProposalRejectsInvalidModeWagerAndTrailingData(t *testing.T) {
	tests := []GuildWarProposal{
		{GuildName: "Aegis", Mode: 1},
		{GuildName: "Aegis", Wager: GuildWarMaximumWager + 1},
	}
	for _, proposal := range tests {
		packet := BuildGuildWarProposal(proposal)
		if _, err := DecodeGuildWarProposal(&packet); err == nil {
			t.Fatalf("proposal %#v unexpectedly accepted", proposal)
		}
	}
	packet := BuildGuildWarProposal(GuildWarProposal{GuildName: "Aegis"})
	packet.WriteByte(1)
	if _, err := DecodeGuildWarProposal(&packet); err == nil {
		t.Fatal("proposal with trailing byte unexpectedly accepted")
	}
}

func TestGuildWarResultEnvelopesMatchClientHandlers(t *testing.T) {
	tests := []struct {
		name    string
		packet  func(uint16) string
		success string
		failure string
	}{
		{"start", func(code uint16) string {
			packet := BuildGuildWarStartResult(code)
			return hex.EncodeToString(packet.Payload())
		}, "01", "023412"},
		{"end", func(code uint16) string {
			packet := BuildGuildWarEndResult(code)
			return hex.EncodeToString(packet.Payload())
		}, "01", "023412"},
	}
	for _, test := range tests {
		if got := test.packet(0); got != test.success {
			t.Fatalf("%s success = %s, want %s", test.name, got, test.success)
		}
		if got := test.packet(0x1234); got != test.failure {
			t.Fatalf("%s failure = %s, want %s", test.name, got, test.failure)
		}
	}
}

func TestGuildWarEndRequestMatchesClientLayout(t *testing.T) {
	packet := BuildGuildWarEndRequest(0x11223344)
	if packet.MessageID != opcode.GuildWarEndRequest || hex.EncodeToString(packet.Payload()) != "44332211" {
		t.Fatalf("end request = opcode 0x%04X payload %X", packet.MessageID, packet.Payload())
	}
	reader := packet
	warID, err := DecodeGuildWarEndRequest(&reader)
	if err != nil || warID != 0x11223344 {
		t.Fatalf("decoded war id = 0x%08X, error = %v", warID, err)
	}
	invalid := BuildGuildWarEndRequest(0)
	if _, err = DecodeGuildWarEndRequest(&invalid); err == nil {
		t.Fatal("zero war id unexpectedly accepted")
	}
}

func TestGuildWarInfoMatchesClientParserLayout(t *testing.T) {
	packet, err := BuildGuildWarInfo(GuildWarInfo{
		WarID: 0x11223344, WarPeriodID: 0x55667788, VictoryPointID: 0xaa,
		Wager: 0x01020304, ChallengerGuildID: 0x0a0b0c0d, TargetGuildID: 0x11121314,
		ChallengerScore: 0x21222324, TargetScore: 0x31323334,
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.MessageID != opcode.GuildWarInfo {
		t.Fatalf("opcode = 0x%04X", packet.MessageID)
	}
	if got, want := hex.EncodeToString(packet.Payload()), "014433221188776655aa040302010d0c0b0a141312112423222134333231"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestGuildWarRewardMatchesClientLayout(t *testing.T) {
	request := BuildGuildWarRewardRequest(0x11223344)
	if request.MessageID != opcode.GuildWarRewardRequest || hex.EncodeToString(request.Payload()) != "44332211" {
		t.Fatalf("request = %#v", request)
	}
	reader := request
	warID, err := DecodeGuildWarRewardRequest(&reader)
	if err != nil || warID != 0x11223344 {
		t.Fatalf("war id = %#x, error = %v", warID, err)
	}
	success := BuildGuildWarRewardResult(1_000_000_000, 0)
	if got := hex.EncodeToString(success.Payload()); got != "0100ca9a3b" {
		t.Fatalf("success = %s", got)
	}
	failure := BuildGuildWarRewardResult(0, 0x1234)
	if got := hex.EncodeToString(failure.Payload()); got != "023412" {
		t.Fatalf("failure = %s", got)
	}
}

func TestGuildWarCompensationClaimMatchesClientLayout(t *testing.T) {
	request := BuildGuildWarCompensationClaimRequest(0x11223344)
	if request.MessageID != opcode.GuildWarCompensationClaimRequest || hex.EncodeToString(request.Payload()) != "44332211" {
		t.Fatalf("request = %#v", request)
	}
	reader := request
	reward, err := DecodeGuildWarCompensationClaimRequest(&reader)
	if err != nil || reward != 0x11223344 {
		t.Fatalf("reward = %#x, error = %v", reward, err)
	}
	success := BuildGuildWarCompensationClaimResult(0)
	if got := hex.EncodeToString(success.Payload()); got != "01" {
		t.Fatalf("success = %s", got)
	}
	failure := BuildGuildWarCompensationClaimResult(0x1234)
	if got := hex.EncodeToString(failure.Payload()); got != "023412" {
		t.Fatalf("failure = %s", got)
	}
}
