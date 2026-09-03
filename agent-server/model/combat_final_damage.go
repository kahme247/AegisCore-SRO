package model

const MaximumSRDamage = 0xFFFFFF

// CombatMitigatedDamage reproduces the common physical/magical mitigation
// slice in SR_GameServer 0x0040E450 and 0x0040E830. Callers supply the
// corresponding defense and absorption stat slots.
func CombatMitigatedDamage(varianceAttack, defense, absorption float32) float32 {
	damage := varianceAttack/(1+absorption/100) - defense
	if damage < 0 {
		return 0
	}
	return damage
}

// CombatLowDamageCorrection reproduces the extra rand()-driven branch shared
// by SR_GameServer's final physical and magical damage routines. The branch is
// entered only when the accumulated result is below five percent of the
// pre-mitigation variance attack. Its binary default scale is 10 percent.
func CombatLowDamageCorrection(damage, varianceAttack float32, roll int) float32 {
	if damage >= varianceAttack*0.05 {
		return damage
	}
	upper := damage * 10 / 100
	if upper <= 0 {
		upper = 1
	}
	if roll < 0 {
		roll = -roll
	}
	roll %= sroRandMax + 1
	normalized := float32(roll) / sroRandMax
	return 1 + (upper-1)*normalized
}

// CombatFinalDamageFactor applies one of SR_GameServer's final percentage
// accumulators (0xB2 through 0xB5). tisc supplies the target-side 0xB4/0xB5
// factor. The integer runtime has already truncated its preceding stages, so
// int64 preserves the equivalent multiply-before-divide ordering here.
func CombatFinalDamageFactor(damage, factor int) int {
	if damage <= 0 {
		return 0
	}
	result := int64(damage) * int64(100+factor) / 100
	if result <= 0 {
		return 0
	}
	if result > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(result)
}

// CombatDamageClamp applies the final range proven at the common return tail.
// The x87 conversion immediately following this clamp truncates toward zero.
func CombatDamageClamp(damage float32) int {
	if damage < 0 {
		return 0
	}
	if damage >= MaximumSRDamage {
		return MaximumSRDamage
	}
	return int(damage)
}
