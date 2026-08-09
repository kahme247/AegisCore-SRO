package character

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type masteryRepositoryFake struct {
	result        model.MasteryIncreaseResult
	err           error
	calls         int
	expectedLevel byte
	nextLevel     byte
	totalLimit    int
	cost          uint64
}

func (f *masteryRepositoryFake) LoadMasteries(context.Context, int) (map[uint32]byte, error) {
	return nil, nil
}

func (f *masteryRepositoryFake) IncreaseMastery(_ context.Context, _ int, _ uint32, expectedLevel, nextLevel byte, totalLimit int, cost uint64) (model.MasteryIncreaseResult, error) {
	f.calls++
	f.expectedLevel = expectedLevel
	f.nextLevel = nextLevel
	f.totalLimit = totalLimit
	f.cost = cost
	return f.result, f.err
}

func TestMasteryHandlerPersistsThenEmitsStockPackets(t *testing.T) {
	levels, err := model.LoadLevelTable(strings.NewReader("1 118 1 0 0 24\n2 470 1 0 0 47\n3 1058 1 0 0 71\n4 1880 2 0 0 94\n"))
	if err != nil {
		t.Fatalf("load levels: %v", err)
	}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 9}}
	player := &model.Player{
		SRObject:    model.SRObject{UniqueID: 77, RefObjectID: 1907},
		Session:     session,
		ID:          9,
		Level:       10,
		SkillPoints: 20,
		Masteries:   map[uint32]byte{257: 3},
	}
	repository := &masteryRepositoryFake{result: model.MasteryIncreaseResult{
		Mastery: model.MasteryProgress{ID: 257, Level: 4}, SkillPoints: 18,
	}}
	handler := &MasteryHandler{repository: repository, levels: levels, world: &selectEntityWorldFake{player: player}}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteUInt32(257)
	request.WriteByte(1)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	if repository.calls != 1 || repository.expectedLevel != 3 || repository.nextLevel != 4 || repository.totalLimit != 330 || repository.cost != 2 {
		t.Fatalf("repository call = %+v", repository)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d packets, want 2", len(sent))
	}
	if sent[0].MessageID != opcode.SkillMasteryLearnResponse || !bytes.Equal(sent[0].Payload(), []byte{1, 1, 1, 0, 0, 4}) {
		t.Fatalf("mastery response = 0x%04X %x", sent[0].MessageID, sent[0].Payload())
	}
	if sent[1].MessageID != opcode.EntityUpdatePoints || !bytes.Equal(sent[1].Payload(), []byte{2, 18, 0, 0, 0, 0}) {
		t.Fatalf("SP response = 0x%04X %x", sent[1].MessageID, sent[1].Payload())
	}
	if level, ok := player.MasteryLevel(257); !ok || level != 4 || player.SkillPoints != 18 {
		t.Fatalf("player mastery/SP = %d/%d, exists=%t", level, player.SkillPoints, ok)
	}
}

func TestMasteryHandlerRejectsInvalidIncrementWithoutMutation(t *testing.T) {
	levels, err := model.LoadLevelTable(strings.NewReader("1 118 1 0 0 24\n"))
	if err != nil {
		t.Fatal(err)
	}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 9}}
	repository := &masteryRepositoryFake{}
	handler := &MasteryHandler{repository: repository, levels: levels, world: &selectEntityWorldFake{}}
	var sent network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteUInt32(257)
	request.WriteByte(2)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	if repository.calls != 0 {
		t.Fatalf("repository called %d times", repository.calls)
	}
	if sent.MessageID != opcode.SkillMasteryLearnResponse || !reflect.DeepEqual(sent.Payload(), []byte{2, 0x38, 0x01}) {
		t.Fatalf("failure response = 0x%04X %x", sent.MessageID, sent.Payload())
	}
}

func TestMasteryHandlerChargesUnlockedLevel(t *testing.T) {
	levels, err := model.LoadLevelTable(strings.NewReader("1 118 1 0 0 24\n"))
	if err != nil {
		t.Fatal(err)
	}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 9}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77, RefObjectID: 1907}, Session: session,
		ID: 9, Level: 1, Masteries: map[uint32]byte{257: 0},
	}
	repository := &masteryRepositoryFake{result: model.MasteryIncreaseResult{Mastery: model.MasteryProgress{ID: 257, Level: 1}}}
	handler := &MasteryHandler{repository: repository, levels: levels, world: &selectEntityWorldFake{player: player}}
	handler.send = func(_ *server.Session, _ network.Packet) error { return nil }
	request := network.EmptyClientPacket()
	request.WriteUInt32(257)
	request.WriteByte(1)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if repository.cost != 1 {
		t.Fatalf("first mastery level cost = %d, want 1", repository.cost)
	}
}

func TestMasteryPacketLayouts(t *testing.T) {
	if got := buildMasterySuccess(model.MasteryProgress{ID: 0x01020304, Level: 5}); !bytes.Equal(got.Payload(), []byte{1, 4, 3, 2, 1, 5}) {
		t.Fatalf("success payload = %x", got.Payload())
	}
	if got := buildMasteryFailure(masteryErrorInsufficient); !bytes.Equal(got.Payload(), []byte{2, 0x38, 0x02}) {
		t.Fatalf("failure payload = %x", got.Payload())
	}
}
