package service

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestWinterEventDropUsesBinaryAuthoredMonsterItemChanceAndCount(t *testing.T) {
	previousChars, previousItems := model.RefChars, model.RefItems
	t.Cleanup(func() {
		model.RefChars, model.RefItems = previousChars, previousItems
	})
	model.RefChars = map[uint32]model.RefChar{
		28811: {RefObject: model.RefObject{ID: 28811, CodeName: winterEventMonsterCode}},
	}
	model.RefItems = map[uint32]model.RefItem{
		28681: {RefObject: model.RefObject{ID: 28681, CodeName: winterEventItemCode}},
	}
	random := &scriptedDropRandom{floats: []float64{0.4999, 0.5}}
	service := NewMonsterDropService(nil, random, nil, nil)
	if err := service.ConfigureWinterEvent(true); err != nil {
		t.Fatalf("ConfigureWinterEvent() error = %v", err)
	}
	snowman := &model.NPC{SRObject: model.SRObject{RefObjectID: 28811}}
	first := service.appendWinterEventDrop(nil, snowman)
	if len(first) != 1 || first[0].ReferenceItemID != 28681 || first[0].StackCount != 1 {
		t.Fatalf("first Winter drop = %#v, want one snowflake", first)
	}
	if second := service.appendWinterEventDrop(nil, snowman); len(second) != 0 {
		t.Fatalf("boundary roll produced %#v, want no drop", second)
	}
}

func TestWinterEventDropRequiresFeatureAndExactReferences(t *testing.T) {
	previousChars, previousItems := model.RefChars, model.RefItems
	t.Cleanup(func() {
		model.RefChars, model.RefItems = previousChars, previousItems
	})
	model.RefChars = map[uint32]model.RefChar{28811: {RefObject: model.RefObject{ID: 28811, CodeName: winterEventMonsterCode}}}
	model.RefItems = map[uint32]model.RefItem{28681: {RefObject: model.RefObject{ID: 28681, CodeName: winterEventItemCode}}}
	service := NewMonsterDropService(nil, &scriptedDropRandom{floats: []float64{0}}, nil, nil)
	snowman := &model.NPC{SRObject: model.SRObject{RefObjectID: 28811}}
	if got := service.appendWinterEventDrop(nil, snowman); len(got) != 0 {
		t.Fatalf("disabled Winter event produced %#v", got)
	}
	if err := service.ConfigureWinterEvent(true); err != nil {
		t.Fatalf("ConfigureWinterEvent() error = %v", err)
	}
	ordinary := &model.NPC{SRObject: model.SRObject{RefObjectID: 123}}
	if got := service.appendWinterEventDrop(nil, ordinary); len(got) != 0 {
		t.Fatalf("ordinary monster produced Winter drop %#v", got)
	}
	model.RefItems = nil
	if err := service.ConfigureWinterEvent(true); err == nil {
		t.Fatal("ConfigureWinterEvent accepted a missing snowflake reference")
	}
}

func TestBattleArenaTicketDropRequiresAuthoredArenaInstance(t *testing.T) {
	previousItems := model.RefItems
	t.Cleanup(func() { model.RefItems = previousItems })
	model.RefItems = map[uint32]model.RefItem{
		25835: {RefObject: model.RefObject{ID: 25835, CodeName: battleArenaTicketCode}},
	}
	service := NewMonsterDropService(nil, &scriptedDropRandom{}, nil, nil)
	if err := service.ConfigureBattleArenaTicketDropRate(100); err != nil {
		t.Fatalf("ConfigureBattleArenaTicketDropRate() error = %v", err)
	}
	arenaScope, err := model.NewGameWorldScope(42, 7)
	if err != nil {
		t.Fatalf("NewGameWorldScope() error = %v", err)
	}
	arenaMonster := &model.NPC{SRObject: model.SRObject{RefObjectID: 900}}
	arenaMonster.SetGameWorldID(uint32(arenaScope))
	got := service.appendBattleArenaTicket(nil, arenaMonster)
	if len(got) != 1 || got[0].ReferenceItemID != 25835 || got[0].StackCount != 1 {
		t.Fatalf("Arena ticket drop = %#v, want one ticket", got)
	}
	questScope, err := model.NewGameWorldScope(10, 7)
	if err != nil {
		t.Fatalf("NewGameWorldScope() error = %v", err)
	}
	arenaMonster.SetGameWorldID(uint32(questScope))
	if got := service.appendBattleArenaTicket(nil, arenaMonster); len(got) != 0 {
		t.Fatalf("quest instance produced Arena ticket %#v", got)
	}
	arenaMonster.SetGameWorldID(42) // world without an allocated match layer
	if got := service.appendBattleArenaTicket(nil, arenaMonster); len(got) != 0 {
		t.Fatalf("uninstanced Arena world produced ticket %#v", got)
	}
}

func TestBattleArenaTicketAdaptiveAccumulatorIsScopedPerMatch(t *testing.T) {
	service := NewMonsterDropService(nil, &scriptedDropRandom{ints: []int{75, 100, 75, 0}}, nil, nil)
	service.battleArena = battleArenaTicketDrop{rate: 50, itemRefID: 25835, accumulator: make(map[uint32]int)}
	if service.rollBattleArenaTicket(0x0007002A) {
		t.Fatal("first roll 75 unexpectedly passed rate 50")
	}
	if !service.rollBattleArenaTicket(0x0007002A) {
		t.Fatal("second adaptive roll 100 did not pass accumulator 100")
	}
	if service.rollBattleArenaTicket(0x0007002A) {
		t.Fatal("third roll 75 unexpectedly passed accumulator 50")
	}
	if !service.rollBattleArenaTicket(0x0008002A) {
		t.Fatal("separate match did not start a fresh accumulator")
	}
	if got := service.battleArena.accumulator[0x0007002A]; got != 100 {
		t.Fatalf("first match accumulator = %d, want 100", got)
	}
	if got := service.battleArena.accumulator[0x0008002A]; got != 0 {
		t.Fatalf("second match accumulator = %d, want 0", got)
	}
}

func TestBattleArenaTicketDropRateValidationAndDisabledCatalogDefault(t *testing.T) {
	service := NewMonsterDropService(nil, &scriptedDropRandom{}, nil, nil)
	if err := service.ConfigureBattleArenaTicketDropRate(0); err != nil {
		t.Fatalf("zero authored rate error = %v", err)
	}
	for _, rate := range []int{-1, 101} {
		if err := service.ConfigureBattleArenaTicketDropRate(rate); err == nil {
			t.Fatalf("rate %d was accepted", rate)
		}
	}
}

func TestMonsterDropRollDirectCountAndGoldClassification(t *testing.T) {
	random := &scriptedDropRandom{ints: []int{0, 0, 0, 0, 0, 16384}}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {
			Direct: []DropRecord{{ReferenceItemID: 900, DropType: 3, MinCount: 2, MaxCount: 2, Chance: 1}},
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
	if specs[2].ReferenceItemID != 1 || specs[2].StackCount != 1 || specs[2].Data != 15 {
		t.Fatalf("gold spec = %+v, want ref 1 data 15 with unit stack", specs[2])
	}
	for _, spec := range specs[:2] {
		if spec.ReferenceItemID != 900 || spec.DropType != 3 || spec.StackCount != 1 {
			t.Fatalf("direct spec = %+v", spec)
		}
	}
}

func TestMonsterDropCatalogMaterializesRecoveredProfileConsumable(t *testing.T) {
	profiles := NewDropProfileCatalog(
		[]DropClassSelection{{Family: DropProfileRecover, MonsterLevel: 10, Group: 0, Probability: 1}},
		[]DropItemAssignment{{SourceRow: 2, Family: DropProfileRecover, ReferenceItemID: 900, Relative: 100, Absolute: 100, AssignedGroup: 0, DropCount: 25, Country: 3}},
		nil,
	)
	catalog := NewMonsterDropCatalogWithProfiles(map[uint32]MonsterDropTable{77: {}}, nil, profiles)
	specs, err := catalog.RollForMonsterCountry(77, 10, 1, &scriptedDropRandom{})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].ReferenceItemID != 900 || specs[0].StackCount != 25 {
		t.Fatalf("profile specs = %+v", specs)
	}
}

func TestMonsterDropRollRandomGroupHonorsOverlap(t *testing.T) {
	random := &scriptedDropRandom{ints: []int{0, 0}}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{
		77: {
			Random: []DropGroupAssignment{{GroupID: 5, MinCount: 2, MaxCount: 2, Chance: 1}},
			Groups: map[uint32]DropGroup{5: {ID: 5, Candidates: []DropGroupCandidate{
				{ReferenceItemID: 901, MagicGroupID: 71, Weight: 1, Overlap: 1},
				{ReferenceItemID: 902, Weight: 1, Overlap: 1},
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
			Direct: []DropRecord{{ReferenceItemID: 900, MinCount: 4, MaxCount: 2, Chance: 1}},
			Random: []DropGroupAssignment{{GroupID: 99, MinCount: 1, MaxCount: 1, Chance: 1}},
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

func TestMonsterDropChanceUsesBinaryTwoDrawMillionPointRoll(t *testing.T) {
	// (1 | 15<<15) == 491521. An inclusive ratio of 0.491521 accepts this
	// exact roll; reducing either the ratio or factor rejects it.
	random := &scriptedDropRandom{ints: []int{1, 15}}
	if !rollChance(random, 0.491521, 1) {
		t.Fatal("inclusive binary million-point boundary rejected")
	}
	if len(random.ints) != 0 {
		t.Fatalf("roll consumed %d draws, want 2", 2-len(random.ints))
	}
	if rollChance(&scriptedDropRandom{ints: []int{1, 15}}, 0.491520, 1) {
		t.Fatal("roll above ratio threshold accepted")
	}
	if rollChance(&scriptedDropRandom{ints: []int{1, 15}}, 0.983041, 0.5) {
		t.Fatal("roll above factor-adjusted threshold accepted")
	}

	// Ratio one still consumes the two native draws so subsequent selector
	// decisions see the same PRNG sequence as SR_GameServer.
	one := &scriptedDropRandom{ints: []int{32767, 32767}}
	if !rollChance(one, 1, 1) || len(one.ints) != 0 {
		t.Fatal("ratio-one roll did not accept after consuming both draws")
	}
}

func TestMonsterDropRuntimeRateMultipliesAuthoredLootProbability(t *testing.T) {
	table := MonsterDropTable{Direct: []DropRecord{{ReferenceItemID: 900, MinCount: 1, MaxCount: 1, Chance: 0.000001}}}

	base, err := rollMonsterDropTableAtRate(table, 1, 0, NativeDropExceptionContext{}, nil, &scriptedDropRandom{ints: []int{5000, 0}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != 0 {
		t.Fatalf("base rate produced drops = %+v", base)
	}

	scaled, err := rollMonsterDropTableAtRate(table, 1, 0, NativeDropExceptionContext{}, nil, &scriptedDropRandom{ints: []int{5000, 0}}, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(scaled) != 1 || scaled[0].ReferenceItemID != 900 {
		t.Fatalf("10000x rate produced drops = %+v, want item 900", scaled)
	}
}

func TestGoldAmountRateScalesAmountWithoutChangingProbability(t *testing.T) {
	record := &GoldDropRecord{Chance: 50, Min: 10, Max: 10}
	if _, ok := rollBinaryGoldAtAmountRate(record, 0, &scriptedDropRandom{ints: []int{75}}, 10_000); ok {
		t.Fatal("gold amount rate incorrectly increased drop probability")
	}
	amount, ok := rollBinaryGoldAtAmountRate(record, 0, &scriptedDropRandom{ints: []int{25, 0}}, 500)
	if !ok || amount != 50 {
		t.Fatalf("scaled gold = %d, %t; want 50, true", amount, ok)
	}
}

func TestLootRateScalesGoldProbability(t *testing.T) {
	record := &GoldDropRecord{Chance: 0.7, Min: 10, Max: 10}
	if _, ok := rollBinaryGoldAtRates(record, 0, &scriptedDropRandom{ints: []int{75, 0}}, 10_000, 100); !ok {
		t.Fatal("10000x loot rate did not scale the native gold probability")
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
	if len(specs) != 5 || specs[0].Data != 720 || specs[0].StackCount != 1 {
		t.Fatalf("gold specs = %+v, want five class-4 passes beginning with data 720/unit stack", specs)
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

func TestMonsterDropServiceUsesSpawnedNPCRarityAsDropClass(t *testing.T) {
	world := &groundItemWorldFake{}
	groundItems := NewGroundItemService(world, nil)
	materializer := &groundItemMaterializerFake{}
	catalog := NewMonsterDropCatalog(map[uint32]MonsterDropTable{77: {
		Gold: &GoldDropRecord{Chance: 100, Min: 10, Max: 10},
	}})
	service := NewMonsterDropService(catalog, &scriptedDropRandom{}, groundItems, materializer)
	target := &model.NPC{SRObject: model.SRObject{RefObjectID: 77}}
	target.SetRarity(byte(model.MonsterPartyGiant))

	spawned, err := service.SpawnForKill(context.Background(), nil, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(spawned) != 5 {
		t.Fatalf("spawned %d drops, want five selector passes for party giant class 0x14", len(spawned))
	}
}

func TestMonsterDropServiceSpawnsQualifiedQuestCollectionItem(t *testing.T) {
	world := &groundItemWorldFake{}
	groundItems := NewGroundItemService(world, nil)
	materializer := &groundItemMaterializerFake{}
	random := &scriptedDropRandom{floats: []float64{0.24, 0.25}}
	service := NewMonsterDropService(NewMonsterDropCatalog(nil), random, groundItems, materializer)
	target := &model.NPC{SRObject: model.SRObject{RefObjectID: 77}}
	drop := model.QuestCollectionDrop{QuestID: 13, ReferenceItemID: 2211, StackCount: 1, ChancePercent: 25}

	item, spawned, err := service.SpawnQuestCollectionItem(context.Background(), nil, target, drop)
	if err != nil {
		t.Fatal(err)
	}
	if !spawned || item == nil || item.GetRefObjectID() != 2211 || world.object != item {
		t.Fatalf("quest drop = item=%v spawned=%t world=%v", item, spawned, world.object)
	}

	world.object = nil
	item, spawned, err = service.SpawnQuestCollectionItem(context.Background(), nil, target, drop)
	if err != nil {
		t.Fatal(err)
	}
	if spawned || item != nil || world.object != nil {
		t.Fatalf("failed quest roll materialized item=%v spawned=%t world=%v", item, spawned, world.object)
	}
}

func TestGoldAmountRateIncludesLiveMerchantBlessing(t *testing.T) {
	now := time.Unix(1000, 0)
	player := &model.Player{}
	if _, ok := player.CommitLinkedActiveBuffAt(
		7001,
		9001,
		1,
		0,
		30*time.Minute,
		model.BuffModifiers{GoldDropAmountPercent: 50},
		now,
	); !ok {
		t.Fatal("failed to install merchant blessing")
	}

	if got := goldAmountRateForKiller(200, player, now.Add(time.Minute)); got != 300 {
		t.Fatalf("gold amount rate = %d%%, want 300%%", got)
	}
	if got := goldAmountRateForKiller(200, player, now.Add(31*time.Minute)); got != 200 {
		t.Fatalf("expired gold amount rate = %d%%, want 200%%", got)
	}
}
