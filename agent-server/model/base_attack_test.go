package model

import "testing"

func TestPlayerBaseAttackSkillIDUsesEquippedWeapon(t *testing.T) {
	tests := []struct {
		name    string
		typeID4 int
		want    uint32
	}{
		{name: "unarmed", want: 1},
		{name: "chinese sword", typeID4: 2, want: 2},
		{name: "chinese blade", typeID4: 3, want: 2},
		{name: "chinese spear", typeID4: 4, want: 40},
		{name: "chinese glaive", typeID4: 5, want: 40},
		{name: "chinese bow", typeID4: 6, want: 70},
		{name: "european sword", typeID4: 7, want: 8419},
		{name: "european two handed sword", typeID4: 8, want: 8420},
		{name: "european dual axe", typeID4: 9, want: 8421},
		{name: "warlock rod", typeID4: 10, want: 10625},
		{name: "wizard staff", typeID4: 11, want: 9944},
		{name: "crossbow", typeID4: 12, want: 9354},
		{name: "dagger", typeID4: 13, want: 9355},
		{name: "harp", typeID4: 14, want: 11162},
		{name: "cleric rod", typeID4: 15, want: 11526},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			player := &Player{Inventory: NewInventory(nil)}
			if test.typeID4 != 0 {
				player.Inventory.Items[SlotPrimaryWeapon] = Item{SRObject: SRObject{TypeInfo: TypeInfo{
					TypeID1: TypeID1Item,
					TypeID2: TypeID2Wearable,
					TypeID3: TypeID3Weapon,
					TypeID4: test.typeID4,
				}}}
			}
			if got := player.BaseAttackSkillID(); got != test.want {
				t.Fatalf("BaseAttackSkillID() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPlayerBaseAttackHitCountUsesTwoHitsForChineseSwordAndBlade(t *testing.T) {
	for _, typeID4 := range []int{2, 3} {
		player := &Player{Inventory: NewInventory(nil)}
		player.Inventory.Items[SlotPrimaryWeapon] = Item{SRObject: SRObject{TypeInfo: TypeInfo{
			TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: TypeID3Weapon, TypeID4: typeID4,
		}}}
		if got := player.BaseAttackHitCount(); got != 2 {
			t.Fatalf("type ID 4 %d hit count = %d, want 2", typeID4, got)
		}
	}
}

func TestPlayerBaseAttackRangeUsesEquippedWeaponReference(t *testing.T) {
	previous := RefItems
	t.Cleanup(func() { RefItems = previous })
	RefItems = map[uint32]RefItem{
		100: {RefObject: RefObject{ID: 100}, Range: 6},
		101: {RefObject: RefObject{ID: 101}, Range: 180},
	}

	for _, test := range []struct {
		name    string
		refID   uint32
		typeID4 int
		want    float32
	}{
		{name: "unarmed", want: 5},
		{name: "sword database distance", refID: 100, typeID4: 2, want: 11},
		{name: "bow database distance", refID: 101, typeID4: 6, want: 185},
		{name: "magical minimum", refID: 100, typeID4: 11, want: 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			player := &Player{Inventory: NewInventory(nil)}
			if test.refID != 0 {
				player.Inventory.Items[SlotPrimaryWeapon] = Item{SRObject: SRObject{
					RefObjectID: test.refID,
					TypeInfo:    TypeInfo{TypeID1: TypeID1Item, TypeID2: TypeID2Wearable, TypeID3: TypeID3Weapon, TypeID4: test.typeID4},
				}}
			}
			if got := player.BaseAttackRange(); got != test.want {
				t.Fatalf("BaseAttackRange() = %g, want %g", got, test.want)
			}
		})
	}
}
