package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/questscript"
)

func TestRetainedQuestScriptHasNoUnsupportedRewardBearingQuest(t *testing.T) {
	path := filepath.Join("..", "..", "Script", "VIETNAM", "Quest.sct")
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skip("ignored retained Quest.sct is not installed")
	}
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := questscript.Decode(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := questscript.Definitions(chunk)
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		quest   string
		variant int
	}
	rewardBearing := make(map[key]bool)
	rewards, err := questscript.RewardDeclarations(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, reward := range rewards {
		if reward.Variant != 0 {
			t.Errorf("reward %s uses unsupported variant %d", reward.QuestCodeName, reward.Variant)
		}
		rewardBearing[key{reward.QuestCodeName, reward.Variant}] = true
	}
	starts, err := questscript.StartConditions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	rewardless := 0
	for _, start := range starts {
		if rewardBearing[key{start.QuestCodeName, start.Variant}] {
			continue
		}
		rewardless++
		if !strings.HasPrefix(start.QuestCodeName, "QNO_FW_") && start.QuestCodeName != "QNO_WC_FERRY4_1" {
			t.Errorf("unclassified retained reward-less quest: %s/%d", start.QuestCodeName, start.Variant)
		}
	}
	if rewardless != 99 {
		t.Errorf("retained reward-less definition count = %d; want 99 classified fortress/ferry scripts", rewardless)
	}

	for _, parse := range []struct {
		name string
		run  func() error
	}{
		{"dependency", func() error {
			values, parseErr := questscript.Dependencies(definitions)
			for _, value := range values {
				if !safeDependencyGroup(value) {
					t.Errorf("unsupported retained dependency: %#v", value)
				}
			}
			return parseErr
		}},
		{"completion policy", func() error {
			values, parseErr := questscript.CompletionPolicies(definitions)
			for _, value := range values {
				if !safeCompletionPolicy(value) {
					t.Errorf("unsupported retained completion policy: %#v", value)
				}
			}
			return parseErr
		}},
		{"job condition", func() error {
			values, parseErr := questscript.JobStartConditions(definitions)
			for _, value := range values {
				if !safeJobStartCondition(value) {
					t.Errorf("unsupported retained job condition: %#v", value)
				}
			}
			return parseErr
		}},
		{"race condition", func() error {
			values, parseErr := questscript.RaceStartConditions(definitions)
			for _, value := range values {
				if !safeRaceStartCondition(value) {
					t.Errorf("unsupported retained race condition: %#v", value)
				}
			}
			return parseErr
		}},
		{"point reward", func() error {
			values, parseErr := questscript.PointRewards(definitions)
			for _, value := range values {
				if !safePointReward(value) {
					t.Errorf("unsupported retained point reward: %#v", value)
				}
			}
			return parseErr
		}},
	} {
		if err = parse.run(); err != nil {
			t.Fatalf("parse %s: %v", parse.name, err)
		}
	}

	objectiveCounts := make(map[key]int)
	enabled := make(map[key]bool)
	kills, err := questscript.KillMonsterObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	killGroups := make(map[key][]questscript.KillMonsterObjective)
	for _, value := range kills {
		k := key{value.QuestCodeName, value.Variant}
		objectiveCounts[k]++
		killGroups[k] = append(killGroups[k], value)
	}
	deliverEX, err := questscript.DeliverItemEXObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range deliverEX {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}
	deliver, err := questscript.DeliverItemObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range deliver {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}
	dialogs, err := questscript.DialogObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range dialogs {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}
	killPlayers, err := questscript.KillPlayerObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range killPlayers {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}
	collections, err := questscript.CollectionItemObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range collections {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}
	extended, err := questscript.ExtendedKillMonsterObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range extended {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}
	talismans, err := questscript.TalismanObjectives(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range talismans {
		objectiveCounts[key{value.QuestCodeName, value.Variant}]++
	}

	for k, group := range killGroups {
		if safeKillMonsterGroup(group, objectiveCounts[k]) {
			enabled[k] = true
		}
	}
	for _, value := range deliverEX {
		if value.QuestCodeName == "QNO_CH_SMITH_1" && value.Variant == 0 && value.MissionIndex == 0 {
			enabled[key{value.QuestCodeName, value.Variant}] = true
		}
	}
	for _, value := range deliver {
		k := key{value.QuestCodeName, value.Variant}
		if value.Variant == 0 && value.MissionIndex == 0 && objectiveCounts[k] == 1 {
			enabled[k] = true
		}
	}
	for _, value := range dialogs {
		k := key{value.QuestCodeName, value.Variant}
		if value.Variant == 0 && value.MissionIndex == 0 && objectiveCounts[k] == 1 {
			enabled[k] = true
		}
	}
	jobConditions, err := questscript.JobStartConditions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	jobsByQuest := make(map[key][]questscript.JobStartCondition)
	for _, value := range jobConditions {
		k := key{value.QuestCodeName, value.Variant}
		jobsByQuest[k] = append(jobsByQuest[k], value)
	}
	starts, err = questscript.StartConditions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	systemStart := make(map[key]bool)
	for _, value := range starts {
		systemStart[key{value.QuestCodeName, value.Variant}] = value.SystemRequired
	}
	for _, value := range killPlayers {
		k := key{value.QuestCodeName, value.Variant}
		jobs := jobsByQuest[k]
		if safeKillPlayerObjective(value, objectiveCounts[k]) && systemStart[k] && len(jobs) == 1 && safeJobStartCondition(jobs[0]) {
			enabled[k] = true
		}
	}
	collectionSafeCounts := make(map[key]int)
	for _, value := range collections {
		k := key{value.QuestCodeName, value.Variant}
		if safeCollectionObjective(value, objectiveCounts[k]) {
			collectionSafeCounts[k]++
		}
	}
	for k, count := range collectionSafeCounts {
		if count == objectiveCounts[k] {
			enabled[k] = true
		}
	}
	for _, value := range extended {
		k := key{value.QuestCodeName, value.Variant}
		if safeExtendedKillObjective(value, objectiveCounts[k]) {
			enabled[k] = true
		}
	}
	for _, value := range talismans {
		k := key{value.QuestCodeName, value.Variant}
		if safeTalismanObjective(value, objectiveCounts[k]) {
			enabled[k] = true
		}
	}

	for k := range rewardBearing {
		if !enabled[k] {
			t.Errorf("reward-bearing quest would be imported runtime-disabled: %s/%d", k.quest, k.variant)
		}
	}
	fortressCount := 0
	policies, err := questscript.CompletionPolicies(definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		if !strings.HasPrefix(policy.QuestCodeName, "QNO_FW_") {
			continue
		}
		fortressCount++
		k := key{policy.QuestCodeName, policy.Variant}
		if policy.Variant != 0 || policy.DivideType == nil || *policy.DivideType != 5 || !enabled[k] {
			t.Errorf("fortress reference-reward quest is not safely admitted: %#v objectiveSafe=%t", policy, enabled[k])
		}
	}
	if fortressCount != 98 {
		t.Errorf("fortress reference-reward quest count = %d; want 98", fortressCount)
	}
	ferryKey := key{"QNO_WC_FERRY4_1", 0}
	if objectiveCounts[ferryKey] != 0 || rewardBearing[ferryKey] {
		t.Errorf("ferry quest unexpectedly has ordinary objectives=%d reward=%t", objectiveCounts[ferryKey], rewardBearing[ferryKey])
	}
	ferryEXP, ferryItemAdds := 0, 0
	for _, prototype := range questscript.AllPrototypes(chunk) {
		if !strings.Contains(prototype.Source, "QNO_WC_FERRY4_1") {
			continue
		}
		for _, call := range questscript.Calls(prototype) {
			switch call.Function {
			case "LuaQuestGiveEventPoint_EXP_Gold":
				if len(call.Args) == 3 && call.Args[0].Kind == questscript.ValueNumber && call.Args[0].Number == 650 &&
					call.Args[1].Kind == questscript.ValueNumber && call.Args[1].Number == 0 && call.Args[2].Kind == questscript.ValueNumber && call.Args[2].Number == 0 {
					ferryEXP++
				}
			case "LuaQuestAddItem_EXT":
				for _, argument := range call.Args {
					if argument.Kind == questscript.ValueString && argument.String == "ITEM_QNO_WC_FERRY4_1_01" {
						ferryItemAdds++
					}
				}
			}
		}
	}
	if ferryEXP != 1 || ferryItemAdds != 3 {
		t.Errorf("ferry custom reward evidence = exp calls %d item calls %d; want 1/3", ferryEXP, ferryItemAdds)
	}
}

func TestRetainedQuestRuntimeCoverageReportsExcludedShapes(t *testing.T) {
	coverage := retainedQuestRuntimeCoverage(nil,
		[]questscript.CompletionPolicy{{QuestCodeName: "QUEST_BAD", Variant: 1}}, nil, nil, nil, nil,
		[]questscript.KillMonsterObjective{{QuestCodeName: "QUEST_GOOD", Variant: 0, MissionIndex: 0, ObjectiveKey: "KILL", TargetCode: "MOB", RequiredCount: 1}},
		nil, nil, nil, nil, nil, nil, nil, nil)
	if family := coverage["kill_monster_quests"]; family.Imported != 1 || family.Enabled != 1 || len(family.Excluded) != 0 {
		t.Fatalf("kill coverage = %#v", family)
	}
	if family := coverage["completion_policies"]; family.Imported != 1 || family.Enabled != 0 || len(family.Excluded) != 1 || family.Excluded[0] != "QUEST_BAD/1" {
		t.Fatalf("policy coverage = %#v", family)
	}
}

func TestSafeRaceStartConditionAcceptsOnlyRecoveredCountryKeys(t *testing.T) {
	for _, country := range []string{"COUNTRY_CHINA", "COUNTRY_EUROPE"} {
		if !safeRaceStartCondition(questscript.RaceStartCondition{Variant: 0, ConditionKey: "QSC_RACE", CountryKey: country}) {
			t.Fatalf("recovered race condition %s was rejected", country)
		}
	}
	for _, condition := range []questscript.RaceStartCondition{
		{Variant: 1, ConditionKey: "QSC_RACE", CountryKey: "COUNTRY_CHINA"},
		{Variant: 0, ConditionKey: "QSC_UNKNOWN", CountryKey: "COUNTRY_CHINA"},
		{Variant: 0, ConditionKey: "QSC_RACE", CountryKey: "COUNTRY_UNKNOWN"},
	} {
		if safeRaceStartCondition(condition) {
			t.Fatalf("unsupported race condition was accepted: %#v", condition)
		}
	}
}

func TestSafeDependencyGroupAcceptsRecoveredModesOnly(t *testing.T) {
	for _, mode := range []string{"all_clear", "one_clear"} {
		if !safeDependencyGroup(questscript.Dependency{Variant: 0, Mode: mode, QuestCodes: []string{"QUEST_A"}}) {
			t.Fatalf("recovered dependency mode %s was rejected", mode)
		}
	}
	for _, group := range []questscript.Dependency{
		{Variant: 1, Mode: "all_clear", QuestCodes: []string{"QUEST_A"}},
		{Variant: 0, Mode: "unknown", QuestCodes: []string{"QUEST_A"}},
		{Variant: 0, Mode: "all_clear"},
	} {
		if safeDependencyGroup(group) {
			t.Fatalf("unsafe dependency group accepted: %#v", group)
		}
	}
}

func TestSafeCompletionPolicyAcceptsOptionalRecoveredHwanRequirement(t *testing.T) {
	level := uint32(4)
	if !safeCompletionPolicy(questscript.CompletionPolicy{Variant: 0}) ||
		!safeCompletionPolicy(questscript.CompletionPolicy{Variant: 0, RealTime: true}) ||
		!safeCompletionPolicy(questscript.CompletionPolicy{Variant: 0, HwanLevel: &level}) {
		t.Fatal("supported completion policy was rejected")
	}
	zero, invalid := uint32(0), uint32(6)
	for _, policy := range []questscript.CompletionPolicy{
		{Variant: 1}, {Variant: 0, DayLimit: &level}, {Variant: 0, DayOrNight: &level},
		{Variant: 0, HwanLevel: &zero}, {Variant: 0, HwanLevel: &invalid},
	} {
		if safeCompletionPolicy(policy) {
			t.Fatalf("unsupported completion policy was accepted: %#v", policy)
		}
	}
}

func TestSafeCompletionPolicyAcceptsExactRecoveredDayPolicies(t *testing.T) {
	nightLimit, nightSelector := uint32(20), uint32(1)
	dayLimit, daySelector := uint32(5), uint32(2)
	for _, policy := range []questscript.CompletionPolicy{
		{QuestCodeName: "QNO_KT_SOLDIER_WE2_1", Variant: 0, DayLimit: &nightLimit, DayOrNight: &nightSelector},
		{QuestCodeName: "QNO_WC_GENARAL_SW_1", Variant: 0, DayLimit: &dayLimit, DayOrNight: &daySelector},
	} {
		if !safeCompletionPolicy(policy) {
			t.Fatalf("recovered day policy was rejected: %#v", policy)
		}
	}
	for _, policy := range []questscript.CompletionPolicy{
		{QuestCodeName: "QNO_UNKNOWN", Variant: 0, DayLimit: &nightLimit, DayOrNight: &nightSelector},
		{QuestCodeName: "QNO_KT_SOLDIER_WE2_1", Variant: 0, DayLimit: &dayLimit, DayOrNight: &daySelector},
		{QuestCodeName: "QNO_WC_GENARAL_SW_1", Variant: 0, DayLimit: &dayLimit},
	} {
		if safeCompletionPolicy(policy) {
			t.Fatalf("unsupported day policy was accepted: %#v", policy)
		}
	}
}

func TestSafePointRewardAcceptsRecoveredFixedAndTempleKeys(t *testing.T) {
	for _, reward := range []questscript.PointReward{
		{Variant: 0, Kind: questscript.PointRewardSkill, Amount: 0},
		{Variant: 0, Kind: questscript.PointRewardSkill, Amount: 1},
		{Variant: 0, Kind: questscript.PointRewardNamed, Amount: 100, PointKey: "TEMPLE_ANUBIS"},
		{Variant: 0, Kind: questscript.PointRewardNamed, Amount: 200, PointKey: "TEMPLE_HAROERIS"},
	} {
		if !safePointReward(reward) {
			t.Fatalf("recovered point reward was rejected: %#v", reward)
		}
	}
	for _, reward := range []questscript.PointReward{
		{Variant: 1, Kind: questscript.PointRewardNamed, Amount: 1, PointKey: "TEMPLE_ANUBIS"},
		{Variant: 0, Kind: questscript.PointRewardNamed, Amount: 0, PointKey: "TEMPLE_ANUBIS"},
		{Variant: 0, Kind: questscript.PointRewardNamed, Amount: 1, PointKey: "UNKNOWN"},
		{Variant: 0, Kind: questscript.PointRewardSkill, Amount: 1, PointKey: "TEMPLE_ANUBIS"},
	} {
		if safePointReward(reward) {
			t.Fatalf("unsupported point reward was accepted: %#v", reward)
		}
	}
}

func TestSafeCompletionPolicyAcceptsBinaryCountdownDuration(t *testing.T) {
	valid := uint32(120)
	zero := uint32(0)
	overflow := uint32(256)
	if !safeCompletionPolicy(questscript.CompletionPolicy{Variant: 0, LimitTime: &valid}) {
		t.Fatal("binary-authored uint8 countdown duration was rejected")
	}
	for _, duration := range []*uint32{&zero, &overflow} {
		if safeCompletionPolicy(questscript.CompletionPolicy{Variant: 0, LimitTime: duration}) {
			t.Fatalf("invalid countdown duration %d was accepted", *duration)
		}
	}
}

func TestSafeTalismanObjectiveRequiresRecoveredSingleMissionShape(t *testing.T) {
	base := questscript.TalismanObjective{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 0, Parameter6: 1, ThemeCode: "THEME"}
	if !safeTalismanObjective(base, 1) {
		t.Fatal("recovered talisman shape was rejected")
	}
	for _, objective := range []questscript.TalismanObjective{
		{QuestCodeName: "QUEST", Variant: 1, MissionIndex: 0, Parameter6: 1},
		{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 1, Parameter6: 1},
		{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 0, Parameter6: 0},
	} {
		if safeTalismanObjective(objective, 1) {
			t.Fatalf("unsafe talisman shape accepted: %#v", objective)
		}
	}
	if safeTalismanObjective(base, 2) {
		t.Fatal("multi-objective talisman shape was accepted")
	}
}

func TestSafeExtendedKillObjectiveAcceptsRecoveredClassifiedCounters(t *testing.T) {
	base := questscript.ExtendedKillMonsterObjective{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 0,
		Kind: questscript.ExtendedKillMultiTarget, ObjectiveKey: "SN_SHARED", RequiredCount: 25, Targets: []string{"MOB_A", "MOB_B"}}
	if !safeExtendedKillObjective(base, 1) {
		t.Fatal("recovered multi-target extended kill shape was rejected")
	}
	classified := questscript.ExtendedKillMonsterObjective{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 0,
		Kind: questscript.ExtendedKillClassified, ObjectiveKey: "SN_NORMAL", TargetClass: "MONSTER_CLASS_NORMAL",
		RequiredCount: 30, Targets: []string{"MOB_A"}}
	if !safeExtendedKillObjective(classified, 3) {
		t.Fatal("recovered classified normal counter was rejected")
	}
	classified.MissionIndex = 2
	classified.TargetClass = "MONSTER_CLASS_GIANT"
	if !safeExtendedKillObjective(classified, 3) {
		t.Fatal("recovered classified giant counter was rejected")
	}
	classified.TargetClass = "MONSTER_CLASS_UNKNOWN"
	if safeExtendedKillObjective(classified, 3) {
		t.Fatal("unsupported extended kill shape was accepted")
	}
	base.MissionIndex = 1
	if safeExtendedKillObjective(base, 1) {
		t.Fatal("non-zero extended kill mission was accepted")
	}
}

func TestSafeExtendedKillObjectiveAcceptsConsecutiveMultiTargetMissions(t *testing.T) {
	for mission := uint32(0); mission < 2; mission++ {
		objective := questscript.ExtendedKillMonsterObjective{QuestCodeName: "QNO_FW_RB1_021", Variant: 0,
			MissionIndex: mission, Kind: questscript.ExtendedKillMultiTarget, ObjectiveKey: "SN_OBJECTIVE",
			RequiredCount: 8, Targets: []string{"MOB_A", "MOB_B"}}
		if !safeExtendedKillObjective(objective, 2) {
			t.Fatalf("multi-target mission %d was rejected", mission)
		}
	}
}

func TestSafeCollectionObjectiveRequiresRecoveredRuntimeShape(t *testing.T) {
	base := questscript.CollectionItemObjective{
		QuestCodeName: "QNO_WC_POTION_1",
		Variant:       0,
		MissionIndex:  0,
		Kind:          questscript.CollectionItemStandard,
		ObjectiveKey:  "SN_CON_QNO_WC_POTION_1",
		Parameter6:    1,
		RequiredCount: 50,
		Sources:       []questscript.CollectionItemSource{{MonsterCode: "MOB_WC_GHOSTBUG", Parameter: 33}},
	}
	if !safeCollectionObjective(base, 1) {
		t.Fatal("recovered single standard collection objective was not runtime safe")
	}

	tests := []func(*questscript.CollectionItemObjective){
		func(value *questscript.CollectionItemObjective) { value.Kind = questscript.CollectionItemExtended },
		func(value *questscript.CollectionItemObjective) { value.Variant = 1 },
		func(value *questscript.CollectionItemObjective) { value.MissionIndex = 1 },
		func(value *questscript.CollectionItemObjective) { value.Parameter6 = math.MaxUint16 + 1 },
		func(value *questscript.CollectionItemObjective) { value.Sources[0].Parameter = 0 },
		func(value *questscript.CollectionItemObjective) { value.Sources[0].Parameter = 101 },
	}
	for index, mutate := range tests {
		objective := base
		objective.Sources = append([]questscript.CollectionItemSource(nil), base.Sources...)
		mutate(&objective)
		if safeCollectionObjective(objective, 1) {
			t.Fatalf("unsupported collection objective case %d was runtime safe", index)
		}
	}
	if !safeCollectionObjective(base, 2) {
		t.Fatal("first mission of a recovered multi-objective collection was rejected")
	}
	base.MissionIndex = 1
	base.ObjectiveKey = "SN_CON_QNO_WC_POTION_1_2"
	if !safeCollectionObjective(base, 2) {
		t.Fatal("second mission of a recovered multi-objective collection was rejected")
	}
	base.MissionIndex = 4
	if safeCollectionObjective(base, 4) {
		t.Fatal("out-of-range collection mission was accepted")
	}
}

func TestSafeCollectionObjectiveAcceptsExactUniqueDeadHandlers(t *testing.T) {
	targets := map[string]string{
		"QNO_SD_MA_029": "MOB_SD_SELKIS", "QNO_SD_MA_030": "MOB_SD_NEITH",
		"QNO_SD_MA_031": "MOB_SD_ANUBIS", "QNO_SD_MA_032": "MOB_SD_ISIS",
		"QNO_SD_MA_033": "MOB_SD_HAROERIS", "QNO_SD_MA_034": "MOB_SD_SETH",
	}
	for quest, target := range targets {
		objective := questscript.CollectionItemObjective{
			QuestCodeName: quest, Variant: 0, MissionIndex: 0, Kind: questscript.CollectionItemStandard,
			ObjectiveKey: "SN_CON_" + quest + "_1", Parameter6: 1, RequiredCount: 1,
			ItemCode: "ITEM_" + quest + "_2", Sources: []questscript.CollectionItemSource{{MonsterCode: target, Parameter: 0}},
		}
		if !safeCollectionObjective(objective, 1) {
			t.Fatalf("retained UNIQUE_DEAD objective %s was rejected", quest)
		}
		objective.Sources[0].Parameter = 1
		if safeCollectionObjective(objective, 1) {
			t.Fatalf("ordinary drop masquerading as UNIQUE_DEAD objective %s was accepted", quest)
		}
	}
}

func TestSafeKillMonsterGroupAcceptsSharedCounterMissions(t *testing.T) {
	single := []questscript.KillMonsterObjective{{QuestCodeName: "UNIQUE", Variant: 0, MissionIndex: 0, ObjectiveKey: "U", TargetCode: "MOB_UNIQUE", TargetClass: "MONSTER_CLASS_UNIQUE", RequiredCount: 10}}
	if !safeKillMonsterGroup(single, 1) {
		t.Fatal("recovered single unique-class kill was rejected")
	}
	objectives := []questscript.KillMonsterObjective{
		{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 0, ObjectiveKey: "A", Parameter4: 1, TargetCode: "MOB_A", TargetClass: "MONSTER_CLASS_NORMAL", RequiredCount: 100, Parameter9: 0},
		{QuestCodeName: "QUEST", Variant: 0, MissionIndex: 1, ObjectiveKey: "B", Parameter4: 1, TargetCode: "MOB_B", TargetClass: "MONSTER_CLASS_NORMAL", RequiredCount: 100, Parameter9: 1},
	}
	if !safeKillMonsterGroup(objectives, 2) {
		t.Fatal("recovered shared-counter kill group was rejected")
	}
	objectives[1].TargetCode = "MOB_A"
	if safeKillMonsterGroup(objectives, 2) {
		t.Fatal("duplicate-target kill group was admitted")
	}
}

func TestSafeCollectionObjectiveAcceptsExactExtendedCompanionPair(t *testing.T) {
	values := func(numbers ...float64) []questscript.Value {
		result := make([]questscript.Value, len(numbers))
		for index, number := range numbers {
			result[index] = questscript.Value{Kind: questscript.ValueNumber, Number: number}
		}
		return result
	}
	base := questscript.CollectionItemObjective{
		QuestCodeName: "QNO_WC_GENARAL_SP_1", Variant: 0, Kind: questscript.CollectionItemExtended,
		ObjectiveKey: "SN_CON_QNO_WC_GENARAL_SP_1", Parameter6: 1, RequiredCount: 15,
		Sources:    []questscript.CollectionItemSource{{MonsterCode: "MOB_A", Parameter: 5}, {MonsterCode: "MOB_B", Parameter: 1}},
		Parameters: values(1, 0, 0, 0, 0, 0),
	}
	if !safeCollectionObjective(base, 2) {
		t.Fatal("visible extended collection mission was rejected")
	}
	base.MissionIndex = 1
	base.ObjectiveKey = ""
	base.Parameters = values(0, 1, 0, 0, 0, 0)
	if !safeCollectionObjective(base, 2) {
		t.Fatal("internal extended collection companion was rejected")
	}
	base.Parameters[0].Number = 1
	if safeCollectionObjective(base, 2) {
		t.Fatal("internal companion with a visible-update control flag was accepted")
	}
}

func TestSafeKillPlayerObjectiveRequiresRecoveredThreeFilterShape(t *testing.T) {
	base := questscript.KillPlayerObjective{
		QuestCodeName: "QNO_SD_GU_008", Variant: 0, MissionIndex: 0, Parameter4: 1, Parameter6: 20,
		FilterCount: 3, LevelFilterKey: "MKP_LEVEL", JobFilterKey: "MKP_JOB", RegionFilterKey: "MKP_REGION",
		RegionValue: -32752, RegionID: -32752, Parameter12: 105, RequiredCount: 20, MinimumLevel: 105,
		JobStates: []string{"OBJ_JOBSTATE_ROBBER"},
	}
	if !safeKillPlayerObjective(base, 1) {
		t.Fatal("recovered kill-player objective was not runtime safe")
	}
	tests := []func(*questscript.KillPlayerObjective){
		func(value *questscript.KillPlayerObjective) { value.Variant = 1 },
		func(value *questscript.KillPlayerObjective) { value.MissionIndex = 1 },
		func(value *questscript.KillPlayerObjective) { value.FilterCount = 2 },
		func(value *questscript.KillPlayerObjective) { value.MinimumLevel = 0 },
		func(value *questscript.KillPlayerObjective) { value.RegionID = 1 },
		func(value *questscript.KillPlayerObjective) { value.JobStates[0] = "OBJ_JOBSTATE_UNKNOWN" },
	}
	for index, mutate := range tests {
		objective := base
		objective.JobStates = append([]string(nil), base.JobStates...)
		mutate(&objective)
		if safeKillPlayerObjective(objective, 1) {
			t.Fatalf("unsupported kill-player objective case %d was runtime safe", index)
		}
	}
	if safeKillPlayerObjective(base, 2) {
		t.Fatal("multi-objective kill-player quest was runtime safe")
	}
}

func TestSafeJobStartConditionRequiresSingleKnownJobShape(t *testing.T) {
	base := questscript.JobStartCondition{Variant: 0, ConditionIndex: 0, Parameter0: 1, JobStateKey: "OBJ_JOBSTATE_TRADER", Parameter2: 1}
	if !safeJobStartCondition(base) {
		t.Fatal("recovered kill-player job start condition was not runtime safe")
	}
	for index, mutate := range []func(*questscript.JobStartCondition){
		func(value *questscript.JobStartCondition) { value.Variant = 1 },
		func(value *questscript.JobStartCondition) { value.ConditionIndex = 1 },
		func(value *questscript.JobStartCondition) { value.Parameter0 = 2 },
		func(value *questscript.JobStartCondition) { value.Parameter2 = 2 },
		func(value *questscript.JobStartCondition) { value.JobStateKey = "UNKNOWN" },
	} {
		condition := base
		mutate(&condition)
		if safeJobStartCondition(condition) {
			t.Fatalf("unsupported job start condition case %d was runtime safe", index)
		}
	}
}
