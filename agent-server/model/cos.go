package model

import (
	"errors"
	"math"
	"sync"
	"time"
)

var (
	ErrInvalidCOSKind = errors.New("invalid COS kind")
	ErrCOSNotOwned    = errors.New("COS is not owned by player")
)

// COSKind is the vSRO 1.188 NPC_COS TypeID4 discriminator.
type COSKind byte

const (
	COSRideTransport COSKind = 1
	COSJobTransport  COSKind = 2
	COSGrowthPet     COSKind = 3
	COSAbilityPet    COSKind = 4
	COSGuild         COSKind = 5
	COSCaptured      COSKind = 6
	COSQuest         COSKind = 7
	COSQuest2        COSKind = 8
	COSFellowPet     COSKind = 9
)

func (k COSKind) Valid() bool { return k >= COSRideTransport && k <= COSFellowPet }

// COS is a summoned world character. PersistentID is the database identity;
// UniqueID is the short-lived world identity used by Agent packets.
type COS struct {
	NPC
	stateMu       sync.RWMutex
	PersistentID  uint64
	OwnerCharID   int
	OwnerUniqueID uint32
	OwnerName     string
	GuildName     string
	Kind          COSKind
	Level         byte
	Health        int32
	MaxHealth     int32
	Experience    int64
	HGP           uint16
	Satiety       int32
	Settings      uint32
	InventorySlot byte
	InventorySize byte
	RentEndsAt    time.Time
}

func NewCOS(ref RefChar, owner *Player) (*COS, error) {
	kind := COSKind(ref.TypeID4)
	if owner == nil || ref.ID == 0 || !ref.TypeInfo.IsCOS() || !kind.Valid() {
		return nil, ErrInvalidCOSKind
	}
	cos := &COS{
		NPC: NPC{
			SRObject:    SRObject{RefObjectID: ref.ID, TypeInfo: ref.TypeInfo},
			Type:        "COS",
			Mutex:       &sync.Mutex{},
			LifeState:   Alive,
			MotionState: NoMotion,
			BodyState:   Normal,
		},
		OwnerCharID: owner.ID, OwnerUniqueID: owner.GetUniqueID(),
		OwnerName: owner.GetName(), Kind: kind, Level: byte(max(1, ref.Level)),
		Health: int32(ref.MaxHP), MaxHealth: int32(ref.MaxHP),
	}
	if ref.InventorySize > 0 {
		cos.InventorySize = byte(min(ref.InventorySize, int(^uint8(0))))
	}
	cos.ConfigureMovementSpeeds(float32(ref.Speed1), float32(ref.Speed2))
	cos.SetHwanSpeed(100)
	cos.RtNavmeshPosition = owner.GetNavmeshPosition()
	cos.KnownObjectList = NewKnownObjectList(cos)
	return cos, nil
}

func (c *COS) GetType() string { return "COS" }

type COSSnapshot struct {
	PersistentID  uint64
	UniqueID      uint32
	RefObjectID   uint32
	OwnerCharID   int
	OwnerUniqueID uint32
	OwnerName     string
	Name          string
	GuildName     string
	Kind          COSKind
	Level         byte
	Health        int32
	MaxHealth     int32
	Experience    int64
	HGP           uint16
	Satiety       int32
	Settings      uint32
	InventorySlot byte
	InventorySize byte
	RentEndsAt    time.Time
}

func (c *COS) Snapshot() COSSnapshot {
	if c == nil {
		return COSSnapshot{}
	}
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return COSSnapshot{
		PersistentID: c.PersistentID, UniqueID: c.GetUniqueID(), RefObjectID: c.GetRefObjectID(),
		OwnerCharID: c.OwnerCharID, OwnerUniqueID: c.OwnerUniqueID, OwnerName: c.OwnerName,
		Name: c.GetName(), GuildName: c.GuildName, Kind: c.Kind, Level: c.Level,
		Health: c.Health, MaxHealth: c.MaxHealth, Experience: c.Experience, HGP: c.HGP,
		Satiety: c.Satiety, Settings: c.Settings, InventorySlot: c.InventorySlot, InventorySize: c.InventorySize, RentEndsAt: c.RentEndsAt,
	}
}

func (c *COS) Rename(name string) {
	c.stateMu.Lock()
	c.NPC.SetName(name)
	c.stateMu.Unlock()
}

func (c *COS) UpdateSettings(settings uint32) {
	c.stateMu.Lock()
	c.Settings = settings
	c.stateMu.Unlock()
}

// AwardExperience applies growth-pet EXP against the client-matched character
// level curve and advances to the matching COS reference row when available.
func (c *COS) AwardExperience(levels *LevelTable, gained uint64) (levelsGained int, refChanged bool) {
	if c == nil || c.Kind != COSGrowthPet || levels == nil || gained == 0 {
		return 0, false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	current := uint64(max(int64(0), c.Experience))
	progress := ApplyExperience(levels, int(c.Level), current, 0, gained, 0, 0, 0)
	c.Experience = int64(min(progress.Experience, uint64(math.MaxInt64)))
	levelsGained = progress.LevelsGained
	if levelsGained == 0 {
		return levelsGained, false
	}
	c.Level = byte(min(progress.Level, int(^uint8(0))))
	currentRef := RefChars[c.GetRefObjectID()]
	for _, candidate := range RefChars {
		if candidate.Level != progress.Level || COSKind(candidate.TypeID4) != c.Kind || candidate.OrgObjCodeName != currentRef.OrgObjCodeName {
			continue
		}
		oldMax := max(c.MaxHealth, 1)
		c.RefObjectID = candidate.ID
		c.MaxHealth = int32(max(candidate.MaxHP, 1))
		c.Health = max(int32(1), c.Health*c.MaxHealth/oldMax)
		c.ConfigureMovementSpeeds(float32(candidate.Speed1), float32(candidate.Speed2))
		refChanged = true
		break
	}
	return levelsGained, refChanged
}
