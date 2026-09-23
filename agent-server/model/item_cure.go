package model

import (
	"sort"
	"time"
)

// ItemCureKind identifies the two direct player-cure branches recovered from
// SR_GameServer.exe. These are full item TypeIDs 3/3/2/1 and 3/3/2/6; COS
// TypeID4 7 and mall-bag TypeID4 8 remain separate families.
type ItemCureKind byte

const (
	ItemCureRandomStatus ItemCureKind = iota + 1
	ItemCureShortenElemental
)

// ItemCureEffect is the runtime descriptor returned only after the inventory
// transaction has committed consumption. ElementalPower retains all six
// authored values because the native callback applies a different duration
// multiplier to each low abnormal-state bucket.
type ItemCureEffect struct {
	Kind           ItemCureKind
	StatusMask     uint32
	StatusGrade    int
	Chance         int
	Count          int
	ElementalPower [6]uint16
}

// ItemCureApplication describes the committed runtime status mutation. A
// shortened status remains active until RefreshAt and needs a second aggregate
// 0x3057 publication at that time.
type ItemCureApplication struct {
	Changed   bool
	Removed   int
	RefreshAt []time.Time
}

var itemCureElementalKinds = [6]CombatStatusKind{
	CombatStatusFreeze,
	CombatStatusFrostbite,
	CombatStatusShock,
	CombatStatusBurn,
	CombatStatusPoison,
	CombatStatusZombie,
}

// SR_GameServer.exe 0x00410B40 maps low abnormal buckets 0..5 to these
// millisecond multipliers. 0x0049B710 loads Param1..Param6 for TypeID4 6 and
// 0x004A56C0 subtracts each mapped duration from the active status clock.
var itemCureElementalMilliseconds = [6]int64{97, 250, 750, 500, 1000, 750}

func isCharacterStatusCureType(ref RefItem) bool {
	return ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 2 && (ref.TypeID4 == 1 || ref.TypeID4 == 6)
}

// ItemCureFromReference decodes the two stock direct-cure parameter layouts.
// It deliberately rejects malformed rows instead of inventing an effect.
func ItemCureFromReference(ref RefItem) (ItemCureEffect, bool) {
	if !ref.IsUseable || !isCharacterStatusCureType(ref) {
		return ItemCureEffect{}, false
	}
	switch ref.TypeID4 {
	case 1:
		if ref.Param1 <= 0 || uint64(ref.Param1) > uint64(^uint32(0)) || ref.Param2 < 0 || ref.Param3 <= 0 {
			return ItemCureEffect{}, false
		}
		return ItemCureEffect{
			Kind: ItemCureRandomStatus, StatusMask: uint32(ref.Param1),
			StatusGrade: ref.Param2, Chance: ref.Param3, Count: 1,
		}, true
	case 6:
		values := [...]int{ref.Param1, ref.Param2, ref.Param3, ref.Param4, ref.Param5, ref.Param6}
		var power [6]uint16
		for index, value := range values {
			if value <= 0 || value > int(^uint16(0)) {
				return ItemCureEffect{}, false
			}
			power[index] = uint16(value)
		}
		return ItemCureEffect{Kind: ItemCureShortenElemental, ElementalPower: power}, true
	default:
		return ItemCureEffect{}, false
	}
}

func IsCOSCure(ref RefItem) bool {
	return ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 2 && ref.TypeID4 == 7
}

// COSItemCureFromReference decodes the stock growth-pet cure-all family
// (TypeID 3/3/2/7). Its six authored values use the same elemental duration
// reductions as the character TypeID4 6 callback.
func COSItemCureFromReference(ref RefItem) (ItemCureEffect, bool) {
	if !IsCOSCure(ref) {
		return ItemCureEffect{}, false
	}
	values := [...]int{ref.Param1, ref.Param2, ref.Param3, ref.Param4, ref.Param5, ref.Param6}
	var power [6]uint16
	for index, value := range values {
		if value <= 0 || value > int(^uint16(0)) {
			return ItemCureEffect{}, false
		}
		power[index] = uint16(value)
	}
	return ItemCureEffect{Kind: ItemCureShortenElemental, ElementalPower: power}, true
}

// ApplyItemCure mutates only in-memory combat status authority after the item
// transaction commits. randomIntn follows math/rand.Intn's [0,n) contract and
// is injected so selection/chance behavior stays deterministic in tests.
func (p *Player) ApplyItemCure(effect ItemCureEffect, now time.Time, randomIntn func(int) int) ItemCureApplication {
	if p == nil {
		return ItemCureApplication{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.ApplyItemCureLocked(effect, now, randomIntn)
}

// ApplyItemCureLocked is the transaction-commit form used while ItemUseService
// already owns Player.Mutex. Callers must hold that mutex. Keeping the status
// mutation inside the same locked section as the committed inventory snapshot
// prevents a later debuff or death from being mistaken for the item target.
func (p *Player) ApplyItemCureLocked(effect ItemCureEffect, now time.Time, randomIntn func(int) int) ItemCureApplication {
	if p == nil {
		return ItemCureApplication{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	if randomIntn == nil {
		randomIntn = func(int) int { return 0 }
	}

	switch effect.Kind {
	case ItemCureShortenElemental:
		return p.applyElementalItemCureLocked(effect, now)
	case ItemCureRandomStatus:
		return p.applyRandomItemCureLocked(effect, now, randomIntn)
	default:
		return ItemCureApplication{}
	}
}

func (p *Player) applyElementalItemCureLocked(effect ItemCureEffect, now time.Time) ItemCureApplication {
	application := ItemCureApplication{}
	for index, kind := range itemCureElementalKinds {
		status, active := p.combatStatuses[kind]
		if !active {
			continue
		}
		if !status.ExpiresAt.After(now) {
			delete(p.combatStatuses, kind)
			continue
		}
		power := effect.ElementalPower[index]
		if power == 0 {
			continue
		}
		reduction := time.Duration(int64(power)*itemCureElementalMilliseconds[index]) * time.Millisecond
		status.ExpiresAt = status.ExpiresAt.Add(-reduction)
		application.Changed = true
		if !status.ExpiresAt.After(now) {
			delete(p.combatStatuses, kind)
			application.Removed++
			continue
		}
		p.combatStatuses[kind] = status
		application.RefreshAt = append(application.RefreshAt, status.ExpiresAt)
	}
	sort.Slice(application.RefreshAt, func(i, j int) bool { return application.RefreshAt[i].Before(application.RefreshAt[j]) })
	return application
}

func (p *Player) applyRandomItemCureLocked(effect ItemCureEffect, now time.Time, randomIntn func(int) int) ItemCureApplication {
	if effect.StatusMask == 0 || effect.Count <= 0 || effect.Chance <= 0 {
		return ItemCureApplication{}
	}
	candidates := make([]CombatStatus, 0, len(combatBadEffectKinds))
	for _, kind := range combatBadEffectKinds {
		bit := CombatStatusBadEffectBit(kind)
		if bit == 0 || effect.StatusMask&bit == 0 {
			continue
		}
		status, active := p.combatStatuses[kind]
		if !active {
			continue
		}
		if !status.ExpiresAt.After(now) {
			delete(p.combatStatuses, kind)
			continue
		}
		candidates = append(candidates, status)
	}
	// SR_GameServer.exe 0x004A6560 performs the same forward Fisher-Yates
	// shuffle before 0x004A56C0 limits the candidate count.
	for index := 1; index < len(candidates); index++ {
		selected := randomIntn(index + 1)
		candidates[index], candidates[selected] = candidates[selected], candidates[index]
	}

	application := ItemCureApplication{}
	limit := min(effect.Count, len(candidates))
	for _, status := range candidates[:limit] {
		chance := effect.Chance
		if difference := status.Level - effect.StatusGrade; difference > 0 {
			chance /= difference
		}
		if !itemCureChanceSucceeds(chance, randomIntn) {
			continue
		}
		current, active := p.combatStatuses[status.Kind]
		if !active || current.Token != status.Token {
			continue
		}
		delete(p.combatStatuses, status.Kind)
		application.Changed = true
		application.Removed++
	}
	return application
}

// ApplyItemCure applies the COS cure-all descriptor to NPC/COS abnormal
// status authority after the item transaction commits.
func (n *NPC) ApplyItemCure(effect ItemCureEffect, now time.Time) ItemCureApplication {
	if n == nil || effect.Kind != ItemCureShortenElemental {
		return ItemCureApplication{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	application := ItemCureApplication{}
	restored := make([]CombatStatusKind, 0, len(itemCureElementalKinds))
	n.combat.mutex.Lock()
	for index, kind := range itemCureElementalKinds {
		statuses := n.combat.statuses[kind]
		if len(statuses) == 0 {
			continue
		}
		power := effect.ElementalPower[index]
		if power == 0 {
			continue
		}
		reduction := time.Duration(int64(power)*itemCureElementalMilliseconds[index]) * time.Millisecond
		for token, status := range statuses {
			if !status.ExpiresAt.After(now) {
				delete(statuses, token)
				continue
			}
			status.ExpiresAt = status.ExpiresAt.Add(-reduction)
			application.Changed = true
			if !status.ExpiresAt.After(now) {
				delete(statuses, token)
				application.Removed++
				continue
			}
			statuses[token] = status
			application.RefreshAt = append(application.RefreshAt, status.ExpiresAt)
		}
		if len(statuses) == 0 {
			delete(n.combat.statuses, kind)
			restored = append(restored, kind)
		}
	}
	n.combat.mutex.Unlock()
	for _, kind := range restored {
		n.restoreStatusMovementEffect(kind)
	}
	sort.Slice(application.RefreshAt, func(i, j int) bool { return application.RefreshAt[i].Before(application.RefreshAt[j]) })
	return application
}

func itemCureChanceSucceeds(chance int, randomIntn func(int) int) bool {
	if chance <= 0 {
		return false
	}
	if chance >= 100 {
		return true
	}
	// The native base roll in 0x00599CC0 is rand()%101 and succeeds when the
	// draw is less than or equal to the effective chance.
	return randomIntn(101) <= chance
}
