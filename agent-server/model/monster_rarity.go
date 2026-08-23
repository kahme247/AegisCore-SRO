package model

// MonsterRarity is the vSRO 1.188 rarity byte written in an NPC_MOB spawn.
// The party flag occupies bit 4; the low nibble retains the ordinary class.
type MonsterRarity byte

const (
	MonsterNormal   MonsterRarity = 0
	MonsterChampion MonsterRarity = 1
	MonsterUnique   MonsterRarity = 3
	MonsterGiant    MonsterRarity = 4
	MonsterTitan    MonsterRarity = 5
	MonsterElite    MonsterRarity = 6

	MonsterPartyNormal   MonsterRarity = 16
	MonsterPartyChampion MonsterRarity = 17
	MonsterPartyUnique   MonsterRarity = 19
	MonsterPartyGiant    MonsterRarity = 20
	MonsterPartyTitan    MonsterRarity = 21
	MonsterPartyElite    MonsterRarity = 22
)

func (r MonsterRarity) Valid() bool {
	switch r {
	case MonsterNormal, MonsterChampion, MonsterUnique, MonsterGiant, MonsterTitan, MonsterElite,
		MonsterPartyNormal, MonsterPartyChampion, MonsterPartyUnique, MonsterPartyGiant,
		MonsterPartyTitan, MonsterPartyElite:
		return true
	default:
		return false
	}
}

func (r MonsterRarity) IsParty() bool { return byte(r)&0x10 != 0 }

func (r MonsterRarity) BaseClass() MonsterRarity { return r &^ MonsterPartyNormal }

func (r MonsterRarity) IsUniqueBoss() bool { return r.BaseClass() == MonsterUnique }

func (r MonsterRarity) IsElite() bool { return r.BaseClass() == MonsterElite }

// UsesOrdinaryAggro distinguishes field monsters from authored boss classes.
// Champions and giants are stronger field spawns and retain the same awareness
// behavior as their normal base monster.
func (r MonsterRarity) UsesOrdinaryAggro() bool {
	switch r.BaseClass() {
	case MonsterNormal, MonsterChampion, MonsterGiant:
		return true
	default:
		return false
	}
}

// RuntimeStatMultiplier applies only to classes generated from an ordinary
// reference row. Party, unique, and elite reference rows already author their
// HP and EXP in CHAR_REF_DATA and must not be multiplied a second time.
func (r MonsterRarity) RuntimeStatMultiplier() int {
	switch r {
	case MonsterChampion:
		return 2
	case MonsterGiant:
		return 20
	default:
		return 1
	}
}

// Canonical vSRO 1.188 unique references requested by the initial world
// implementation. Their stats, skills, spawn positions, and localization stay
// data-driven in CHAR_REF_DATA/SPAWN_REF_*.
const (
	MonsterRefTigerGirl    uint32 = 1954
	MonsterRefCerberus     uint32 = 5871
	MonsterRefUruchi       uint32 = 1982
	MonsterRefIsyutaru     uint32 = 2002
	MonsterRefLordYarkan   uint32 = 3810
	MonsterRefDemonShaitan uint32 = 3875
)

var canonicalUniqueBosses = map[uint32]string{
	MonsterRefTigerGirl:    "Tiger Girl",
	MonsterRefCerberus:     "Cerberus",
	MonsterRefUruchi:       "Uruchi",
	MonsterRefIsyutaru:     "Isyutaru",
	MonsterRefLordYarkan:   "Lord Yarkan",
	MonsterRefDemonShaitan: "Demon Shaitan",
}

func CanonicalUniqueBossName(refObjectID uint32) (string, bool) {
	name, ok := canonicalUniqueBosses[refObjectID]
	return name, ok
}
