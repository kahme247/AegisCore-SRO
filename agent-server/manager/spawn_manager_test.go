package manager

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

func TestTwoClientSpawnDespawnCoverage(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000),
		Offset: math32.NewVector3(12, 0, 34),
	}
	character := func(uid, ref uint32, typeInfo model.TypeInfo) model.NPC {
		return model.NPC{
			SRObject: model.SRObject{UniqueID: uid, RefObjectID: ref, TypeInfo: typeInfo, RtNavmeshPosition: position},
			Mutex:    &sync.Mutex{}, LifeState: model.Alive, MotionState: model.Stand, BodyState: model.Normal,
		}
	}
	cos := func(uid, ref uint32, kind model.COSKind) *model.COS {
		level, health := byte(10), int32(100)
		if kind == model.COSRideTransport {
			level, health = 1, 200
		}
		return &model.COS{
			NPC: model.NPC{
				SRObject: model.SRObject{
					UniqueID: uid, RefObjectID: ref,
					TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: int(kind)},
					RtNavmeshPosition: position,
				},
				Mutex: &sync.Mutex{}, LifeState: model.Alive, MotionState: model.Stand, BodyState: model.Normal,
			},
			OwnerUniqueID: 900, OwnerName: "Owner", Kind: kind, Level: level, Health: health, MaxHealth: health,
		}
	}

	scenarios := []struct {
		name   string
		object model.ISRObject
	}{
		{name: "player", object: &model.Player{
			SRObject: model.SRObject{UniqueID: 101, RefObjectID: 1907, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 1}, RtNavmeshPosition: position},
			Scale:    34, CharName: "VisiblePlayer", LifeState: model.Alive, MotionState: model.Stand, BodyState: model.Normal,
			Inventory: model.NewInventory(nil),
		}},
		{name: "mob", object: func() model.ISRObject {
			mob := character(102, 5002, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1})
			return &mob
		}()},
		{name: "drop", object: &model.Item{SRObject: model.SRObject{
			UniqueID: 103, RefObjectID: 1, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 5}, RtNavmeshPosition: position,
		}, Data: 777}},
		{name: "pet", object: cos(104, 5004, model.COSGrowthPet)},
		{name: "transport", object: cos(105, 5005, model.COSRideTransport)},
		{name: "npc", object: func() model.ISRObject {
			npc := character(106, 5006, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2})
			return &npc
		}()},
		{name: "specialized_structure", object: &model.TeleportGate{SRObject: model.SRObject{
			UniqueID: 107, RefObjectID: 2094, TypeInfo: model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1}, RtNavmeshPosition: position,
		}, TeleportID: 1}},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			for _, clientName := range []string{"alice", "bob"} {
				t.Run(clientName, func(t *testing.T) {
					_, observer := mobAIFixture(100)
					conn := &mobAICaptureConn{}
					observer.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "spawn-coverage-"+clientName)
					defer observer.Session.Close()

					objects := map[uint32]model.ISRObject{scenario.object.GetUniqueID(): scenario.object}
					groupSpawnObjects(observer, objects, true)
					spawnPacketCount := 3
					if _, isItem := scenario.object.(model.IItem); isItem {
						spawnPacketCount = 1
					}
					packets := readCapturedPackets(t, conn, spawnPacketCount)
					if len(packets) != spawnPacketCount {
						t.Fatalf("spawn packet count = %d, want %d", len(packets), spawnPacketCount)
					}
					expected := network.EmptyPacket()
					model.WriteEntitySpawnData(&expected, scenario.object)
					expectedSpawnData := expected.Payload()
					spawnData := packets[0].Payload()
					if spawnPacketCount == 3 {
						if got := packetOpcodes(packets); got[0] != opcode.EntityGroupSpawnBegin || got[1] != opcode.EntityGroupSpawnData || got[2] != opcode.EntityGroupSpawnEnd {
							t.Fatalf("spawn opcodes = %#v", got)
						}
						spawnData = packets[1].Payload()
					} else if packets[0].MessageID != opcode.EntitySingleSpawn {
						t.Fatalf("spawn opcode = 0x%04X, want 0x%04X", packets[0].MessageID, opcode.EntitySingleSpawn)
					} else {
						// Direct 0x3015 has the one-byte CIItem update-state tail
						// that is deliberately absent from grouped 0x3019 records.
						expectedSpawnData = append(expectedSpawnData, 0)
					}
					if !bytes.Equal(spawnData, expectedSpawnData) {
						t.Fatalf("spawn data lost alignment: got % X, want % X", spawnData, expectedSpawnData)
					}
					if !observer.GetCharKnownObjectList().ClientKnows(scenario.object.GetUniqueID()) {
						t.Fatal("client object table was not updated after spawn")
					}

					groupSpawnObjects(observer, objects, false)
					packets = readCapturedPackets(t, conn, spawnPacketCount+1)
					despawn := packets[spawnPacketCount]
					if despawn.MessageID != opcode.EntitySingleDespawn || !bytes.Equal(despawn.Payload(), func() []byte {
						b := make([]byte, 4)
						binary.LittleEndian.PutUint32(b, scenario.object.GetUniqueID())
						return b
					}()) {
						t.Fatalf("despawn packet = opcode 0x%04X payload % X", despawn.MessageID, despawn.Payload())
					}
					if observer.GetCharKnownObjectList().ClientKnows(scenario.object.GetUniqueID()) {
						t.Fatal("client object table retained entity after despawn")
					}
				})
			}
		})
	}
}

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

func TestGroundItemSpawnUsesExactSingle3015Boundary(t *testing.T) {
	_, player := mobAIFixture(100)
	conn := &mobAICaptureConn{}
	player.Session = server.NewSession(conn, network.EncodingOptions{}, nil, nil, "ground-item-single-spawn-test")
	defer player.Session.Close()
	item := &model.Item{SRObject: model.SRObject{
		UniqueID:          300,
		RefObjectID:       1,
		TypeInfo:          model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 5, TypeID4: 0},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(2, 0, 2)},
	}, Data: 777}
	groupSpawnObjects(player, map[uint32]model.ISRObject{item.GetUniqueID(): item}, true)
	packets := readCapturedPackets(t, conn, 1)
	if len(packets) != 1 || packets[0].MessageID != opcode.EntitySingleSpawn {
		t.Fatalf("ground-item spawn packets = %#v, want one 0x3015", packetOpcodes(packets))
	}
	if got := binary.LittleEndian.Uint32(packets[0].Payload()[4:8]); got != 777 {
		t.Fatalf("ground-item spawn amount = %d, want 777", got)
	}
	payload := packets[0].Payload()
	if got, want := len(payload), 31; got != want {
		t.Fatalf("ground-item 0x3015 payload length = %d, want %d", got, want)
	}
	if got := payload[len(payload)-1]; got != 0 {
		t.Fatalf("ground-item 0x3015 update state = %d, want ordinary state 0", got)
	}
	want := []byte{
		0x01, 0x00, 0x00, 0x00, // reference item
		0x09, 0x03, 0x00, 0x00, // gold amount 777
		0x2C, 0x01, 0x00, 0x00, // runtime UID 300
		0xA8, 0x61, // region 25000
		0x00, 0x00, 0x00, 0x40, // X 2
		0x00, 0x00, 0x00, 0x00, // Y 0
		0x00, 0x00, 0x00, 0x40, // Z 2
		0x00, 0x00, // heading
		0x00, // no ownership marker
		0x00, // item rarity/state
		0x00, // 0x3015 update state
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("ground-item 0x3015 payload = % X, want % X", payload, want)
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

func TestSupportedSpawnObjectsAdmitsGroundItemLayout(t *testing.T) {
	item := &model.Item{SRObject: model.SRObject{
		UniqueID:    88,
		RefObjectID: 900,
		TypeInfo:    model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Offset: math32.NewVector3(1, 0, 1),
		},
	}}
	filtered := supportedSpawnObjects(map[uint32]model.ISRObject{88: item})
	if len(filtered) != 1 || filtered[88] != item {
		t.Fatalf("supported ground-item objects = %#v, want UID 88", filtered)
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

func TestSupportedSpawnObjectsAdmitsDimensionHoleStateSixLayout(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{
		Region:  navmeshv2.NewRegionFromInt16(25000),
		Offset:  math32.NewVector3(1254, -6, 1374),
		Heading: 0,
	}
	owner := &model.Player{ID: 7, CharName: "Owner", SRObject: model.SRObject{UniqueID: 900}}
	portal, err := model.NewDimensionHolePortal(model.DimensionHoleCatalog()[0], owner, position)
	if err != nil {
		t.Fatalf("NewDimensionHolePortal() error = %v", err)
	}
	portal.SetUniqueID(77)
	malformed := &model.DimensionHolePortal{SRObject: model.SRObject{
		UniqueID: 78, RefObjectID: model.DimensionHolePortalRefObjectID,
		TypeInfo: model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 2}, RtNavmeshPosition: position,
	}}
	filtered := supportedSpawnObjects(map[uint32]model.ISRObject{77: portal, 78: malformed})
	if len(filtered) != 1 || filtered[77] != portal {
		t.Fatalf("supported Dimension Hole objects = %#v, want only UID 77", filtered)
	}

	begin, body, end, err := buildGroupSpawnPackets(filtered, true)
	if err != nil {
		t.Fatalf("build Dimension Hole group spawn: %v", err)
	}
	if got := begin.Payload(); !bytes.Equal(got, []byte{1, 1, 0}) {
		t.Fatalf("Dimension Hole group begin = %x, want 010100", got)
	}
	wantBody := []byte{
		0x64, 0x4c, 0, 0, 0x4d, 0, 0, 0,
		0xa8, 0x61, 0, 0xc0, 0x9c, 0x44, 0, 0, 0xc0, 0xc0, 0, 0xc0, 0xab, 0x44, 0, 0,
		0, 0, 1, 6, 5, 0, 'O', 'w', 'n', 'e', 'r', 0x2b, 0x8c, 0, 0,
	}
	if got := body.Payload(); !bytes.Equal(got, wantBody) {
		t.Fatalf("Dimension Hole group data = %x, want %x", got, wantBody)
	}
	if len(end.Payload()) != 0 {
		t.Fatalf("Dimension Hole group end = %x, want empty", end.Payload())
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
