package character

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type hwanTestWorld struct{ player *model.Player }

func (w hwanTestWorld) GetPlayerByUniqueId(uint32) (*model.Player, error) { return w.player, nil }

type hwanTestRepository struct {
	snapshot model.HwanSnapshot
	consumed byte
}

func (r *hwanTestRepository) Load(context.Context, int) (model.HwanSnapshot, error) {
	return r.snapshot, nil
}
func (r *hwanTestRepository) ConsumeBerserk(context.Context, int) (model.HwanSnapshot, byte, error) {
	return r.snapshot, r.consumed, nil
}
func (r *hwanTestRepository) AwardPoints(context.Context, int, byte) (model.HwanSnapshot, bool, error) {
	return r.snapshot, false, nil
}
func (r *hwanTestRepository) SetLevel(context.Context, int, byte) error { return nil }

func TestHwanHandlerActivatesAndExpiresBerserk(t *testing.T) {
	session := &server.Session{}
	session.UserContext.UniqueID = 77
	player := &model.Player{ID: 9, Session: session, BodyState: model.Normal, LifeState: model.Alive}
	player.SetUniqueID(77)
	repository := &hwanTestRepository{snapshot: model.HwanSnapshot{Points: 0, Level: 4}, consumed: 4}
	var sent []network.Packet
	var expire func()
	handler := &HwanHandler{
		world: hwanTestWorld{player: player}, repository: repository,
		now: func() time.Time { return time.Unix(100, 0) },
		schedule: func(delay time.Duration, fn func()) {
			if delay != 48*time.Second {
				t.Fatalf("delay = %s", delay)
			}
			expire = fn
		},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}
	request := network.EmptyPacket()
	request.WriteByte(1)
	if err := handler.handleBodyState(context.Background(), session, request); err != nil {
		t.Fatalf("activate Berserk: %v", err)
	}
	if player.GetBodyState() != model.Berserker || expire == nil {
		t.Fatal("Berserk runtime state was not activated")
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.CharacterBodyStateResponse || sent[1].MessageID != opcode.EntityUpdatePoints {
		t.Fatalf("sent packets = %#v", sent)
	}
	if payload := sent[0].Payload(); len(payload) != 1 || payload[0] != 1 {
		t.Fatalf("B0A7 payload = %x", payload)
	}
	expire()
	if player.GetBodyState() != model.Normal {
		t.Fatal("Berserk did not expire")
	}
}

func TestHwanHandlerRejectsPrivilegedBodyState(t *testing.T) {
	var sent network.Packet
	handler := &HwanHandler{send: func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }}
	request := network.EmptyPacket()
	request.WriteByte(2)
	if err := handler.handleBodyState(context.Background(), &server.Session{}, request); err != nil {
		t.Fatalf("reject body state: %v", err)
	}
	if sent.MessageID != opcode.CharacterBodyStateResponse || len(sent.Payload()) != 3 || sent.Payload()[0] != 2 {
		t.Fatalf("failure response = opcode %#x payload %x", sent.MessageID, sent.Payload())
	}
}
