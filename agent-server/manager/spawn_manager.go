package manager

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/sirupsen/logrus"
)

type SpawnManager struct {
	Manager
}

var spawnManagerInstance *SpawnManager
var spawnManagerOnce sync.Once

func GetSpawnManagerInstance() *SpawnManager {
	spawnManagerOnce.Do(func() {
		spawnManagerInstance = &SpawnManager{}
		spawnManagerInstance.Name = "SpawnManager"
		spawnManagerInstance.initialDelay = time.Second
		spawnManagerInstance.rate = time.Millisecond * 100
		spawnManagerInstance.runnerFunc = spawnManagerInstance.updateSpawns
	})
	return spawnManagerInstance
}

func (s *SpawnManager) updateSpawns() {
	world := service.GetWorldServiceInstance()
	for s.started {
		select {
		case <-s.ticker.C:
			for _, player := range world.GetActivePlayers() {
				objectsToDespawn := player.GetCharKnownObjectList().GetObjectsToDespawn()
				if len(objectsToDespawn) > 0 {
					groupSpawnObjects(player, objectsToDespawn, false)
				}

				objectsToSpawn := player.GetCharKnownObjectList().GetObjectsToSpawn()
				if len(objectsToSpawn) > 0 {
					groupSpawnObjects(player, objectsToSpawn, true)
				}
			}
		}
	}
}

func groupSpawnObjects(player model.IPlayer, objects map[uint32]model.ISRObject, spawning bool) {
	if player == nil || player.GetSession() == nil {
		return
	}
	// An entity omitted from spawn must also be omitted from despawn. Sending a
	// UID that the client was never introduced to can corrupt its visibility
	// state and terminate the stock client.
	objects = supportedSpawnObjects(objects)
	if len(objects) == 0 {
		return
	}
	tracePlayerVisibility(player, objects, spawning)
	if !spawning && len(objects) == 1 {
		for uid := range objects {
			packet := network.EmptyPacket()
			packet.MessageID = opcode.EntitySingleDespawn
			packet.WriteUInt32(uid)
			if err := player.GetSession().Send(packet); err != nil {
				logrus.Errorf("send single despawn for object %d: %v", uid, err)
				return
			}
			player.GetCharKnownObjectList().MarkDespawned(objects)
			return
		}
	}
	// A single ground item uses the native direct 0x3015 edge. Its item record
	// includes the exact one-byte update tail consumed by CIItem after the
	// polymorphic initial parser; character/NPC visibility remains grouped.
	if spawning && len(objects) == 1 {
		for _, object := range objects {
			if _, isItem := object.(model.IItem); !isItem {
				break
			}
			packet := network.EmptyPacket()
			packet.MessageID = opcode.EntitySingleSpawn
			model.WriteEntitySpawnData(&packet, object)
			if err := player.GetSession().Send(packet); err != nil {
				logrus.Errorf("send single ground-item spawn: %v", err)
				return
			}
			player.GetCharKnownObjectList().MarkSpawned(objects)
			return
		}
	}
	packets, err := buildGroupSpawnPackets(objects, spawning)
	if err != nil {
		logrus.Errorf("build grouped %s packet: %v", spawnActionName(spawning), err)
		return
	}
	if err := player.GetSession().SendBatch(packets); err != nil {
		logrus.Errorf("send grouped %s packet: %v", spawnActionName(spawning), err)
		return
	}
	if spawning {
		player.GetCharKnownObjectList().MarkSpawned(objects)
		sendSpawnedObjectMovements(player, objects)
	} else {
		player.GetCharKnownObjectList().MarkDespawned(objects)
	}
}

func sendSpawnedObjectMovements(player model.IPlayer, objects map[uint32]model.ISRObject) {
	ids := make([]uint32, 0, len(objects))
	for uid := range objects {
		ids = append(ids, uid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, uid := range ids {
		character, ok := objects[uid].(model.ICharacter)
		if !ok {
			continue
		}
		movement := character.GetMovementData()
		if movement == nil {
			continue
		}
		packet := model.BuildMovementUpdatePacket(character, *movement, character.GetNavmeshPosition())
		if err := player.GetSession().Send(packet); err != nil {
			logrus.Errorf("send movement for spawned object %d: %v", uid, err)
			return
		}
	}
}

func tracePlayerVisibility(player model.IPlayer, objects map[uint32]model.ISRObject, spawning bool) {
	if (os.Getenv("SRO_SESSION_TRACE") != "1" && !service.MovementTraceEnabled()) || player == nil {
		return
	}
	ids := make([]uint32, 0, len(objects))
	for uid := range objects {
		ids = append(ids, uid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, uid := range ids {
		object := objects[uid]
		if object == nil {
			continue
		}
		typeInfo := object.GetTypeInfo()
		logrus.Infof("VISIBILITY_TRACE action=%s player=%d uid=%d ref=%d type=(%d,%d,%d,%d) name=%s",
			spawnActionName(spawning), player.GetUniqueID(), uid, object.GetRefObjectID(),
			typeInfo.TypeID1, typeInfo.TypeID2, typeInfo.TypeID3, typeInfo.TypeID4, object.GetName())
	}
}

// supportedSpawnObjects admits only world entities whose vSRO 1.88 group-spawn
// layout is implemented. The same filter applies to despawns so the client is
// never asked to remove an entity that was deliberately withheld from it.
// NPC_COS, ground items, and specialized structures require their own
// trailing fields;
// serializing them as ordinary NPCs corrupts the rest of a 3019 batch. Teleport
// gates are the one verified structure exception: their serializer supplies the
// stock client's four state bytes and two zeroed CGObjStruct runtime fields.
func supportedSpawnObjects(objects map[uint32]model.ISRObject) map[uint32]model.ISRObject {
	supported := make(map[uint32]model.ISRObject, len(objects))
	for uid, object := range objects {
		if object == nil {
			continue
		}
		typeInfo := object.GetTypeInfo()
		_, isCharacter := object.(model.ICharacter)
		validPosition := object.GetNavmeshPosition().Offset != nil
		_, isPlayer := object.(model.IPlayer)
		verifiedPlayerLayout := isPlayer && typeInfo.IsPlayerCharacter()
		verifiedMobLayout := typeInfo.IsNPCMob() && typeInfo.TypeID4 != 2 && typeInfo.TypeID4 != 3
		verifiedNPCLayout := typeInfo.IsNPCNpc() && typeInfo.TypeID4 == 0
		_, verifiedCOSLayout := object.(*model.COS)
		verifiedCOSLayout = verifiedCOSLayout && typeInfo.IsCOS()
		verifiedTeleportGateLayout := model.IsTeleportGateObject(object)
		verifiedDimensionHoleLayout := model.IsDimensionHolePortalObject(object)
		_, verifiedItemLayout := object.(model.IItem)
		verifiedItemLayout = verifiedItemLayout && typeInfo.IsItem()
		if validPosition && (verifiedPlayerLayout || verifiedMobLayout || verifiedNPCLayout || verifiedCOSLayout || verifiedTeleportGateLayout || verifiedDimensionHoleLayout || verifiedItemLayout) {
			supported[uid] = object
			continue
		}
		logrus.Warnf("deferred unsupported group spawn uid=%d ref=%d type=(%d,%d,%d,%d) character=%t valid-position=%t", uid, object.GetRefObjectID(), typeInfo.TypeID1, typeInfo.TypeID2, typeInfo.TypeID3, typeInfo.TypeID4, isCharacter, validPosition)
	}
	return supported
}

const groupSpawnDataChunkSize = int(network.BufferSize) - 0x20

// buildGroupSpawnPackets creates the stock begin/data/end batch before any
// bytes are sent. SR_GameServer 0x00430A20 splits the serialized record stream
// into 0x3019 chunks that reserve 0x20 bytes of each 0x1000-byte message block.
func buildGroupSpawnPackets(objects map[uint32]model.ISRObject, spawning bool) ([]network.Packet, error) {
	if len(objects) > int(^uint16(0)) {
		return nil, fmt.Errorf("too many objects: %d", len(objects))
	}
	ids := make([]uint32, 0, len(objects))
	for id, object := range objects {
		if object == nil {
			return nil, fmt.Errorf("object %d is nil", id)
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	begin := network.EmptyPacket()
	begin.MessageID = opcode.EntityGroupSpawnBegin
	if spawning {
		if err := begin.WriteByte(1); err != nil {
			return nil, err
		}
	} else {
		if err := begin.WriteByte(2); err != nil {
			return nil, err
		}
	}
	if err := begin.WriteUInt16(uint16(len(ids))); err != nil {
		return nil, err
	}

	data := network.EmptyPacket()
	for _, id := range ids {
		if spawning {
			model.WriteEntitySpawnData(&data, objects[id])
		} else if err := data.WriteUInt32(objects[id].GetUniqueID()); err != nil {
			return nil, err
		}
	}
	if err := data.WriteError(); err != nil {
		return nil, err
	}

	payload := data.Payload()
	chunkCount := max(1, (len(payload)+groupSpawnDataChunkSize-1)/groupSpawnDataChunkSize)
	packets := make([]network.Packet, 0, chunkCount+2)
	packets = append(packets, begin)
	for offset := 0; offset < len(payload) || offset == 0; offset += groupSpawnDataChunkSize {
		endOffset := min(offset+groupSpawnDataChunkSize, len(payload))
		chunk := network.EmptyPacket()
		chunk.MessageID = opcode.EntityGroupSpawnData
		if err := chunk.WriteBytes(payload[offset:endOffset]); err != nil {
			return nil, err
		}
		packets = append(packets, chunk)
		if endOffset == len(payload) {
			break
		}
	}
	end := network.EmptyPacket()
	end.MessageID = opcode.EntityGroupSpawnEnd
	packets = append(packets, end)
	return packets, nil
}

func spawnActionName(spawning bool) string {
	if spawning {
		return "spawn"
	}
	return "despawn"
}
