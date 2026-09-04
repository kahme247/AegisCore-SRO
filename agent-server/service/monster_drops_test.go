package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type scriptedDropRandom struct {
	ints   []int
	floats []float64
}

func (r *scriptedDropRandom) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	if len(r.ints) == 0 {
		return 0
	}
	value := r.ints[0]
	r.ints = r.ints[1:]
	if value < 0 {
		value = -value
	}
	return value % n
}

func (r *scriptedDropRandom) Float64() float64 {
	if len(r.floats) == 0 {
		return 0
	}
	value := r.floats[0]
	r.floats = r.floats[1:]
	return value
}

func TestMonsterDropRollDirectCountAndGoldClassification(t *testing.T) {
	random := &scriptedDropRandom{ints: []int{0, 16384}}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {
			Direct: []DropRecord{{ReferenceItemID: 900, DropType: 3, MinCount: 2, MaxCount: 2, Chance: dropRandomMillion}},
			Gold:   &GoldDropRecord{Chance: 100, Min: 10, Max: 20},
		},
	})
	specs, err := catalog.Roll(77, random)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("drop count = %d, want 3: %+v", len(specs), specs)
	}
	if specs[0].ReferenceItemID != 1 || specs[0].StackCount != 1 || specs[0].Data != 15 {
		t.Fatalf("gold spec = %+v, want ref 1 data 15 with unit stack", specs[0])
	}
	for _, spec := range specs[1:] {
		if spec.ReferenceItemID != 900 || spec.DropType != 3 || spec.StackCount != 1 {
			t.Fatalf("direct spec = %+v", spec)
		}
	}
}

func TestMonsterDropRollRandomGroupHonorsOverlap(t *testing.T) {
	random := &scriptedDropRandom{ints: []int{0, 0}}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {
			Random: []DropGroupAssignment{{GroupID: 5, MinCount: 2, MaxCount: 2, Chance: dropRandomMillion}},
			Groups: map[uint32]DropGroup{5: {ID: 5, Candidates: []DropGroupCandidate{
				{ReferenceItemID: 901, MagicGroupID: 71, Weight: dropRandomMillion, Overlap: 1},
				{ReferenceItemID: 902, Weight: dropRandomMillion, Overlap: 1},
			}}},
		},
	})
	specs, err := catalog.Roll(77, random)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].ReferenceItemID != 901 || specs[0].MagicGroupID != 71 || specs[1].ReferenceItemID != 902 {
		t.Fatalf("group specs = %+v, want refs 901 then 902", specs)
	}
}

func TestMonsterDropRollSkipsMalformedAndMissingGroups(t *testing.T) {
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {
			Direct: []DropRecord{{ReferenceItemID: 900, MinCount: 4, MaxCount: 2, Chance: dropRandomMillion}},
			Random: []DropGroupAssignment{{GroupID: 99, MinCount: 1, MaxCount: 1, Chance: dropRandomMillion}},
		},
	})
	specs, err := catalog.Roll(77, &scriptedDropRandom{})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("malformed table produced specs = %+v", specs)
	}
	if _, err := catalog.Roll(88, &scriptedDropRandom{}); !errors.Is(err, ErrMonsterDropCatalogUnavailable) {
		t.Fatalf("missing monster error = %v", err)
	}
}

func TestMonsterDropCatalogUsesLevelGoldForUnassignedMonster(t *testing.T) {
	catalog := NewMonsterDropCatalogWithGold(nil, map[int]GoldDropRecord{
		42: {Chance: 100, Min: 100, Max: 100},
	})
	specs, err := catalog.RollForMonster(7000, 42, &scriptedDropRandom{})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Data != 100 || specs[0].StackCount != 1 {
		t.Fatalf("specs = %+v, want one level-gold drop with unit stack", specs)
	}
}

func TestMonsterDropGoldUsesBinaryChanceInterpolationAndClassMultiplier(t *testing.T) {
	// chance roll 100 succeeds at DropProb 100; the second draw is the
	// maximum 15-bit value, so the unscaled amount is exactly GoldMax.
	random := &scriptedDropRandom{ints: []int{100, 32767}}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {
			DropClass: 0x14, // high nibble 1 => x9; low nibble 4 => x4
			Gold:      &GoldDropRecord{Chance: 100, Min: 10, Max: 20},
		},
	})
	specs, err := catalog.Roll(77, random)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Data != 720 || specs[0].StackCount != 1 {
		t.Fatalf("gold specs = %+v, want data 720/unit stack", specs)
	}
}

func TestMonsterDropGoldRejectsInvalidHighClassNibble(t *testing.T) {
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {DropClass: 0x20, Gold: &GoldDropRecord{Chance: 100, Min: 1, Max: 1}},
	})
	specs, err := catalog.Roll(77, &scriptedDropRandom{ints: []int{0, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("invalid class produced gold specs = %+v", specs)
	}
}

func TestMonsterDropServicePlacesDropsAtKilledMobPosition(t *testing.T) {
	world := &groundItemWorldFake{}
	groundItems := NewGroundItemService(world, nil)
	materializer := &groundItemMaterializerFake{}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{77: {
		Direct: []DropRecord{{ReferenceItemID: 900, MinCount: 1, MaxCount: 1, Chance: dropRandomMillion}},
	}})
	service := NewMonsterDropService(catalog, &scriptedDropRandom{}, groundItems, materializer)
	target := &model.NPC{SRObject: model.SRObject{RefObjectID: 77}}
	spawned, err := service.SpawnForKill(context.Background(), nil, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(spawned) != 1 || world.object != spawned[0] || spawned[0].GetRefObjectID() != 900 {
		t.Fatalf("spawned = %+v/world=%v", spawned, world.object)
	}
}
