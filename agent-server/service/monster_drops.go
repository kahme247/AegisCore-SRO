package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	profiles           *DropProfileCatalog
}

func NewMonsterDropCatalog(tables map[uint32]MonsterDropTable) *MonsterDropCatalog {
	return NewMonsterDropCatalogWithGold(tables, nil)
}

func NewMonsterDropCatalogWithGold(tables map[uint32]MonsterDropTable, defaultGold map[int]GoldDropRecord) *MonsterDropCatalog {
	return NewMonsterDropCatalogWithProfiles(tables, defaultGold, nil)
}

func NewMonsterDropCatalogWithProfiles(tables map[uint32]MonsterDropTable, defaultGold map[int]GoldDropRecord, profiles *DropProfileCatalog) *MonsterDropCatalog {
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
	return &MonsterDropCatalog{tables: copyTables, defaultGoldByLevel: cloneGoldByLevel(defaultGold), profiles: profiles}
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

// Roll returns materialized drop descriptions in the recovered coordinator
// order: random-group assignments, direct assignments, then the repeated
// generic gold/class-selector cycle. A full result list stops later attempts, matching the binary's
// bounded-list behavior. The identity-aware kill path additionally runs the
// named exception generator for native drop classes 3/8; the legacy ref-only
// entry point cannot do so because it has no monster code or TypeID4.
func (c *MonsterDropCatalog) Roll(monsterRefID uint32, random DropRandom) ([]model.GroundItemSpec, error) {
	return c.RollForMonster(monsterRefID, 0, random)
}

// RollForMonster selects the level-specific gold row when the catalog has
// one. A zero level preserves the legacy ref-only API and uses the table's
// fallback Gold row.
func (c *MonsterDropCatalog) RollForMonster(monsterRefID uint32, monsterLevel int, random DropRandom) ([]model.GroundItemSpec, error) {
	return c.RollForMonsterCountry(monsterRefID, monsterLevel, 0, random)
}

func (c *MonsterDropCatalog) RollForMonsterCountry(monsterRefID uint32, monsterLevel, country int, random DropRandom) ([]model.GroundItemSpec, error) {
	return c.rollForMonsterIdentity(monsterRefID, monsterLevel, country, NativeDropExceptionContext{}, random)
}

func (c *MonsterDropCatalog) rollForMonsterIdentity(monsterRefID uint32, monsterLevel, country int, exception NativeDropExceptionContext, random DropRandom) ([]model.GroundItemSpec, error) {
	return c.rollForMonsterIdentityAtRate(monsterRefID, monsterLevel, country, exception, random, 100)
}

func (c *MonsterDropCatalog) rollForMonsterIdentityAtRate(monsterRefID uint32, monsterLevel, country int, exception NativeDropExceptionContext, random DropRandom, ratePercent uint32) ([]model.GroundItemSpec, error) {
	return c.rollForMonsterIdentityAtRates(monsterRefID, monsterLevel, country, exception, random, ratePercent, 100)
}

func (c *MonsterDropCatalog) rollForMonsterIdentityAtRates(monsterRefID uint32, monsterLevel, country int, exception NativeDropExceptionContext, random DropRandom, itemRatePercent, goldAmountRatePercent uint32) ([]model.GroundItemSpec, error) {
	return c.rollForMonsterIdentityAtRuntimeRates(monsterRefID, monsterLevel, country, exception, random, itemRatePercent, goldAmountRatePercent, nil)
}

func (c *MonsterDropCatalog) rollForMonsterIdentityAtRuntimeRates(monsterRefID uint32, monsterLevel, country int, exception NativeDropExceptionContext, random DropRandom, itemRatePercent, goldAmountRatePercent uint32, runtimeDropClass *byte) ([]model.GroundItemSpec, error) {
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
	if runtimeDropClass != nil {
		table.DropClass = *runtimeDropClass
	}
	if table.DropClass&0x0f == 3 || table.DropClass&0x0f == 8 {
		exception.ItemsByCode = nativeExceptionItemReferences()
	}
	return rollMonsterDropTableAtRates(table, monsterLevel, country, exception, c.profiles, random, itemRatePercent, goldAmountRatePercent)
}

func rollMonsterDropTableAtRate(table MonsterDropTable, monsterLevel, country int, exception NativeDropExceptionContext, profiles *DropProfileCatalog, random DropRandom, ratePercent uint32) ([]model.GroundItemSpec, error) {
	return rollMonsterDropTableAtRates(table, monsterLevel, country, exception, profiles, random, ratePercent, 100)
}

func rollMonsterDropTableAtRates(table MonsterDropTable, monsterLevel, country int, exception NativeDropExceptionContext, profiles *DropProfileCatalog, random DropRandom, ratePercent, goldAmountRatePercent uint32) ([]model.GroundItemSpec, error) {
	if table.MaxItems <= 0 {
		table.MaxItems = nativeDropItemCap(table.DropClass)
	}
	if table.MaxItems > 255 {
		table.MaxItems = 255
	}
	factor := table.ItemFactor
	if factor == 0 {
		factor = 1
	}
	if ratePercent == 0 {
		ratePercent = 100
	}
	factor *= float64(ratePercent) / 100
	result := make([]model.GroundItemSpec, 0, min(table.MaxItems, 8))
	if (table.DropClass&0x0f == 3 || table.DropClass&0x0f == 8) && exception.MonsterCode != "" {
		appendNativeDropExceptions(&result, exception, profiles, monsterLevel, country, factor, random)
	}
	// SR_GameServer 0x007245C0 runs random-group assignments before direct
	// assignments, then enters the generic gold/class-selector cycle.
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

	gold := table.Gold
	if configured, ok := table.GoldByLevel[monsterLevel]; ok {
		gold = &configured
	}
	passes := nativeDropSelectorPasses(table.DropClass)
	for pass := 0; pass < passes && len(result) < table.MaxItems; pass++ {
		if gold != nil {
			if amount, ok := rollBinaryGoldAtRates(gold, table.DropClass, random, factor, goldAmountRatePercent); ok {
				refID := uint32(3)
				switch {
				case amount < 1000:
					refID = 1
				case amount < 10000:
					refID = 2
				}
				result = append(result, model.GroundItemSpec{ReferenceItemID: refID, StackCount: 1, Data: clampDropData(amount)})
			}
		}
		if profiles == nil || len(result) >= table.MaxItems {
			continue
		}
		equipmentFamily := DropProfileEquip
		if random.Intn(1000) == 1 {
			equipmentFamily = DropProfileRareEquip
		}
		appendProfileDrop(&result, table.MaxItems, profiles, equipmentFamily, monsterLevel, country, factor, nativeDropClassMultiplier(table.DropClass), random)
		for _, family := range []DropProfileFamily{DropProfileRecover, DropProfileCure, DropProfileReinforce, DropProfileTablet} {
			appendProfileDrop(&result, table.MaxItems, profiles, family, monsterLevel, country, factor, nativeDropClassMultiplier(table.DropClass), random)
		}
		family := DropProfileAmmo
		if random.Intn(2) != 0 {
			family = DropProfileScroll
		}
		appendProfileDrop(&result, table.MaxItems, profiles, family, monsterLevel, country, factor, nativeDropClassMultiplier(table.DropClass), random)
		family = DropProfileMagicStone
		if random.Intn(2) != 0 {
			family = DropProfileAttributeStone
		}
		appendProfileDrop(&result, table.MaxItems, profiles, family, monsterLevel, country, factor, nativeDropClassMultiplier(table.DropClass), random)
	}
	return result, nil
}

func nativeExceptionItemReferences() map[string]uint32 {
	result := make(map[string]uint32, len(nativeExceptionRecipes)+1)
	for id, ref := range model.RefItems {
		if ref.CodeName == nativeExceptionReturn {
			result[ref.CodeName] = id
			continue
		}
		for _, code := range nativeExceptionRecipes {
			if ref.CodeName == code {
				result[code] = id
				break
			}
		}
	}
	return result
}

func appendProfileDrop(result *[]model.GroundItemSpec, maximum int, profiles *DropProfileCatalog, family DropProfileFamily, monsterLevel, country int, factor float64, multiplier int, random DropRandom) {
	if len(*result) >= maximum {
		return
	}
	group, ok := profiles.RollClassGroup(family, monsterLevel, factor, multiplier, random)
	if !ok {
		return
	}
	assignment, accepted := profiles.RollAssignment(family, group, country, random)
	if !accepted {
		return
	}
	if family == DropProfileEquip || family == DropProfileRareEquip {
		variance, durability, valid := GenerateEquipmentBaseState(assignment, random)
		if !valid {
			return
		}
		*result = append(*result, model.GroundItemSpec{ReferenceItemID: assignment.ReferenceItemID, Variance: variance, OptLevel: profiles.RollOptLevel(random), Data: durability, StackCount: 1, MagicOptions: profiles.GenerateEquipmentMagicOptions(assignment, random)})
		return
	}
	*result = append(*result, dropSpec(assignment.ReferenceItemID, "", 0, 0, 0, clampDropStack(uint64(assignment.DropCount)), nil, model.RentInfo{}))
}

func nativeDropItemCap(dropClass byte) int {
	switch dropClass & 0x0f {
	case 1:
		return 9
	case 3:
		return 250
	case 4, 7:
		return 30
	case 5, 8:
		return 60
	case 6:
		return 20
	default:
		return 8
	}
}

func nativeDropSelectorPasses(dropClass byte) int {
	switch dropClass & 0x0f {
	case 1:
		return 2
	case 3, 8:
		return 10
	case 4:
		return 5
	case 5:
		return 60
	case 6:
		return 4
	case 7:
		return 8
	default:
		return 1
	}
}

func nativeDropClassMultiplier(dropClass byte) int {
	multiplier := 1
	if dropClass&0xf0 == 0x10 {
		multiplier = 9
	}
	switch dropClass & 0x0f {
	case 3, 8:
		multiplier *= 60
	case 4:
		multiplier *= 4
	}
	return multiplier
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
	// SR_GameServer 0x00724A00 and 0x00724E30 concatenate two 15-bit
	// rand() results, reduce the 30-bit value modulo one million, and compare
	// it inclusively with DropRatio * 1,000,000. Always consume both draws,
	// including for a ratio of one, because later drop rolls share the PRNG.
	roll := binaryModuloRoll(random, dropRandomMillion)
	threshold := chance * factor * dropRandomMillion
	return float64(roll) <= threshold
}

func binaryModuloRoll(random DropRandom, modulus int) int {
	low := uint32(random.Intn(32768))
	high := uint32(random.Intn(32768))
	return int((low | high<<15) % uint32(modulus))
}

func rollBinaryGold(record *GoldDropRecord, dropClass byte, random DropRandom) (uint64, bool) {
	return rollBinaryGoldAtAmountRate(record, dropClass, random, 100)
}

func rollBinaryGoldAtAmountRate(record *GoldDropRecord, dropClass byte, random DropRandom, amountRatePercent uint32) (uint64, bool) {
	return rollBinaryGoldAtRates(record, dropClass, random, 1, amountRatePercent)
}

func rollBinaryGoldAtRates(record *GoldDropRecord, dropClass byte, random DropRandom, chanceFactor float64, amountRatePercent uint32) (uint64, bool) {
	if record == nil || random == nil || record.Max < record.Min {
		return 0, false
	}
	// 0x00724330 uses rand() % 101 <= DropProb. DropProb is the
	// percentage field in _RefDropGold, not the million-point item ratio.
	if float64(random.Intn(101)) > record.Chance*chanceFactor {
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
	if amountRatePercent == 0 {
		amountRatePercent = 100
	}
	amount = math.Round(amount * float64(amountRatePercent) / 100)
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
	catalog       *MonsterDropCatalog
	random        DropRandom
	groundItems   *GroundItemService
	materializer  model.GroundItemMaterializer
	gameplayRates interface{ Current() model.GameplayRates }
	winterEvent   winterEventDrop
	battleArena   battleArenaTicketDrop
}

const (
	winterEventMonsterCode = "MOB_EV_WINTER_SNOWMAN"
	winterEventItemCode    = "ITEM_ETC_E091216_SNOWFLAKE"
	winterEventChance      = 50.0
	winterEventCount       = 1
)

// winterEventDrop is the maintained form of the hardcoded Winter Event 2009
// fields recovered at SR_GameServer 0x00876DF0/0x00877870. It deliberately
// does not share the ordinary monster-drop profile or its rate multiplier.
type winterEventDrop struct {
	enabled      bool
	monsterRefID uint32
	itemRefID    uint32
}

const battleArenaTicketCode = "ITEM_ETC_BATTLE_ARENA_TICKET"

var battleArenaWorldIDs = map[uint16]struct{}{
	42: {}, 43: {}, 44: {}, // occupy 8/16/32
	45: {}, 46: {}, 47: {}, // flag 8/16/32
	48: {}, 49: {}, 50: {}, // score 8/16/32
}

// battleArenaTicketDrop mirrors the per-CBattleArenaInfo probability state.
// Native helper 0x00599CC0 stores its accumulator under key 1111; one state
// per packed world/layer scope prevents separate matches influencing each
// other while preserving the recovered adaptive sequence.
type battleArenaTicketDrop struct {
	mu          sync.Mutex
	rate        int
	itemRefID   uint32
	accumulator map[uint32]int
}

func (s *MonsterDropService) ConfigureGameplayRates(rates interface{ Current() model.GameplayRates }) {
	if s != nil {
		s.gameplayRates = rates
	}
}

// ConfigureWinterEvent resolves the binary-authored identities from the
// already-published reference snapshot. Enabling the feature with incomplete
// Media data is a startup error instead of silently inventing another item.
func (s *MonsterDropService) ConfigureWinterEvent(enabled bool) error {
	if s == nil {
		return ErrMonsterDropCatalogUnavailable
	}
	s.winterEvent = winterEventDrop{}
	if !enabled {
		return nil
	}
	for id, ref := range model.RefChars {
		if ref.CodeName == winterEventMonsterCode {
			s.winterEvent.monsterRefID = id
			break
		}
	}
	for id, ref := range model.RefItems {
		if ref.CodeName == winterEventItemCode {
			s.winterEvent.itemRefID = id
			break
		}
	}
	if s.winterEvent.monsterRefID == 0 || s.winterEvent.itemRefID == 0 {
		return fmt.Errorf("configure Winter Event 2009 drop: monster %q=%d item %q=%d", winterEventMonsterCode, s.winterEvent.monsterRefID, winterEventItemCode, s.winterEvent.itemRefID)
	}
	s.winterEvent.enabled = true
	return nil
}

// ConfigureBattleArenaTicketDropRate applies the Media
// BATTLE_ARENA_TICKET_DROP_RATE value. The supplied vSRO catalog authors zero;
// operators may select 0..100 without changing ordinary monster loot.
func (s *MonsterDropService) ConfigureBattleArenaTicketDropRate(rate int) error {
	if s == nil {
		return ErrMonsterDropCatalogUnavailable
	}
	if rate < 0 || rate > 100 {
		return fmt.Errorf("configure Battle Arena ticket drop rate: %d is outside 0..100", rate)
	}
	s.battleArena = battleArenaTicketDrop{rate: rate, accumulator: make(map[uint32]int)}
	for id, ref := range model.RefItems {
		if ref.CodeName == battleArenaTicketCode {
			s.battleArena.itemRefID = id
			break
		}
	}
	if s.battleArena.itemRefID == 0 {
		if rate == 0 {
			return nil
		}
		return fmt.Errorf("configure Battle Arena ticket drop: item %q is missing", battleArenaTicketCode)
	}
	return nil
}

func NewMonsterDropService(catalog *MonsterDropCatalog, random DropRandom, groundItems *GroundItemService, materializer model.GroundItemMaterializer) *MonsterDropService {
	return &MonsterDropService{catalog: catalog, random: random, groundItems: groundItems, materializer: materializer}
}

func (s *MonsterDropService) SpawnForKill(ctx context.Context, killer *model.Player, target *model.NPC) ([]*model.Item, error) {
	if s == nil || s.catalog == nil || s.random == nil || s.groundItems == nil || s.materializer == nil || target == nil {
		return nil, ErrMonsterDropCatalogUnavailable
	}
	level, _, _, _ := target.AIProfile()
	ratePercent := uint32(100)
	goldAmountRatePercent := uint32(100)
	if s.gameplayRates != nil {
		rates := s.gameplayRates.Current()
		if configured := rates.LootDropRatePercent; configured != 0 {
			ratePercent = configured
		}
		if configured := rates.GoldDropAmountRatePercent; configured != 0 {
			goldAmountRatePercent = configured
		}
	}
	goldAmountRatePercent = goldAmountRateForKiller(goldAmountRatePercent, killer, time.Now())
	dropClass := target.Rarity()
	country := 0
	if ref, ok := model.RefChars[target.GetRefObjectID()]; ok {
		country = ref.Country
		specs, err := s.catalog.rollForMonsterIdentityAtRuntimeRates(target.GetRefObjectID(), level, country, NativeDropExceptionContext{MonsterCode: ref.CodeName, MonsterTypeID4: ref.TypeID4}, s.random, ratePercent, goldAmountRatePercent, &dropClass)
		if err != nil {
			return nil, err
		}
		specs = s.appendBinaryEventDrops(specs, target)
		return s.spawnDropSpecs(ctx, killer, target, specs)
	}
	specs, err := s.catalog.rollForMonsterIdentityAtRuntimeRates(target.GetRefObjectID(), level, country, NativeDropExceptionContext{}, s.random, ratePercent, goldAmountRatePercent, &dropClass)
	if err != nil {
		return nil, err
	}
	specs = s.appendBinaryEventDrops(specs, target)
	return s.spawnDropSpecs(ctx, killer, target, specs)
}

func goldAmountRateForKiller(base uint32, killer *model.Player, now time.Time) uint32 {
	if base == 0 {
		base = 100
	}
	bonus := killer.ActiveGoldDropAmountPercent(now)
	if bonus <= 0 {
		return base
	}
	scaled := uint64(base) * uint64(100+bonus) / 100
	if scaled > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(scaled)
}

func (s *MonsterDropService) appendBinaryEventDrops(specs []model.GroundItemSpec, target *model.NPC) []model.GroundItemSpec {
	specs = s.appendWinterEventDrop(specs, target)
	return s.appendBattleArenaTicket(specs, target)
}

func (s *MonsterDropService) appendWinterEventDrop(specs []model.GroundItemSpec, target *model.NPC) []model.GroundItemSpec {
	if s == nil || target == nil || !s.winterEvent.enabled || target.GetRefObjectID() != s.winterEvent.monsterRefID {
		return specs
	}
	// The native event controller receives 50.0 at 0x00877870. A roll below
	// that percentage appends exactly one snowflake through the same monster
	// ground-drop virtual used by the ordinary materializer.
	chanceBP := model.RuntimeWinterDropChanceBP()
	if s.random.Float64()*10000 >= float64(chanceBP) {
		return specs
	}
	return append(specs, model.GroundItemSpec{ReferenceItemID: s.winterEvent.itemRefID, StackCount: uint16(model.RuntimeWinterDropItemCount())})
}

func (s *MonsterDropService) appendBattleArenaTicket(specs []model.GroundItemSpec, target *model.NPC) []model.GroundItemSpec {
	if s == nil || target == nil || s.battleArena.itemRefID == 0 || runtimeBattleArenaRate(s) == 0 {
		return specs
	}
	scope := model.GameWorldScope(target.GetGameWorldID())
	if !runtimeBattleArenaWorld(scope.WorldID()) || !scope.ValidInstance() {
		return specs
	}
	if !s.rollBattleArenaTicket(uint32(scope)) {
		return specs
	}
	return append(specs, model.GroundItemSpec{ReferenceItemID: s.battleArena.itemRefID, StackCount: 1})
}

func (s *MonsterDropService) rollBattleArenaTicket(scope uint32) bool {
	rate := runtimeBattleArenaRate(s)
	if rate <= 0 {
		return false
	}
	if rate >= 100 {
		return true
	}
	roll := s.random.Intn(101)
	s.battleArena.mu.Lock()
	defer s.battleArena.mu.Unlock()
	accumulator, exists := s.battleArena.accumulator[scope]
	if !exists {
		if roll <= rate {
			s.battleArena.accumulator[scope] = 2*rate - 100
			return true
		}
		s.battleArena.accumulator[scope] = 2 * rate
		return false
	}
	if roll > accumulator {
		s.battleArena.accumulator[scope] = accumulator + rate
		return false
	}
	s.battleArena.accumulator[scope] = accumulator + rate - 100
	return true
}

func runtimeBattleArenaWorld(worldID uint16) bool {
	for _, raw := range strings.Split(model.RuntimeBattleArenaWorldIDs(), ",") {
		value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 16)
		if err == nil && uint16(value) == worldID {
			return true
		}
	}
	return false
}

func runtimeBattleArenaRate(s *MonsterDropService) int {
	if model.RuntimeGameplayPolicyLoaded() {
		return model.RuntimeBattleArenaTicketRate()
	}
	if s == nil {
		return 0
	}
	return s.battleArena.rate
}

func (s *MonsterDropService) spawnDropSpecs(ctx context.Context, killer *model.Player, target *model.NPC, specs []model.GroundItemSpec) ([]*model.Item, error) {
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

// SpawnQuestCollectionItem applies the authored per-source percentage and
// uses the same ground-item admission boundary as ordinary monster loot.
// Quest qualification remains owned by the character quest repository.
func (s *MonsterDropService) SpawnQuestCollectionItem(ctx context.Context, killer *model.Player, target *model.NPC, drop model.QuestCollectionDrop) (*model.Item, bool, error) {
	if s == nil || s.random == nil || s.groundItems == nil || s.materializer == nil || target == nil || drop.ReferenceItemID == 0 || drop.StackCount == 0 {
		return nil, false, ErrMonsterDropCatalogUnavailable
	}
	chance := math.Min(100, math.Max(0, drop.ChancePercent))
	if chance <= 0 || s.random.Float64()*100 >= chance {
		return nil, false, nil
	}
	partyNumber := uint32(0)
	if killer != nil {
		if party := killer.GetParty(); party != nil {
			partyNumber = party.Number
		}
	}
	spec := model.GroundItemSpec{ReferenceItemID: drop.ReferenceItemID, StackCount: drop.StackCount, NavmeshPosition: target.GetNavmeshPosition()}
	item, err := s.groundItems.MaterializeAndSpawn(ctx, s.materializer, spec, partyNumber)
	if err != nil {
		return nil, false, err
	}
	return item, true, nil
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
