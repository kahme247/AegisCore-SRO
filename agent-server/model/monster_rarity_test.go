package model

import "testing"

func TestMonsterRarityClasses(t *testing.T) {
	tests := []struct {
		rarity     MonsterRarity
		base       MonsterRarity
		party      bool
		multiplier int
	}{
		{MonsterNormal, MonsterNormal, false, 1},
		{MonsterChampion, MonsterChampion, false, 2},
		{MonsterGiant, MonsterGiant, false, 20},
		{MonsterUnique, MonsterUnique, false, 1},
		{MonsterElite, MonsterElite, false, 1},
		{MonsterPartyNormal, MonsterNormal, true, 1},
		{MonsterPartyChampion, MonsterChampion, true, 1},
		{MonsterPartyGiant, MonsterGiant, true, 1},
		{MonsterPartyUnique, MonsterUnique, true, 1},
		{MonsterPartyElite, MonsterElite, true, 1},
	}
	for _, test := range tests {
		if !test.rarity.Valid() || test.rarity.BaseClass() != test.base || test.rarity.IsParty() != test.party || test.rarity.RuntimeStatMultiplier() != test.multiplier {
			t.Fatalf("rarity %d = valid:%t base:%d party:%t multiplier:%d", test.rarity, test.rarity.Valid(), test.rarity.BaseClass(), test.rarity.IsParty(), test.rarity.RuntimeStatMultiplier())
		}
	}
	if !MonsterNormal.UsesOrdinaryAggro() || !MonsterChampion.UsesOrdinaryAggro() || !MonsterGiant.UsesOrdinaryAggro() {
		t.Fatal("field monster classes do not use ordinary aggro")
	}
	if MonsterUnique.UsesOrdinaryAggro() || MonsterElite.UsesOrdinaryAggro() {
		t.Fatal("boss classes incorrectly use delayed ordinary aggro")
	}
	if MonsterRarity(2).Valid() || MonsterRarity(255).Valid() {
		t.Fatal("reserved rarity bytes were accepted")
	}
}

func TestCanonicalUniqueBossCatalog(t *testing.T) {
	tests := map[uint32]string{
		MonsterRefTigerGirl: "Tiger Girl", MonsterRefCerberus: "Cerberus",
		MonsterRefUruchi: "Uruchi", MonsterRefIsyutaru: "Isyutaru",
		MonsterRefLordYarkan: "Lord Yarkan", MonsterRefDemonShaitan: "Demon Shaitan",
	}
	for refID, want := range tests {
		if got, ok := CanonicalUniqueBossName(refID); !ok || got != want {
			t.Fatalf("unique ref %d = %q, %t; want %q", refID, got, ok, want)
		}
	}
}
