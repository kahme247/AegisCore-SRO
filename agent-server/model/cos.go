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
type COSTimedService struct {
	Category  int
	JobID     int
	Data1     int
	ExpiresAt time.Time
}

type COS struct {
	NPC
	stateMu           sync.RWMutex
	PersistentID      uint64
	OwnerCharID       int
	OwnerUniqueID     uint32
	OwnerName         string
	GuildName         string
	Kind              COSKind
	Level             byte
	Health            int32
	MaxHealth         int32
	Experience        int64
	HGP               uint16
	Satiety           int32
	Settings          uint32
	InventorySlot     byte
	InventorySize     byte
	BaseInventorySize byte
	Inventory         Inventory
	RentEndsAt        time.Time
	TimedServices     map[int]COSTimedService
	ownerSpeed        float32
	ownerSpeedEnabled bool
	tradeScale        byte
}

func (c *COS) GetTradeScale() byte {
	if c == nil {
		return 0
	}
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.tradeScale
}

func (c *COS) SetTradeScale(scale byte) {
	if c == nil {
		return
	}
	c.stateMu.Lock()
	c.tradeScale = scale
	c.stateMu.Unlock()
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
		Inventory: NewInventory(nil), TimedServices: make(map[int]COSTimedService),
	}
	if ref.InventorySize > 0 {
		cos.InventorySize = byte(min(ref.InventorySize, int(^uint8(0))))
		cos.BaseInventorySize = cos.InventorySize
	}
	cos.ConfigureMovementSpeeds(float32(ref.Speed1), float32(ref.Speed2))
	cos.SetHwanSpeed(100)
	cos.RtNavmeshPosition = owner.GetNavmeshPosition()
	cos.KnownObjectList = NewKnownObjectList(cos)
	return cos, nil
}

func (c *COS) GetType() string { return "COS" }

// SetOwnerMovementSpeed makes companion pets follow their owner's current
// effective movement speed without changing transport or trade-pet speeds.
func (c *COS) SetOwnerMovementSpeed(speed float32, enabled bool) {
	if c == nil {
		return
	}
	c.stateMu.Lock()
	c.ownerSpeed = speed
	c.ownerSpeedEnabled = enabled && speed > 0 && !math.IsNaN(float64(speed)) && !math.IsInf(float64(speed), 0)
	c.stateMu.Unlock()
}

func (c *COS) GetMovementSpeed() float32 {
	if c == nil {
		return 0
	}
	c.stateMu.RLock()
	ownerSpeed, ownerSpeedEnabled := c.ownerSpeed, c.ownerSpeedEnabled
	c.stateMu.RUnlock()
	if ownerSpeedEnabled && (c.Kind == COSGrowthPet || c.Kind == COSAbilityPet) {
		return ownerSpeed
	}
	return c.NPC.GetMovementSpeed()
}

// Revive applies a database-committed pet revival to a summoned runtime COS.
// COS health is separate from NPC combat hit points because the stock owner
// snapshot carries this resource directly.
func (c *COS) Revive(health int32) bool {
	if c == nil || health <= 0 {
		return false
	}
	c.stateMu.Lock()
	if c.MaxHealth <= 0 {
		c.stateMu.Unlock()
		return false
	}
	c.Health = min(health, c.MaxHealth)
	c.stateMu.Unlock()
	c.SetLifeState(Alive)
	return true
}

// ApplyCommittedResources mirrors a database-committed potion result into a
// summoned COS. It never performs revival; dead-state transitions remain
// owned by Revive.
func (c *COS) ApplyCommittedResources(health int32, hgp int) bool {
	if c == nil || health <= 0 || hgp < 0 {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.Health <= 0 || c.LifeState == Dead || c.MaxHealth <= 0 {
		return false
	}
	c.Health = min(health, c.MaxHealth)
	c.HGP = uint16(min(hgp, int(^uint16(0))))
	return true
}

func (c *COS) ApplyCommittedTimedService(service COSTimedService) bool {
	if c == nil || service.JobID == 0 || service.ExpiresAt.IsZero() {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.TimedServices == nil {
		c.TimedServices = make(map[int]COSTimedService)
	}
	if _, exists := c.TimedServices[service.JobID]; exists {
		return false
	}
	c.TimedServices[service.JobID] = service
	if service.Category == 5 && service.Data1 > 0 {
		expanded := int(c.BaseInventorySize) + service.Data1
		if expanded > int(c.InventorySize) {
			c.InventorySize = byte(min(expanded, int(^uint8(0))))
		}
	}
	return true
}

func (c *COS) ExpireTimedServices(now time.Time) []COSTimedService {
	if c == nil {
		return nil
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	var expired []COSTimedService
	for jobID, service := range c.TimedServices {
		if !service.ExpiresAt.After(now) {
			expired = append(expired, service)
			delete(c.TimedServices, jobID)
		}
	}
	inventorySize := c.BaseInventorySize
	for _, service := range c.TimedServices {
		if service.Category == 5 && service.Data1 > 0 {
			expanded := int(c.BaseInventorySize) + service.Data1
			if expanded > int(inventorySize) {
				inventorySize = byte(min(expanded, int(^uint8(0))))
			}
		}
	}
	// Never hide committed items when an inventory-expansion watch expires.
	// Capacity can return to the base only after expanded slots are empty.
	for slot := range c.Inventory.Snapshot().Items {
		if slot < ^uint8(0) && slot+1 > inventorySize {
			inventorySize = slot + 1
		}
	}
	c.InventorySize = inventorySize
	return expired
}

func (c *COS) ClearTimedService(jobID int) bool {
	if c == nil || jobID == 0 {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.TimedServices == nil {
		return false
	}
	if _, exists := c.TimedServices[jobID]; !exists {
		return false
	}
	delete(c.TimedServices, jobID)
	return true
}

func (c *COS) ApplyCommittedRentEnd(rentEndsAt time.Time) bool {
	if c == nil || rentEndsAt.IsZero() || c.Kind != COSAbilityPet {
		return false
	}
	c.stateMu.Lock()
	c.RentEndsAt = rentEndsAt
	c.stateMu.Unlock()
	return true
}

// MarkDead changes the runtime COS to the durable dead state. Persistence is
// intentionally performed by COSService so SQL and world transitions remain
// ordered.
func (c *COS) MarkDead() bool {
	if c == nil {
		return false
	}
	c.stateMu.Lock()
	c.Health = 0
	c.stateMu.Unlock()
	c.SetLifeState(Dead)
	return true
}

// ApplyDamage reduces a summoned COS's durable health and transitions it to
// the dead state when exhausted. COS health is independent from NPC combat
// health because it is persisted in CHAR_COS.
func (c *COS) ApplyDamage(damage int) (applied int, killed bool) {
	if c == nil || damage <= 0 {
		return 0, false
	}
	c.stateMu.Lock()
	if c.Health <= 0 || c.LifeState == Dead {
		c.stateMu.Unlock()
		return 0, false
	}
	applied = min(damage, int(c.Health))
	c.Health -= int32(applied)
	killed = c.Health == 0
	c.stateMu.Unlock()
	if killed {
		c.SetLifeState(Dead)
	}
	return applied, killed
}

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
	Inventory     Inventory
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
		Satiety: c.Satiety, Settings: c.Settings, InventorySlot: c.InventorySlot, InventorySize: c.InventorySize, Inventory: c.Inventory.Snapshot(), RentEndsAt: c.RentEndsAt,
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

func (c *COS) ReplaceInventory(inventory Inventory) {
	if c == nil {
		return
	}
	c.stateMu.Lock()
	c.Inventory = inventory.Snapshot()
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
