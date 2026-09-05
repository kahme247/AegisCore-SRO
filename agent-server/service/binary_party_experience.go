package service

import "math"

// The values below are the pinned TestIn SR_GameServer constants used by the
// recovered award stage at 0x004EA6A0/0x004FCB00. The two base formula
// helpers, 0x00410110 and 0x004106B0, are deliberately inputs to this module:
// their C++ virtual data sources are not represented by the current NPC model.
const (
	BinaryPartyBaseScale       = 0.3
	BinaryPartyAdjustmentScale = 100.0
	BinaryPartyRateOffset      = 1.0
	BinaryPartyRateThreshold   = 1.01
)

// BinaryPartyExperiencePair is the signed EXP/SP pair used by the original
// award path before its final player virtual call (+0x170).
type BinaryPartyExperiencePair struct {
	Experience      int64
	SkillExperience int64
}

// BinaryPartyExperienceAdjustments contains values obtained through the
// player-state lookup at 0x004B3740. The first four fields correspond to the
// recovered offsets 0x100, 0x102, 0xBA, and 0xCA. C2Pair is the already
// materialized signed pair produced by the 0xC2 branch; keeping it as a pair
// avoids inventing a field interpretation for the packed legacy state.
type BinaryPartyExperienceAdjustments struct {
	Experience100      int64
	SkillExperience102 int64
	ExperienceBA       int64
	SkillExperienceCA  int64
	C2Pair             BinaryPartyExperiencePair
}

// BinaryPartyExperienceInput is the data boundary between the recovered base
// formula helpers and the recovered award arithmetic. Rates are binary global
// values (for example 1.0 means 100%), not integer percentages.
type BinaryPartyExperienceInput struct {
	Base BinaryPartyExperiencePair

	// AwardMultiplier is the positive param3 multiplier at 0x004EA6A0. A
	// non-positive value leaves the already scaled pair unchanged.
	AwardMultiplier float64

	Party          bool
	IndividualRate float64
	PartyRate      float64
	Flags          uint8
	Adjustments    BinaryPartyExperienceAdjustments

	// These are the two later optional floating-point globals at 0x00C82568
	// and 0x00C8256C. They are normally zero in the current compatibility
	// configuration, but remain explicit for binary replay tests.
	ExperienceGate      float64
	SkillExperienceGate float64
}

// BinaryPartyExperienceResult is the final non-negative award pair. The
// Intermediate fields make each recovered stage testable without coupling
// tests to Player or the combat handler.
type BinaryPartyExperienceResult struct {
	Award         BinaryPartyExperiencePair
	ScaledBase    BinaryPartyExperiencePair
	RateAddition  BinaryPartyExperiencePair
	StateAddition BinaryPartyExperiencePair
}

// BinaryPartyExperience applies the arithmetic recovered from the pinned
// executable. It intentionally does not perform eligibility, member walking,
// persistence, or packet emission; those are separate lifecycle boundaries.
func BinaryPartyExperience(input BinaryPartyExperienceInput) BinaryPartyExperienceResult {
	result := BinaryPartyExperienceResult{}
	base := BinaryPartyExperiencePair{
		Experience:      binaryRound(float64(input.Base.Experience) * BinaryPartyBaseScale),
		SkillExperience: binaryRound(float64(input.Base.SkillExperience) * BinaryPartyBaseScale),
	}
	if input.AwardMultiplier > 0 {
		// param3 is multiplied into the scaled floating-point values before
		// the final round helper is called by the binary.
		base.Experience = binaryRound(float64(input.Base.Experience) * BinaryPartyBaseScale * input.AwardMultiplier)
		base.SkillExperience = binaryRound(float64(input.Base.SkillExperience) * BinaryPartyBaseScale * input.AwardMultiplier)
	}
	result.ScaledBase = base

	rate := input.IndividualRate
	if input.Party {
		rate = input.PartyRate
	}
	if input.Flags&2 != 0 && rate < BinaryPartyRateThreshold {
		ratePair := BinaryPartyExperiencePair{
			Experience:      binaryRound(float64(base.Experience) * (rate - BinaryPartyRateOffset)),
			SkillExperience: binaryRound(float64(base.SkillExperience) * (rate - BinaryPartyRateOffset)),
		}
		result.RateAddition = clampBinaryPair(ratePair)
	}

	state := BinaryPartyExperiencePair{}
	if input.Adjustments.Experience100 > 0 || input.Adjustments.SkillExperience102 > 0 ||
		input.Adjustments.ExperienceBA > 0 || input.Adjustments.SkillExperienceCA > 0 {
		state.Experience = binaryRound(float64(base.Experience) * float64(input.Adjustments.Experience100+input.Adjustments.ExperienceBA) / BinaryPartyAdjustmentScale)
		state.SkillExperience = binaryRound(float64(base.SkillExperience) * float64(input.Adjustments.SkillExperience102+input.Adjustments.SkillExperienceCA) / BinaryPartyAdjustmentScale)
	}
	// The executable clamps each intermediate pair before adding the C2
	// contribution; it does not clamp the combined sum as one value.
	state = clampBinaryPair(state)
	c2 := clampBinaryPair(input.Adjustments.C2Pair)
	result.StateAddition = BinaryPartyExperiencePair{
		Experience:      state.Experience + c2.Experience,
		SkillExperience: state.SkillExperience + c2.SkillExperience,
	}

	award := clampBinaryPair(BinaryPartyExperiencePair{
		Experience:      base.Experience + result.RateAddition.Experience + result.StateAddition.Experience,
		SkillExperience: base.SkillExperience + result.RateAddition.SkillExperience + result.StateAddition.SkillExperience,
	})
	if input.ExperienceGate > 0 {
		award.Experience = clampBinaryNonNegative(award.Experience + binaryRound(float64(award.Experience)*input.ExperienceGate))
	}
	if input.SkillExperienceGate > 0 {
		award.SkillExperience = clampBinaryNonNegative(award.SkillExperience + binaryRound(float64(award.SkillExperience)*input.SkillExperienceGate))
	}
	result.Award = award
	return result
}

// BinaryRateFromPercent converts the maintained SQL rate representation to the
// normalized binary global representation used by BinaryPartyExperience.
func BinaryRateFromPercent(percent uint32) float64 {
	return float64(percent) / BinaryPartyAdjustmentScale
}

func binaryRound(value float64) int64 {
	if math.IsNaN(value) {
		return 0
	}
	if value >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	if value <= float64(math.MinInt64) {
		return math.MinInt64
	}
	return int64(math.Round(value))
}

func clampBinaryNonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func clampBinaryPair(pair BinaryPartyExperiencePair) BinaryPartyExperiencePair {
	pair.Experience = clampBinaryNonNegative(pair.Experience)
	pair.SkillExperience = clampBinaryNonNegative(pair.SkillExperience)
	return pair
}
