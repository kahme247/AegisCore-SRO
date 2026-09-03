package model

import "math"

// CombatThreat reproduces SR_GameServer's 2tnt threat arithmetic recovered at
// 0x0058E5F0: round(base * (1 + percent/100)) + flat.
func CombatThreat(base, flat, percent int) int {
	threat := int(math.RoundToEven(float64(base)*(1+float64(percent)/100))) + flat
	return max(0, threat)
}
