package model

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestWriteCharacterSpawnBuffsMatchesNativeOwnerAndObserverLayouts(t *testing.T) {
	previousSkills := RefSkills
	t.Cleanup(func() { RefSkills = previousSkills })
	atfe := RefSkill{ID: 101}
	atfe.Params[0] = 1701213281
	RefSkills = map[uint32]RefSkill{100: {ID: 100}, 101: atfe}
	now := time.Unix(1000, 0)
	player := &Player{SRObject: SRObject{UniqueID: 77}, ActiveBuffs: map[uint32]ActiveBuff{
		10: {SkillID: 100, Token: 10, SourceUID: 88, ExpiresAt: now.Add(5 * time.Second)},
		5:  {SkillID: 101, Token: 5, SourceUID: 77, ExpiresAt: now.Add(3 * time.Second)},
		20: {SkillID: 100, Token: 20, SourceUID: 77, ExpiresAt: now.Add(-time.Second)},
	}}

	observer := network.EmptyPacket()
	WriteCharacterSpawnBuffs(&observer, player, false, now)
	wantObserver := []byte{2, 101, 0, 0, 0, 5, 0, 0, 0, 1, 100, 0, 0, 0, 10, 0, 0, 0}
	if got := observer.Payload(); string(got) != string(wantObserver) {
		t.Fatalf("observer buffs = %x, want %x", got, wantObserver)
	}

	owner := network.EmptyPacket()
	WriteCharacterSpawnBuffs(&owner, player, true, now)
	payload := owner.Payload()
	if len(payload) != 26 || payload[0] != 2 || binary.LittleEndian.Uint32(payload[1:5]) != 101 ||
		binary.LittleEndian.Uint32(payload[5:9]) != 5 || binary.LittleEndian.Uint32(payload[9:13]) != 3000 || payload[13] != 1 ||
		binary.LittleEndian.Uint32(payload[14:18]) != 100 || binary.LittleEndian.Uint32(payload[18:22]) != 10 ||
		binary.LittleEndian.Uint32(payload[22:26]) != 5000 {
		t.Fatalf("owner buffs = %x", payload)
	}
}

func TestWriteGuildPublishesPlayerAffiliationSnapshot(t *testing.T) {
	player := &Player{}
	player.SetGuildAffiliation(PlayerGuildAffiliation{
		ID: 7, Name: "Aegis", MemberNickname: "Lead", CrestRevision: 11,
		UnionID: 9, UnionCrestRevision: 13, SiegeAuthority: 2,
	})
	packet := network.EmptyPacket()
	WriteGuild(&packet, player)
	payload := packet.Payload()
	if len(payload) != 31 || binary.LittleEndian.Uint16(payload[0:2]) != 5 ||
		string(payload[2:7]) != "Aegis" || binary.LittleEndian.Uint32(payload[7:11]) != 7 ||
		binary.LittleEndian.Uint16(payload[11:13]) != 4 || string(payload[13:17]) != "Lead" ||
		binary.LittleEndian.Uint32(payload[17:21]) != 11 || binary.LittleEndian.Uint32(payload[21:25]) != 9 ||
		binary.LittleEndian.Uint32(payload[25:29]) != 13 || payload[29] != 0 || payload[30] != 2 {
		t.Fatalf("guild spawn payload = %x", payload)
	}
}
