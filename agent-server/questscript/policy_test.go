package questscript

import (
	"errors"
	"testing"
)

func TestCompletionPoliciesDecodeOptionalDeclarations(t *testing.T) {
	definitions := []Definition{{CodeName: "QUEST", Variant: 2, Declarations: []Call{
		{Function: "LuaSetAchievementLimit", Args: []Value{{Kind: ValueNumber, Number: 3}}},
		{Function: "LuaSetDivideType", Args: []Value{{Kind: ValueNumber, Number: 5}}},
		{Function: "LuaSetRealTime"},
		{Function: "LuaSetHwanLevel", Args: []Value{{Kind: ValueNumber, Number: 4}}},
		{Function: "LuaSetMissionCompleteNum", Args: []Value{{Kind: ValueNumber, Number: 1}}},
	}}}
	policies, err := CompletionPolicies(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].Variant != 2 || policies[0].MissionCompleteNum != 1 ||
		policies[0].AchievementLimit == nil || *policies[0].AchievementLimit != 3 ||
		policies[0].DivideType == nil || *policies[0].DivideType != 5 || !policies[0].RealTime ||
		policies[0].HwanLevel == nil || *policies[0].HwanLevel != 4 || len(policies[0].Parameters) != 5 {
		t.Fatalf("policies = %#v", policies)
	}
}

func TestCompletionPoliciesRejectDuplicateDeclaration(t *testing.T) {
	_, err := CompletionPolicies([]Definition{{CodeName: "QUEST", Declarations: []Call{
		{Function: "LuaSetMissionCompleteNum", Args: []Value{{Kind: ValueNumber}}},
		{Function: "LuaSetMissionCompleteNum", Args: []Value{{Kind: ValueNumber}}},
	}}})
	if !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("error = %v, want ErrInvalidChunk", err)
	}
}

func TestJobAndRaceStartConditionsDecodeRecoveredLayouts(t *testing.T) {
	definitions := []Definition{{CodeName: "QUEST", Declarations: []Call{
		{Function: "LuaSetJobTypeStartCondition", Args: []Value{{Kind: ValueNumber, Number: 1}, {Kind: ValueGlobal, String: "OBJ_JOBSTATE_HUNTER"}, {Kind: ValueNumber, Number: 1}}},
		{Function: "LuaSetRaceStartCodition", Args: []Value{{Kind: ValueGlobal, String: "QSC_RACE"}, {Kind: ValueGlobal, String: "COUNTRY_CHINA"}}},
	}}}
	jobs, err := JobStartConditions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	races, err := RaceStartConditions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].JobStateKey != "OBJ_JOBSTATE_HUNTER" || jobs[0].Parameter0 != 1 || jobs[0].Parameter2 != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
	if len(races) != 1 || races[0].CountryKey != "COUNTRY_CHINA" {
		t.Fatalf("races = %#v", races)
	}
}

func TestPointRewardsDecodeSkillAndNamedPoints(t *testing.T) {
	rewards, err := PointRewards([]Definition{{CodeName: "QUEST", Declarations: []Call{
		{Function: "LuaSetAchievedSkillPont", Args: []Value{{Kind: ValueNumber, Number: 700}}},
		{Function: "LuaSetAchievedPoint", Args: []Value{{Kind: ValueNumber, Number: 100}, {Kind: ValueString, String: "POINT_KEY"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 2 || rewards[0].Kind != PointRewardSkill || rewards[0].Amount != 700 ||
		rewards[1].Kind != PointRewardNamed || rewards[1].PointKey != "POINT_KEY" || rewards[1].RewardIndex != 1 {
		t.Fatalf("rewards = %#v", rewards)
	}
}
