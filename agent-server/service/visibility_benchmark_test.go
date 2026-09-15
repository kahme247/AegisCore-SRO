package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	frameworkserver "github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/g3n/engine/math32"
)

const (
	visibilityBenchmarkPlayers          = 10_000
	visibilityBenchmarkRegionRows       = 10
	visibilityBenchmarkRegionColumns    = 10
	visibilityBenchmarkPlayersPerRegion = visibilityBenchmarkPlayers / (visibilityBenchmarkRegionRows * visibilityBenchmarkRegionColumns)
)

type visibilityBenchmarkWriter struct{}

func (visibilityBenchmarkWriter) Write(bytes []byte) (int, error)  { return len(bytes), nil }
func (visibilityBenchmarkWriter) SetWriteDeadline(time.Time) error { return nil }

type visibilityBenchmarkRuntime struct {
	world   *WorldService
	players []*model.Player
	senders []*frameworkserver.Sender
	links   int
}

func newVisibilityBenchmarkRuntime() *visibilityBenchmarkRuntime {
	world := &WorldService{
		visibleObjects:         make(map[uint32]model.ISRObject, visibilityBenchmarkPlayers),
		playersByUniqueId:      make(map[uint32]*model.Player, visibilityBenchmarkPlayers),
		playersByCharName:      make(map[string]*model.Player, visibilityBenchmarkPlayers),
		playersByCharacterID:   make(map[int]*model.Player, visibilityBenchmarkPlayers),
		npcsByUniqueId:         make(map[uint32]*model.NPC),
		visibleObjectsByRegion: make(map[int16][]model.ISRObject, visibilityBenchmarkRegionRows*visibilityBenchmarkRegionColumns),
		movingObjects:          make(map[uint32]model.ICharacter),
		mutex:                  &sync.Mutex{},
	}
	runtime := &visibilityBenchmarkRuntime{
		world:   world,
		players: make([]*model.Player, visibilityBenchmarkPlayers),
		senders: make([]*frameworkserver.Sender, visibilityBenchmarkPlayers),
	}

	for index := range runtime.players {
		regionIndex := index / visibilityBenchmarkPlayersPerRegion
		regionX := 100 + regionIndex%visibilityBenchmarkRegionColumns
		regionZ := 50 + regionIndex/visibilityBenchmarkRegionColumns
		regionID := utils.XAndZToInt16(byte(regionX), byte(regionZ))
		localIndex := index % visibilityBenchmarkPlayersPerRegion
		uid := uint32(index + 1)
		player := &model.Player{
			ID:        index + 1,
			CharName:  fmt.Sprintf("visibility-%d", uid),
			LifeState: model.Alive,
			BaseStats: model.BaseStats{HP: 100, MP: 100},
			SRObject: model.SRObject{
				UniqueID: uid,
				RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
					Region: navmeshv2.NewRegionFromInt16(regionID),
					Offset: math32.NewVector3(
						100+float32(localIndex%10)*160,
						0,
						100+float32(localIndex/10)*160,
					),
				},
			},
		}
		player.CharKnownObjectList = model.NewCharKnownObjectList(player)
		player.KnownObjectList = player.CharKnownObjectList
		player.SetBodyState(model.Normal)
		sender := frameworkserver.NewSender(visibilityBenchmarkWriter{}, func(packet network.Packet) ([]byte, error) {
			return packet.MarshalBinary()
		})
		player.Session = &frameworkserver.Session{Sender: sender}
		runtime.players[index] = player
		runtime.senders[index] = sender
		world.visibleObjects[uid] = player
		world.playersByUniqueId[uid] = player
		world.playersByCharName[player.CharName] = player
		world.playersByCharacterID[player.ID] = player
		world.visibleObjectsByRegion[regionID] = append(world.visibleObjectsByRegion[regionID], player)
	}

	for _, player := range runtime.players {
		world.ReconcileCharacterVisibility(player)
	}
	for _, player := range runtime.players {
		known := player.GetCharKnownObjectList()
		spawned := known.GetObjectsToSpawn()
		known.MarkSpawned(spawned)
		runtime.links += len(spawned)
	}
	return runtime
}

func (runtime *visibilityBenchmarkRuntime) close() {
	for _, sender := range runtime.senders {
		sender.Close()
	}
	for _, sender := range runtime.senders {
		sender.Wait()
	}
}

func (runtime *visibilityBenchmarkRuntime) moveAndReconcile(step int) {
	delta := float32(1)
	if step%2 == 0 {
		delta = -1
	}
	for _, player := range runtime.players {
		position := player.GetNavmeshPosition()
		position.Offset.X += delta
		player.SetNavmeshPosition(position)
		runtime.world.ReconcileCharacterVisibility(player)
	}
}

func (runtime *visibilityBenchmarkRuntime) broadcast(packet network.Packet) uint64 {
	before := uint64(0)
	for _, sender := range runtime.senders {
		before += sender.Stats().Rejected
	}
	for _, player := range runtime.players {
		player.Broadcast(&packet)
	}
	after := uint64(0)
	for _, sender := range runtime.senders {
		after += sender.Stats().Rejected
	}
	return after - before
}

func visibilityBenchmarkPacket() network.Packet {
	packet := network.NewServerPacket(0xB021)
	_ = packet.WriteUInt32(12345)
	_ = packet.WriteUInt16(32000)
	_ = packet.WriteFloat32(12.5)
	return *packet
}

// BenchmarkVisibility10K measures the maintained visibility and nonblocking
// packet paths. Setup establishes stable reciprocal known lists before timing.
func BenchmarkVisibility10K(b *testing.B) {
	benchmarks := []struct {
		name      string
		reconcile bool
		broadcast bool
	}{
		{name: "Reconcile", reconcile: true},
		{name: "PacketFanout", broadcast: true},
		{name: "Combined", reconcile: true, broadcast: true},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			runtime := newVisibilityBenchmarkRuntime()
			b.Cleanup(runtime.close)
			packet := visibilityBenchmarkPacket()
			step := 0
			rejected := uint64(0)
			b.ReportAllocs()
			for b.Loop() {
				step++
				if benchmark.reconcile {
					runtime.moveAndReconcile(step)
				}
				if benchmark.broadcast {
					rejected += runtime.broadcast(packet)
				}
			}
			b.ReportMetric(float64(runtime.links), "directed-links/op")
			if benchmark.broadcast {
				b.ReportMetric(float64(rejected)/float64(b.N), "rejected-sends/op")
			}
		})
	}
}
