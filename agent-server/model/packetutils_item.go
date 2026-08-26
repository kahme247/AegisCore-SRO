package model

import (
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func WriteRentInfo(p *network.Packet, item Item) {
	rent := item.RentInfo
	if rent.Type < 1 || rent.Type > 3 {
		p.WriteUInt32(0)
		return
	}
	p.WriteUInt32(rent.Type)

	switch rent.Type {
	case 1:
		p.WriteUInt16(rent.CanDelete)
		p.WriteUInt32(rent.PeriodBeginTime)
		p.WriteUInt32(rent.PeriodEndTime)
	case 2:
		p.WriteUInt16(rent.CanDelete)
		p.WriteUInt16(rent.CanRecharge)
		p.WriteUInt32(rent.MeterRateTime)
	case 3:
		p.WriteUInt16(rent.CanDelete)
		p.WriteUInt16(rent.CanRecharge)
		p.WriteUInt32(rent.PeriodBeginTime)
		p.WriteUInt32(rent.PeriodEndTime)
		p.WriteUInt32(rent.PackingTime)
	}
}

func WriteInventoryItem(p *network.Packet, item Item) {
	if item.IsEquipment() {
		WriteEquipmentItem(p, item)
	} else if item.IsContainer() {
		WriteContainerItem(p, item)
	} else if item.IsExpendable() {
		WriteExpendableItem(p, item)
	}
}

func WriteEquipmentItem(p *network.Packet, item Item) {
	p.WriteByte(item.OptLevel)
	p.WriteUInt64(item.Variance)
	data := item.Data
	if data == 0 {
		data = 10 // Preserve legacy durability for pre-economy item rows.
	}
	p.WriteUInt32(data)
	magicOptions := boundedMagicOptions(item.MagicOptions)
	p.WriteByte(byte(len(magicOptions)))
	for _, option := range magicOptions {
		p.WriteUInt32(option.Type)
		p.WriteUInt32(option.Value)
	}

	p.WriteByte(1) // bindingOptionType - 1 = Socket
	sockets := boundedBindingOptions(item.SocketOptions)
	p.WriteByte(byte(len(sockets)))
	for _, option := range sockets {
		p.WriteByte(option.Slot)
		p.WriteUInt32(option.ID)
		p.WriteUInt32(option.Value)
	}

	p.WriteByte(2) // bindingOptionType - 2 = Advanced Elixir
	advanced := boundedBindingOptions(item.AdvancedElixirs)
	p.WriteByte(byte(len(advanced)))
	for _, option := range advanced {
		p.WriteByte(option.Slot)
		p.WriteUInt32(option.ID)
		p.WriteUInt32(option.Value)
	}
}

func boundedMagicOptions(options []MagicOption) []MagicOption {
	if len(options) > 12 {
		return options[:12]
	}
	return options
}

func boundedBindingOptions(options []BindingOption) []BindingOption {
	if len(options) > 12 {
		return options[:12]
	}
	return options
}

func WriteContainerItem(p *network.Packet, item Item) {
	if item.TypeID3 == 1 {
		state := item.COS.State
		if !item.COS.Present || state < COSItemInactive || state > COSItemDead {
			state = COSItemInactive
		}
		p.WriteByte(byte(state))
		if state != COSItemInactive {
			p.WriteUInt32(item.COS.RefObjectID)
			p.WriteString(item.COS.Name)
			if item.COS.Kind == COSAbilityPet || (item.COS.Kind == 0 && item.TypeID4 == 2) {
				writeCOSAbilityPetLifetime(p, item.COS.RentEndsAt, time.Now().UTC())
			}
			p.WriteByte(0) // active COS item buff count
		}

	} else if item.TypeID3 == 2 {
		// ITEM_ETC_TRANS_MONSTER
		p.WriteUInt32(item.Data)
	} else if item.TypeID3 == 3 {
		// MAGIC_CUBE
		p.WriteUInt32(0) // TODO Amount of elixirs inside the cube
	}
}

func writeCOSAbilityPetLifetime(p *network.Packet, endsAt, now time.Time) {
	const maxLifetimeSeconds = int64(^uint32(0) >> 1)

	remaining := int64(0)
	if endsAt.After(now) {
		remaining = int64((endsAt.Sub(now) + time.Second - 1) / time.Second)
		if remaining > maxLifetimeSeconds {
			remaining = maxLifetimeSeconds
		}
	}
	// The stock vSRO 1.188 SOItem reader (0x008BE8F0) reads one signed
	// 32-bit remaining-lifetime value and converts it from seconds to
	// milliseconds. It does not read the generic RentInfo structure here.
	p.WriteUInt32(uint32(remaining))
}

func WriteExpendableItem(p *network.Packet, item Item) {
	stackSize := item.StackSize
	if stackSize < 1 {
		stackSize = 1
	}
	if stackSize > int(^uint16(0)) {
		stackSize = int(^uint16(0))
	}
	p.WriteUInt16(uint16(stackSize))

	if item.TypeID3 == 11 {
		if item.TypeID4 == 1 || item.TypeID4 == 2 {
			// MAGICSTONE, ATTRSTONE
			assimilation := item.Data
			if assimilation > 100 {
				assimilation = 100
			}
			p.WriteByte(byte(assimilation))
		}
	} else if item.TypeID3 == 14 && (item.TypeID4 == 1 || item.TypeID4 == 2) {
		// ITEM_MALL_GACHA_CARD_WIN
		// ITEM_MALL_GACHA_CARD_LOSE
		numMagParams := 0
		p.WriteByte(0) // TODO item.MagParamCount

		for i := 0; i < numMagParams; i++ {
			p.WriteUInt32(0) // TODO magParam.Type
			p.WriteUInt32(0) // TODO magParam.Value
		}
	}
}
