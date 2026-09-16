package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestCompetitiveRequestLayouts(t *testing.T) {
	freePVP := network.EmptyClientPacket()
	freePVP.WriteByte(FRPVPModeBlue)
	if mode, err := DecodeFRPVPUpdateRequest(&freePVP); err != nil || mode != FRPVPModeBlue {
		t.Fatalf("free PVP = %d, %v", mode, err)
	}
	register := network.EmptyClientPacket()
	register.WriteByte(1)
	register.WriteByte(2)
	register.WriteByte(3)
	if request, err := DecodeBattleArenaRequest(&register); err != nil || request != (BattleArenaRequest{Register: true, GameType: 2, MatchType: 3}) {
		t.Fatalf("register = %#v, %v", request, err)
	}
	cancel := network.EmptyClientPacket()
	cancel.WriteByte(2)
	if request, err := DecodeBattleArenaRequest(&cancel); err != nil || request.Register {
		t.Fatalf("cancel = %#v, %v", request, err)
	}
	empty := network.EmptyClientPacket()
	if err := DecodeFlagWarRegisterRequest(&empty); err != nil {
		t.Fatalf("Flag War register: %v", err)
	}
}

func TestCompetitiveResponseLayoutsMatchClientParsers(t *testing.T) {
	freePVP, err := BuildFRPVPUpdateSuccess(0x12345678, FRPVPModeRed)
	if err != nil || freePVP.MessageID != opcode.FRPVPUpdateResponse || hex.EncodeToString(freePVP.Payload()) != "017856341201" {
		t.Fatalf("free PVP success = %04x/%x, %v", freePVP.MessageID, freePVP.Payload(), err)
	}
	failure := BuildFRPVPUpdateFailure(0x8007)
	if hex.EncodeToString(failure.Payload()) != "020780" {
		t.Fatalf("free PVP failure = %x", failure.Payload())
	}
	begin, err := BuildFlagWarUpdate(FlagWarBegin)
	if err != nil || begin.MessageID != opcode.FlagWarUpdate || hex.EncodeToString(begin.Payload()) != "04" {
		t.Fatalf("Flag War begin = %04x/%x, %v", begin.MessageID, begin.Payload(), err)
	}
	result := BuildFlagWarOperationResult(0x1234)
	if hex.EncodeToString(result.Payload()) != "ff3412" {
		t.Fatalf("Flag War result = %x", result.Payload())
	}
	unavailable := BuildFlagWarUnavailable()
	if hex.EncodeToString(unavailable.Payload()) != "ff0100" {
		t.Fatalf("Flag War unavailable = %x", unavailable.Payload())
	}
	arenaUnavailable, err := BuildBattleArenaUnavailable()
	if err != nil || arenaUnavailable.MessageID != opcode.BattleArenaOperation || hex.EncodeToString(arenaUnavailable.Payload()) != "ff04" {
		t.Fatalf("Battle Arena unavailable = %04x/%x, %v", arenaUnavailable.MessageID, arenaUnavailable.Payload(), err)
	}
}

func TestCompetitiveCodecsRejectMalformedPackets(t *testing.T) {
	badMode := network.EmptyClientPacket()
	badMode.WriteByte(6)
	if _, err := DecodeFRPVPUpdateRequest(&badMode); err == nil {
		t.Fatal("expected invalid free PVP mode")
	}
	extraFlagWar := network.EmptyClientPacket()
	extraFlagWar.WriteByte(1)
	if err := DecodeFlagWarRegisterRequest(&extraFlagWar); err == nil {
		t.Fatal("expected invalid Flag War payload")
	}
	if _, err := BuildFlagWarUpdate(0xff); err == nil {
		t.Fatal("expected operation-result builder requirement")
	}
}

func TestBattleArenaOperationLayoutsMatchClientParser(t *testing.T) {
	start, err := BuildBattleArenaOperation(BattleArenaOperation{Operation: 8, MaxTime: 120000})
	if err != nil || start.MessageID != opcode.BattleArenaOperation || hex.EncodeToString(start.Payload()) != "08c0d40100" {
		t.Fatalf("start = %04x/%x, %v", start.MessageID, start.Payload(), err)
	}
	ranks, err := BuildBattleArenaOperation(BattleArenaOperation{
		Operation:  0xff,
		Update:     0x41,
		RedPoints:  10,
		BluePoints: 20,
		Ranks:      []BattleArenaRank{{Team: 1, Name: "Aegis", Points: 30}},
	})
	if err != nil || hex.EncodeToString(ranks.Payload()) != "ff410a000000140000000101050041656769731e000000" {
		t.Fatalf("ranks = %x, %v", ranks.Payload(), err)
	}
	flag, err := BuildBattleArenaOperation(BattleArenaOperation{Operation: 0xff, Update: 0x84, Team: 2, Name: "Blue"})
	if err != nil || hex.EncodeToString(flag.Payload()) != "ff84020400426c7565" {
		t.Fatalf("flag = %x, %v", flag.Payload(), err)
	}
	timer, err := BuildBattleArenaOperation(BattleArenaOperation{Operation: 0xff, Update: 0xf0, MaxTime: 1000, ElapsedTime: 250, First: 0, Second: 1})
	if err != nil || hex.EncodeToString(timer.Payload()) != "fff0e8030000fa0000000001" {
		t.Fatalf("timer = %x, %v", timer.Payload(), err)
	}
}
