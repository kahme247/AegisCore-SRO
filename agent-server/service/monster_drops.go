package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

var (
	ErrMonsterDropCatalogUnavailable = errors.New("monster-drop catalog is unavailable")
	ErrMonsterDropTableInvalid       = errors.New("monster-drop table is invalid")
)

// MonsterDropCatalogLoader is the startup boundary between SQL reference
// data and the pure selector. Implementations must return a complete snapshot
// or an error; partial tables are not safe to publish to combat.
type MonsterDropCatalogLoader interface {
	Load(context.Context) (*MonsterDropCatalog, error)
}

const dropRandomMillion = 1_000_000

// DropRandom is deliberately small so binary-derived roll sequences can be
// replayed in tests without depending on the process-global PRNG.
type DropRandom interface {
	Intn(int) int
	Float64() float64
}

// DropRecord is the normalized form of the direct assigned-item record. The
// pinned binary uses a 0x30-byte private record; these fields are the values
// recovered from its roll routine, not a claim about the original C++ type.
type DropRecord struct {
	ReferenceItemID uint32
	RentCodeName    string
	DropType        byte
	OptLevel        byte
	MinCount        byte
	MaxCount        byte
	Chance          float64
	MagicOptions    []model.MagicOption
	Rent            model.RentInfo
}

type DropGroupCandidate struct {
	ReferenceItemID uint32
	MagicGroupID    uint32
	Weight          float64
	Overlap         byte
	MagicOptions    []model.MagicOption
}

type DropGroup struct {
	ID         uint32
	Candidates []DropGroupCandidate
}

type DropGroupAssignment struct {
	GroupID  uint32
	DropType byte
	Overlap  byte
	MinCount byte
	MaxCount byte
	Chance   float64
	Rent     model.RentInfo
}

type GoldDropRecord struct {
	// Chance is the DropProb percentage compared against rand()%101, unlike
	// direct/group Chance values which use the million-point roll.
	Chance float64
	Min    uint64
	Max    uint64
}

// MonsterDropTable is a hydrated, immutable-at-runtime table for one monster
// reference. It contains only selector inputs; ITEM insertion and world
// admission remain GroundItemService responsibilities.
type MonsterDropTable struct {
	Direct      []DropRecord
	Random      []DropGroupAssignment
	Groups      map[uint32]DropGroup
	Gold        *GoldDropRecord
	GoldByLevel map[int]GoldDropRecord
	MaxItems    int
	DropClass   byte
	ItemFactor  float64
	// GoldFactor is intentionally not consumed by the binary-confirmed
	// gold path. The executable reads its separate configured gold factor in
	// another path that has not been recovered.
	GoldFactor float64
}

// MonsterDropCatalog stores hydrated tables keyed by monster reference ID.
// Loading these records from SQL is intentionally separate because the pinned
// executable's original reference-data query was not recovered.
type MonsterDropCatalog struct {
	tables             map[uint32]MonsterDropTable
	defaultGoldByLevel map[int]GoldDropRecord
}

func NewMonsterDropCatalog(tables map[uint32]MonsterDropTable) *MonsterDropCatalog {
	return NewMonsterDropCatalogWithGold(tables, nil)
}

func NewMonsterDropCatalogWithGold(tables map[uint32]MonsterDropTable, defaultGold map[int]GoldDropRecord) *MonsterDropCatalog {
	copyTables := make(map[uint32]MonsterDropTable, len(tables))
	for id, table := range tables {
		table.Direct = append([]DropRecord(nil), table.Direct...)
		table.Random = append([]DropGroupAssignment(nil), table.Random...)
		table.Groups = cloneDropGroups(table.Groups)
		if table.Gold != nil {
			gold := *table.Gold
			table.Gold = &gold
		}
		if table.GoldByLevel != nil {
			table.GoldByLevel = cloneGoldByLevel(table.GoldByLevel)
		}
		copyTables[id] = table
	}
	return &MonsterDropCatalog{tables: copyTables, defaultGoldByLevel: cloneGoldByLevel(defaultGold)}
}

func cloneGoldByLevel(source map[int]GoldDropRecord) map[int]GoldDropRecord {
	if source == nil {
		return nil
	}
	copy := make(map[int]GoldDropRecord, len(source))
	for level, gold := range source {
		copy[level] = gold
	}
	return copy
}

func cloneDropGroups(groups map[uint32]DropGroup) map[uint32]DropGroup {
	if groups == nil {
		return nil
	}
	result := make(map[uint32]DropGroup, len(groups))
	for id, group := range groups {
		group.Candidates = append([]DropGroupCandidate(nil), group.Candidates...)
		for index := range group.Candidates {
			group.Candidates[index].MagicOptions = append([]model.MagicOption(nil), group.Candidates[index].MagicOptions...)
		}
		result[id] = group
	}
	return result
}

// Roll returns the materialized drop descriptions in the same broad order as
// the recovered coordinator: gold first, then direct and random-group item
// selections. A full result list stops later attempts, matching the binary's
// bounded-list behavior. Profile-driven and named exception generators are
// intentionally not synthesized here until their reference records are
// recovered; callers must install those as a separate producer extension.
func (c *MonsterDropCatalog) Roll(monsterRefID uint32, random DropRandom) ([]model.GroundItemSpec, error) {
	return c.RollForMonster(monsterRefID, 0, random)
}

// RollForMonster selects the level-specific gold row when the catalog has
// one. A zero level preserves the legacy ref-only API and uses the table's
// fallback Gold row.
func (c *MonsterDropCatalog) RollForMonster(monsterRefID uint32, monsterLevel int, random DropRandom) ([]model.GroundItemSpec, error) {
	if c == nil || c.tables == nil {
		return nil, ErrMonsterDropCatalogUnavailable
	}
	table, ok := c.tables[monsterRefID]
	if !ok {
		if len(c.defaultGoldByLevel) == 0 {
			return nil, fmt.Errorf("%w: monster reference %d", ErrMonsterDropCatalogUnavailable, monsterRefID)
		}
		table = MonsterDropTable{GoldByLevel: cloneGoldByLevel(c.defaultGoldByLevel)}
	}
	if random == nil {
		return nil, ErrMonsterDropCatalogUnavailable
	}
	return rollMonsterDropTable(table, monsterLevel, random)
}

func rollMonsterDropTable(table MonsterDropTable, monsterLevel int, random DropRandom) ([]model.GroundItemSpec, error) {
	if table.MaxItems <= 0 {
		table.MaxItems = 16
	}
	if table.MaxItems > 255 {
		table.MaxItems = 255
	}
	factor := table.ItemFactor
	if factor == 0 {
		factor = 1
	}
	result := make([]model.GroundItemSpec, 0, min(table.MaxItems, 8))
	gold := table.Gold
	if configured, ok := table.GoldByLevel[monsterLevel]; ok {
		gold = &configured
	}
	if gold != nil && len(result) < table.MaxItems {
		if amount, ok := rollBinaryGold(gold, table.DropClass, random); ok {
			refID := uint32(3)
			switch {
			case amount < 1000:
				refID = 1
			case amount < 10000:
				refID = 2
			}
			// The recovered amount-credit path reads item-data +0x38. The
			// persisted equivalent is DATA; STACK_COUNT remains one because
			// this is currency credit, not an item-stack merge.
			result = append(result, model.GroundItemSpec{ReferenceItemID: refID, StackCount: 1, Data: clampDropData(amount)})
		}
	}

	for _, record := range table.Direct {
		if len(result) >= table.MaxItems {
			break
		}
		if record.ReferenceItemID == 0 || record.MaxCount < record.MinCount {
			continue
		}
		count := inclusiveCount(record.MinCount, record.MaxCount, random)
		for attempt := byte(0); attempt < count && len(result) < table.MaxItems; attempt++ {
			if !rollChance(random, record.Chance, factor) {
				continue
			}
			result = append(result, dropSpec(record.ReferenceItemID, record.RentCodeName, 0, record.DropType, record.OptLevel, 1, record.MagicOptions, record.Rent))
		}
	}

	for _, assignment := range table.Random {
		if len(result) >= table.MaxItems {
			break
		}
		group, exists := table.Groups[assignment.GroupID]
		if !exists || len(group.Candidates) == 0 || assignment.MaxCount < assignment.MinCount || !rollChance(random, assignment.Chance, factor) {
			continue
		}
		count := inclusiveCount(assignment.MinCount, assignment.MaxCount, random)
		available := append([]DropGroupCandidate(nil), group.Candidates...)
		for selection := byte(0); selection < count && len(result) < table.MaxItems && len(available) > 0; selection++ {
			var candidate DropGroupCandidate
			index := 0
			accepted := false
			// A failed weight roll may retry another candidate. The binary's
			// loop is candidate-driven; this bounded safety cap prevents a
			// malformed all-zero group from hanging an Agent worker.
			for attempt := 0; attempt < len(available); attempt++ {
				candidate, index = chooseCandidate(available, random)
				if rollChance(random, candidate.Weight, factor) {
					accepted = true
					break
				}
			}
			if !accepted {
				break
			}
			result = append(result, dropSpec(candidate.ReferenceItemID, "", candidate.MagicGroupID, assignment.DropType, 0, 1, candidate.MagicOptions, assignment.Rent))
			if candidate.Overlap == 1 {
				available = append(available[:index], available[index+1:]...)
			}
		}
	}
	return result, nil
}

func dropSpec(refID uint32, rentCodeName string, magicGroupID uint32, dropType, optLevel byte, count uint16, magic []model.MagicOption, rent model.RentInfo) model.GroundItemSpec {
	return model.GroundItemSpec{ReferenceItemID: refID, RentCodeName: rentCodeName, MagicGroupID: magicGroupID, DropType: dropType, OptLevel: optLevel, StackCount: count, MagicOptions: append([]model.MagicOption(nil), magic...), Rent: rent}
}

func chooseCandidate(candidates []DropGroupCandidate, random DropRandom) (DropGroupCandidate, int) {
	index := random.Intn(len(candidates))
	return candidates[index], index
}

func inclusiveCount(minimum, maximum byte, random DropRandom) byte {
	if minimum >= maximum {
		return minimum
	}
	return byte(int(minimum) + random.Intn(int(maximum-minimum)+1))
}

func rollChance(random DropRandom, chance, factor float64) bool {
	if chance <= 0 || factor <= 0 {
		return false
	}
	threshold := chance * factor
	if threshold >= dropRandomMillion {
		return true
	}
	return float64(random.Intn(dropRandomMillion)) <= threshold
}

func rollBinaryGold(record *GoldDropRecord, dropClass byte, random DropRandom) (uint64, bool) {
	if record == nil || random == nil || record.Max < record.Min {
		return 0, false
	}
	// 0x00724330 uses rand() % 101 <= DropProb. DropProb is the
	// percentage field in _RefDropGold, not the million-point item ratio.
	if float64(random.Intn(101)) > record.Chance {
		return 0, false
	}
	// The source uses a 15-bit rand() value and divides by 32767.0.
	unit := float64(random.Intn(32768)) / 32767.0
	amount := math.Round(float64(record.Min) + float64(record.Max-record.Min)*unit)
	if amount <= 0 || amount >= float64(^uint64(0)) {
		return 0, false
	}

	// 0x00726900 accepts high nibbles 0 and 1. Other values take the
	// executable's assertion path and must not silently create currency.
	switch dropClass & 0xF0 {
	case 0:
	case 0x10:
		amount = math.Round(amount * 9.0)
	default:
		return 0, false
	}
	// These low-nibble class multipliers are directly recovered. Profile
	// supplied factors remain an explicit future input rather than an
	// invented SQL/config value.
	switch dropClass & 0x0F {
	case 3, 8:
		amount = math.Round(amount * 30.0)
	case 4, 7:
		amount = math.Round(amount * 4.0)
	}
	if amount <= 0 || amount >= float64(^uint64(0)) {
		return 0, false
	}
	return uint64(amount), true
}

func clampDropStack(amount uint64) uint16 {
	if amount > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(max(uint64(1), amount))
}

func clampDropData(amount uint64) uint32 {
	if amount > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(amount)
}

// MonsterDropService connects a hydrated selector to the persisted/world
// lifecycle. It is deliberately injectable so a server can use either the
// recovered TestIn catalog adapter or a compatibility reference loader.
type MonsterDropService struct {
	catalog      *MonsterDropCatalog
	random       DropRandom
	groundItems  *GroundItemService
	materializer model.GroundItemMaterializer
}

func NewMonsterDropService(catalog *MonsterDropCatalog, random DropRandom, groundItems *GroundItemService, materializer model.GroundItemMaterializer) *MonsterDropService {
	return &MonsterDropService{catalog: catalog, random: random, groundItems: groundItems, materializer: materializer}
}

func (s *MonsterDropService) SpawnForKill(ctx context.Context, killer *model.Player, target *model.NPC) ([]*model.Item, error) {
	if s == nil || s.catalog == nil || s.random == nil || s.groundItems == nil || s.materializer == nil || target == nil {
		return nil, ErrMonsterDropCatalogUnavailable
	}
	level, _, _, _ := target.AIProfile()
	specs, err := s.catalog.RollForMonster(target.GetRefObjectID(), level, s.random)
	if err != nil {
		return nil, err
	}
	partyNumber := uint32(0)
	if killer != nil {
		if party := killer.GetParty(); party != nil {
			partyNumber = party.Number
		}
	}
	spawned := make([]*model.Item, 0, len(specs))
	position := target.GetNavmeshPosition()
	for _, spec := range specs {
		spec.NavmeshPosition = position
		item, spawnErr := s.groundItems.MaterializeAndSpawn(ctx, s.materializer, spec, partyNumber)
		if spawnErr != nil {
			return spawned, spawnErr
		}
		spawned = append(spawned, item)
	}
	return spawned, nil
}

// sortedDropIDs is kept for deterministic catalog diagnostics and tests.
func (c *MonsterDropCatalog) sortedDropIDs() []uint32 {
	ids := make([]uint32, 0, len(c.tables))
	for id := range c.tables {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
