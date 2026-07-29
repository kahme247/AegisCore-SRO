package model

import (
	"context"
)

// TeleportGate is a server-owned type-4 structure. The stock client has its
// visual model in Media.pk2, but still requires a runtime UID and spawn packet
// before that map structure can be selected.
type TeleportGate struct{ SRObject }

func (g *TeleportGate) GetType() string { return "TeleportGate" }

func IsTeleportGateObject(object ISRObject) bool {
	gate, ok := object.(*TeleportGate)
	return ok && gate != nil && gate.GetTypeInfo().IsStructure()
}

type TeleportGateSpawn struct {
	RefObjectID uint32
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
		{RefObjectID: 2094, CodeName: "STORE_CH_GATE", RegionID: 25000, X: 1254, Y: -6, Z: 1374},
		{RefObjectID: 2095, CodeName: "STORE_WC_GATE", RegionID: 26521, X: 962, Y: -35, Z: 8},
		{RefObjectID: 2096, CodeName: "STORE_KT_GATE", RegionID: 23687, X: 1131, Y: 305, Z: 488},
		{RefObjectID: 19554, CodeName: "STORE_EU_GATE", RegionID: 26959, X: 700, Y: 200, Z: 886},
		{RefObjectID: 19555, CodeName: "STORE_CA_GATE", RegionID: 27499, X: 1917, Y: 250, Z: 108},
	}, nil
}
