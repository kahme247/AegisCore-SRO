package manager

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

func TestBuildGroupSpawnPacketsDespawnSortsUniqueIDs(t *testing.T) {
	objects := map[uint32]model.ISRObject{
		20: &model.SRObject{UniqueID: 20},
		3:  &model.SRObject{UniqueID: 3},
		11: &model.SRObject{UniqueID: 11},
	}

	begin, body, end, err := buildGroupSpawnPackets(objects, false)
	if err != nil {
		t.Fatalf("build group despawn packets: %v", err)
	}
	if begin.MessageID != opcode.EntityGroupSpawnBegin || body.MessageID != opcode.EntityGroupSpawnData || end.MessageID != opcode.EntityGroupSpawnEnd {
		t.Fatalf("group opcodes = 0x%04X, 0x%04X, 0x%04X", begin.MessageID, body.MessageID, end.MessageID)
	}
	if got, want := begin.Payload(), []byte{2, 3, 0}; string(got) != string(want) {
		t.Fatalf("begin payload = %v, want %v", got, want)
	}

	got := body.Payload()
	if len(got) != 12 {
		t.Fatalf("despawn payload length = %d, want 12", len(got))
	}
	for index, want := range []uint32{3, 11, 20} {
		if actual := binary.LittleEndian.Uint32(got[index*4:]); actual != want {
			t.Fatalf("despawn UID %d = %d, want %d", index, actual, want)
		}
	}
}

func TestBuildGroupSpawnPacketsRejectsNilObject(t *testing.T) {
	_, _, _, err := buildGroupSpawnPackets(map[uint32]model.ISRObject{7: nil}, true)
	if err == nil {
		t.Fatal("expected nil object error")
	}
}

func TestSingleObjectDespawnUsesDedicatedOpcode(t *testing.T) {
	npc, player := mobAIFixture(100)
	conn := &mobAICaptureConn{}
	player.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "single-despawn-test")
	defer player.Session.Close()

	groupSpawnObjects(player, map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, false)
	packets := readCapturedPackets(t, conn, 1)
	if len(packets) != 1 || packets[0].MessageID != opcode.EntitySingleDespawn {
		t.Fatalf("single despawn packets = %#v, want one 0x%04X packet", packetOpcodes(packets), opcode.EntitySingleDespawn)
	}
	uid, err := packets[0].ReadUInt32()
	if err != nil || uid != npc.GetUniqueID() {
		t.Fatalf("single despawn UID = %d, %v; want %d", uid, err, npc.GetUniqueID())
	}
}

func TestSupportedSpawnObjectsAlsoProtectsDespawnBatches(t *testing.T) {
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID:          10,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Offset: math32.NewVector3(1, 0, 1)},
	}}
	cos := &model.SRObject{
		UniqueID: 20,
		TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3},
	}

	filtered := supportedSpawnObjects(map[uint32]model.ISRObject{
		mob.GetUniqueID(): mob,
		cos.GetUniqueID(): cos,
	})
	if len(filtered) != 1 || filtered[mob.GetUniqueID()] != mob {
		t.Fatalf("supported visibility objects = %#v; want only mob UID %d", filtered, mob.GetUniqueID())
	}
	if _, exists := filtered[cos.GetUniqueID()]; exists {
		t.Fatalf("unsupported COS UID %d can enter a grouped despawn", cos.GetUniqueID())
	}
}

func TestSupportedSpawnObjectsRejectsUnverifiedGlobalLayouts(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{Offset: math32.NewVector3(1, 0, 1)}
	ordinaryNPC := &model.NPC{SRObject: model.SRObject{
		UniqueID:          1,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2, TypeID4: 0},
		RtNavmeshPosition: position,
	}}
	questMob := &model.NPC{SRObject: model.SRObject{
		UniqueID:          2,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 4},
		RtNavmeshPosition: position,
	}}
	gatePulley := &model.NPC{SRObject: model.SRObject{
		UniqueID:          3,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2, TypeID4: 1},
		RtNavmeshPosition: position,
	}}
	appearanceMob := &model.NPC{SRObject: model.SRObject{
		UniqueID:          5,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 2},
		RtNavmeshPosition: position,
	}}
	missingPosition := &model.NPC{SRObject: model.SRObject{
		UniqueID: 4,
		TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
	}}

	filtered := supportedSpawnObjects(map[uint32]model.ISRObject{
		1: ordinaryNPC,
		2: questMob,
		3: gatePulley,
		4: missingPosition,
		5: appearanceMob,
	})
	if len(filtered) != 2 || filtered[1] != ordinaryNPC || filtered[2] != questMob {
		t.Fatalf("verified global spawn layouts = %#v; want ordinary NPC and quest mob", filtered)
	}
}

func TestSupportedSpawnObjectsAcceptsPlayerLayout(t *testing.T) {
	player := &model.Player{SRObject: model.SRObject{
		UniqueID:          77,
		RefObjectID:       1907,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 1},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Offset: math32.NewVector3(1, 0, 1)},
	}}
	filtered := supportedSpawnObjects(map[uint32]model.ISRObject{77: player})
	if len(filtered) != 1 || filtered[77] != player {
		t.Fatalf("supported player objects = %#v, want UID 77", filtered)
	}

	begin, body, end, err := buildGroupSpawnPackets(filtered, true)
	if err != nil {
		t.Fatalf("build player group spawn: %v", err)
	}
	if begin.MessageID != opcode.EntityGroupSpawnBegin || body.MessageID != opcode.EntityGroupSpawnData || end.MessageID != opcode.EntityGroupSpawnEnd {
		t.Fatalf("player group opcodes = %#v", []uint16{begin.MessageID, body.MessageID, end.MessageID})
	}
}

func TestSupportedSpawnObjectsAdmitsTeleportGatesWithVerifiedStructureLayout(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{
		Region:  navmeshv2.NewRegionFromInt16(25000),
		Offset:  math32.NewVector3(1254, -6, 1374),
		Heading: 0,
	}
	gate := &model.TeleportGate{SRObject: model.SRObject{
		UniqueID: 77, RefObjectID: 2094, Name: "STORE_CH_GATE",
		TypeInfo: model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1}, RtNavmeshPosition: position,
	}, TeleportID: 1}
	unknownStructure := &model.SRObject{
		UniqueID: 78, RefObjectID: 9999,
		TypeInfo: model.TypeInfo{TypeID1: 4}, RtNavmeshPosition: position,
	}
	filtered := supportedSpawnObjects(map[uint32]model.ISRObject{77: gate, 78: unknownStructure})
	if len(filtered) != 1 || filtered[77] != gate {
		t.Fatalf("supported structure objects = %#v, want only teleport gate", filtered)
	}

	begin, body, end, err := buildGroupSpawnPackets(filtered, true)
	if err != nil {
		t.Fatalf("build teleport gate group spawn: %v", err)
	}
	if got := begin.Payload(); !bytes.Equal(got, []byte{1, 1, 0}) {
		t.Fatalf("teleport gate group begin = %x, want 010100", got)
	}
	// Ref ID, runtime UID, position, four gate-state bytes, then two required
	// zeroed CGObjStruct runtime fields. RefObjectID already selects the client
	// teleport record; TeleportID must not be serialized here.
	wantBody := []byte{
		0x2e, 0x08, 0, 0, 0x4d, 0, 0, 0,
		0xa8, 0x61, 0, 0xc0, 0x9c, 0x44, 0, 0, 0xc0, 0xc0, 0, 0xc0, 0xab, 0x44, 0, 0,
		0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0,
	}
	if got := body.Payload(); !bytes.Equal(got, wantBody) {
		t.Fatalf("teleport gate group data = %x, want %x", got, wantBody)
	}
	if len(end.Payload()) != 0 {
		t.Fatalf("teleport gate group end = %x, want empty", end.Payload())
	}
}

func TestGroupSpawnDeliversPlayerBatch(t *testing.T) {
	_, observer := mobAIFixture(100)
	conn := &mobAICaptureConn{}
	observer.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "player-spawn-test")
	defer observer.Session.Close()

	subject := &model.Player{
		SRObject: model.SRObject{
			UniqueID:          77,
			RefObjectID:       1907,
			TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 1},
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Offset: math32.NewVector3(1, 0, 1)},
		},
		Scale:       34,
		CharName:    "VisiblePlayer",
		LifeState:   model.Alive,
		MotionState: model.Stand,
		BodyState:   model.Normal,
		Inventory:   model.NewInventory(nil),
	}
	groupSpawnObjects(observer, map[uint32]model.ISRObject{77: subject}, true)
	packets := readCapturedPackets(t, conn, 3)
	want := []uint16{opcode.EntityGroupSpawnBegin, opcode.EntityGroupSpawnData, opcode.EntityGroupSpawnEnd}
	if got := packetOpcodes(packets); len(got) != len(want) {
		t.Fatalf("player spawn opcodes = %#v, want %#v", got, want)
	} else {
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("player spawn opcodes = %#v, want %#v", got, want)
			}
		}
	}
}

func TestGroupSpawnSendsMovingEntityUpdateAfterBatch(t *testing.T) {
	npc, player := mobAIFixture(100)
	npc.BeginMovement(model.MovementData{
		HasDestination: true,
		TargetPosition: player.GetNavmeshPosition(),
	})
	conn := &mobAICaptureConn{}
	player.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "moving-spawn-test")
	defer player.Session.Close()

	prematureMovement := model.BuildMovementUpdatePacket(npc, *npc.GetMovementData(), npc.GetNavmeshPosition())
	npc.SendToKnown(&prematureMovement)
	if got := len(conn.Bytes()); got != 0 {
		t.Fatalf("mob sent %d bytes before its spawn was delivered", got)
	}

	groupSpawnObjects(player, map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, true)
	packets := readCapturedPackets(t, conn, 4)
	want := []uint16{
		opcode.EntityGroupSpawnBegin,
		opcode.EntityGroupSpawnData,
		opcode.EntityGroupSpawnEnd,
		opcode.EntityMovementResponse,
	}
	if got := packetOpcodes(packets); len(got) != len(want) {
		t.Fatalf("moving spawn opcodes = %#v, want %#v", got, want)
	} else {
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("moving spawn opcodes = %#v, want %#v", got, want)
			}
		}
	}
	if got := packets[1].DataSize; got != 48 {
		t.Fatalf("moving spawn body size = %d, want stationary 48-byte body", got)
	}
}
