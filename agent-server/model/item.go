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
	ID          int
	Name        string
	Variance    uint64
	OptLevel    byte
	Data        uint32
	CreatorName string
	// TransientGroundItem marks COS-death cargo whose old persisted identity
	// was cleared. Pickup allocates a replacement ITEM identity.
	TransientGroundItem bool
	RentInfo            RentInfo
	// RentCodeName is retained for drop records whose source references a
	// rent-item definition not yet represented by the maintained shard schema.
	RentCodeName string
	MagicGroupID uint32
	// DropType is the direct-reference category byte copied by the binary
	// into its generated item description. Persistence does not have a
	// dedicated column, so it remains runtime metadata.
	DropType byte
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
	OwnerUniqueID   uint32
	DropRarity      byte
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

func (i *Item) GetOwner() uint32 {
	if i == nil {
		return 0
	}
	return i.OwnerUniqueID
}

func (i *Item) GetRarity() byte {
	if i == nil {
		return 0
	}
	return i.DropRarity
}

func (i Item) IsActiveDevilSpirit(now time.Time) bool {
	ref, exists := RefItems[i.GetRefObjectID()]
	return exists && ref.IsDevilSpirit() && i.Data > 0 && i.RentInfo.PeriodEndTime > uint32(now.Unix())
}
