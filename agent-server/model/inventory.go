package model

import (
	"sync"
)

const (
	InventoryEquipmentSlots byte = 13
	// MaxInventoryWireSize is the highest slot ceiling accepted by the pinned
	// SR_GameServer equipment path: normal wire slots end at 0x6f, immediately
	// after the 13 body slots. BaseInventorySize remains the configured default,
	// not the protocol ceiling.
	MaxInventoryWireSize   byte = 0x70
	MaxNormalInventorySlot byte = MaxInventoryWireSize - 1
	InventoryPageSlots     byte = 32
	InventoryPages         byte = 3
	BaseInventorySize           = InventoryEquipmentSlots + InventoryPageSlots*InventoryPages
)

const (
	SlotHelmet          byte = 0
	SlotChest           byte = 1
	SlotShoulder        byte = 2
	SlotGlove           byte = 3
	SlotPants           byte = 4
	SlotBoots           byte = 5
	SlotPrimaryWeapon   byte = 6
	SlotSecondaryWeapon byte = 7
	SlotExtra           byte = 8
	SlotEarring         byte = 9
	SlotNecklace        byte = 10
	SlotLeftRing        byte = 11
	SlotRightRing       byte = 12
)

const (
	EquipItem = iota
	UnequipItem
	MoveItem
)

type Inventory struct {
	Items map[byte]Item
	mutex *sync.Mutex
}

// zeroInventoryMutex protects legacy zero-value Inventory literals. New
// mutable inventories receive their own mutex from NewInventory; sharing this
// fallback is intentional because a shallow copy of a zero-value inventory
// can still share its Items map.
var zeroInventoryMutex sync.Mutex

// NewInventory creates an inventory that owns the supplied item map and has
// independent synchronization. Callers that need to retain an immutable
// source map should use Snapshot instead.
func NewInventory(items map[byte]Item) Inventory {
	if items == nil {
		items = make(map[byte]Item)
	}
	return Inventory{
		Items: items,
		mutex: &sync.Mutex{},
	}
}

func (i *Inventory) synchronizationMutex() *sync.Mutex {
	if i == nil || i.mutex == nil {
		return &zeroInventoryMutex
	}
	return i.mutex
}

// Snapshot returns an independently synchronized copy of the current item
// map. It is the only safe way to hand an inventory value to a packet
// serializer or other reader that may outlive a concurrent relocation.
func (i *Inventory) Snapshot() Inventory {
	if i == nil {
		return NewInventory(nil)
	}

	mutex := i.synchronizationMutex()
	mutex.Lock()
	items := make(map[byte]Item, len(i.Items))
	for slot, item := range i.Items {
		items[slot] = item
	}
	mutex.Unlock()

	return NewInventory(items)
}

func (i *Inventory) FirstFreeSlot(capacity byte) (byte, bool) {
	if i == nil || capacity == 0 {
		return 0, false
	}
	mutex := i.synchronizationMutex()
	mutex.Lock()
	defer mutex.Unlock()
	for slot := byte(0); slot < capacity; slot++ {
		if _, occupied := i.Items[slot]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

// FirstFreeNormalSlot excludes the thirteen equipment slots. Ground pickups
// are ordinary inventory links and must never be placed in an equipment slot,
// even when the character's inventory is only partially expanded.
func (i *Inventory) FirstFreeNormalSlot(capacity byte) (byte, bool) {
	if i == nil || capacity <= InventoryEquipmentSlots {
		return 0, false
	}
	if capacity > MaxInventoryWireSize {
		capacity = MaxInventoryWireSize
	}
	mutex := i.synchronizationMutex()
	mutex.Lock()
	defer mutex.Unlock()
	for slot := InventoryEquipmentSlots; slot < capacity; slot++ {
		if _, occupied := i.Items[slot]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func (i *Inventory) Put(slot, capacity byte, item Item) bool {
	if i == nil || slot >= capacity {
		return false
	}
	mutex := i.synchronizationMutex()
	mutex.Lock()
	defer mutex.Unlock()
	if _, occupied := i.Items[slot]; occupied {
		return false
	}
	i.Items[slot] = item
	return true
}

func (i *Inventory) MoveItems(sourceSlot, targetSlot byte, player *Player) (bool, int) {
	if i == nil {
		return false, -1
	}
	mutex := i.synchronizationMutex()
	mutex.Lock()
	defer mutex.Unlock()

	plan, err := PlanInventoryRelocation(i.Items, sourceSlot, targetSlot, player)
	if err != nil {
		return false, -1
	}
	if err := ApplyInventoryRelocation(i.Items, plan); err != nil {
		return false, -1
	}
	return true, int(plan.Action)
}

func (i *Inventory) canEquipPrimaryWeapon(sourceItem Item, player *Player) bool {
	if (player.IsChinese() && sourceItem.IsChineseWeapon()) || (player.IsEuropean() && sourceItem.IsEuropeanWeapon()) {
		if sourceItem.RequiredLevelType1 == 1 && player.Level >= sourceItem.RequiredLevel1 {
			return true
		}
	}

	return false
}

func (i *Inventory) canEquipArmorPart(item Item, targetSlot byte) bool {
	numItemsOfOtherType := 0
	itemsHaveSameArmorType := false
	if item.IsCHGarmentPart() || item.IsEUGarmentPart() {
		numItemsOfOtherType = i.equippedArmorOrProtectorParts()
		if equippedItem, ok := i.Items[targetSlot]; ok && (equippedItem.IsCHGarmentPart() || equippedItem.IsEUGarmentPart()) {
			itemsHaveSameArmorType = true
		}
	} else if item.IsCHArmorPart() || item.IsCHProtectorPart() || item.IsEUArmorPart() || item.IsEUProtectorPart() {
		numItemsOfOtherType = i.equippedGarmentParts()
		if equippedItem, ok := i.Items[targetSlot]; ok && (equippedItem.IsCHArmorPart() || equippedItem.IsCHProtectorPart() || equippedItem.IsEUArmorPart() || equippedItem.IsEUProtectorPart()) {
			itemsHaveSameArmorType = true
		}
	}
	switch numItemsOfOtherType {
	case 0:
		return true
	case 1:
		return itemsHaveSameArmorType
	default:
		return false
	}
}

func (i *Inventory) canEquipHelmet(item Item, player *Player) bool {
	if player.IsChinese() && item.IsCHHelmet() {
		return i.canEquipArmorPart(item, SlotHelmet) && i.fulfillsLevelRequirements(item, player)
	} else if player.IsEuropean() && item.IsEUHelmet() {
		if i.canEquipArmorPart(item, SlotHelmet) {
			return i.fulfillsSkillRequirements(item, player) && i.fulfillsLevelRequirements(item, player)
		}
	}
	return false
}

func (i *Inventory) canEquipShoulder(item Item, player *Player) bool {
	if player.IsChinese() && item.IsCHShoulder() {
		return i.canEquipArmorPart(item, SlotShoulder) && i.fulfillsLevelRequirements(item, player)
	} else if player.IsEuropean() && item.IsEUShoulder() {
		if i.canEquipArmorPart(item, SlotShoulder) {
			return i.fulfillsSkillRequirements(item, player) && i.fulfillsLevelRequirements(item, player)
		}
	}
	return false
}

func (i *Inventory) canEquipChest(item Item, player *Player) bool {
	if player.IsChinese() && item.IsCHChest() {
		return i.canEquipArmorPart(item, SlotChest) && i.fulfillsLevelRequirements(item, player)
	} else if player.IsEuropean() && item.IsEUChest() {
		if i.canEquipArmorPart(item, SlotChest) {
			return i.fulfillsSkillRequirements(item, player) && i.fulfillsLevelRequirements(item, player)
		}
	}
	return false
}

func (i *Inventory) canEquipPants(item Item, player *Player) bool {
	if player.IsChinese() && item.IsCHPant() {
		return i.canEquipArmorPart(item, SlotPants) && i.fulfillsLevelRequirements(item, player)
	} else if player.IsEuropean() && item.IsEUPant() {
		if i.canEquipArmorPart(item, SlotPants) {
			return i.fulfillsSkillRequirements(item, player) && i.fulfillsLevelRequirements(item, player)
		}
	}
	return false
}

func (i *Inventory) canEquipGloves(item Item, player *Player) bool {
	if player.IsChinese() && item.IsCHGlove() {
		return i.canEquipArmorPart(item, SlotGlove) && i.fulfillsLevelRequirements(item, player)
	} else if player.IsEuropean() && item.IsEUGlove() {
		if i.canEquipArmorPart(item, SlotGlove) {
			return i.fulfillsSkillRequirements(item, player) && i.fulfillsLevelRequirements(item, player)
		}
	}
	return false
}

func (i *Inventory) canEquipBoots(item Item, player *Player) bool {
	if player.IsChinese() && item.IsCHBoots() {
		return i.canEquipArmorPart(item, SlotBoots) && i.fulfillsLevelRequirements(item, player)
	} else if player.IsEuropean() && item.IsEUBoots() {
		if i.canEquipArmorPart(item, SlotBoots) {
			return i.fulfillsSkillRequirements(item, player) && i.fulfillsLevelRequirements(item, player)
		}
	}
	return false
}

func (i *Inventory) canEquipNecklace(item Item, player *Player) bool {
	if (player.IsChinese() && item.IsCHNecklace()) || (player.IsEuropean() && item.IsEUNecklace()) {
		return i.fulfillsLevelRequirements(item, player)
	}
	return false
}

func (i *Inventory) canEquipEarring(item Item, player *Player) bool {
	if (player.IsChinese() && item.IsCHEarring()) || (player.IsEuropean() && item.IsEUEarring()) {
		return i.fulfillsLevelRequirements(item, player)
	}
	return false
}

func (i *Inventory) canEquipRing(item Item, player *Player) bool {
	if (player.IsChinese() && item.IsCHRing()) || (player.IsEuropean() && item.IsEURing()) {
		return i.fulfillsLevelRequirements(item, player)
	}
	return false
}

func (i *Inventory) equippedGarmentParts() int {
	numGarmentItems := 0
	for j := byte(0); j < SlotBoots; j++ {
		if item, ok := i.Items[j]; ok && (item.IsCHGarmentPart() || item.IsEUGarmentPart()) {
			numGarmentItems++
		}
	}
	return numGarmentItems
}

func (i *Inventory) equippedArmorOrProtectorParts() int {
	numArmorOrProtectorParts := 0
	for j := byte(0); j < SlotBoots; j++ {
		if item, ok := i.Items[j]; ok && (item.IsCHArmorPart() || item.IsEUArmorPart() || item.IsCHProtectorPart() || item.IsEUProtectorPart()) {
			numArmorOrProtectorParts++
		}
	}
	return numArmorOrProtectorParts
}

func (i *Inventory) fulfillsLevelRequirements(item Item, player *Player) bool {
	// TODO: is this all that has to be checked?
	return item.IsEquipment() && item.RequiredLevelType1 == 1 && item.RequiredLevel1 <= player.Level
}

func (i *Inventory) fulfillsSkillRequirements(item Item, player *Player) bool {
	// TODO: Check level and skill requirements
	return false
}

func (i *Inventory) canEquipSecondaryWeapon(item Item, player *Player) bool {
	// TODO any further checks needed? (e.g. siege weapons)
	primWeapon, ok := i.Items[SlotPrimaryWeapon]
	if player.IsChinese() {
		if ok && primWeapon.IsOneHandedWeapon() && item.IsCHShield() && i.fulfillsLevelRequirements(item, player) {
			return true
		} else if !ok && item.IsCHShield() && i.fulfillsLevelRequirements(item, player) {
			return true
		} else if ok && primWeapon.IsBow() && item.IsArrow() {
			return true
		}

	} else if player.IsEuropean() {
		if ok && primWeapon.IsOneHandedWeapon() && item.IsEUShield() && i.fulfillsLevelRequirements(item, player) {
			return true
		} else if !ok && item.IsEUShield() && i.fulfillsLevelRequirements(item, player) {
			return true
		} else if ok && primWeapon.IsCrossbow() && item.IsBolt() {
			return true
		}
	}
	return false
}
