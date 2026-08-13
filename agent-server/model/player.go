package model

import (
	"fmt"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
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
	CharKnownObjectList   *CharKnownObjectList
	Session               *server.Session
	Mutex                 sync.Mutex
	economyGate           sync.Mutex
	Scale                 byte
	ID                    int
	CharName              string
	Inventory             Inventory
	InventorySize         byte
	BaseStats             BaseStats
	BaseAttackStats       AttackStats
	BaseDefenseStats      DefenseStats
	BonusStats            BonusBaseStats
	BonusAttackStats      BonusAttackStats
	BonusDefenseStats     BonusDefenseStats
	PassiveSkillModifiers BuffModifiers
	PhyAbsorbPercent      int
	MagAbsorbPercent      int
	PhyBalancePercent     int
	MagBalancePercent     int
	SkillPoints           int
	StatPoints            int
	ExpOffset             uint64
	SkillExpOffset        uint
	Gold                  uint64
	Masteries             map[uint32]byte
	Skills                map[uint32]byte
	ActiveBuffs           map[uint32]ActiveBuff
	QuickSlots            map[byte]CharacterQuickSlot
	Quests                []CharacterQuest
	Level                 int
	MaxLevel              int
	PKState               PKState
	CTFTeam               CTFTeam
	PVPCape               PVPCape
	IsGm                  bool
	Party                 *Party
	WalkSpeed             float32
	RunSpeed              float32
	HwanSpeed             float32
	speedEffectPercent    int
	speedEffectGeneration uint64
	speedEffectToken      uint32
	speedEffectSkillID    uint32
	speedEffectExpiresAt  time.Time
	itemServices          map[ItemServiceKind]ItemServiceEffect
	itemServiceTokens     map[ItemServiceKind]uint32
	COSState              PlayerCOSState

	/* TODO
	- Teleport Position
	- JobInfo
	- TeleportLocation
	- Active Buffs / Effects
	*/
}

func (p *Player) GetInventorySize() byte {
	if p == nil || p.InventorySize == 0 {
		return BaseInventorySize
	}
	return p.InventorySize
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

func (p *Player) GoldSnapshot() uint64 {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.Gold
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

// CommitQuestActivation publishes the database-backed active quest to the
// session snapshot used by subsequent quest updates and reconnect bootstrap.
func (p *Player) CommitQuestActivation(quest CharacterQuest) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for index := range p.Quests {
		if p.Quests[index].QuestID == quest.QuestID {
			p.Quests[index] = quest
			return
		}
	}
	p.Quests = append(p.Quests, quest)
}

func (p *Player) CommitQuestMutation(quest CharacterQuest) {
	p.CommitQuestActivation(quest)
}

func (p *Player) CommitQuestItemGrant(result QuestItemGrantResult) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.Inventory = result.Inventory.Snapshot()
	p.commitQuestLocked(result.Quest)
	p.Mutex.Unlock()
}

// CommitQuestCompletion publishes every persistence-backed completion result
// under one lock after the cross-resource database transaction commits.
func (p *Player) CommitQuestCompletion(result QuestCompletionResult) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.Inventory = result.Inventory.Snapshot()
	p.Gold = result.Gold
	p.Level = result.Progress.Level
	p.ExpOffset = result.Progress.Experience
	if result.Progress.SkillExperience > uint64(^uint(0)) {
		p.SkillExpOffset = ^uint(0)
	} else {
		p.SkillExpOffset = uint(result.Progress.SkillExperience)
	}
	p.SkillPoints = result.Progress.SkillPoints
	p.StatPoints = result.Progress.StatPoints
	if p.MaxLevel < p.Level {
		p.MaxLevel = p.Level
	}
	p.commitQuestLocked(result.Quest)
	p.Mutex.Unlock()
}

func (p *Player) commitQuestLocked(quest CharacterQuest) {
	for index := range p.Quests {
		if p.Quests[index].QuestID == quest.QuestID {
			p.Quests[index] = quest
			return
		}
	}
	p.Quests = append(p.Quests, quest)
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

func BuildMovementSpeedUpdatePacket(uniqueID uint32, walkSpeed, runSpeed float32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateMovementSpeed
	packet.WriteUInt32(uniqueID)
	packet.WriteFloat32(walkSpeed)
	packet.WriteFloat32(runSpeed)
	return packet
}

func (p *Player) ApplyMovementSpeedEffect(skillID uint32, percent int, expiresAt time.Time, token uint32) (uint64, uint32) {
	if p == nil || skillID == 0 || percent <= 0 || !expiresAt.After(time.Now()) || token == 0 {
		return 0, 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	replacedToken := p.speedEffectToken
	p.speedEffectGeneration++
	p.speedEffectPercent = percent
	p.speedEffectToken = token
	p.speedEffectSkillID = skillID
	p.speedEffectExpiresAt = expiresAt
	return p.speedEffectGeneration, replacedToken
}

func (p *Player) ClearMovementSpeedEffect(generation uint64) (uint32, bool) {
	if p == nil || generation == 0 {
		return 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.speedEffectGeneration != generation || p.speedEffectPercent == 0 {
		return 0, false
	}
	token := p.speedEffectToken
	p.speedEffectPercent = 0
	p.speedEffectToken = 0
	p.speedEffectSkillID = 0
	p.speedEffectExpiresAt = time.Time{}
	return token, true
}

func (p *Player) MovementSpeedEffect() (uint32, int, time.Time, uint64, uint32, bool) {
	if p == nil {
		return 0, 0, time.Time{}, 0, 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.speedEffectPercent <= 0 || !p.speedEffectExpiresAt.After(time.Now()) {
		return 0, 0, time.Time{}, 0, 0, false
	}
	return p.speedEffectSkillID, p.speedEffectPercent, p.speedEffectExpiresAt, p.speedEffectGeneration, p.speedEffectToken, true
}

func (p *Player) MovementSpeeds() (float32, float32) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	multiplier := float32(100+p.speedEffectPercent) / 100
	return p.WalkSpeed * multiplier, p.RunSpeed * multiplier
}

func (p *Player) SendMovementSpeedUpdate() {
	walkSpeed, runSpeed := p.MovementSpeeds()
	packet := BuildMovementSpeedUpdatePacket(p.GetUniqueID(), walkSpeed, runSpeed)
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
	maxHP := utils.BaseHPOrMP(level, stats.Str+bonus.Str) + bonus.HP
	maxMP := utils.BaseHPOrMP(level, stats.Int+bonus.Int) + bonus.MP
	maxHP += maxHP * bonus.HpPercent / 100
	maxMP += maxMP * bonus.MpPercent / 100
	return maxHP, maxMP
}

func (p *Player) AverageAttackPower(magical bool) int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	attribute := p.BaseStats.Str + p.BonusStats.Str
	minimumBonus, maximumBonus := p.BonusAttackStats.PhyAttackMin, p.BonusAttackStats.PhyAttackMax
	percent := p.BonusAttackStats.PhyAttackPercent
	if magical {
		attribute = p.BaseStats.Int + p.BonusStats.Int
		minimumBonus, maximumBonus = p.BonusAttackStats.MagAttackMin, p.BonusAttackStats.MagAttackMax
		percent = p.BonusAttackStats.MagAttackPercent
	}
	minimum := utils.BaseMinAttack(attribute) + minimumBonus
	maximum := utils.BaseMaxAttack(attribute) + maximumBonus
	return applyStatPercent((minimum+maximum)/2, percent)
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

func (p *Player) PreviewHealing(hpAmount, mpAmount int) (hp, mp, healedHP, healedMP int, ok bool) {
	if p == nil || hpAmount < 0 || mpAmount < 0 {
		return 0, 0, 0, 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState != Alive || p.BaseStats.HP <= 0 {
		return 0, 0, 0, 0, false
	}
	maxHP := utils.BaseHPOrMP(p.Level, p.BaseStats.Str+p.BonusStats.Str) + p.BonusStats.HP
	maxMP := utils.BaseHPOrMP(p.Level, p.BaseStats.Int+p.BonusStats.Int) + p.BonusStats.MP
	maxHP += maxHP * p.BonusStats.HpPercent / 100
	maxMP += maxMP * p.BonusStats.MpPercent / 100
	hp, mp = p.BaseStats.HP, p.BaseStats.MP
	healedHP, healedMP = min(hp+hpAmount, maxHP), min(mp+mpAmount, maxMP)
	return hp, mp, healedHP, healedMP, true
}

func (p *Player) CommitHealing(expectedHP, expectedMP, hp, mp int) bool {
	if p == nil || hp <= 0 || mp < 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState != Alive || p.BaseStats.HP != expectedHP || p.BaseStats.MP != expectedMP {
		return false
	}
	p.BaseStats.HP, p.BaseStats.MP = hp, mp
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
	attackBonus := p.BonusAttackStats
	defenseBonus := p.BonusDefenseStats
	p.Mutex.Unlock()
	effectiveStr, effectiveInt := stats.Str+bonus.Str, stats.Int+bonus.Int
	phyMin := applyStatPercent(utils.BaseMinAttack(effectiveStr)+attackBonus.PhyAttackMin, attackBonus.PhyAttackPercent)
	phyMax := applyStatPercent(utils.BaseMaxAttack(effectiveStr)+attackBonus.PhyAttackMax, attackBonus.PhyAttackPercent)
	magMin := applyStatPercent(utils.BaseMinAttack(effectiveInt)+attackBonus.MagAttackMin, attackBonus.MagAttackPercent)
	magMax := applyStatPercent(utils.BaseMaxAttack(effectiveInt)+attackBonus.MagAttackMax, attackBonus.MagAttackPercent)
	phyDef := applyStatPercent(utils.BaseDef(effectiveStr)+defenseBonus.PhyDef, defenseBonus.PhyDefPercent)
	magDef := applyStatPercent(utils.BaseDef(effectiveInt)+defenseBonus.MagDef, defenseBonus.MagDefPercent)
	hitRate := applyStatPercent(11+attackBonus.HitRate, attackBonus.HitRatePercent)
	parryRate := applyStatPercent(11+defenseBonus.ParryRate, defenseBonus.ParryRatePercent)

	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateStats
	packet.WriteUInt32(uint32(phyMin))
	packet.WriteUInt32(uint32(phyMax))
	packet.WriteUInt32(uint32(magMin))
	packet.WriteUInt32(uint32(magMax))
	packet.WriteUInt16(uint16(phyDef))
	packet.WriteUInt16(uint16(magDef))
	packet.WriteUInt16(uint16(hitRate))
	packet.WriteUInt16(uint16(parryRate))
	maxHP := utils.BaseHPOrMP(level, effectiveStr) + bonus.HP
	maxMP := utils.BaseHPOrMP(level, effectiveInt) + bonus.MP
	maxHP += maxHP * bonus.HpPercent / 100
	maxMP += maxMP * bonus.MpPercent / 100
	packet.WriteUInt32(uint32(maxHP))
	packet.WriteUInt32(uint32(maxMP))
	packet.WriteUInt16(uint16(effectiveStr))
	packet.WriteUInt16(uint16(effectiveInt))
	return packet
}

func applyStatPercent(value, percent int) int {
	return value + value*percent/100
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
	p.SendToSelf(packet)
	p.Broadcast(packet)
}

func (p *Player) SendToSelf(packet *network.Packet) {
	if p != nil && packet != nil && p.Session != nil && p.Session.Conn != nil {
		_ = p.Session.Send(*packet)
	}
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
