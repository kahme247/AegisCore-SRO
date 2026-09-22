package model

import (
	"sort"
	"strings"
	"time"
)

type ActiveBuff struct {
	SkillID     uint32
	Token       uint32
	SourceUID   uint32
	ExpiresAt   time.Time
	Modifiers   BuffModifiers
	UseCount    int
	ParentToken uint32
	RecoveryHP  int
	RecoveryMP  int
}

func (p *Player) ConfigureActiveBuffRecovery(token uint32, recoveryHP, recoveryMP int) bool {
	if p == nil || token == 0 || recoveryHP < 0 || recoveryMP < 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	buff, exists := p.ActiveBuffs[token]
	if !exists {
		return false
	}
	buff.RecoveryHP = recoveryHP
	buff.RecoveryMP = recoveryMP
	p.ActiveBuffs[token] = buff
	return true
}

// CommitActiveBuffUse advances a subordinate effect's authored use counter.
// A nonzero limit tears down the owning buff exactly when the limit is met.
func (p *Player) CommitActiveBuffUse(token uint32, limit int) (removed, ok bool) {
	if p == nil || token == 0 {
		return false, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	buff, exists := p.ActiveBuffs[token]
	if !exists {
		return false, false
	}
	buff.UseCount++
	if limit > 0 && buff.UseCount >= limit {
		p.applyBuffModifiersLocked(buff.Modifiers, -1)
		delete(p.ActiveBuffs, token)
		delete(p.activeRecovery, token)
		return true, true
	}
	p.ActiveBuffs[token] = buff
	return false, true
}

func (p *Player) ActiveBuffSnapshot() []ActiveBuff {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	buffs := make([]ActiveBuff, 0, len(p.ActiveBuffs))
	for _, buff := range p.ActiveBuffs {
		buffs = append(buffs, buff)
	}
	return buffs
}

// RegisterActiveRecoveryStatus attaches a binary `real` descriptor to an
// active buff actor. The descriptor is data-only until actor-specific native
// recovery evaluation is integrated.
func (p *Player) RegisterActiveRecoveryStatus(token uint32, descriptor RecoveryStatusDescriptor) {
	if p == nil || token == 0 || descriptor.Mask == 0 {
		return
	}
	p.Mutex.Lock()
	if p.activeRecovery == nil {
		p.activeRecovery = make(map[uint32]RecoveryStatusDescriptor)
	}
	p.activeRecovery[token] = descriptor
	p.Mutex.Unlock()
}

// RecoveryStatusSnapshot combines enabled passive and active-effect `real`
// descriptors for the owning actor.
func (p *Player) RecoveryStatusSnapshot() []RecoveryStatusDescriptor {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	result := append([]RecoveryStatusDescriptor(nil), p.PassiveRecovery...)
	for _, descriptor := range p.activeRecovery {
		result = append(result, descriptor)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Grade != result[j].Grade {
			return result[i].Grade < result[j].Grade
		}
		if result[i].Value != result[j].Value {
			return result[i].Value < result[j].Value
		}
		return result[i].SkillID < result[j].SkillID
	})
	return result
}

// RecoveryStatusForMask mirrors the native grade-indexed lookup boundary. It
// selects the highest-grade overlapping descriptor, with value and skill ID as
// deterministic tie-breakers; it does not apply a resistance formula.
func (p *Player) RecoveryStatusForMask(mask uint32) (RecoveryStatusDescriptor, bool) {
	if p == nil || mask == 0 {
		return RecoveryStatusDescriptor{}, false
	}
	best, found := RecoveryStatusDescriptor{}, false
	for _, descriptor := range p.RecoveryStatusSnapshot() {
		if descriptor.Mask&mask == 0 {
			continue
		}
		if !found || descriptor.Grade > best.Grade || descriptor.Grade == best.Grade && (descriptor.Value > best.Value || descriptor.Value == best.Value && descriptor.SkillID > best.SkillID) {
			best, found = descriptor, true
		}
	}
	return best, found
}

// HiddenBodyState derives the native hide actor's body-state projection. The
// GameServer maps hide type 1 to body state 6 and type 2 to body state 7.
// The strongest active hide descriptor wins so replacing or removing one buff
// cannot accidentally expose a player that still owns another hide actor.
func (p *Player) HiddenBodyState(catalog *ReferenceDataCatalog, now time.Time) (BodyState, int, bool) {
	if p == nil || catalog == nil {
		return Normal, 0, false
	}
	state, level := Normal, 0
	for _, buff := range p.ActiveBuffSnapshot() {
		if !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		skill, exists := catalog.Skill(buff.SkillID)
		if !exists || skill.HideLevel <= 0 || (skill.HideType != 1 && skill.HideType != 2) {
			continue
		}
		candidate := Stealth
		if skill.HideType == 2 {
			candidate = Invisible
		}
		if skill.HideLevel > level || skill.HideLevel == level && candidate == Invisible {
			state, level = candidate, skill.HideLevel
		}
	}
	return state, level, level > 0
}

// DetectsHidden reproduces the visibility predicate in SR_GameServer
// 0x0059E130. Hide and detector type words are masks, not enum values: at
// least one bit must overlap and the detector level must cover the hide level.
func (p *Player) DetectsHidden(catalog *ReferenceDataCatalog, hideMask, hideLevel int, now time.Time) bool {
	if p == nil || catalog == nil || hideMask <= 0 || hideLevel <= 0 {
		return false
	}
	for _, buff := range p.ActiveBuffSnapshot() {
		if !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		skill, exists := catalog.Skill(buff.SkillID)
		if !exists {
			continue
		}
		mask, level := skill.DetectionType, skill.DetectionLevel
		// The attached detector actor uses dttp at native reference +0x424.
		// Catalog family 6 is exclusively Detect/Descry in the pinned old DB.
		if mask == 0 && skill.DamageTypeFamily == 6 {
			mask, level = skill.DamageTypeFamily, skill.DamageTypePrecedence
		}
		if mask&hideMask == 0 || level < hideLevel {
			continue
		}
		return true
	}
	return false
}

func (p *Player) HasAggroSuppression(catalog *ReferenceDataCatalog) bool {
	if p == nil || catalog == nil {
		return false
	}
	for _, buff := range p.ActiveBuffSnapshot() {
		skill, exists := catalog.Skill(buff.SkillID)
		if exists && skill.AggroSuppression {
			return true
		}
	}
	return false
}

func (p *Player) CommitActiveBuff(skillID, token uint32, duration time.Duration, modifiers BuffModifiers) (replacedToken uint32, ok bool) {
	return p.CommitActiveBuffAt(skillID, token, p.GetUniqueID(), duration, modifiers, time.Now())
}

func (p *Player) CommitActiveBuffFrom(skillID, token, sourceUID uint32, duration time.Duration, modifiers BuffModifiers) (replacedToken uint32, ok bool) {
	return p.CommitActiveBuffAt(skillID, token, sourceUID, duration, modifiers, time.Now())
}

func (p *Player) CommitLinkedActiveBuff(skillID, token, sourceUID, parentToken uint32, duration time.Duration, modifiers BuffModifiers) (replacedToken uint32, ok bool) {
	return p.CommitLinkedActiveBuffAt(skillID, token, sourceUID, parentToken, duration, modifiers, time.Now())
}

// CommitActiveBuffAt is the timestamped form used by authoritative execution
// paths. Keeping the timestamp explicit prevents an injected combat clock from
// disagreeing with the buff's expiry and visibility projections.
func (p *Player) CommitActiveBuffAt(skillID, token, sourceUID uint32, duration time.Duration, modifiers BuffModifiers, now time.Time) (replacedToken uint32, ok bool) {
	return p.CommitLinkedActiveBuffAt(skillID, token, sourceUID, 0, duration, modifiers, now)
}

// CommitLinkedActiveBuffAt commits a linked buff using the supplied execution
// timestamp. The legacy non-timestamped wrapper remains wall-clock based for
// callers outside the combat lifecycle.
func (p *Player) CommitLinkedActiveBuffAt(skillID, token, sourceUID, parentToken uint32, duration time.Duration, modifiers BuffModifiers, now time.Time) (replacedToken uint32, ok bool) {
	if p == nil || skillID == 0 || token == 0 || duration < 0 {
		return 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.ActiveBuffs == nil {
		p.ActiveBuffs = make(map[uint32]ActiveBuff)
	}
	for existingToken, buff := range p.ActiveBuffs {
		if buff.SkillID == skillID {
			p.applyBuffModifiersLocked(buff.Modifiers, -1)
			delete(p.ActiveBuffs, existingToken)
			delete(p.activeRecovery, existingToken)
			replacedToken = existingToken
			break
		}
	}
	p.applyBuffModifiersLocked(modifiers, 1)
	expiresAt := time.Time{}
	if duration > 0 {
		expiresAt = now.Add(duration)
	}
	p.ActiveBuffs[token] = ActiveBuff{SkillID: skillID, Token: token, SourceUID: sourceUID, ParentToken: parentToken, ExpiresAt: expiresAt, Modifiers: modifiers}
	return replacedToken, true
}

// ActiveGoldDropAmountPercent returns the additive gold-amount bonus carried
// by live item buffs. Expired buffs are ignored even if their scheduled
// teardown has not run yet.
func (p *Player) ActiveGoldDropAmountPercent(now time.Time) int {
	if p == nil {
		return 0
	}
	total := 0
	for _, buff := range p.ActiveBuffSnapshot() {
		if !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		if buff.Modifiers.GoldDropAmountPercent > 0 {
			total += buff.Modifiers.GoldDropAmountPercent
		}
	}
	return total
}

func (p *Player) AggroAbsorber(catalog *ReferenceDataCatalog) (uint32, int, bool) {
	if p == nil || catalog == nil {
		return 0, 0, false
	}
	now := time.Now()
	for _, buff := range p.ActiveBuffSnapshot() {
		skill, exists := catalog.Skill(buff.SkillID)
		if exists && buff.SourceUID != 0 && buff.SourceUID != p.GetUniqueID() && (buff.ExpiresAt.IsZero() || buff.ExpiresAt.After(now)) && skill.AggroAbsorbPercent > 0 {
			return buff.SourceUID, min(skill.AggroAbsorbPercent, 100), true
		}
	}
	return 0, 0, false
}

func (p *Player) RemoveActiveBuff(token uint32) bool {
	if p == nil || token == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	buff, exists := p.ActiveBuffs[token]
	if !exists {
		return false
	}
	p.applyBuffModifiersLocked(buff.Modifiers, -1)
	delete(p.ActiveBuffs, token)
	delete(p.activeRecovery, token)
	return true
}

func (p *Player) RemoveActiveBuffBySkill(skillID uint32) (uint32, bool) {
	if p == nil || skillID == 0 {
		return 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for token, buff := range p.ActiveBuffs {
		if buff.SkillID != skillID {
			continue
		}
		p.applyBuffModifiersLocked(buff.Modifiers, -1)
		delete(p.ActiveBuffs, token)
		delete(p.activeRecovery, token)
		return token, true
	}
	return 0, false
}

// RemoveInvalidFireShieldBuffs implements SR_GameServer's one named
// conditional-effect teardown exception. Fire Shield requires an equipped
// shield (`reqi` 4/1); unlike ordinary conditional effects, losing that
// requirement destroys the active actor instead of merely deactivating it.
func (p *Player) RemoveInvalidFireShieldBuffs(catalog *ReferenceDataCatalog) []uint32 {
	if p == nil || catalog == nil {
		return nil
	}
	inventory := p.GetInventory()
	avatar := p.GetAvatarInventory()
	playerUID := p.GetUniqueID()
	removed := make([]uint32, 0, 1)
	for _, buff := range p.ActiveBuffSnapshot() {
		skill, exists := catalog.Skill(buff.SkillID)
		if !exists || buff.SourceUID != playerUID || !strings.HasPrefix(skill.CodeName, "SKILL_CH_FIRE_SHIELD_") || len(skill.RequiredItems) == 0 || passiveSkillItemRequirementMet(skill, inventory, avatar) {
			continue
		}
		if p.RemoveActiveBuff(buff.Token) {
			removed = append(removed, buff.Token)
		}
	}
	return removed
}

func (p *Player) applyBuffModifiersLocked(modifiers BuffModifiers, direction int) {
	p.BonusStats.HP += direction * modifiers.HP
	p.BonusStats.HpPercent += direction * modifiers.HPPercent
	p.BonusStats.MP += direction * modifiers.MP
	p.BonusStats.MpPercent += direction * modifiers.MPPercent
	p.BonusStats.Str += direction * modifiers.Strength
	p.BonusStats.Int += direction * modifiers.Intellect
	p.BonusAttackStats.HitRate += direction * modifiers.HitRate
	p.BonusAttackStats.HitRatePercent += direction * modifiers.HitRatePercent
	p.BonusAttackStats.PhyAttackMin += direction * modifiers.PhysicalAttack
	p.BonusAttackStats.PhyAttackMax += direction * modifiers.PhysicalAttack
	p.BonusAttackStats.MagAttackMin += direction * modifiers.MagicalAttack
	p.BonusAttackStats.MagAttackMax += direction * modifiers.MagicalAttack
	p.BonusAttackStats.CriticalRate += direction * modifiers.CriticalRate
	p.buffMovementSpeedPercent += direction * modifiers.MovementSpeedPercent
	p.CombatDamagePercent += direction * modifiers.DamagePercent
	p.PhyAbsorbPercent += direction * modifiers.DamageAbsorptionPercent
	p.PhyAbsorbPercent += direction * modifiers.PhysicalDamageAbsorptionPercent
	p.MagAbsorbPercent += direction * modifiers.MagicalDamageAbsorptionPercent
	p.MagAbsorbPercent += direction * modifiers.DamageAbsorptionPercent
	p.BonusDefenseStats.PhyDef += direction * modifiers.PhysicalDefense
	p.BonusDefenseStats.MagDef += direction * modifiers.MagicalDefense
	p.BonusDefenseStats.BlockRate += direction * modifiers.BlockRate
	p.BonusDefenseStats.ParryRate += direction * modifiers.ParryRate
	p.BonusDefenseStats.ParryRatePercent += direction * modifiers.ParryRatePercent
}

func (p *Player) RecalculatePassiveSkillModifiers(catalog *ReferenceDataCatalog) {
	if p == nil || catalog == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.applyBuffModifiersLocked(p.PassiveSkillModifiers, -1)
	p.PassiveSkillModifiers = BuffModifiers{}
	p.PassiveRecovery = nil
	p.passiveSkillRangeBonus = 0
	p.passiveCriticalDefense = 0
	p.passiveHealingPercent = 0
	p.passiveBlessingPercent = 0
	p.passiveStatusDurationPercent = [6]int{}
	inventory := p.Inventory.Snapshot()
	avatar := p.AvatarInventory.Snapshot()
	for skillID, enabled := range p.Skills {
		skill, exists := catalog.Skill(skillID)
		if enabled != 1 || !exists || !skill.IsPassive() || !passiveSkillItemRequirementMet(skill, inventory, avatar) {
			continue
		}
		p.PassiveSkillModifiers = addBuffModifiers(p.PassiveSkillModifiers, skill.BuffModifiers)
		p.passiveSkillRangeBonus += skill.PassiveSkillRangeBonus
		p.passiveCriticalDefense += skill.PassiveCriticalDefense
		p.passiveHealingPercent += skill.PassiveHealingPercent
		p.passiveBlessingPercent += skill.PassiveBlessingPercent
		for bit := uint32(1); bit <= 1<<5; bit <<= 1 {
			if skill.PassiveStatusDurationMask&bit != 0 {
				p.passiveStatusDurationPercent[statusDurationResistanceIndex(bit)] += skill.PassiveStatusDurationPercent
			}
		}
		if skill.RecoveryStatusMask != 0 {
			p.PassiveRecovery = append(p.PassiveRecovery, RecoveryStatusDescriptor{
				SkillID: skill.ID, Mask: skill.RecoveryStatusMask,
				Value: skill.RecoveryStatusValue, Grade: skill.RecoveryStatusGrade,
			})
		}
	}
	p.applyBuffModifiersLocked(p.PassiveSkillModifiers, 1)
}

// PassiveRecoveryStatusSnapshot returns the currently enabled passive `real`
// descriptors. Registration is exposed for future actor-specific abnormal
// recovery evaluation without applying an unproven resistance formula here.
func (p *Player) PassiveRecoveryStatusSnapshot() []RecoveryStatusDescriptor {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return append([]RecoveryStatusDescriptor(nil), p.PassiveRecovery...)
}

func passiveSkillItemRequirementMet(skill RefSkill, inventory, avatar Inventory) bool {
	if len(skill.RequiredItems) == 0 {
		return true
	}
	for _, slot := range []byte{SlotPrimaryWeapon, SlotSecondaryWeapon} {
		item, equipped := inventory.Items[slot]
		if !equipped {
			continue
		}
		for _, required := range skill.RequiredItems {
			typeInfo := item.GetTypeInfo()
			if typeInfo.TypeID3 == required.TypeID3 && typeInfo.TypeID4 == required.TypeID4 {
				return true
			}
		}
	}
	if devil, equipped := avatar.Items[4]; equipped && devil.IsActiveDevilSpirit(time.Now()) {
		for _, required := range skill.RequiredItems {
			typeInfo := devil.GetTypeInfo()
			if typeInfo.TypeID3 == required.TypeID3 && typeInfo.TypeID4 == required.TypeID4 {
				return true
			}
		}
	}
	return false
}

func addBuffModifiers(left, right BuffModifiers) BuffModifiers {
	left.HP += right.HP
	left.HPPercent += right.HPPercent
	left.MP += right.MP
	left.MPPercent += right.MPPercent
	left.Strength += right.Strength
	left.Intellect += right.Intellect
	left.HitRate += right.HitRate
	left.HitRatePercent += right.HitRatePercent
	left.DamagePercent += right.DamagePercent
	left.PhysicalAttack += right.PhysicalAttack
	left.MagicalAttack += right.MagicalAttack
	left.CriticalRate += right.CriticalRate
	left.MovementSpeedPercent += right.MovementSpeedPercent
	left.PhysicalDamageAbsorptionPercent += right.PhysicalDamageAbsorptionPercent
	left.MagicalDamageAbsorptionPercent += right.MagicalDamageAbsorptionPercent
	left.PhysicalDefense += right.PhysicalDefense
	left.MagicalDefense += right.MagicalDefense

	left.BlockRate += right.BlockRate
	left.ParryRate += right.ParryRate
	left.ParryRatePercent += right.ParryRatePercent
	return left
}

func (p *Player) PassiveSkillRangeBonus() int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.passiveSkillRangeBonus
}

func (p *Player) PassiveCriticalDefense() int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.passiveCriticalDefense
}

func (p *Player) PassiveHealingPercent() int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.passiveHealingPercent
}

func (p *Player) PassiveBlessingPercent() int {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.passiveBlessingPercent
}

func (p *Player) CombatStatusDurationMS(kind CombatStatusKind, durationMS int) int {
	if p == nil || durationMS <= 0 {
		return durationMS
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	index := statusDurationResistanceIndex(CombatStatusBadEffectBit(kind))
	if index < 0 {
		return durationMS
	}
	resistance := min(max(p.passiveStatusDurationPercent[index], 0), 100)
	return durationMS * (100 - resistance) / 100
}

func statusDurationResistanceIndex(bit uint32) int {
	switch bit {
	case 1:
		return 0 // Freeze
	case 2:
		return 1 // Frostbite
	case 4:
		return 2 // Shock
	case 8:
		return 3 // Burn
	case 16:
		return 4 // Poison
	case 32:
		return 5 // Zombie
	default:
		return -1
	}
}
