package model

import (
	"fmt"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/ferdoran/go-sro/framework/utils"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"sync"
)

const PlayerType = "Player"

// PKState is deliberately separate from BattleState. It represents legacy PvP
// reputation/cape state, not the entity's peace/battle mode.
type PKState int

const (
	PvpWhite PKState = iota
	PvpPurple
	PvpRed
)

// PVP Flag (For CTF)
type CTFTeam int

const (
	CtfRed CTFTeam = iota
	CtfBlue
	CtfNone = 0xFF
)

// PVP Cape
type PVPCape int

const (
	PvpCapeNone PVPCape = iota
	PvpCapeRed
	PvpCapeGray
	PvpCapeBlue
	PvpCapeWhite
	PvpCapeGold
)

type IPlayer interface {
	ICharacter
	GetPKState() PKState
	GetInventory() Inventory
	GetSession() *server.Session
	GetScale() byte
	GetPVPCape() PVPCape
	GetCharKnownObjectList() *CharKnownObjectList
	GetCOSStateSnapshot() PlayerCOSSnapshot
}

type Player struct {
	SRObject
	MotionState
	LifeState
	BodyState
	BattleState
	CharKnownObjectList *CharKnownObjectList
	Session             *server.Session
	Mutex               sync.Mutex
	economyGate         sync.Mutex
	Scale               byte
	ID                  int
	CharName            string
	Inventory           Inventory
	BaseStats           BaseStats
	BaseAttackStats     AttackStats
	BaseDefenseStats    DefenseStats
	BonusStats          BonusBaseStats
	BonusAttackStats    BonusAttackStats
	BonusDefenseStats   BonusDefenseStats
	PhyAbsorbPercent    int
	MagAbsorbPercent    int
	PhyBalancePercent   int
	MagBalancePercent   int
	SkillPoints         int
	StatPoints          int
	ExpOffset           uint64
	SkillExpOffset      uint
	Gold                uint64
	Masteries           map[uint32]byte
	Skills              map[uint32]byte
	ActiveBuffs         map[uint32]ActiveBuff
	QuickSlots          map[byte]CharacterQuickSlot
	Quests              []CharacterQuest
	Level               int
	MaxLevel            int
	PKState             PKState
	CTFTeam             CTFTeam
	PVPCape             PVPCape
	IsGm                bool
	Party               *Party
	WalkSpeed           float32
	RunSpeed            float32
	HwanSpeed           float32
	COSState            PlayerCOSState

	/* TODO
	- Teleport Position
	- JobInfo
	- TeleportLocation
	- Active Buffs / Effects
	*/
}

func (p *Player) SetQuickSlots(slots []CharacterQuickSlot) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.QuickSlots = make(map[byte]CharacterQuickSlot, len(slots))
	for _, slot := range slots {
		p.QuickSlots[slot.Sequence] = slot
	}
	p.Mutex.Unlock()
}

func (p *Player) CommitQuickSlot(slot CharacterQuickSlot) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.QuickSlots == nil {
		p.QuickSlots = make(map[byte]CharacterQuickSlot)
	}
	if slot.ContentType == 0 {
		delete(p.QuickSlots, slot.Sequence)
		return
	}
	p.QuickSlots[slot.Sequence] = slot
}

func (p *Player) QuickSlotSnapshot() []CharacterQuickSlot {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	slots := make([]CharacterQuickSlot, 0, len(p.QuickSlots))
	for sequence := byte(0); int(sequence) < MaxCharacterQuickSlots; sequence++ {
		if slot, exists := p.QuickSlots[sequence]; exists {
			slots = append(slots, slot)
		}
	}
	return slots
}

// LockEconomy serializes a persistence-backed inventory/gold mutation with
// publication of its runtime snapshot. Multi-player operations acquire these
// gates in ascending character-ID order.
func (p *Player) LockEconomy() {
	if p != nil {
		p.economyGate.Lock()
	}
}

func (p *Player) UnlockEconomy() {
	if p != nil {
		p.economyGate.Unlock()
	}
}

func (p *Player) CommitEconomy(gold uint64, inventory Inventory) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.Gold = gold
	p.Inventory = inventory.Snapshot()
	p.Mutex.Unlock()
}

// CommitInventory replaces only the persistence-backed inventory snapshot.
// Alchemy does not mutate gold, so it must not overwrite a concurrent balance.
func (p *Player) CommitInventory(inventory Inventory) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.Inventory = inventory.Snapshot()
	p.Mutex.Unlock()
}

// SetGold applies a persistence-backed balance change without replacing the
// independently mutable inventory snapshot.
func (p *Player) SetGold(gold uint64) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.Gold = gold
	p.Mutex.Unlock()
}

// MarkQuestAbandoned updates the session-owned snapshot after the database
// compare-and-swap succeeds. Later bootstrap streams (including reconnect-like
// world reloads) therefore cannot resurrect the abandoned quest.
func (p *Player) MarkQuestAbandoned(questID int) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for i := range p.Quests {
		if p.Quests[i].QuestID == questID {
			p.Quests[i].Status = QuestAbandoned
			return
		}
	}
}

func (p *Player) QuestSnapshot() []CharacterQuest {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return append([]CharacterQuest(nil), p.Quests...)
}

func (p *Player) GetLifeState() LifeState {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.LifeState
}

func (p *Player) SetLifeState(state LifeState) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.LifeState = state
}

func (p *Player) GetBodyState() BodyState {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.BodyState
}

func (p *Player) SetBodyState(state BodyState) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.BodyState = state
}

func (p *Player) GetBattleState() BattleState {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.BattleState
}

func (p *Player) SetBattleState(state BattleState) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.BattleState = state
}

func (p *Player) GetWalkSpeed() float32 {
	return p.WalkSpeed
}

func (p *Player) SetWalkSpeed(speed float32) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.WalkSpeed = speed
}

func (p *Player) GetRunSpeed() float32 {
	return p.RunSpeed
}

func (p *Player) SetRunSpeed(speed float32) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.RunSpeed = speed
}

func (p *Player) GetHwanSpeed() float32 {
	return p.HwanSpeed
}

func (p *Player) SetHwanSpeed(speed float32) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.HwanSpeed = speed
}

func (p *Player) SetName(name string) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.Name = name
}

func (p *Player) SetUniqueID(uniqueId uint32) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.UniqueID = uniqueId
}

func (p *Player) SendPositionUpdate() {
	packet := BuildPositionUpdatePacket(p)
	p.SendToSelfAndKnown(&packet)
}

func (p *Player) SendStatsUpdate() {
	packet := p.BuildStatsUpdatePacket()
	_ = p.Session.Send(packet)
}

func (p *Player) MaxVitals() (int, int) {
	if p == nil {
		return 0, 0
	}
	p.Mutex.Lock()
	level, stats, bonus := p.Level, p.BaseStats, p.BonusStats
	p.Mutex.Unlock()
	maxHP := utils.BaseHPOrMP(level, stats.Str) + bonus.HP
	maxMP := utils.BaseHPOrMP(level, stats.Int) + bonus.MP
	maxHP += maxHP * bonus.HpPercent / 100
	maxMP += maxMP * bonus.MpPercent / 100
	return maxHP, maxMP
}

func (p *Player) CurrentVitals() (int, int) {
	if p == nil {
		return 0, 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.BaseStats.HP, p.BaseStats.MP
}

func (p *Player) CommitMP(expectedMP, cost int) bool {
	if p == nil || expectedMP < 0 || cost < 0 || expectedMP < cost {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.BaseStats.MP != expectedMP {
		return false
	}
	p.BaseStats.MP -= cost
	return true
}

func (p *Player) CommitResurrection(hp, mp int) bool {
	if p == nil || hp <= 0 || mp < 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState != Dead || p.BaseStats.HP != 0 {
		return false
	}
	p.BaseStats.HP, p.BaseStats.MP = hp, mp
	p.LifeState = Alive
	return true
}

func BuildVitalsUpdatePacket(uniqueID uint32, hp, mp int) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateStatus
	packet.WriteUInt32(uniqueID)
	packet.WriteUInt16(0)
	packet.WriteByte(3)
	packet.WriteUInt32(uint32(hp))
	packet.WriteUInt32(uint32(mp))
	return packet
}

func BuildLifeStateUpdatePacket(uniqueID uint32, state LifeState) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateMovementState
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(0)
	packet.WriteByte(byte(state))
	return packet
}

func (p *Player) SendVitalsUpdate() {
	hp, mp := p.CurrentVitals()
	packet := BuildVitalsUpdatePacket(p.GetUniqueID(), hp, mp)
	if p.Session != nil {
		_ = p.Session.Send(packet)
	}
}

// BuildStatsUpdatePacket matches the original 0x303D recalculation packet.
// Current HP/MP remain unchanged; only their calculated maxima change when a
// base attribute or level changes.
func (p *Player) BuildStatsUpdatePacket() network.Packet {
	p.Mutex.Lock()
	level := p.Level
	stats := p.BaseStats
	bonus := p.BonusStats
	p.Mutex.Unlock()

	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateStats
	packet.WriteUInt32(uint32(utils.BaseMinAttack(stats.Str))) // PhyAttackMin
	packet.WriteUInt32(uint32(utils.BaseMaxAttack(stats.Str))) // PhyAttackMax
	packet.WriteUInt32(uint32(utils.BaseMinAttack(stats.Int))) // MagAttackMin
	packet.WriteUInt32(uint32(utils.BaseMaxAttack(stats.Int))) // MagAttackMax
	packet.WriteUInt16(uint16(utils.BaseDef(stats.Str)))       // Phy Def
	packet.WriteUInt16(uint16(utils.BaseDef(stats.Int)))       // Mag Def
	packet.WriteUInt16(11)                                     // Hit Rate
	packet.WriteUInt16(11)                                     // Parry Rate
	maxHP := utils.BaseHPOrMP(level, stats.Str) + bonus.HP
	maxMP := utils.BaseHPOrMP(level, stats.Int) + bonus.MP
	maxHP += maxHP * bonus.HpPercent / 100
	maxMP += maxMP * bonus.MpPercent / 100
	packet.WriteUInt32(uint32(maxHP))
	packet.WriteUInt32(uint32(maxMP))
	packet.WriteUInt16(uint16(stats.Str))
	packet.WriteUInt16(uint16(stats.Int))
	return packet
}

func (p *Player) SendMovementStateUpdate() {
	packet := BuildBodyStateUpdatePacket(p.UniqueID, p.GetBodyState())
	p.Session.Send(packet)
}

// BuildBodyStateUpdatePacket emits entity state category 4, which the vSRO
// client parses as BodyState. Sending the old hardcoded value 2 placed every
// joining character into Untouchable mode indefinitely.
func BuildBodyStateUpdatePacket(uniqueID uint32, state BodyState) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateMovementState
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(4)
	packet.WriteByte(byte(state))
	return packet
}

func (p *Player) PreviewExperienceAward(table *LevelTable, experience, skillExperience uint64) ProgressionResult {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return ApplyExperience(table, p.Level, p.ExpOffset, uint64(p.SkillExpOffset), experience, skillExperience, p.SkillPoints, p.StatPoints)
}

func (p *Player) CommitProgress(progress ProgressionResult) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.Level = progress.Level
	p.ExpOffset = progress.Experience
	if progress.SkillExperience > uint64(^uint(0)) {
		p.SkillExpOffset = ^uint(0)
	} else {
		p.SkillExpOffset = uint(progress.SkillExperience)
	}
	p.SkillPoints = progress.SkillPoints
	p.StatPoints = progress.StatPoints
	if p.MaxLevel < p.Level {
		p.MaxLevel = p.Level
	}
}

func (p *Player) CommitStatPoint(attribute CharacterAttribute) bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.StatPoints <= 0 {
		return false
	}
	switch attribute {
	case CharacterStrength:
		p.BaseStats.Str++
	case CharacterIntellect:
		p.BaseStats.Int++
	default:
		return false
	}
	p.StatPoints--
	return true
}

func (p *Player) ApplyDamage(damage int) (applied int, killed bool) {
	if p == nil || damage <= 0 {
		return 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState != Alive || p.BaseStats.HP <= 0 {
		return 0, false
	}
	if damage > p.BaseStats.HP {
		damage = p.BaseStats.HP
	}
	p.BaseStats.HP -= damage
	if p.BaseStats.HP == 0 {
		p.LifeState = Dead
		return damage, true
	}
	return damage, false
}

func (p *Player) CurrentHP() int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.BaseStats.HP
}

func (p *Player) SendEquipItemPacket(item Item, slot byte) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityEquipItem
	packet.WriteUInt32(p.UniqueID)
	packet.WriteByte(slot)
	packet.WriteUInt32(item.GetRefObjectID())
	packet.WriteBool(item.IsOneHandedWeapon())

	p.Session.Send(packet)
}

func (p *Player) SendUnequipItemPacket(item Item, slot byte) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUnequipItem
	packet.WriteUInt32(p.UniqueID)
	packet.WriteByte(slot)
	packet.WriteUInt32(item.GetRefObjectID())

	p.Session.Send(packet)
}

func (p *Player) IsChinese() bool {
	return p.RefObjectID >= 1907 && p.RefObjectID <= 1932
}

func (p *Player) IsEuropean() bool {
	return p.RefObjectID >= 14875 && p.RefObjectID <= 14900
}

func (p *Player) IsMale() bool {
	return (p.RefObjectID >= 1907 && p.RefObjectID <= 1919) || (p.RefObjectID >= 14875 && p.RefObjectID <= 14887)
}

func (p *Player) IsFemale() bool {
	return (p.RefObjectID >= 1920 && p.RefObjectID <= 1932) || (p.RefObjectID >= 14888 && p.RefObjectID <= 14900)
}

func (p *Player) GetPosition() Position {
	return p.Position
}

func (p *Player) GetUniqueID() uint32 {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.UniqueID
}

func (p *Player) GetPKState() PKState {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	s := p.PKState
	return s
}

func (p *Player) GetInventory() Inventory {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.Inventory.Snapshot()
}

func (p *Player) GetName() string {
	return p.CharName
}

func (p *Player) GetSession() *server.Session {
	return p.Session
}

func (p *Player) GetCOSStateSnapshot() PlayerCOSSnapshot {
	return p.COSState.Snapshot()
}

func (p *Player) Broadcast(packet *network.Packet) {
	if p.CharKnownObjectList == nil {
		return
	}
	playerCount := 0
	for _, object := range p.CharKnownObjectList.GetKnownObjects() {
		if player, isPlayer := object.(IPlayer); isPlayer {
			knownList := player.GetCharKnownObjectList()
			if knownList == nil || !knownList.ClientKnows(p.GetUniqueID()) {
				continue
			}
			playerCount++
			if player.GetSession() != nil {
				_ = player.GetSession().Send(*packet)
			}
		}
	}
	logrus.Debugf("broadcasted message %02X to %d known players of %s", packet.MessageID, playerCount, p.GetName())
}

func (p *Player) SendToSelfAndKnown(packet *network.Packet) {
	if p.Session != nil && p.Session.Conn != nil {
		p.Session.Send(*packet)
	}
	p.Broadcast(packet)
}

func (p *Player) StopMovement() {
	p.CancelMovement(p.GetNavmeshPosition(), "legacy-stop")
	p.SetMotionState(NoMotion)
}

func (p *Player) GetCharKnownObjectList() *CharKnownObjectList {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.CharKnownObjectList
}

func (p *Player) GetKnownObjectList() IKnownObjectList {
	p.Mutex.Lock()
	knownList := p.CharKnownObjectList
	p.Mutex.Unlock()
	if knownList == nil {
		// Returning a typed nil pointer through IKnownObjectList would produce a
		// non-nil interface and let callers panic when they invoke it during
		// disconnect cleanup. A player may be constructed before its known list
		// has been attached. Preserve an explicitly supplied generic list for
		// narrow world tests and tooling, otherwise return a real nil interface.
		return p.SRObject.GetKnownObjectList()
	}
	return knownList
}

func (p *Player) SetMotionState(newState MotionState) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.MotionState = newState
}

func (p *Player) GetMotionState() MotionState {
	return p.MotionState
}

func (p *Player) GetScale() byte {
	return p.Scale
}

func (p *Player) GetPVPCape() PVPCape {
	return p.PVPCape
}

func (p *Player) GetType() string {
	return "Player"
}

func (p *Player) HasParty() bool {
	mutex := p.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()

	return p.Party != nil
}

func (p *Player) AddToParty(party *Party) error {
	mutex := p.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	if p.Party != nil {
		return errors.New(fmt.Sprintf("player is already member of party %d", p.Party.Number))
	}
	p.Party = party

	return nil
}

func (p *Player) GetParty() *Party {
	mutex := p.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()

	return p.Party
}

func (p *Player) RemoveFromParty() {
	mutex := p.mutexForObject()
	mutex.Lock()
	p.Party = nil
	mutex.Unlock()
}

// RemoveFromPartyIf clears membership only when it still points at the
// expected party. Party transitions use it for rollback and disconnect
// cleanup without erasing a newer membership installed concurrently.
func (p *Player) RemoveFromPartyIf(party *Party) bool {
	mutex := p.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	if p.Party != party {
		return false
	}
	p.Party = nil
	return true
}
