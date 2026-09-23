package model

import (
	"math"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func BuildItemPerkAdd(characterUID uint32, effect ItemServiceEffect, remaining time.Duration) network.Packet {
	refItemID, action, value := itemServicePerkFields(effect)
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemPerkAdd
	packet.WriteUInt32(characterUID)
	packet.WriteUInt32(refItemID)
	packet.WriteUInt32(action)
	packet.WriteUInt32(value)
	packet.WriteUInt32(durationSecondsCeil(remaining))
	return packet
}

func BuildItemPerkRemove(characterUID uint32, effect ItemServiceEffect) network.Packet {
	refItemID, action, _ := itemServicePerkFields(effect)
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemPerkRemove
	packet.WriteUInt32(characterUID)
	packet.WriteUInt32(refItemID)
	packet.WriteUInt32(action)
	return packet
}

func BuildItemServiceTargetLevelUpdate(effect ItemServiceEffect, active bool) (network.Packet, bool, error) {
	if !effect.TargetSlotSet {
		return network.Packet{}, false, nil
	}
	level := effect.TargetBaseOptLevel
	expiry := uint32(0)
	if active {
		level = effect.TargetProjectedOptLevel
		if effect.ExpiresAt.After(time.Now()) {
			expiry = uint32(effect.ExpiresAt.Unix())
		}
	}
	packet, err := vsro188.BuildInventoryItemUpdate(vsro188.InventoryItemUpdate{
		ItemKind:  effect.TargetSlot,
		Flags:     0x82,
		Flag02U8:  &level,
		Flag80U32: &expiry,
	})
	return packet, err == nil, err
}

func BuildPlayerItemServiceTargetLevelUpdate(player *Player, effect ItemServiceEffect, active bool) (network.Packet, bool, error) {
	if player == nil || !effect.TargetSlotSet {
		return network.Packet{}, false, nil
	}
	item, exists := player.Inventory.Snapshot().Items[effect.TargetSlot]
	if !exists {
		return network.Packet{}, false, nil
	}
	var advanced byte
	for _, option := range item.AdvancedElixirs {
		if option.Value > uint32(math.MaxUint8-advanced) {
			advanced = math.MaxUint8
			break
		}
		advanced += byte(option.Value)
	}
	projected := effect
	projected.TargetBaseOptLevel = byte(min(math.MaxUint8, int(effect.TargetBaseOptLevel)+int(advanced)))
	projected.TargetProjectedOptLevel = byte(min(math.MaxUint8, int(effect.TargetProjectedOptLevel)+int(advanced)))
	return BuildItemServiceTargetLevelUpdate(projected, active)
}

func ItemServiceTargetMatchesInventory(player *Player, effect ItemServiceEffect) bool {
	if player == nil || !effect.TargetSlotSet {
		return false
	}
	item, exists := player.Inventory.Snapshot().Items[effect.TargetSlot]
	if !exists || !item.IsEquipment() || item.OptLevel != effect.TargetBaseOptLevel {
		return false
	}
	switch effect.Kind {
	case ItemServiceReinforceWeapon:
		return item.TypeID3 == TypeID3Weapon
	case ItemServiceReinforceArmor:
		return item.TypeID3 >= 1 && item.TypeID3 <= 4
	case ItemServiceReinforceAccessory:
		return item.TypeID3 == 5 || item.TypeID3 == 12
	default:
		return false
	}
}

func itemServicePerkFields(effect ItemServiceEffect) (refItemID, action, value uint32) {
	refItemID = effect.SourceRefItemID
	ref, exists := RefItems[refItemID]
	if !exists {
		return refItemID, 0, 0
	}
	actionRef := ref
	if effect.Kind == ItemServiceHwanDuration &&
		ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 13 && ref.TypeID4 == 14 &&
		refItemID >= 6 {
		if consumable, paired := RefItems[refItemID-6]; paired &&
			consumable.TypeID1 == 3 && consumable.TypeID2 == 3 && consumable.TypeID3 == 3 && consumable.TypeID4 == 10 {
			actionRef = consumable
		}
	}
	return refItemID, uint32(max(0, actionRef.Param2)), uint32(max(0, actionRef.Param3))
}

func ItemServiceUsesItemPerk(effect ItemServiceEffect) bool {
	switch effect.SourceRefItemID {
	case 9277, 35567:
		return false
	default:
		return true
	}
}

func BuildPremiumDailyAdd(token uint32, effect ItemServiceEffect, remaining time.Duration) (network.Packet, bool) {
	serviceRefItemID := premiumDailyServiceRefItemID(effect)
	if serviceRefItemID == 0 || token == 0 || remaining <= 0 {
		return network.Packet{}, false
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PremiumDailyAdd
	packet.WriteUInt32(serviceRefItemID)
	packet.WriteUInt32(durationSecondsCeil(remaining))
	packet.WriteUInt32(token)
	packet.WriteByte(premiumDailyAvailableCount(effect, time.Now().UTC()))
	return packet, true
}

func BuildPremiumDailyUseSuccess(serviceRefItemID, token uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PremiumDailyUseResponse
	packet.WriteByte(1)
	packet.WriteUInt32(serviceRefItemID)
	packet.WriteUInt32(token)
	return packet
}

func premiumDailyAvailableCount(effect ItemServiceEffect, now time.Time) byte {
	if effect.DailyUsedOn.IsZero() || !sameUTCDate(effect.DailyUsedOn, now) {
		return 1
	}
	return 0
}

func BuildPremiumDailyRemove(token uint32, effect ItemServiceEffect) (network.Packet, bool) {
	serviceRefItemID := premiumDailyServiceRefItemID(effect)
	if serviceRefItemID == 0 || token == 0 {
		return network.Packet{}, false
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PremiumDailyRemove
	packet.WriteUInt32(serviceRefItemID)
	packet.WriteUInt32(token)
	return packet, true
}

func premiumDailyServiceRefItemID(effect ItemServiceEffect) uint32 {
	switch effect.SourceRefItemID {
	case 9277, 35567:
		return 9278 // ITEM_PRE_GOLTAM_4W, linked by the Premium package's UIU1 action
	default:
		return 0
	}
}

func ItemServiceBuffSkillID(effect ItemServiceEffect) uint32 {
	switch effect.SourceRefItemID {
	case 9277, 35567:
		return 5412 // SKILL_MALL_PRE_APRU_4W_01
	default:
		return 0
	}
}

func BuildItemServiceBuffAdd(characterUID, token uint32, effect ItemServiceEffect, remaining time.Duration) (network.Packet, bool) {
	skillID := ItemServiceBuffSkillID(effect)
	if skillID == 0 || remaining <= 0 {
		return network.Packet{}, false
	}
	return BuildTimedBuffAdd(characterUID, skillID, token, durationMilliseconds(remaining)), true
}

func durationSecondsCeil(value time.Duration) uint32 {
	if value <= 0 {
		return 0
	}
	seconds := value / time.Second
	if value%time.Second != 0 {
		seconds++
	}
	if seconds > time.Duration(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(seconds)
}

func durationMilliseconds(value time.Duration) uint32 {
	if value <= 0 {
		return 0
	}
	if value.Milliseconds() > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value.Milliseconds())
}
