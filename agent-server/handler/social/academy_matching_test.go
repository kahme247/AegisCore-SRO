package social

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type academyMatchingWorldFake struct{ player *model.Player }

func (w academyMatchingWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return w.player, nil
}

func (w academyMatchingWorldFake) GetPlayerByCharacterID(id int) (*model.Player, error) {
	if w.player == nil || w.player.ID != id {
		return nil, errors.New("character not found")
	}
	return w.player, nil
}

func TestAcademyMatchingListSerializesLeaderRuntimeUID(t *testing.T) {
	leader := &model.Player{ID: 42, SRObject: model.SRObject{UniqueID: 0x11223344}}
	packet := buildAcademyMatchingListResponse([]model.AcademyMatch{{
		AcademyID: 7, LeaderCharacter: 42, CountryType: 1, Title: "Mentors", DisplayLevel: 2,
		LeaderLevel: 80, LeaderName: "Master", GraduatedCount: 3, HonorRank: 4,
	}}, 1, 0, nil, academyMatchingWorldFake{player: leader})
	if packet.MessageID != opcode.AcademyMatchingListResponse {
		t.Fatalf("opcode = 0x%04X", packet.MessageID)
	}
	reader := packet
	if _, err := reader.ReadByte(); err != nil { // result
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != nil { // page count
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != nil { // page index
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != nil { // match count
		t.Fatal(err)
	}
	if _, err := reader.ReadUInt32(); err != nil { // academy ID
		t.Fatal(err)
	}
	if _, err := reader.ReadUInt32(); err != nil { // unknown
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadString(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadUInt32(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadUInt32(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != nil {
		t.Fatal(err)
	}
	uid, err := reader.ReadUInt32()
	if err != nil || uid != 0x11223344 {
		t.Fatalf("leader UID = 0x%08X, err=%v", uid, err)
	}
	if reader.Remaining() == 0 {
		t.Fatal("leader name and ranking tail missing")
	}
}
