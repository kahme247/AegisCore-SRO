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

type combatPKRepositoryFake struct {
	characterID int
	state       model.CharacterPKState
}

func TestRecordPlayerKillDoesNotPenalizeFortressBattleScore(t *testing.T) {
	repository := &combatPKRepositoryFake{}
	called := false
	handler := &CombatHandler{pkKills: repository, fortressKill: func(_ context.Context, killerID, victimID int, at time.Time) error {
		called = killerID == 44 && victimID == 45 && !at.IsZero()
		return nil
	}}
	if err := handler.recordPlayerKill(context.Background(), &model.Player{ID: 44}, &model.Player{ID: 45}); err != nil {
		t.Fatalf("record fortress kill: %v", err)
	}
	if !called || repository.characterID != 0 {
		t.Fatalf("fortress scoring called=%t PK character=%d", called, repository.characterID)
	}
}

func (f *combatPKRepositoryFake) RecordKill(_ context.Context, characterID int) (model.CharacterPKState, error) {
	f.characterID = characterID
	return f.state, nil
}

func TestRecordPlayerKillPersistsAndPublishesPKCounters(t *testing.T) {
	repository := &combatPKRepositoryFake{state: model.CharacterPKState{DailyPK: 2, TotalPK: 3, PKPenaltyPoint: 10_000}}
	var sent []network.Packet
	handler := &CombatHandler{pkKills: repository, send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	killer := &model.Player{ID: 44, Session: &server.Session{}}
	target := &model.Player{ID: 45}
	if err := handler.recordPlayerKill(context.Background(), killer, target); err != nil {
		t.Fatalf("record player kill: %v", err)
	}
	if repository.characterID != killer.ID || killer.PKCounters() != repository.state {
		t.Fatalf("PK mutation = character %d, state %#v", repository.characterID, killer.PKCounters())
	}
	want := []uint16{opcode.PKUpdatePenalty, opcode.PKUpdateDaily, opcode.PKUpdateLevel}
	if len(sent) != len(want) {
		t.Fatalf("sent %d PK packets, want %d", len(sent), len(want))
	}
	for index := range want {
		if sent[index].MessageID != want[index] {
			t.Fatalf("packet %d opcode = %04x, want %04x", index, sent[index].MessageID, want[index])
		}
	}
}

func TestRecordPlayerKillDoesNotPenalizeCompetitiveScore(t *testing.T) {
	repository := &combatPKRepositoryFake{}
	handler := &CombatHandler{pkKills: repository, competitiveKill: func(killerID, victimID int) error { return nil }}
	if err := handler.recordPlayerKill(context.Background(), &model.Player{ID: 44}, &model.Player{ID: 45}); err != nil {
		t.Fatalf("record competitive kill: %v", err)
	}
	if repository.characterID != 0 {
		t.Fatalf("competitive kill persisted PK penalty for character %d", repository.characterID)
	}
}
