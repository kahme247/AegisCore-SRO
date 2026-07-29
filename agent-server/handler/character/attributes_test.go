package character

import (
	"context"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

type attributeRepositoryFake struct{ spent bool }

func (f *attributeRepositoryFake) SpendStatPoint(context.Context, int, model.CharacterAttribute) (bool, error) {
	return f.spent, nil
}

type attributeWorldFake struct{ player *model.Player }

func (f *attributeWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) { return f.player, nil }

func TestAttributeIncreaseSendsCalculatedStatsBeforeSuccess(t *testing.T) {
	session := &server.Session{}
	session.UserContext.UniqueID = 99
	player := &model.Player{ID: 7, Session: session, Level: 2, StatPoints: 1, BaseStats: model.BaseStats{HP: 200, MP: 200, Str: 20, Int: 20}}
	var sent []network.Packet
	handler := &AttributeHandler{
		world:      &attributeWorldFake{player: player},
		repository: &attributeRepositoryFake{spent: true},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}

	if err := handler.increaseStrength(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.EntityUpdateStats || sent[1].MessageID != opcode.CharacterIncreaseStrengthResponse {
		t.Fatalf("packet order = %#v, want 303D then B050", sent)
	}
	stats := sent[0]
	for i := 0; i < 4; i++ {
		if _, err := stats.ReadUInt32(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		if _, err := stats.ReadUInt16(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stats.ReadUInt32(); err != nil {
		t.Fatal(err)
	}
	if _, err := stats.ReadUInt32(); err != nil {
		t.Fatal(err)
	}
	strength, err := stats.ReadUInt16()
	if err != nil || strength != 21 {
		t.Fatalf("strength = %d, want 21: %v", strength, err)
	}
	if player.StatPoints != 0 || player.BaseStats.Str != 21 {
		t.Fatalf("runtime stats = points %d str %d", player.StatPoints, player.BaseStats.Str)
	}
}
