package model

import "time"

const (
	TypeID1Item     = 3
	TypeID2Wearable = 1
	TypeID3Weapon   = 6
)

type IItem interface {
	ISRObject
	GetOwner() uint32
	GetRarity() byte
}

type Item struct {
	SRObject
	ID       int
	Name     string
	Variance uint64
	OptLevel byte
	Data     uint32
	RentInfo RentInfo
	TradeInfo
	PriceInfo
	LevelInfo
	StackSize       int
	SpeedClass      int
	COS             COSItemInfo
	MagicOptions    []MagicOption
	SocketOptions   []BindingOption
	AdvancedElixirs []BindingOption
	Protection      EquipmentProtection
}

type MagicOption struct {
	Type      uint32
	Value     uint32
	ParamType uint32
}

type BindingOption struct {
	Slot  byte
	ID    uint32
	Value uint32
}

type COSItemState byte

const (
	COSItemInactive COSItemState = 1
	COSItemSummoned COSItemState = 2
	COSItemActive   COSItemState = 3
	COSItemDead     COSItemState = 4
)

type COSItemInfo struct {
	Present       bool
	RefObjectID   uint32
	Kind          COSKind
	State         COSItemState
	Name          string
	Level         byte
	RentEndsAt    time.Time
	InventorySlot byte
}

type RentInfo struct {
	Type            uint32
	CanDelete       uint16
	CanRecharge     uint16
	PeriodBeginTime uint32
	PeriodEndTime   uint32
	MeterRateTime   uint32
	PackingTime     uint32
}

func (i *Item) GetType() string {
	return "Item"
}

func (i Item) IsActiveDevilSpirit(now time.Time) bool {
	ref, exists := RefItems[i.GetRefObjectID()]
	return exists && ref.IsDevilSpirit() && i.Data > 0 && i.RentInfo.PeriodEndTime > uint32(now.Unix())
}
