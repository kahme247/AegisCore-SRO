package character

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestPartyExperienceRateUsesDedicatedBinarySetting(t *testing.T) {
	if got := partyExperienceRate(model.GameplayRates{
		ExperienceRatePercent:      200,
		PartyExperienceRatePercent: 350,
	}); got != 350 {
		t.Fatalf("party EXP rate = %d, want dedicated rate 350", got)
	}
}

func TestPartyExperienceRateFallsBackForLegacyProviders(t *testing.T) {
	if got := partyExperienceRate(model.GameplayRates{ExperienceRatePercent: 200}); got != 200 {
		t.Fatalf("legacy party EXP rate = %d, want ordinary rate 200", got)
	}
}
