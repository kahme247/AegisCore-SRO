package service

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type groundItemWorldFake struct{ object model.ISRObject }

func (f *groundItemWorldFake) AddVisibleObject(object model.ISRObject) { f.object = object }

type groundItemLifecycleWorldFake struct {
	groundItemWorldFake
	removed chan model.ISRObject
}

func (f *groundItemLifecycleWorldFake) RemoveVisibleObject(object model.ISRObject) {
	f.removed <- object
}

type groundPartyLookup struct{ player *model.Player }

type groundItemMaterializerFake struct {
	item   *model.Item
	called bool
	err    error
}

func (f *groundItemMaterializerFake) Materialize(_ context.Context, spec model.GroundItemSpec) (*model.Item, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	if f.item == nil {
		f.item = &model.Item{ID: 55, SRObject: model.SRObject{RefObjectID: spec.ReferenceItemID}}
	}
	return f.item, nil
}

func (f groundPartyLookup) GetPlayerByUniqueId(uint32) (*model.Player, error) { return f.player, nil }

func TestGroundItemServiceAppliesPartyOwnershipBeforeSpawn(t *testing.T) {
	player := &model.Player{ID: 7, LifeState: model.Alive, Session: &server.Session{}, SRObject: model.SRObject{UniqueID: 88}}
	party := NewPartyService(groundPartyLookup{player: player})
	party.Parties[12] = &model.Party{Number: 12, MasterUniqueID: 88, Members: []uint32{player.GetUniqueID()}}
	world := &groundItemWorldFake{}
	item := &model.Item{ID: 55, SRObject: model.SRObject{RefObjectID: 900}}

	service := NewGroundItemService(world, party)
	var notified int
	service.ConfigureDistributionNotifier(func(player *model.Player, packet network.Packet) error {
		notified++
		if player != nil && packet.MessageID != opcode.PartyDistribution {
			t.Fatalf("distribution packet opcode = 0x%04X", packet.MessageID)
		}
		if world.object != nil {
			t.Fatal("ground item entered the world before distribution notification")
		}
		return nil
	})
	if err := service.Spawn(item, 12); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if item.OwnerUniqueID != player.GetUniqueID() || world.object != item || notified != 1 {
		t.Fatalf("spawned item = owner %d/object %v/notified %d, want owner %d and one notification", item.OwnerUniqueID, world.object, notified, player.GetUniqueID())
	}
}

func TestGroundItemServiceRejectsUnmaterializedItem(t *testing.T) {
	if err := NewGroundItemService(&groundItemWorldFake{}, nil).Spawn(&model.Item{ID: 55}, 0); err != ErrGroundItemInvalid {
		t.Fatalf("Spawn invalid item error = %v, want ErrGroundItemInvalid", err)
	}
}

func TestGroundItemServiceAcceptsTransientCargoWithoutPersistedIdentity(t *testing.T) {
	world := &groundItemWorldFake{}
	item := &model.Item{CreatorName: "TraderAlias", TransientGroundItem: true, SRObject: model.SRObject{RefObjectID: 900}}
	if err := NewGroundItemService(world, nil).Spawn(item, 0); err != nil {
		t.Fatal(err)
	}
	if world.object != item {
		t.Fatalf("world object = %v, want transient specialty", world.object)
	}
}

func TestGroundItemServiceUsesReferenceDecayTimeForDespawn(t *testing.T) {
	previous := model.RefItems
	model.RefItems = map[uint32]model.RefItem{900: {RefObject: model.RefObject{DecayTime: 10}}}
	t.Cleanup(func() { model.RefItems = previous })

	world := &groundItemLifecycleWorldFake{removed: make(chan model.ISRObject, 1)}
	service := NewGroundItemService(world, nil)
	service.ConfigureLifecycle(NewGroundItemLifecycle(world), 0, 0)
	item := &model.Item{ID: 55, SRObject: model.SRObject{UniqueID: 77, RefObjectID: 900}}
	if err := service.Spawn(item, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case removed := <-world.removed:
		if removed != item {
			t.Fatalf("removed object = %v, want item", removed)
		}
	case <-time.After(time.Second):
		t.Fatal("reference decay time did not despawn ground item")
	}
}

func TestGroundItemServiceMaterializesBeforePartyWorldHandoff(t *testing.T) {
	player := &model.Player{ID: 7, LifeState: model.Alive, Session: &server.Session{}, SRObject: model.SRObject{UniqueID: 88}}
	party := NewPartyService(groundPartyLookup{player: player})
	party.Parties[12] = &model.Party{Number: 12, MasterUniqueID: 88, Members: []uint32{player.GetUniqueID()}}
	world := &groundItemWorldFake{}
	materializer := &groundItemMaterializerFake{}
	service := NewGroundItemService(world, party)
	service.ConfigureDistributionNotifier(func(_ *model.Player, _ network.Packet) error { return nil })
	if _, err := service.MaterializeAndSpawn(context.Background(), materializer, model.GroundItemSpec{ReferenceItemID: 900, StackCount: 1}, 12); err != nil {
		t.Fatalf("MaterializeAndSpawn: %v", err)
	}
	if !materializer.called || world.object != materializer.item || materializer.item.OwnerUniqueID != player.GetUniqueID() {
		t.Fatalf("materializer called=%t/world=%v/owner=%d", materializer.called, world.object, materializer.item.OwnerUniqueID)
	}
}

func TestGroundItemServiceDoesNotAdmitWhenMaterializationFails(t *testing.T) {
	world := &groundItemWorldFake{}
	wantErr := errors.New("materialization failed")
	materializer := &groundItemMaterializerFake{err: wantErr}
	service := NewGroundItemService(world, nil)
	if _, err := service.MaterializeAndSpawn(context.Background(), materializer, model.GroundItemSpec{ReferenceItemID: 900, StackCount: 1}, 0); !errors.Is(err, wantErr) {
		t.Fatalf("MaterializeAndSpawn error = %v, want %v", err, wantErr)
	}
	if world.object != nil {
		t.Fatal("world admitted an item after materialization failure")
	}
}

func TestBuildPartyDistributionPacketBranches(t *testing.T) {
	tests := []struct {
		name     string
		typeInfo model.TypeInfo
		opt      byte
		stack    int
		data     uint32
		suffix   []byte
	}{
		{name: "equipment", typeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1}, opt: 7, suffix: []byte{7}},
		{name: "cos", typeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2}, suffix: nil},
		{name: "etc", typeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3}, stack: 321, suffix: []byte{0x41, 0x01}},
		{name: "gold", typeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 5, TypeID4: 0}, data: 0x11223344, suffix: []byte{0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := &model.Item{SRObject: model.SRObject{RefObjectID: 0x55667788, TypeInfo: test.typeInfo}, OptLevel: test.opt, StackSize: test.stack, Data: test.data}
			packet, err := BuildPartyDistributionPacket(0x11223344, item)
			if err != nil {
				t.Fatal(err)
			}
			if packet.MessageID != opcode.PartyDistribution {
				t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.PartyDistribution)
			}
			payload := packet.Payload()
			if got := binary.LittleEndian.Uint32(payload[0:4]); got != 0x11223344 {
				t.Fatalf("member JID = 0x%08X", got)
			}
			if got := binary.LittleEndian.Uint32(payload[4:8]); got != 0x55667788 {
				t.Fatalf("reference ID = 0x%08X", got)
			}
			if string(payload[8:]) != string(test.suffix) {
				t.Fatalf("suffix = % X, want % X", payload[8:], test.suffix)
			}
		})
	}
}
