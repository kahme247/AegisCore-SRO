package social

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type guildUpdateWorldFake map[int]*model.Player

func (w guildUpdateWorldFake) GetPlayerByUniqueId(uniqueID uint32) (*model.Player, error) {
	for _, player := range w {
		if player.GetUniqueID() == uniqueID {
			return player, nil
		}
	}
	return nil, errors.New("player not found")
}

func (w guildUpdateWorldFake) GetPlayerByCharacterID(characterID int) (*model.Player, error) {
	player := w[characterID]
	if player == nil {
		return nil, errors.New("player not found")
	}
	return player, nil
}

func TestPublishGuildUpdateExceptTargetsExistingMembers(t *testing.T) {
	first := &server.Session{ID: "first"}
	joining := &server.Session{ID: "joining"}
	h := &Handler{world: guildUpdateWorldFake{
		1: {ID: 1, Session: first},
		2: {ID: 2, Session: joining},
	}}

	var recipients []string
	h.send = func(session *server.Session, packet network.Packet) error {
		if packet.MessageID != opcode.GuildUpdate {
			t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.GuildUpdate)
		}
		recipients = append(recipients, session.ID)
		return nil
	}

	guild := model.GuildSnapshot{Members: []model.GuildMember{{CharacterID: 1}, {CharacterID: 2}}}
	if err := h.publishGuildUpdateExcept(guild, vsro188.BuildGuildMemberLeaveUpdate(3, vsro188.GuildMemberLeft), 2); err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0] != "first" {
		t.Fatalf("recipients = %v, want [first]", recipients)
	}
}

func TestGuildMemberUpdatePreservesSnapshotFields(t *testing.T) {
	member := model.GuildMember{CharacterID: 7, Name: "A", Class: 10, Level: 80, Donation: 9, Permissions: 16, Contribution: 11, Nickname: "B", RefObjectID: 1907, SiegeAuthority: 2, Online: true}
	got := guildMemberUpdate(member)
	if got.CharacterID != 7 || got.Name != "A" || got.Class != 10 || got.Level != 80 || got.Donation != 9 || got.Permissions != 16 || got.Contribution != 11 || got.Nickname != "B" || got.RefObjectID != 1907 || got.SiegeAuthority != 2 || !got.Online {
		t.Fatalf("guild member update = %#v", got)
	}
}

func TestPublishGuildEntityUpdateTargetsSelfAndVisibleObservers(t *testing.T) {
	subject := &model.Player{ID: 1, Session: &server.Session{ID: "subject"}, SRObject: model.SRObject{UniqueID: 101}}
	observer := &model.Player{ID: 2, Session: &server.Session{ID: "observer"}, SRObject: model.SRObject{UniqueID: 202}}
	remote := &model.Player{ID: 3, Session: &server.Session{ID: "remote"}, SRObject: model.SRObject{UniqueID: 303}}
	subject.CharKnownObjectList = model.NewCharKnownObjectList(subject)
	observer.CharKnownObjectList = model.NewCharKnownObjectList(observer)
	remote.CharKnownObjectList = model.NewCharKnownObjectList(remote)
	subject.CharKnownObjectList.AddObject(observer)
	observer.CharKnownObjectList.AddObject(subject)
	observer.CharKnownObjectList.MarkSpawned(observer.CharKnownObjectList.GetObjectsToSpawn())

	h := &Handler{world: guildUpdateWorldFake{1: subject, 2: observer, 3: remote}}
	recipients := make(map[string]int)
	h.send = func(session *server.Session, packet network.Packet) error {
		if packet.MessageID != opcode.GuildEntityUpdate {
			t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.GuildEntityUpdate)
		}
		recipients[session.ID]++
		return nil
	}

	guild := model.GuildSnapshot{ID: 7, Name: "Aegis", Members: []model.GuildMember{{CharacterID: 1, Nickname: "Lead"}}}
	if err := h.publishGuildEntityUpdate(1, guild); err != nil {
		t.Fatal(err)
	}
	if recipients["subject"] != 1 || recipients["observer"] != 1 || recipients["remote"] != 0 || len(recipients) != 2 {
		t.Fatalf("recipients = %v, want subject and observer once", recipients)
	}
}
