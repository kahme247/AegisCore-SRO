package model

import (
	"fmt"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

const PlayerType = "Player"

const (
	PlayerJobTrader byte = 1
	PlayerJobThief  byte = 2
	PlayerJobHunter byte = 3
	PlayerJobNone   byte = 4
)

type PlayerJob struct {
	Type       byte
	Level      byte
	Experience uint64
	Alias      string
}

func (j PlayerJob) Active() bool {
	return j.Type >= PlayerJobTrader && j.Type <= PlayerJobHunter
}

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
	GetJob() PlayerJob
	GetInventory() Inventory
	GetAvatarInventory() Inventory
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
	activeMagicCubeID     int
	Scale                 byte
	ID                    int
	CharName              string
	Inventory             Inventory
	AvatarInventory       Inventory
	InventorySize         byte
	BaseStats             BaseStats
	BaseAttackStats       AttackStats
	BaseDefenseStats      DefenseStats
	BonusStats            BonusBaseStats
	BonusAttackStats      BonusAttackStats
	BonusDefenseStats     BonusDefenseStats
	PassiveSkillModifiers BuffModifiers
	PassiveRecovery       []RecoveryStatusDescriptor
	CombatDamagePercent   int
	PhyAbsorbPercent      int
	MagAbsorbPercent      int
	PhyBalancePercent     int
	MagBalancePercent     int
	SkillPoints           int
	StatPoints            int
	ExpOffset             uint64
	SkillExpOffset        uint
	Gold                  uint64
	Job                   PlayerJob
	Masteries             map[uint32]byte
	Skills                map[uint32]byte
	ActiveBuffs           map[uint32]ActiveBuff
	activeRecovery        map[uint32]RecoveryStatusDescriptor
	combatStatuses        map[CombatStatusKind]CombatStatus
	characterMask         CharacterMask
	activeBarrier         ActiveBarrier
	QuickSlots            map[byte]CharacterQuickSlot
	AutoPotionSettings    AutoPotionSettings
	HasAutoPotionSettings bool
	Quests                []CharacterQuest
	questMarks            map[uint32]QuestMark
	nextQuestMarkID       uint32
	questTimers           map[int]QuestTimer
	questTimerGeneration  uint64
	Level                 int
	MaxLevel              int
	PKState               PKState
	pkCounters            CharacterPKState
	CTFTeam               CTFTeam
	PVPCape               PVPCape
	IsGm                  bool
	Party                 *Party
	WalkSpeed             float32
	RunSpeed              float32
	HwanSpeed             float32
	HwanLevel             byte
	RemainHwanCount       byte
	GatheredHwanPoint     uint32
	berserkGeneration     uint64
	berserkExpiresAt      time.Time
	speedEffectPercent    int
	speedEffectGeneration uint64
	speedEffectToken      uint32
	speedEffectSkillID    uint32
	speedEffectExpiresAt  time.Time
	itemServices          map[ItemServiceKind]ItemServiceEffect
	itemServiceTokens     map[ItemServiceKind]uint32
	premiumDecoration     PremiumDecorationState
	premiumPaused         map[byte]bool
	COSState              PlayerCOSState

	/* TODO
	- Teleport Position
	- JobInfo
	- TeleportLocation
	- Active Buffs / Effects
	*/
}

// ActivateMagicCube records the authenticated ITEM identity selected through
// the stock 0x704C item-use path. The following context-free 0x27 operation
// is authorized only against this per-player state.
func (p *Player) ActivateMagicCube(itemID int) bool {
	if p == nil || itemID <= 0 {
		return false
	}
	p.Mutex.Lock()
	p.activeMagicCubeID = itemID
	p.Mutex.Unlock()
	return true
}

func (p *Player) ActiveMagicCube() (int, bool) {
	if p == nil {
		return 0, false
	}
	p.Mutex.Lock()
	itemID := p.activeMagicCubeID
	p.Mutex.Unlock()
	return itemID, itemID > 0
}

func (p *Player) DeactivateMagicCube() {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.activeMagicCubeID = 0
	p.Mutex.Unlock()
}

type PremiumDecorationState struct {
	Token     uint32
	Reference uint32
	Value     uint32
}

func (p *Player) SetPremiumDecoration(state PremiumDecorationState) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.premiumDecoration = state
	p.Mutex.Unlock()
}

func (p *Player) PremiumDecoration() PremiumDecorationState {
	if p == nil {
		return PremiumDecorationState{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.premiumDecoration
}

func (p *Player) SetPremiumCategoryActive(category byte, active bool) bool {
	if p == nil || category == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.premiumPaused == nil {
		p.premiumPaused = make(map[byte]bool)
	}
	p.premiumPaused[category] = !active
	return true
}

func (p *Player) PremiumCategoryPaused(category byte) bool {
	if p == nil || category == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.premiumPaused[category]
}

// ActiveCombatDamagePercent is SR_GameServer's urd modifier. Unlike ordinary
// attack-range bonuses, the binary consumes it after target mitigation.
func (p *Player) ActiveCombatDamagePercent() int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.CombatDamagePercent
}

func (p *Player) SetAutoPotionSettings(settings AutoPotionSettings, present bool) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.AutoPotionSettings = settings
	p.HasAutoPotionSettings = present
	p.Mutex.Unlock()
}

func (p *Player) AutoPotionSettingsSnapshot() (AutoPotionSettings, bool) {
	if p == nil {
		return AutoPotionSettings{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.AutoPotionSettings, p.HasAutoPotionSettings
}

func (p *Player) GetJob() PlayerJob {
	if p == nil {
		return PlayerJob{Type: PlayerJobNone}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	job := p.Job
	if !job.Active() {
		job.Type = PlayerJobNone
		job.Level = 0
	}
	return job
}

func (p *Player) SetJob(job PlayerJob) {
	if p == nil {
		return
	}
	if !job.Active() {
		job = PlayerJob{Type: PlayerJobNone}
	}
	p.Mutex.Lock()
	p.Job = job
	p.Mutex.Unlock()
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

func (p *Player) CommitInventoryAndAvatar(inventory, avatar Inventory) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.Inventory = inventory.Snapshot()
	p.AvatarInventory = avatar.Snapshot()
	p.Mutex.Unlock()
}

func (p *Player) SetCOSItemState(slot byte, cos *COS, state COSItemState) bool {
	if p == nil || cos == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	item, exists := p.Inventory.Items[slot]
	if !exists || item.Data != uint32(cos.PersistentID) || !item.IsContainer() || item.TypeID3 != 1 {
		return false
	}
	snapshot := cos.Snapshot()
	item.COS = COSItemInfo{
		Present: true, RefObjectID: snapshot.RefObjectID, Kind: snapshot.Kind,
		State: state, Name: snapshot.Name, Level: snapshot.Level,
		RentEndsAt: snapshot.RentEndsAt, InventorySlot: snapshot.InventorySlot,
	}
	p.Inventory.Items[slot] = item
	return true
}

func (p *Player) ClearCOSItem(slot byte, cos *COS) bool {
	if p == nil || cos == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	item, exists := p.Inventory.Items[slot]
	if !exists || item.Data != uint32(cos.PersistentID) {
		return false
	}
	item.Data = 0
	item.COS = COSItemInfo{}
	p.Inventory.Items[slot] = item
	return true
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

// MarkQuestAborted updates the session-owned snapshot after the database
// compare-and-swap succeeds. Later bootstrap streams (including reconnect-like
// world reloads) therefore cannot resurrect the aborted quest.
func (p *Player) MarkQuestAborted(questID int) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for i := range p.Quests {
		if p.Quests[i].QuestID == questID {
			p.Quests[i].Status = QuestAborted
			return
		}
	}
}

// CommitQuestAbandon publishes the atomic event-8 result to the live player
// snapshot after persistence commits. Removed quest-item slots must disappear
// here as well as from SQL and the client, otherwise later inventory actions
// can operate on stale item identities.
func (p *Player) CommitQuestAbandon(questID int, result QuestAbandonResult) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for _, slot := range result.RemovedSlots {
		delete(p.Inventory.Items, slot)
	}
	for index := range p.Quests {
		if p.Quests[index].QuestID == questID {
			p.Quests[index].Status = QuestAborted
			p.Quests[index].AchievementCount = 0
			p.Quests[index].Data1 = 0
			p.Quests[index].Data2 = 0
		}
	}
	for _, relatedQuestID := range result.ResetQuestIDs {
		for index := range p.Quests {
			if p.Quests[index].QuestID == relatedQuestID && p.Quests[index].Status.ClientActive() {
				p.Quests[index].Status = QuestAborted
				p.Quests[index].AchievementCount = 0
				p.Quests[index].Data1 = 0
				p.Quests[index].Data2 = 0
			}
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

func (p *Player) CommitQuestActivationWithItems(result QuestActivationResult) {
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
	if result.InventorySize != 0 {
		p.InventorySize = result.InventorySize
	}
	if result.HwanChanged {
		p.HwanLevel = result.HwanLevel
	}
	if p.MaxLevel < p.Level {
		p.MaxLevel = p.Level
	}
	p.commitQuestLocked(result.Quest)
	p.Mutex.Unlock()
}

// CommitEventReward publishes the durable economic portion of the binary
// event-reward continuation. Event rewards do not implicitly retire a quest.
func (p *Player) CommitEventReward(result EventRewardResult) {
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
	if state == Dead {
		p.clearBerserkLocked()
	}
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

func (p *Player) GetHwanLevel() byte {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.HwanLevel
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
	if p.BodyState == Berserker {
		multiplier *= BerserkSpeedMultiplier
	}
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
	stats := p.DerivedStats()
	return stats.MaxHP, stats.MaxMP
}

func (p *Player) AverageAttackPower(magical bool) int {
	if p == nil {
		return 0
	}
	stats := p.DerivedStats()
	minimum, maximum := stats.PhyAttackMin, stats.PhyAttackMax
	if magical {
		minimum, maximum = stats.MagAttackMin, stats.MagAttackMax
	}
	power := (minimum + maximum) / 2
	if p.GetBodyState() == Berserker {
		power *= BerserkAttackMultiplier
	}
	return power
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

// CommitHP applies an authored skill HP debit without allowing the cast to
// kill its owner. The expected-value check mirrors the SQL repository's
// compare-and-debit predicate and prevents stale concurrent casts from
// consuming health twice.
func (p *Player) CommitHP(expectedHP, cost int) bool {
	if p == nil || expectedHP <= 0 || cost < 0 || expectedHP <= cost {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.BaseStats.HP != expectedHP || p.LifeState != Alive {
		return false
	}
	p.BaseStats.HP -= cost
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
	magic := equippedMagicOptionStats(p.Inventory, p.AvatarInventory)
	maxHP := utils.BaseHPOrMP(p.Level, p.BaseStats.Str+p.BonusStats.Str+magic.Strength) + p.BonusStats.HP + magic.HP
	maxMP := utils.BaseHPOrMP(p.Level, p.BaseStats.Int+p.BonusStats.Int+magic.Intellect) + p.BonusStats.MP + magic.MP
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
	if p.LifeState != Dead {
		return false
	}
	p.BaseStats.HP, p.BaseStats.MP = hp, mp
	p.LifeState = Alive
	return true
}

// CommitSkillResurrection publishes the already-committed database transition
// to both runtime aggregates under one lock order. The caster MP debit and the
// dead target revival therefore cannot become half-visible in memory.
func CommitSkillResurrection(caster, target *Player, expectedCasterMP, cost, hp, mp int) bool {
	if caster == nil || target == nil || caster == target || caster.ID <= 0 || target.ID <= 0 || caster.ID == target.ID || expectedCasterMP < cost || cost < 0 || hp <= 0 || mp < 0 {
		return false
	}
	first, second := caster, target
	if target.ID < caster.ID {
		first, second = target, caster
	}
	first.Mutex.Lock()
	second.Mutex.Lock()
	defer first.Mutex.Unlock()
	defer second.Mutex.Unlock()
	if caster.LifeState != Alive || caster.BaseStats.HP <= 0 || caster.BaseStats.MP != expectedCasterMP || target.LifeState != Dead || target.BaseStats.HP != 0 {
		return false
	}
	caster.BaseStats.MP -= cost
	target.BaseStats.HP, target.BaseStats.MP = hp, mp
	target.LifeState = Alive
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

// BuildMotionStateUpdatePacket emits entity state category 1, which the stock
// vSRO 1.188 client parses as MotionState (stand/walk/run/sit).
func BuildMotionStateUpdatePacket(uniqueID uint32, state MotionState) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateMovementState
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(1)
	// The stock parser uses 0 for stand/reset and 2/3/4 for walk/run/sit.
	// Internal locomotion is compact 0/1/2/3, so only active movement states
	// need the +1 conversion.
	wireState := byte(0)
	if state != Stand {
		wireState = byte(state) + 1
	}
	packet.WriteByte(wireState)
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
	stats := p.DerivedStats()

	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateStats
	packet.WriteUInt32(uint32(stats.PhyAttackMin))
	packet.WriteUInt32(uint32(stats.PhyAttackMax))
	packet.WriteUInt32(uint32(stats.MagAttackMin))
	packet.WriteUInt32(uint32(stats.MagAttackMax))
	packet.WriteUInt16(uint16(stats.PhyDef))
	packet.WriteUInt16(uint16(stats.MagDef))
	packet.WriteUInt16(uint16(stats.HitRate))
	packet.WriteUInt16(uint16(stats.ParryRate))
	packet.WriteUInt32(uint32(stats.MaxHP))
	packet.WriteUInt32(uint32(stats.MaxMP))
	packet.WriteUInt16(uint16(stats.Strength))
	packet.WriteUInt16(uint16(stats.Intellect))
	return packet
}

// DerivedStats combines base attributes, passive and active skill effects,
// item effects already represented by the bonus accumulators, and every
// equipped weapon/armor/shield stat from ITEMDATA and the item instance's
// variance/enhancement level.
func (p *Player) DerivedStats() DerivedCharacterStats {
	if p == nil {
		return DerivedCharacterStats{}
	}
	p.Mutex.Lock()
	level := p.Level
	stats := p.BaseStats
	baseAttack := p.BaseAttackStats
	baseDefense := p.BaseDefenseStats
	bonus := p.BonusStats
	attackBonus := p.BonusAttackStats
	defenseBonus := p.BonusDefenseStats
	inventory := p.Inventory.Snapshot()
	avatar := p.AvatarInventory.Snapshot()
	p.Mutex.Unlock()
	equipment := equippedItemStats(inventory)
	magic := equippedMagicOptionStats(inventory, avatar)
	effectiveStr, effectiveInt := stats.Str+bonus.Str+magic.Strength, stats.Int+bonus.Int+magic.Intellect
	maxHP := utils.BaseHPOrMP(level, effectiveStr) + bonus.HP + magic.HP
	maxMP := utils.BaseHPOrMP(level, effectiveInt) + bonus.MP + magic.MP
	maxHP += maxHP * bonus.HpPercent / 100
	maxMP += maxMP * bonus.MpPercent / 100
	return DerivedCharacterStats{
		Strength:     effectiveStr,
		Intellect:    effectiveInt,
		PhyAttackMin: applyStatPercent(utils.BaseMinAttack(effectiveStr)+baseAttack.PhyAttackMin+equipment.PhyAttackMin+attackBonus.PhyAttackMin, attackBonus.PhyAttackPercent),
		PhyAttackMax: applyStatPercent(utils.BaseMaxAttack(effectiveStr)+baseAttack.PhyAttackMax+equipment.PhyAttackMax+attackBonus.PhyAttackMax, attackBonus.PhyAttackPercent),
		MagAttackMin: applyStatPercent(utils.BaseMinAttack(effectiveInt)+baseAttack.MagAttackMin+equipment.MagAttackMin+attackBonus.MagAttackMin, attackBonus.MagAttackPercent),
		MagAttackMax: applyStatPercent(utils.BaseMaxAttack(effectiveInt)+baseAttack.MagAttackMax+equipment.MagAttackMax+attackBonus.MagAttackMax, attackBonus.MagAttackPercent),
		PhyDef:       applyStatPercent(utils.BaseDef(effectiveStr)+baseDefense.PhyDef+equipment.PhyDef+defenseBonus.PhyDef, defenseBonus.PhyDefPercent),
		MagDef:       applyStatPercent(utils.BaseDef(effectiveInt)+baseDefense.MagDef+equipment.MagDef+defenseBonus.MagDef, defenseBonus.MagDefPercent),
		HitRate:      applyStatPercent(11+baseAttack.HitRate+equipment.HitRate+attackBonus.HitRate+magic.HitRate, attackBonus.HitRatePercent),
		ParryRate:    applyStatPercent(11+baseDefense.ParryRate+equipment.ParryRate+defenseBonus.ParryRate+magic.ParryRate, defenseBonus.ParryRatePercent),
		CriticalRate: applyStatPercent(baseAttack.CriticalRate+equipment.CriticalRate+attackBonus.CriticalRate+magic.CriticalRate, attackBonus.CriticalRatePercent),
		BlockRate:    applyStatPercent(baseDefense.BlockRate+equipment.BlockRate+defenseBonus.BlockRate, defenseBonus.BlockRatePercent),
		PhyAbsorb:    equipment.PhyAbsorb,
		MagAbsorb:    equipment.MagAbsorb,
		MaxHP:        maxHP,
		MaxMP:        maxMP,
	}
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
func BuildBodyStateUpdatePacket(uniqueID uint32, state BodyState, berserkEffect ...byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateMovementState
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(4)
	packet.WriteByte(byte(state))
	// The stock vSRO 1.188 type-4 handler conditionally reads one more byte
	// when BodyState is Berserker. This is a boolean effect variant (red/blue),
	// not the numeric HWAN title level. Omitting it leaves the client parsing a
	// truncated packet and can crash it as soon as Berserk is activated.
	if state == Berserker {
		variant := byte(0)
		if len(berserkEffect) != 0 && berserkEffect[0] != 0 {
			variant = 1
		}
		packet.WriteByte(variant)
	}
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
	result := p.ApplyCombatDamage(damage, false)
	return result.AppliedDamage, result.Killed
}

func (p *Player) ApplyMagicalDamage(damage int) (applied int, killed bool) {
	result := p.ApplyCombatDamage(damage, true)
	return result.AppliedDamage, result.Killed
}

type PlayerDamageApplication struct {
	WireDamage       int
	AppliedDamage    int
	Killed           bool
	BarrierDamage    int
	BarrierRemaining int
	BarrierMaximum   int
	BarrierExhausted bool
}

// ApplyCombatDamage returns damage after absorption/barriers separately from
// HP loss, which is capped by the character's remaining HP.
func (p *Player) ApplyCombatDamage(damage int, magical bool) PlayerDamageApplication {
	if p == nil || damage <= 0 {
		return PlayerDamageApplication{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState != Alive || p.BaseStats.HP <= 0 {
		return PlayerDamageApplication{}
	}
	barrierDamage := 0
	barrier := ActiveBarrier{}
	if magical {
		damage, barrierDamage, barrier = p.absorbMagicalDamageLocked(damage)
	} else {
		damage, barrierDamage, barrier = p.absorbPhysicalDamageLocked(damage)
	}
	if damage <= 0 {
		return PlayerDamageApplication{BarrierDamage: barrierDamage, BarrierRemaining: barrier.HitPoints, BarrierMaximum: barrier.MaxHitPoints, BarrierExhausted: barrierDamage > 0 && barrier.HitPoints <= 0}
	}
	wireDamage := damage
	appliedDamage := damage
	if appliedDamage > p.BaseStats.HP {
		appliedDamage = p.BaseStats.HP
	}
	p.BaseStats.HP -= appliedDamage
	if p.BaseStats.HP == 0 {
		p.LifeState = Dead
		p.clearBerserkLocked()
		return PlayerDamageApplication{WireDamage: wireDamage, AppliedDamage: appliedDamage, Killed: true, BarrierDamage: barrierDamage, BarrierRemaining: barrier.HitPoints, BarrierMaximum: barrier.MaxHitPoints, BarrierExhausted: barrierDamage > 0 && barrier.HitPoints <= 0}
	}
	return PlayerDamageApplication{WireDamage: wireDamage, AppliedDamage: appliedDamage, BarrierDamage: barrierDamage, BarrierRemaining: barrier.HitPoints, BarrierMaximum: barrier.MaxHitPoints, BarrierExhausted: barrierDamage > 0 && barrier.HitPoints <= 0}
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
	packet := vsro188.BuildEntityEquipItem(p.UniqueID, slot, item.GetRefObjectID(), item.IsOneHandedWeapon())

	p.Session.Send(packet)
}

func (p *Player) SendUnequipItemPacket(item Item, slot byte) {
	packet := vsro188.BuildEntityUnequipItem(p.UniqueID, slot, item.GetRefObjectID())

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

func (p *Player) GetAvatarInventory() Inventory {
	if p == nil {
		return Inventory{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.AvatarInventory.Snapshot()
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
				_ = player.GetSession().TrySend(*packet)
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
	// Opcode zero is never a stock application message. Codec failures return
	// the zero Packet value; sending it makes the vSRO client deliberately
	// fault when its dispatcher tries to read the missing first payload byte.
	if p != nil && packet != nil && packet.MessageID != 0 && p.Session != nil && p.Session.Conn != nil {
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
	if p == nil {
		return PvpCapeNone
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.PVPCape
}

func (p *Player) SetPVPCape(cape PVPCape) bool {
	if p == nil || cape < PvpCapeNone || cape > PvpCapeGold {
		return false
	}
	p.Mutex.Lock()
	p.PVPCape = cape
	p.Mutex.Unlock()
	return true
}

func (p *Player) CommitPKCounters(state CharacterPKState) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.pkCounters = state
	p.Mutex.Unlock()
}

func (p *Player) PKCounters() CharacterPKState {
	if p == nil {
		return CharacterPKState{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.pkCounters
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
