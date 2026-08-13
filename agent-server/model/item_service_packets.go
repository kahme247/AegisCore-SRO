package model

import (
	"math"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func BuildItemPerkAdd(characterUID, refItemID, token uint32, value time.Duration, remaining time.Duration) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemPerkAdd
	packet.WriteUInt32(characterUID)
	packet.WriteUInt32(refItemID)
	packet.WriteUInt32(token)
	packet.WriteUInt32(durationMilliseconds(value))
	packet.WriteUInt32(durationSecondsCeil(remaining))
	return packet
}

func BuildItemPerkRemove(characterUID, refItemID, token uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ItemPerkRemove
	packet.WriteUInt32(characterUID)
	packet.WriteUInt32(refItemID)
	packet.WriteUInt32(token)
	return packet
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
