package model

const sroRandMax = 32767

// CombatVarianceDamage reproduces SR_GameServer's attack/parry damage-variance
// core recovered at 0x0040DCE0 and 0x0040FDD0. Rolls are C rand() results:
// three choose the deviation and the fourth chooses its sign.
func CombatVarianceDamage(minimum, maximum, attackRate, parryRate, attackerLevel, targetLevel int, rolls [4]int) int {
	if minimum < 0 {
		minimum = 0
	}
	if maximum < minimum {
		maximum = minimum
	}
	center := 0.9
	if parryRate > 0 {
		center = float64(max(attackRate, 0)) / float64(parryRate) * 0.5
	}
	levelBonus := float64(attackerLevel-targetLevel) * 0.03
	if levelBonus < 0 {
		levelBonus = 0
	} else if levelBonus > 0.3 {
		levelBonus = 0.3
	}
	center += levelBonus
	if center < 0.1 {
		center = 0.1
	} else if center > 0.9 {
		center = 0.9
	}

	deviation := 100.0
	for index := 0; index < 3; index++ {
		roll := rolls[index]
		if roll < 0 {
			roll = -roll
		}
		roll %= sroRandMax + 1
		value := float64(roll) / sroRandMax
		if value < deviation {
			deviation = value
		}
	}
	point := center
	if rolls[3]&1 == 0 {
		point -= deviation
	} else {
		point += deviation
	}
	if point < 0 {
		point = 0
	} else if point > 1 {
		point = 1
	}
	return int(float64(minimum) + point*float64(maximum-minimum))
}
