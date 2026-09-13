package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestQuestOfferRepositoryListsOnlyQueryQualifiedRows(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(selectAvailableQuestOffers, catalogRepositoryTestResponse{
		columns: []string{"ID", "CODE_NAME", "NAME_STRING", "PAY_STRING", "PAY_CONTENTS", "OBJECTIVE_KEY", "REQUIRED_COUNT", "TASK_COUNT", "SELECTION_COUNT", "TIMER_DURATION", "DAY_LIMIT", "DAY_OR_NIGHT", "SHARED_PROGRESS", "INSTANT_READY"},
		rows: [][]driver.Value{
			{int64(3), "QNO_CH_SMITH_1", "SN_QNO_CH_SMITH_1", "SN_PAY_QNO_CH_SMITH_1", "SN_PAYCON_QNO_CH_SMITH_1", "SN_CON_QNO_CH_SMITH_1", int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), false, false},
			{int64(58), "QNO_CH_SMITH_2", "SN_QNO_CH_SMITH_2", "SN_PAY_QNO_CH_SMITH_2", "SN_PAYCON_QNO_CH_SMITH_2", "SN_CON_QNO_CH_SMITH_2", int64(50), int64(1), int64(0), int64(0), int64(0), int64(0), false, false},
			{int64(900), "QNO_CLASSIFIED", "SN_CLASSIFIED", "SN_PAY_CLASSIFIED", "SN_PAYCON_CLASSIFIED", "SN_NORMAL\x1fSN_CHAMPION\x1fSN_GIANT", int64(30), int64(3), int64(0), int64(0), int64(0), int64(0), false, false},
			{int64(88), "QNO_WC_FERRY4_1", "SN_FERRY", "SN_PAY_FERRY", "SN_PAYCON_FERRY", "", int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), false, true},
		},
	})
	repository, err := NewQuestOfferRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	offers, err := repository.ListAvailable(context.Background(), 42, 2003)
	if err != nil {
		t.Fatal(err)
	}
	want := []QuestOffer{
		{QuestID: 3, CodeName: "QNO_CH_SMITH_1", NameKey: "SN_QNO_CH_SMITH_1", PayKey: "SN_PAY_QNO_CH_SMITH_1", PayContentsKey: "SN_PAYCON_QNO_CH_SMITH_1", ObjectiveKey: "SN_CON_QNO_CH_SMITH_1"},
		{QuestID: 58, CodeName: "QNO_CH_SMITH_2", NameKey: "SN_QNO_CH_SMITH_2", PayKey: "SN_PAY_QNO_CH_SMITH_2", PayContentsKey: "SN_PAYCON_QNO_CH_SMITH_2", ObjectiveKey: "SN_CON_QNO_CH_SMITH_2", RequiredCount: 50, TaskCount: 1},
		{QuestID: 900, CodeName: "QNO_CLASSIFIED", NameKey: "SN_CLASSIFIED", PayKey: "SN_PAY_CLASSIFIED", PayContentsKey: "SN_PAYCON_CLASSIFIED", ObjectiveKey: "SN_NORMAL", ObjectiveKeys: []string{"SN_NORMAL", "SN_CHAMPION", "SN_GIANT"}, RequiredCount: 30, TaskCount: 3},
		{QuestID: 88, CodeName: "QNO_WC_FERRY4_1", NameKey: "SN_FERRY", PayKey: "SN_PAY_FERRY", PayContentsKey: "SN_PAYCON_FERRY", InstantReady: true},
	}
	if !reflect.DeepEqual(offers, want) {
		t.Fatalf("offers = %#v, want %#v", offers, want)
	}
	catalogRepositoryTestState.Lock()
	defer catalogRepositoryTestState.Unlock()
	if len(catalogRepositoryTestState.queries) != 1 || len(catalogRepositoryTestState.queries[0].args) != 2 ||
		catalogRepositoryTestState.queries[0].query != selectAvailableQuestOffers ||
		catalogRepositoryTestState.queries[0].args[0].Value != int64(42) || catalogRepositoryTestState.queries[0].args[1].Value != int64(2003) {
		t.Fatalf("queries = %#v", catalogRepositoryTestState.queries)
	}
}

func TestQuestOfferQueryLetsAbandonedAndActiveQuestsBypassInitialLevelWindow(t *testing.T) {
	for _, required := range []string{
		"current_quest.STATUS IN (1,6,7,8)",
		"current_quest.STATUS IN (1,4,6,7,8)",
		"character_state.CURRENT_LEVEL BETWEEN start_condition.LEVEL_LOWER AND start_condition.LEVEL_UPPER",
		"current_quest.STATUS IN (1,6,7,8) OR policy.ACHIEVEMENT_LIMIT IS NULL",
		"dependency_state.STATUS = 4 AND dependency_state.ACHIEVEMENT_COUNT>0",
		"dependency_group.RUNTIME_ENABLED<>1",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryAllowsStandaloneRealTimeModifier(t *testing.T) {
	if strings.Contains(selectAvailableQuestOffers, "policy.REAL_TIME=0") {
		t.Fatal("offer query still rejects LuaSetRealTime despite its lack of a standalone predicate")
	}
	for _, required := range []string{"QNO_KT_SOLDIER_WE2_1", "policy.DAY_LIMIT=20", "policy.DAY_OR_NIGHT=1", "QNO_WC_GENARAL_SW_1", "policy.DAY_LIMIT=5", "policy.DAY_OR_NIGHT=2"} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("offer query no longer fails closed for unrecovered day policy %q", required)
		}
	}
}

func TestQuestOfferQueryAllowsRecoveredScalarAndMultiItemRewards(t *testing.T) {
	for _, forbidden := range []string{
		"reward.SKILL_POINTS=0",
		"reward.HWAN=0",
		"reward.INVENTORY_SLOTS=0",
		"script_reward.ITEM_COUNT<=1",
	} {
		if strings.Contains(selectAvailableQuestOffers, forbidden) {
			t.Fatalf("offer query still rejects supported reward with %q", forbidden)
		}
	}
	if !strings.Contains(selectAvailableQuestOffers, "reward.IS_CHECK_COUNTRY=1 AND reward.SELECTION_COUNT>0") {
		t.Fatal("offer query does not admit the recovered country-filtered selection pattern")
	}
	if !strings.Contains(selectAvailableQuestOffers, "reward.ITEM_REWARD_TYPE=1") ||
		!strings.Contains(selectAvailableQuestOffers, "script_reward.ITEM_COUNT>1 THEN 1") {
		t.Fatal("offer query does not project the recovered conditional type-1 choice")
	}
}

func TestQuestOfferQueryRequiresAcademyScalarToMatchNamedTemplePoint(t *testing.T) {
	for _, required := range []string{
		"reward.ACADEMY_POINTS=0 OR EXISTS",
		"academy_point.AMOUNT=reward.ACADEMY_POINTS",
		"academy_point.REWARD_KIND='named_point'",
		"academy_point.POINT_KEY IN ('TEMPLE_ANUBIS','TEMPLE_HAROERIS')",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("offer query is missing Academy/Temple equality guard %q", required)
		}
	}
}

func TestQuestOfferQueryAdmitsRuntimeEnabledCollectionObjectives(t *testing.T) {
	for _, required := range []string{
		"REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection",
		"collection.VARIANT_INDEX=0",
		"collection.MISSION_INDEX=0",
		"collection.RUNTIME_ENABLED=1",
		"collection.OBJECTIVE_KEY",
		"collection.REQUIRED_COUNT",
		"(collection.QUEST_ID IS NOT NULL)",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("collection offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryAdmitsRuntimeEnabledTalismanObjectives(t *testing.T) {
	for _, required := range []string{
		"REF_QUEST_TALISMAN_OBJECTIVE talisman",
		"talisman.VARIANT_INDEX=0",
		"talisman.MISSION_INDEX=0",
		"talisman.RUNTIME_ENABLED=1",
		"talisman.OBJECTIVE_KEY",
		"(talisman.QUEST_ID IS NOT NULL)",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("talisman offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryAdmitsRoutedDialogObjectives(t *testing.T) {
	for _, required := range []string{
		"REF_QUEST_DIALOG_OBJECTIVE dialog",
		"dialog.VARIANT_INDEX=0",
		"dialog.MISSION_INDEX=0",
		"dialog.RUNTIME_ENABLED=1",
		"dialog.OBJECTIVE_KEY",
		"(dialog.QUEST_ID IS NOT NULL)",
		"dialog.TARGET_NPC_REF_OBJECT_ID<>start_npc.NPC_REF_OBJECT_ID",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("dialog offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryAdmitsRecoveredExtendedKillLayouts(t *testing.T) {
	for _, required := range []string{
		"REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_kill",
		"extended_kill.RUNTIME_ENABLED=1",
		"extended_kill.DECLARATION_KIND IN ('multi_target','classified')",
		"extended_kill.TARGET_COUNT+6",
		"'$[8].Number'",
		"GROUP_CONCAT(extended_all.OBJECTIVE_KEY ORDER BY extended_all.MISSION_INDEX",
		"extended_kill.OBJECTIVE_KEY",
		"(extended_kill.QUEST_ID IS NOT NULL)",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("extended-kill offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryAdmitsExactZeroMissionFerry(t *testing.T) {
	for _, required := range []string{
		"quest.CODE_NAME='QNO_WC_FERRY4_1'", "(quest.CODE_NAME='QNO_WC_FERRY4_1')) = 1",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("zero-mission ferry offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryProjectsEveryStandardCollectionMission(t *testing.T) {
	for _, required := range []string{
		"collection.DECLARATION_KIND='standard'",
		"GROUP_CONCAT(collection_all.OBJECTIVE_KEY ORDER BY collection_all.MISSION_INDEX SEPARATOR 0x1F)",
		"SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("multi-collection offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryQualifiesKillPlayerJobStart(t *testing.T) {
	for _, required := range []string{
		"REF_QUEST_KILL_PLAYER_OBJECTIVE kill_player",
		"kill_player.RUNTIME_ENABLED=1",
		"REF_QUEST_JOB_START_CONDITION job_condition",
		"job_condition.RUNTIME_ENABLED=1",
		"CHARACTER_JOB character_job",
		"start_condition.SYSTEM_REQUIRED=1",
		"WHEN 'OBJ_JOBSTATE_TRADER' THEN 1",
		"WHEN 'OBJ_JOBSTATE_ROBBER' THEN 2",
		"WHEN 'OBJ_JOBSTATE_HUNTER' THEN 3",
		"(kill_player.QUEST_ID IS NOT NULL)",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("kill-player offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryAppliesRecoveredJobStartToEverySupportedObjective(t *testing.T) {
	if !strings.Contains(selectAvailableQuestOffers, "OR (start_condition.SYSTEM_REQUIRED=1 AND EXISTS") {
		t.Fatal("offer query does not apply the recovered job predicate generically")
	}
	if strings.Contains(selectAvailableQuestOffers, "kill_player.QUEST_ID IS NOT NULL AND start_condition.SYSTEM_REQUIRED=1") {
		t.Fatal("offer query still couples the generic job predicate to kill-player objectives")
	}
}

func TestQuestOfferQueryQualifiesRecoveredCharacterRace(t *testing.T) {
	for _, required := range []string{
		"CHAR_REF_DATA character_ref",
		"REF_QUEST_RACE_START_CONDITION race_condition",
		"race_condition.RUNTIME_ENABLED<>1",
		"WHEN 'COUNTRY_CHINA' THEN 0",
		"WHEN 'COUNTRY_EUROPE' THEN 1",
		"ELSE 255",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("race-qualified offer query is missing %q", required)
		}
	}
}

func TestQuestOfferQueryQualifiesRecoveredHwanLevel(t *testing.T) {
	for _, required := range []string{
		"policy.RUNTIME_ENABLED=1",
		"policy.HWAN_LEVEL IS NULL OR policy.HWAN_LEVEL=character_state.HWAN_LEVEL",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("Hwan-qualified offer query is missing %q", required)
		}
	}
	if strings.Contains(selectAvailableQuestOffers, "policy.HWAN_LEVEL IS NULL\n\tAND") {
		t.Fatal("offer query still unconditionally rejects Hwan requirements")
	}
}

func TestQuestOfferQueryAdmitsOnlyRecoveredPositiveCountdowns(t *testing.T) {
	for _, required := range []string{
		"COALESCE(policy.LIMIT_TIME,0)",
		"policy.LIMIT_TIME=120",
		"'QNO_KT_ACCESSORY_1'",
		"'QNO_WC_SMITH_1'",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("timed offer query is missing %q", required)
		}
	}
	if strings.Contains(selectAvailableQuestOffers, "policy.LIMIT_TIME IS NULL AND") {
		t.Fatal("offer query still rejects every positive countdown")
	}
}

func TestQuestOfferQueryUsesImmediateAchievementLimitRepeatEligibility(t *testing.T) {
	for _, required := range []string{
		"current_quest.STATUS IN (1,4,6,7,8)",
		"policy.ACHIEVEMENT_LIMIT IS NULL",
		"current_quest.ACHIEVEMENT_COUNT < policy.ACHIEVEMENT_LIMIT",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("repeat offer query is missing %q", required)
		}
	}
	for _, unsupportedDelay := range []string{"COMPLETED_AT <", "TIMESTAMPDIFF", "DATE_ADD("} {
		if strings.Contains(selectAvailableQuestOffers, unsupportedDelay) {
			t.Fatalf("repeat offer query invented a scheduling delay through %q", unsupportedDelay)
		}
	}
}

func TestQuestOfferQueryAllowsRecoveredNamedTemplePointsOnly(t *testing.T) {
	for _, required := range []string{
		"point_reward.REWARD_KIND NOT IN ('skill_point','named_point')",
		"point_reward.POINT_KEY NOT IN ('TEMPLE_ANUBIS','TEMPLE_HAROERIS')",
		"point_reward.RUNTIME_ENABLED<>1",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("named-point offer query is missing %q", required)
		}
	}
}

func TestQuestOfferRepositoryFailsClosedOnInvalidProtocolMetadata(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(selectAvailableQuestOffers, catalogRepositoryTestResponse{
		columns: []string{"ID", "CODE_NAME", "NAME_STRING", "PAY_STRING", "PAY_CONTENTS", "OBJECTIVE_KEY", "REQUIRED_COUNT", "TASK_COUNT", "SELECTION_COUNT", "TIMER_DURATION", "DAY_LIMIT", "DAY_OR_NIGHT", "SHARED_PROGRESS"},
		rows:    [][]driver.Value{{int64(99), "QUEST", "NAME", "PAY", "CONTENTS", "", int64(1), int64(2), int64(0), int64(0), int64(0), int64(0), false}},
	})
	repository, err := NewQuestOfferRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.ListAvailable(context.Background(), 1, 2); err == nil {
		t.Fatal("invalid protocol metadata was exposed")
	}
}

func TestQuestOfferRepositoryValidatesDependencies(t *testing.T) {
	if _, err := NewQuestOfferRepository(nil); !errors.Is(err, ErrQuestOfferRepositoryUnavailable) {
		t.Fatalf("constructor error = %v", err)
	}
	repository, err := NewQuestOfferRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.ListAvailable(context.Background(), 0, 1); err == nil {
		t.Fatal("zero character ID was accepted")
	}
}
