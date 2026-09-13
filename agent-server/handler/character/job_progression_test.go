package character

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type jobProgressionStub struct {
	characterID  int
	experience   uint64
	contribution uint64
	result       model.JobSnapshot
	err          error
}

func (s *jobProgressionStub) AdvanceJob(_ context.Context, characterID int, experience, contribution uint64) (model.JobSnapshot, error) {
	s.characterID, s.experience, s.contribution = characterID, experience, contribution
	return s.result, s.err
}

func TestCombatJobAwardFailureDoesNotEscapeBestEffortBoundary(t *testing.T) {
	player := &model.Player{ID: 55, Session: &server.Session{}}
	player.SetJob(model.PlayerJob{Type: model.PlayerJobTrader, Alias: "StillCurrent", Level: 3, Experience: 77})
	repository := &jobProgressionStub{err: errors.New("database unavailable")}
	handler := &CombatHandler{jobProgress: repository}

	handler.tryAwardCombatJobExperience(context.Background(), player, 91, 1234)

	if repository.characterID != 55 || repository.experience != 91 || repository.contribution != 0 {
		t.Fatalf("repository call = %+v", repository)
	}
	if job := player.GetJob(); job.Type != model.PlayerJobTrader || job.Alias != "StillCurrent" || job.Level != 3 || job.Experience != 77 {
		t.Fatalf("failed persistence changed runtime job = %+v", job)
	}
}

func TestCombatJobAwardCommitsSnapshotAndPublishes30E6(t *testing.T) {
	session := &server.Session{}
	player := &model.Player{ID: 44, Session: session}
	player.SetJob(model.PlayerJob{Type: model.PlayerJobThief, Alias: "Before", Level: 1})
	repository := &jobProgressionStub{result: model.JobSnapshot{CharacterID: 44, Type: model.JobThief, Alias: "After", Level: 2, Experience: 123}}
	handler := &CombatHandler{jobProgress: repository}
	var sent network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	if err := handler.awardCombatJobExperience(context.Background(), player, 87); err != nil {
		t.Fatal(err)
	}
	if repository.characterID != 44 || repository.experience != 87 || repository.contribution != 0 {
		t.Fatalf("repository call = %+v", repository)
	}
	if job := player.GetJob(); job.Type != model.PlayerJobThief || job.Level != 2 || job.Experience != 123 || job.Alias != "After" {
		t.Fatalf("runtime job = %+v", job)
	}
	if sent.MessageID != opcode.JobUpdateExperience || !bytes.Equal(sent.Payload(), []byte{2, 2, 123, 0, 0, 0}) {
		t.Fatalf("0x30E6 = opcode 0x%04X payload % X", sent.MessageID, sent.Payload())
	}
}

func TestCombatJobPartyEligibilityKeepsHunterPartyHomogeneous(t *testing.T) {
	session := &server.Session{}
	killer := &model.Player{Session: session}
	killer.SetJob(model.PlayerJob{Type: model.PlayerJobHunter, Level: 1})
	hunter := &model.Player{Session: session}
	hunter.SetJob(model.PlayerJob{Type: model.PlayerJobHunter, Level: 1})
	thief := &model.Player{Session: session}
	thief.SetJob(model.PlayerJob{Type: model.PlayerJobThief, Level: 1})
	if got := combatJobRecipients(killer, []*model.Player{killer, hunter, thief}); len(got) != 2 || got[0] != killer || got[1] != hunter {
		t.Fatalf("hunter recipients = %#v", got)
	}
}
