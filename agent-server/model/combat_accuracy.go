package model

// CombatHitChance converts the authored hit/parry rates into a bounded hit
// probability. Equal rates produce 50%; neither side can become absolutely
// certain while both rates are present. Incomplete legacy rows retain the
// compatibility behavior of always hitting.
func CombatHitChance(hitRate, parryRate int) int {
	if hitRate <= 0 && parryRate <= 0 {
		return 100
	}
	if hitRate <= 0 {
		return 5
	}
	if parryRate <= 0 {
		return 95
	}
	chance := hitRate * 100 / (hitRate + parryRate)
	if chance < 5 {
		return 5
	}
	if chance > 95 {
		return 95
	}
	return chance
}
