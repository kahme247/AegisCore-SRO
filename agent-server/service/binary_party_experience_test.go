package service

import "testing"

func TestBinaryPartyExperienceScalesAndRoundsBasePair(t *testing.T) {
	got := BinaryPartyExperience(BinaryPartyExperienceInput{
		Base:            BinaryPartyExperiencePair{Experience: 1001, SkillExperience: 399},
		AwardMultiplier: 2,
	})
	if got.ScaledBase.Experience != 601 || got.ScaledBase.SkillExperience != 239 {
		t.Fatalf("scaled base = %+v, want EXP 601/SP 239", got.ScaledBase)
	}
	if got.Award != got.ScaledBase {
		t.Fatalf("award = %+v, want scaled base with no adjustments", got.Award)
	}
}

func TestBinaryPartyExperienceUsesPartyRateOnlyInParty(t *testing.T) {
	input := BinaryPartyExperienceInput{
		Base:           BinaryPartyExperiencePair{Experience: 1000, SkillExperience: 1000},
		Flags:          2,
		IndividualRate: 0.9,
		PartyRate:      1.1,
	}
	individual := BinaryPartyExperience(input)
	party := BinaryPartyExperience(input)
	partyInput := input
	partyInput.Party = true
	party = BinaryPartyExperience(partyInput)
	if individual.RateAddition.Experience != 0 {
		t.Fatalf("individual rate addition = %+v, want zero after negative clamp", individual.RateAddition)
	}
	if party.RateAddition.Experience != 0 {
		t.Fatalf("party rate addition = %+v, want no threshold adjustment above 1.01", party.RateAddition)
	}
	if individual.Award != party.Award {
		t.Fatalf("party/individual awards differ unexpectedly: individual=%+v party=%+v", individual.Award, party.Award)
	}
}

func TestBinaryPartyExperienceCombinesStateOffsetsAndClampsNegativeValues(t *testing.T) {
	got := BinaryPartyExperience(BinaryPartyExperienceInput{
		Base: BinaryPartyExperiencePair{Experience: 1000, SkillExperience: 1000},
		Adjustments: BinaryPartyExperienceAdjustments{
			Experience100:      20,
			ExperienceBA:       10,
			SkillExperience102: 30,
			SkillExperienceCA:  10,
			C2Pair:             BinaryPartyExperiencePair{Experience: -100, SkillExperience: -400},
		},
	})
	if got.StateAddition.Experience != 90 || got.StateAddition.SkillExperience != 120 {
		t.Fatalf("state addition = %+v, want independently clamped offsets 90/120", got.StateAddition)
	}
	if got.Award.Experience != 390 || got.Award.SkillExperience != 420 {
		t.Fatalf("award = %+v, want scaled base plus offsets 390/420", got.Award)
	}
}

func TestBinaryRateFromPercent(t *testing.T) {
	if got := BinaryRateFromPercent(350); got != 3.5 {
		t.Fatalf("normalized rate = %v, want 3.5", got)
	}
}
