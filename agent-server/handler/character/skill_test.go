package character

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type skillUseCaseFake struct {
	result model.SkillLearnResult
	err    error
}

func (f *skillUseCaseFake) LearnSkill(context.Context, *model.Player, uint32) (model.SkillLearnResult, error) {
	return f.result, f.err
}

func TestSkillHandlerEmitsLearnAndSkillPointPackets(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 9, Session: session}
	handler := &SkillHandler{world: &selectEntityWorldFake{player: player}, useCase: &skillUseCaseFake{result: model.SkillLearnResult{Skill: model.CharacterSkill{ID: 0x01020304, Enabled: 1}, SkillPoints: 9}}}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	request := network.EmptyClientPacket()
	request.WriteUInt32(0x01020304)

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.SkillLearnResponse || !bytes.Equal(sent[0].Payload(), []byte{1, 4, 3, 2, 1}) {
		t.Fatalf("skill packets = %#v", sent)
	}
	if sent[1].MessageID != opcode.EntityUpdatePoints || !bytes.Equal(sent[1].Payload(), []byte{2, 9, 0, 0, 0, 0}) {
		t.Fatalf("SP packet = 0x%04X %x", sent[1].MessageID, sent[1].Payload())
	}
}

func TestSkillErrorPacketMappings(t *testing.T) {
	if got := buildSkillFailure(skillErrorCode(model.ErrSkillStrengthRequired)); !bytes.Equal(got.Payload(), []byte{2, 0x34, 0x03}) {
		t.Fatalf("strength failure = %x", got.Payload())
	}
	if got := buildSkillFailure(skillErrorCode(model.ErrSkillInsufficientSP)); !bytes.Equal(got.Payload(), []byte{2, 0x34, 0x0A}) {
		t.Fatalf("SP failure = %x", got.Payload())
	}
	for _, test := range []struct {
		err  error
		code byte
	}{{model.ErrSkillRaceMismatch, 0x05}, {model.ErrSkillLevelChainInvalid, 0x06}, {model.ErrSkillAlreadyLearned, 0x0C}} {
		if got := buildSkillFailure(skillErrorCode(test.err)); !bytes.Equal(got.Payload(), []byte{2, 0x34, test.code}) {
			t.Fatalf("failure %v = %x", test.err, got.Payload())
		}
	}
}
