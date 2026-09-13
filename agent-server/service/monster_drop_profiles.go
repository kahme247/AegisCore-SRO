package service

import (
	"math"
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type DropProfileFamily string

const (
	DropProfileEquip          DropProfileFamily = "EQUIP"
	DropProfileRareEquip      DropProfileFamily = "RAREEQUIP"
	DropProfileRecover        DropProfileFamily = "RECOVER"
	DropProfileCure           DropProfileFamily = "CURE"
	DropProfileAmmo           DropProfileFamily = "AMMO"
	DropProfileScroll         DropProfileFamily = "SCROLL"
	DropProfileReinforce      DropProfileFamily = "REINFORCE"
	DropProfileTablet         DropProfileFamily = "ALCHEMY_TABLET"
	DropProfileMagicStone     DropProfileFamily = "ALCHEMY_MAGICSTONE"
	DropProfileAttributeStone DropProfileFamily = "ALCHEMY_ATTRSTONE"
)

type DropClassSelection struct {
	Family       DropProfileFamily
	MonsterLevel int
	Group        int
	Probability  float64
}

type DropItemAssignment struct {
	SourceRow       uint32
	Family          DropProfileFamily
	ReferenceItemID uint32
	Relative        float64
	Absolute        float64
	AssignedGroup   int
	DropCount       uint32
	Country         int
	TypeID3         int
	TypeID4         int
	DurabilityLower float64
	DurabilityUpper float64
	MaxMagicOptions int
	ItemClass       int
}

type DropMagicOptionCandidate struct {
	Country, TypeID3, TypeID4 int
	AssignmentSlot            int
	OptionID                  uint32
	CodeName                  string
	DropRatio                 float64
	MinItemClass              int
	ParamType, PackedValue    uint32
}

type DropOptLevelSelection struct {
	Level              byte
	Probability        float64
	RequiredOnlineTime uint32
}

// DropProfileCatalog is the immutable normalized input for the profile branch
// rooted at SR_GameServer 0x00725FD0. Class probability selection and item
// assignment remain separate because the binary performs them in that order.
type DropProfileCatalog struct {
	classes     map[DropProfileFamily]map[int][]DropClassSelection
	assignments map[DropProfileFamily]map[int][]DropItemAssignment
	optLevels   []DropOptLevelSelection
	magic       map[[3]int]map[int][]DropMagicOptionCandidate
}

func NewDropProfileCatalog(classes []DropClassSelection, assignments []DropItemAssignment, optLevels []DropOptLevelSelection, magicRows ...[]DropMagicOptionCandidate) *DropProfileCatalog {
	catalog := &DropProfileCatalog{
		classes:     make(map[DropProfileFamily]map[int][]DropClassSelection),
		assignments: make(map[DropProfileFamily]map[int][]DropItemAssignment),
		optLevels:   append([]DropOptLevelSelection(nil), optLevels...),
		magic:       make(map[[3]int]map[int][]DropMagicOptionCandidate),
	}
	for _, row := range classes {
		if row.Family == "" || row.MonsterLevel <= 0 || row.Group < 0 || row.Probability < 0 {
			continue
		}
		if catalog.classes[row.Family] == nil {
			catalog.classes[row.Family] = make(map[int][]DropClassSelection)
		}
		catalog.classes[row.Family][row.MonsterLevel] = append(catalog.classes[row.Family][row.MonsterLevel], row)
	}
	for family := range catalog.classes {
		for level := range catalog.classes[family] {
			sort.Slice(catalog.classes[family][level], func(i, j int) bool {
				return catalog.classes[family][level][i].Group < catalog.classes[family][level][j].Group
			})
		}
	}
	for _, row := range assignments {
		if row.Family == "" || row.ReferenceItemID == 0 || row.AssignedGroup < 0 || row.Relative < 0 || row.Absolute < 0 {
			continue
		}
		if catalog.assignments[row.Family] == nil {
			catalog.assignments[row.Family] = make(map[int][]DropItemAssignment)
		}
		catalog.assignments[row.Family][row.AssignedGroup] = append(catalog.assignments[row.Family][row.AssignedGroup], row)
	}
	for family := range catalog.assignments {
		for group := range catalog.assignments[family] {
			sort.SliceStable(catalog.assignments[family][group], func(i, j int) bool {
				return catalog.assignments[family][group][i].SourceRow < catalog.assignments[family][group][j].SourceRow
			})
		}
	}
	sort.Slice(catalog.optLevels, func(i, j int) bool { return catalog.optLevels[i].Level < catalog.optLevels[j].Level })
	if len(magicRows) != 0 {
		for _, row := range magicRows[0] {
			if row.OptionID == 0 || row.DropRatio < 0 || row.AssignmentSlot < 0 {
				continue
			}
			key := [3]int{row.Country, row.TypeID3, row.TypeID4}
			if catalog.magic[key] == nil {
				catalog.magic[key] = make(map[int][]DropMagicOptionCandidate)
			}
			catalog.magic[key][row.AssignmentSlot] = append(catalog.magic[key][row.AssignmentSlot], row)
		}
	}
	return catalog
}

func (c *DropProfileCatalog) ClassSelections(family DropProfileFamily, monsterLevel int) []DropClassSelection {
	if c == nil || c.classes[family] == nil {
		return nil
	}
	return append([]DropClassSelection(nil), c.classes[family][monsterLevel]...)
}

// RollClassGroup reproduces SR_GameServer 0x007244F0 and the cumulative-map
// builder at 0x006AA6D0. Group is zero-based, as stored in the native map.
func (c *DropProfileCatalog) RollClassGroup(family DropProfileFamily, monsterLevel int, itemFactor float64, classMultiplier int, random DropRandom) (int, bool) {
	if c == nil || random == nil || classMultiplier <= 0 || c.classes[family] == nil {
		return 0, false
	}
	rows := c.classes[family][monsterLevel]
	if len(rows) == 0 {
		return 0, false
	}
	modulus := dropRandomMillion / classMultiplier
	if modulus <= 0 {
		return 0, false
	}
	roll := binaryModuloRoll(random, modulus)
	if delta := math.Abs(1 - itemFactor); delta > 0.000001 {
		roll = int(float64(roll) / delta)
	}
	cumulative := 0.0
	for _, row := range rows {
		if row.Probability <= 0.000001 {
			continue
		}
		cumulative += row.Probability
		if roll <= int(cumulative*dropRandomMillion) {
			return row.Group, true
		}
	}
	return 0, false
}

// RollAssignment reproduces the assignment record's relative weighted choice
// and inclusive rand()%100 <= Prob_Absolute gate at 0x00724120. It always
// consumes the absolute roll after a relative candidate is selected, including
// for an authored value of 100.
func (c *DropProfileCatalog) RollAssignment(family DropProfileFamily, group, country int, random DropRandom) (DropItemAssignment, bool) {
	if c == nil || random == nil || c.assignments[family] == nil {
		return DropItemAssignment{}, false
	}
	candidates := c.assignments[family][group]
	total := 0
	for _, candidate := range candidates {
		if (candidate.Country == country || candidate.Country == 3) && candidate.Relative > 0 {
			total += int(math.Round(candidate.Relative))
		}
	}
	if total <= 0 {
		return DropItemAssignment{}, false
	}
	roll := random.Intn(total)
	for _, candidate := range candidates {
		if candidate.Country != country && candidate.Country != 3 {
			continue
		}
		weight := int(math.Round(candidate.Relative))
		if weight <= 0 {
			continue
		}
		if roll < weight {
			return candidate, float64(random.Intn(100)) <= candidate.Absolute
		}
		roll -= weight
	}
	return DropItemAssignment{}, false
}

// RollOptLevel reproduces SR_GameServer 0x00724400. The retained probabilities
// are already cumulative; the native record stores source OptLevel+1 and the
// selector returns that value minus one after clamping at eight.
func (c *DropProfileCatalog) RollOptLevel(random DropRandom) byte {
	if c == nil || random == nil {
		return 0
	}
	roll := binaryModuloRoll(random, dropRandomMillion)
	for _, row := range c.optLevels {
		threshold := int(row.Probability * dropRandomMillion)
		if roll <= threshold {
			return min(row.Level, 7)
		}
		if threshold >= dropRandomMillion {
			return 0
		}
	}
	return 0
}

// GenerateEquipmentBaseState reproduces the ordinary, non-preauthored
// variance paths selected by 0x00727590. It does not add magic options; that
// is the separate 0x007276A0 catalog-driven transition.
func GenerateEquipmentBaseState(assignment DropItemAssignment, random DropRandom) (variance uint64, durability uint32, ok bool) {
	if random == nil || assignment.Family != DropProfileEquip && assignment.Family != DropProfileRareEquip {
		return 0, 0, false
	}
	slots := 0
	switch assignment.TypeID3 {
	case 1, 2, 3, 9, 10, 11:
		slots = 6
	case 4:
		if assignment.TypeID4 == 1 || assignment.TypeID4 == 2 {
			slots = 6
		}
	case 5, 12:
		slots = 2
		durability = 1
	case 6:
		slots = 7
	}
	if slots == 0 {
		return 0, 0, false
	}
	first := 0
	for slot := 0; slot < slots; slot++ {
		value := nativeVarianceSlot(random)
		if slot == 0 {
			first = value
		}
		variance |= uint64(value&0x1f) << (5 * slot)
	}
	if assignment.TypeID3 != 5 && assignment.TypeID3 != 12 {
		if assignment.DurabilityUpper < assignment.DurabilityLower {
			return 0, 0, false
		}
		difference := uint32(assignment.DurabilityUpper - assignment.DurabilityLower)
		durability = uint32(assignment.DurabilityLower + float64(difference)*float64(first)/31)
	}
	return variance, durability, true
}

// GenerateEquipmentMagicOptions follows the ordinary drop builder rooted at
// SR_GameServer 0x007276A0. An assignment slot is picked uniformly for each
// attempt; its per-level definitions are then selected by the native
// million-point cumulative-weight roll at 0x0072F410.
func (c *DropProfileCatalog) GenerateEquipmentMagicOptions(assignment DropItemAssignment, random DropRandom) []model.MagicOption {
	if c == nil || random == nil || assignment.MaxMagicOptions <= 0 || assignment.Family != DropProfileEquip && assignment.Family != DropProfileRareEquip {
		return nil
	}
	if assignment.Family != DropProfileRareEquip && random.Intn(101) > 30 {
		return nil
	}
	slots := c.magic[[3]int{assignment.Country, assignment.TypeID3, assignment.TypeID4}]
	if len(slots) == 0 {
		return nil
	}
	count := assignment.MaxMagicOptions
	for draw := 0; draw < 4; draw++ {
		count = min(count, random.Intn(6)+1)
	}
	count = min(max(count, 1), 6)
	orderedSlots := make([]int, 0, len(slots))
	for slot := range slots {
		orderedSlots = append(orderedSlots, slot)
	}
	sort.Ints(orderedSlots)
	used := make(map[int]bool)
	result := make([]model.MagicOption, 0, min(count, 12))
	for attempt := 0; attempt < count && len(result) < 12; attempt++ {
		slot := orderedSlots[random.Intn(len(orderedSlots))]
		if used[slot] {
			continue
		}
		used[slot] = true
		candidate, ok := rollDropMagicOption(slots[slot], assignment.ItemClass, random)
		if !ok || candidate.CodeName == "MATTR_NOT_REPARABLE" {
			continue
		}
		minimum, maximum := candidate.PackedValue>>16, candidate.PackedValue&0xffff
		if minimum > maximum {
			continue
		}
		value := minimum
		if minimum < maximum {
			value += uint32(random.Intn(int(maximum-minimum) + 1))
		}
		result = append(result, model.MagicOption{Type: candidate.OptionID, Value: value, ParamType: candidate.ParamType})
	}
	return result
}

func rollDropMagicOption(candidates []DropMagicOptionCandidate, itemClass int, random DropRandom) (DropMagicOptionCandidate, bool) {
	roll := binaryModuloRoll(random, dropRandomMillion)
	cumulative := 0
	for _, candidate := range candidates {
		if itemClass < candidate.MinItemClass {
			continue
		}
		cumulative += int(candidate.DropRatio * dropRandomMillion)
		if roll <= cumulative {
			return candidate, true
		}
	}
	return DropMagicOptionCandidate{}, false
}

func nativeVarianceSlot(random DropRandom) int {
	minimum := float32(1)
	for draw := 0; draw < 3; draw++ {
		value := float32(random.Intn(32768)) / 32767
		if value < minimum {
			minimum = value
		}
	}
	return int(float64(minimum) * 31)
}

func ClassifyDropProfileFamily(typeID2, typeID3, typeID4, rarity int) DropProfileFamily {
	if typeID2 == 1 {
		if rarity != 0 {
			return DropProfileRareEquip
		}
		return DropProfileEquip
	}
	if typeID2 != 3 {
		return ""
	}
	switch typeID3 {
	case 1:
		return DropProfileRecover
	case 2:
		return DropProfileCure
	case 3:
		return DropProfileScroll
	case 4:
		return DropProfileAmmo
	case 10:
		return DropProfileReinforce
	case 11:
		switch typeID4 {
		case 1:
			return DropProfileMagicStone
		case 2:
			return DropProfileAttributeStone
		case 3:
			return DropProfileTablet
		}
	}
	return ""
}
