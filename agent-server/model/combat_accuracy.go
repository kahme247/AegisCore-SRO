package model

// CombatHitChance converts the authored hit/parry rates into a bounded hit
// probability. Equal rates produce 90%, while the relative difference between
// the two rates can move the result within a 70-98% gameplay-friendly range.
// Incomplete legacy rows retain the compatibility behavior of always hitting.
func CombatHitChance(hitRate, parryRate int) int {
	if hitRate <= 0 && parryRate <= 0 {
		return 100
	}
	hitRate = max(hitRate, 0)
	parryRate = max(parryRate, 0)
	total := int64(hitRate) + int64(parryRate)
	chance := 90 + int(20*(int64(hitRate)-int64(parryRate))/total)
	if chance < 70 {
		return 70
	}
	if chance > 98 {
		return 98
	}
	return chance
}
