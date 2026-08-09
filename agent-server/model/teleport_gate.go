package model

import (
	"context"
)

// TeleportGate is a server-owned type-4 structure. The stock client has its
// visual model in Media.pk2, but still requires a runtime UID and spawn packet
// before that map structure can be selected.
type TeleportGate struct {
	SRObject
	TeleportID uint32
	TaxRate    uint16
	TaxActive  bool
}

func (g *TeleportGate) GetType() string { return "TeleportGate" }

func IsTeleportGateObject(object ISRObject) bool {
	gate, ok := object.(*TeleportGate)
	return ok && gate != nil && gate.GetTypeInfo().IsStructure()
}

type TeleportGateSpawn struct {
	RefObjectID uint32
	TeleportID  uint32
	TaxRate     uint16
	TaxActive   bool
	CodeName    string
	RegionID    int16
	X           float32
	Y           float32
	Z           float32
	Heading     float32
}

type TeleportGateRepository interface {
	GetTeleportGates(context.Context) ([]TeleportGateSpawn, error)
}

type ClientTeleportGateRepository struct{}

func NewTeleportGateRepository() *ClientTeleportGateRepository {
	return &ClientTeleportGateRepository{}
}

func (r *ClientTeleportGateRepository) GetTeleportGates(ctx context.Context) ([]TeleportGateSpawn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Client-matched vSRO 1.88 teleportbuilding.txt interaction positions.
	return []TeleportGateSpawn{
		{RefObjectID: 2094, TeleportID: 1, TaxActive: true, CodeName: "STORE_CH_GATE", RegionID: 25000, X: 1254, Y: -6, Z: 1374},
		{RefObjectID: 2095, TeleportID: 2, CodeName: "STORE_WC_GATE", RegionID: 26521, X: 962, Y: -35, Z: 8},
		{RefObjectID: 2096, TeleportID: 5, TaxActive: true, CodeName: "STORE_KT_GATE", RegionID: 23687, X: 1131, Y: 305, Z: 488},
		{RefObjectID: 19554, TeleportID: 20, CodeName: "STORE_EU_GATE", RegionID: 26959, X: 700, Y: 200, Z: 886},
		{RefObjectID: 19555, TeleportID: 25, CodeName: "STORE_CA_GATE", RegionID: 27499, X: 1917, Y: 250, Z: 108},
		{RefObjectID: 27753, TeleportID: 175, CodeName: "STORE_SD_GATE1", RegionID: 23088, X: 612, Y: 910, Z: 1089},
		{RefObjectID: 27754, TeleportID: 176, CodeName: "STORE_SD_GATE2", RegionID: 23602, X: 1719, Y: 1586, Z: 760},
	}, nil
}
