package character

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type tapRepositoryFake struct {
	npcRef  uint32
	records []model.TAPRecord
	ids     []uint64
	points  []model.TAPPoints
}

func (repository *tapRepositoryFake) Snapshot(_ context.Context, npcRef uint32) ([]model.TAPRecord, error) {
	repository.npcRef = npcRef
	return append([]model.TAPRecord(nil), repository.records...), nil
}

func (repository *tapRepositoryFake) Points(_ context.Context, ids []uint64) ([]model.TAPPoints, error) {
	repository.ids = append([]uint64(nil), ids...)
	return append([]model.TAPPoints(nil), repository.points...), nil
}

func TestTAPHandlerPersistsSnapshotOrderAndPublishesActiveIcon(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	npc.RefObjectID = 9251
	repository := &tapRepositoryFake{
		records: []model.TAPRecord{{
			ID: 7, EntryType: 2,
			From: model.TAPClock{Month: 7, Day: 1}, To: model.TAPClock{Month: 8, Day: 1},
			TraderUnionPoints: 11, ThiefUnionPoints: 12,
		}},
		points: []model.TAPPoints{{Trader: 21, Thief: 22}},
	}
	world := &selectEntityWorldFake{player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}}
	var sent []network.Packet
	h := &TAPHandler{repository: repository, world: world, now: func() time.Time { return time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC) }, snapshots: make(map[*server.Session][]uint64), send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	info := network.EmptyClientPacket()
	info.WriteByte(0)
	info.WriteUInt32(npc.GetUniqueID())
	if err := h.handleInfo(context.Background(), session, info); err != nil {
		t.Fatalf("info: %v", err)
	}
	if err := h.handleUpdate(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatalf("update: %v", err)
	}
	if repository.npcRef != 9251 || len(repository.ids) != 1 || repository.ids[0] != 7 {
		t.Fatalf("repository snapshot=%d ids=%v", repository.npcRef, repository.ids)
	}
	if len(sent) != 3 || sent[0].MessageID != opcode.TapInfoResponse || sent[1].MessageID != opcode.TapIcon || sent[2].MessageID != opcode.TapUpdateResponse {
		t.Fatalf("sent = %#v", sent)
	}
	if got := hex.EncodeToString(sent[0].Payload()); got != "010207010000080100000b0000000c000000" {
		t.Fatalf("info = %s", got)
	}
	if got := hex.EncodeToString(sent[1].Payload()); got != "01" {
		t.Fatalf("icon = %s", got)
	}
	if got := hex.EncodeToString(sent[2].Payload()); got != "1500000016000000" {
		t.Fatalf("update = %s", got)
	}
}

func TestTAPUpdateRequiresPriorInfoSnapshot(t *testing.T) {
	h := &TAPHandler{repository: &tapRepositoryFake{}, snapshots: make(map[*server.Session][]uint64)}
	if err := h.handleUpdate(context.Background(), &server.Session{}, network.EmptyClientPacket()); err == nil {
		t.Fatal("update without info snapshot accepted")
	}
}
