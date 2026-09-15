package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

const (
	combinedBenchmarkPlayers             = 4000
	combinedBenchmarkMovementsPerStep    = 4000
	combinedBenchmarkAttacksPerStep      = 1000
	combinedBenchmarkSkillImpactsPerStep = 500
	combinedBenchmarkInventoryOpsPerStep = 100
)

type combinedRuntimeBenchmark struct {
	players      []*model.Player
	persistence  *PositionPersistenceService
	movements    int
	attacks      int
	skills       int
	inventoryOps int
	step         uint64
}

func newCombinedRuntimeBenchmark(playersCount, movements, attacks, skills, inventoryOps int) *combinedRuntimeBenchmark {
	players := make([]*model.Player, playersCount)
	for index := range players {
		uid := uint32(index + 1)
		players[index] = &model.Player{
			ID:            index + 1,
			LifeState:     model.Alive,
			InventorySize: model.BaseInventorySize,
			BaseStats: model.BaseStats{
				HP: 1_000_000_000,
				MP: 1_000_000_000,
			},
			Inventory: model.NewInventory(map[byte]model.Item{
				model.InventoryEquipmentSlots: {
					ID:        index + 1,
					StackSize: 1,
				},
			}),
			SRObject: model.SRObject{
				UniqueID: uid,
				RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
					Region: navmeshv2.Region{ID: int16(25000 + index%16)},
					Offset: math32.NewVector3(
						float32(index%50),
						0,
						float32((index/50)%50),
					),
				},
			},
		}
	}
	return &combinedRuntimeBenchmark{
		players:      players,
		movements:    movements,
		attacks:      attacks,
		skills:       skills,
		inventoryOps: inventoryOps,
		persistence: NewPositionPersistenceServiceWithOptions(
			benchmarkPositionRepository{},
			30*time.Second,
			PositionPersistenceOptions{MaxPending: playersCount},
		),
	}
}

func (runtime *combinedRuntimeBenchmark) movementStep() {
	for index := 0; index < runtime.movements; index++ {
		player := runtime.players[index]
		position := player.GetNavmeshPosition()
		position.Offset.X += 0.25
		if position.Offset.X >= 50 {
			position.Offset.X = 0
		}
		player.SetNavmeshPosition(position)
		runtime.persistence.MarkDirty(player)
	}
}

func (runtime *combinedRuntimeBenchmark) combatStep(now time.Time) {
	playersCount := len(runtime.players)
	for index := 0; index < runtime.attacks; index++ {
		target := runtime.players[(index+playersCount/2)%playersCount]
		target.ApplyDamage(25)
	}
	for index := 0; index < runtime.skills; index++ {
		target := runtime.players[(index+playersCount/3)%playersCount]
		target.ApplyMagicalDamage(40)
		target.ApplyCombatStatusAt(model.CombatStatus{
			Kind:      model.CombatStatusBurn,
			Token:     uint32(runtime.step*uint64(runtime.skills)) + uint32(index+1),
			SourceUID: uint32(index + 1),
			SkillID:   1,
			Power:     10,
			Level:     1,
			ExpiresAt: now.Add(5 * time.Second),
		}, now)
	}
}

func (runtime *combinedRuntimeBenchmark) inventoryStep() {
	from := model.InventoryEquipmentSlots
	to := from + 1
	if runtime.step%2 == 0 {
		from, to = to, from
	}
	for index := 0; index < runtime.inventoryOps; index++ {
		player := runtime.players[index]
		if moved, _ := player.Inventory.MoveItems(from, to, player); !moved {
			panic(fmt.Sprintf("combined benchmark inventory relocation failed for player %d", index+1))
		}
	}
}

// BenchmarkCombinedRuntimeStep measures synchronized in-memory runtime state,
// excluding packet parsing, socket writes, visibility fan-out, timers, and SQL.
// One operation represents one synthetic second at the documented action mix.
func BenchmarkCombinedRuntimeStep(b *testing.B) {
	benchmarks := []struct {
		name      string
		movement  bool
		combat    bool
		inventory bool
	}{
		{name: "Movement", movement: true},
		{name: "Combat", combat: true},
		{name: "Inventory", inventory: true},
		{name: "Combined", movement: true, combat: true, inventory: true},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			runtime := newCombinedRuntimeBenchmark(
				combinedBenchmarkPlayers,
				combinedBenchmarkMovementsPerStep,
				combinedBenchmarkAttacksPerStep,
				combinedBenchmarkSkillImpactsPerStep,
				combinedBenchmarkInventoryOpsPerStep,
			)
			base := time.Unix(1_700_000_000, 0)
			b.ReportAllocs()
			for b.Loop() {
				runtime.step++
				now := base.Add(time.Duration(runtime.step) * time.Second)
				if benchmark.movement {
					runtime.movementStep()
				}
				if benchmark.combat {
					runtime.combatStep(now)
				}
				if benchmark.inventory {
					runtime.inventoryStep()
				}
			}

			if benchmark.movement {
				b.ReportMetric(combinedBenchmarkMovementsPerStep, "moves/op")
			}
			if benchmark.combat {
				b.ReportMetric(combinedBenchmarkAttacksPerStep, "attacks/op")
				b.ReportMetric(combinedBenchmarkSkillImpactsPerStep, "skills/op")
			}
			if benchmark.inventory {
				b.ReportMetric(combinedBenchmarkInventoryOpsPerStep, "inventory-ops/op")
			}
		})
	}
}

// BenchmarkCombinedRuntimeStep10K scales the 4,000-player action mix by 2.5.
func BenchmarkCombinedRuntimeStep10K(b *testing.B) {
	const (
		players      = 10_000
		movements    = 10_000
		attacks      = 2_500
		skills       = 1_250
		inventoryOps = 250
	)
	runtime := newCombinedRuntimeBenchmark(players, movements, attacks, skills, inventoryOps)
	base := time.Unix(1_700_000_000, 0)
	b.ReportAllocs()
	for b.Loop() {
		runtime.step++
		now := base.Add(time.Duration(runtime.step) * time.Second)
		runtime.movementStep()
		runtime.combatStep(now)
		runtime.inventoryStep()
	}
	b.ReportMetric(movements, "moves/op")
	b.ReportMetric(attacks, "attacks/op")
	b.ReportMetric(skills, "skills/op")
	b.ReportMetric(inventoryOps, "inventory-ops/op")
}
