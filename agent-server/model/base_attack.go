package model

const unarmedBaseAttackSkillID uint32 = 1

const (
	baseAttackReach          = float32(5)
	minimumRangedAttackReach = float32(50)
)

// BaseAttackSkillID returns the client reference skill used to animate a
// normal attack for the currently equipped weapon. These IDs are the
// SKILL_*_BASE_01 rows from the pinned vSRO 1.88 Media skill catalog.
func (p *Player) BaseAttackSkillID() uint32 {
	if p == nil {
		return unarmedBaseAttackSkillID
	}
	weapon, equipped := p.GetInventory().Items[SlotPrimaryWeapon]
	if !equipped || !weapon.GetTypeInfo().IsWeapon() {
		return unarmedBaseAttackSkillID
	}

	switch weapon.GetTypeInfo().TypeID4 {
	case 2, 3: // Chinese sword or blade.
		return 2
	case 4, 5: // Chinese spear or glaive.
		return 40
	case 6: // Chinese bow.
		return 70
	case 7: // European one-handed sword.
		return 8419
	case 8: // European two-handed sword.
		return 8420
	case 9: // European dual axe.
		return 8421
	case 10: // Warlock rod.
		return 10625
	case 11: // Wizard staff.
		return 9944
	case 12: // Crossbow.
		return 9354
	case 13: // Dagger.
		return 9355
	case 14: // Harp.
		return 11162
	case 15: // Cleric rod.
		return 11526
	default:
		return unarmedBaseAttackSkillID
	}
}

func (p *Player) BaseAttackHitCount() int {
	if p == nil {
		return 1
	}
	weapon, equipped := p.GetInventory().Items[SlotPrimaryWeapon]
	if equipped && weapon.GetTypeInfo().IsWeapon() && (weapon.GetTypeInfo().TypeID4 == 2 || weapon.GetTypeInfo().TypeID4 == 3) {
		return 2
	}
	return 1
}

// BaseAttackRange mirrors the original SkillRange calculation for normal
// attacks: five world units plus the equipped weapon's database attack
// distance. Magical weapon attacks retain the original 50-unit minimum;
// bows and crossbows already carry their longer distance in ITEMDATA.
func (p *Player) BaseAttackRange() float32 {
	if p == nil {
		return baseAttackReach
	}
	weapon, equipped := p.GetInventory().Items[SlotPrimaryWeapon]
	if !equipped || !weapon.GetTypeInfo().IsWeapon() {
		return baseAttackReach
	}

	reach := baseAttackReach
	if reference, exists := RefItems[weapon.RefObjectID]; exists && reference.Range > 0 {
		reach += float32(reference.Range)
	}

	switch weapon.GetTypeInfo().TypeID4 {
	case 10, 11, 14, 15: // Warlock, wizard, bard, and cleric magical attacks.
		if reach < minimumRangedAttackReach {
			return minimumRangedAttackReach
		}
	}
	return reach
}
