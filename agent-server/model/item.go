package model

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
	StackSize  int
	SpeedClass int
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
