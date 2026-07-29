package model

import (
	"github.com/ferdoran/go-sro/framework/network"
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
	numMagParams := 0
	p.WriteByte(item.OptLevel)
	p.WriteUInt64(item.Variance)
	data := item.Data
	if data == 0 {
		data = 10 // Preserve legacy durability for pre-economy item rows.
	}
	p.WriteUInt32(data)
	p.WriteByte(0) // Number of MagParams / Blues

	// TODO write mag params
	for i := 0; i < numMagParams; i++ {
		p.WriteUInt32(0) // magParam.Type
		p.WriteUInt32(0) // magParam.Value
	}

	// TODO write bind options
	numBindOptions := 0
	p.WriteByte(1) // bindingOptionType - 1 = Socket
	p.WriteByte(0) // bindingOptionCount

	for i := 0; i < numBindOptions; i++ {
		p.WriteByte(0)   // BindingOption.Slot
		p.WriteUInt32(0) // BindingOption.ID
		p.WriteUInt32(0) // BindingOption.nParam1
	}

	p.WriteByte(2) // bindingOptionType - 2 = Advanced Elixir
	p.WriteByte(0) // bindingOptionCount
	for i := 0; i < numBindOptions; i++ {
		p.WriteByte(0)   // BindingOption.Slot
		p.WriteUInt32(0) // BindingOption.ID
		p.WriteUInt32(0) // BindingOption.OptValue / Plus
	}

}

func WriteContainerItem(p *network.Packet, item Item) {
	if item.TypeID3 == 1 {
		// ITEM_COS_P
		p.WriteByte(0) // TODO COS State
		p.WriteUInt32(item.GetRefObjectID())
		p.WriteString(item.Name)

		if item.TypeID4 == 2 {
			p.WriteUInt32(3600) // TODO Seconds to Rent End Time
		}
		p.WriteByte(0) // TODO Unknown

	} else if item.TypeID3 == 2 {
		// ITEM_ETC_TRANS_MONSTER
		p.WriteUInt32(item.GetRefObjectID())
	} else if item.TypeID3 == 3 {
		// MAGIC_CUBE
		p.WriteUInt32(0) // TODO Amount of elixirs inside the cube
	}
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
			p.WriteByte(0) // TODO AttributeAssimilationProbability
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
