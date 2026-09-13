package model

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestRecordPlayerKillQueryUsesRecoveredMinimumLevelAndTempleRegion(t *testing.T) {
	for _, required := range []string{
		"?>=objective.PARAMETER_12",
		"job_state.JOB_STATE_KEY=?",
		"objective.REGION_VALUE=?",
		"objective.RUNTIME_ENABLED=1",
	} {
		if !strings.Contains(recordPlayerKillQuery, required) {
			t.Fatalf("player-kill query is missing %q", required)
		}
	}
	if strings.Contains(recordPlayerKillQuery, "?<=objective.PARAMETER_12") {
		t.Fatal("player-kill query treats the authored minimum level as a maximum")
	}
}

func TestAdvanceClassifiedQuestKeepsIndependentNativeCounters(t *testing.T) {
	quest := CharacterQuest{Status: QuestActive}
	goals := []uint16{2, 1, 1}
	quest, changed, err := advanceClassifiedQuest(quest, 0, goals)
	if err != nil || !changed || quest.Data1 != 1 || quest.Status != QuestProgressed {
		t.Fatalf("normal progress = %#v changed=%t error=%v", quest, changed, err)
	}
	quest, changed, err = advanceClassifiedQuest(quest, 1, goals)
	if err != nil || !changed || quest.Data1 != 1|1<<16 || quest.Status != QuestProgressed {
		t.Fatalf("champion progress = %#v changed=%t error=%v", quest, changed, err)
	}
	quest, changed, err = advanceClassifiedQuest(quest, 2, goals)
	if err != nil || !changed || quest.Data1 != 1|1<<16|1<<32 || quest.Status != QuestProgressed {
		t.Fatalf("giant progress = %#v changed=%t error=%v", quest, changed, err)
	}
	quest, changed, err = advanceClassifiedQuest(quest, 0, goals)
	if err != nil || !changed || quest.Data1 != 2|1<<16|1<<32 || quest.Status != QuestRewardReady {
		t.Fatalf("completed classified quest = %#v changed=%t error=%v", quest, changed, err)
	}
}

func TestAdvanceMultiTargetExtendedQuestKeepsIndependentMissionCounters(t *testing.T) {
	quest := CharacterQuest{Status: QuestActive, ObjectiveKeys: [4]string{"A", "B"}, Data1: 7 | 6<<16}
	goals := [4]uint16{8, 8}
	quest, changed, err := advanceMultiTargetExtendedQuest(quest, goals, [4]bool{true, false}, 2)
	if err != nil || !changed || quest.Data1 != 8|6<<16 || quest.Status != QuestProgressed {
		t.Fatalf("first extended mission = %#v changed=%t error=%v", quest, changed, err)
	}
	quest, changed, err = advanceMultiTargetExtendedQuest(quest, goals, [4]bool{false, true}, 2)
	if err != nil || !changed || quest.Data1 != 8|7<<16 || quest.Status != QuestProgressed {
		t.Fatalf("second extended mission partial = %#v changed=%t error=%v", quest, changed, err)
	}
	quest, changed, err = advanceMultiTargetExtendedQuest(quest, goals, [4]bool{false, true}, 2)
	if err != nil || !changed || quest.Data1 != 8|8<<16 || quest.Status != QuestRewardReady {
		t.Fatalf("extended missions complete = %#v changed=%t error=%v", quest, changed, err)
	}
}

func TestClassifiedMonsterKeyUsesPartyBaseClass(t *testing.T) {
	for rarity, want := range map[MonsterRarity]string{
		MonsterNormal: "MONSTER_CLASS_NORMAL", MonsterPartyNormal: "MONSTER_CLASS_NORMAL",
		MonsterChampion: "MONSTER_CLASS_CHAMPION", MonsterPartyChampion: "MONSTER_CLASS_CHAMPION",
		MonsterGiant: "MONSTER_CLASS_GIANT", MonsterPartyGiant: "MONSTER_CLASS_GIANT",
	} {
		if got := classifiedMonsterClass(rarity); got != want {
			t.Fatalf("rarity %d key = %q; want %q", rarity, got, want)
		}
	}
	if classifiedMonsterClass(MonsterUnique) != "" {
		t.Fatal("unique monster was accepted by a classified field-mob counter")
	}
}

func TestAdvanceSharedKillQuestUsesOneCounterAndMissionBits(t *testing.T) {
	quest := CharacterQuest{Status: QuestActive, ObjectiveKeys: [4]string{"A", "B", "C"}, Data1: 99}
	goals := [3]uint16{100, 100, 200}
	quest, changed, err := advanceSharedKillQuest(quest, goals, [3]bool{true, false, false}, 3)
	if err != nil || !changed || quest.Data1 != 100 || quest.Data2 != 1 || quest.Status != QuestProgressed {
		t.Fatalf("first threshold = %#v changed=%t error=%v", quest, changed, err)
	}
	quest, changed, err = advanceSharedKillQuest(quest, goals, [3]bool{false, true, false}, 3)
	if err != nil || !changed || quest.Data1 != 100 || quest.Data2 != 3 || quest.Status != QuestProgressed {
		t.Fatalf("second mission bit = %#v changed=%t error=%v", quest, changed, err)
	}
	quest.Data1 = 199
	quest, changed, err = advanceSharedKillQuest(quest, goals, [3]bool{false, false, true}, 3)
	if err != nil || !changed || quest.Data1 != 200 || quest.Data2 != 7 || quest.Status != QuestRewardReady {
		t.Fatalf("shared completion = %#v changed=%t error=%v", quest, changed, err)
	}
}

func TestApplyExtendedCollectionSnapshotKeepsInternalMissionOffWire(t *testing.T) {
	quest := CharacterQuest{QuestID: 900, Status: QuestActive, ObjectiveKey: "SN_VISIBLE", TaskCount: 1}
	goals := [3]uint16{15, 15}
	updated, changed, err := applyExtendedCollectionSnapshot(quest, goals, [3]uint16{15, 7})
	if err != nil || !changed || updated.Data1 != 15 || updated.Data2 != 7 || updated.Status != QuestProgressed {
		t.Fatalf("partial extended collection = %#v changed=%t error=%v", updated, changed, err)
	}
	updated, changed, err = applyExtendedCollectionSnapshot(updated, goals, [3]uint16{15, 15})
	if err != nil || !changed || updated.Data1 != 15 || updated.Data2 != 15 || updated.Status != QuestRewardReady {
		t.Fatalf("complete extended collection = %#v changed=%t error=%v", updated, changed, err)
	}
	record, err := ProjectActiveQuestProtocol(updated)
	if err != nil || len(record.Values) != 1 || record.Values[0] != 15 || record.Text != "SN_VISIBLE" {
		t.Fatalf("visible extended collection record = %#v error=%v", record, err)
	}
}

func TestApplyMultiCollectionSnapshotPacksIndependentCounters(t *testing.T) {
	for _, test := range []struct {
		name   string
		count  int
		goals  [4]uint16
		counts [4]uint16
		want   uint64
		status QuestStatus
	}{
		{name: "two partial", count: 2, goals: [4]uint16{150, 100}, counts: [4]uint16{150, 99}, want: 150 | 99<<16, status: QuestProgressed},
		{name: "three complete", count: 3, goals: [4]uint16{10, 20, 20}, counts: [4]uint16{10, 20, 20}, want: 10 | 20<<16 | 20<<32, status: QuestRewardReady},
		{name: "four complete", count: 4, goals: [4]uint16{1, 1, 2, 2}, counts: [4]uint16{1, 1, 2, 2}, want: 1 | 1<<16 | 2<<32 | 2<<48, status: QuestRewardReady},
	} {
		t.Run(test.name, func(t *testing.T) {
			quest := CharacterQuest{Status: QuestActive, ObjectiveKeys: [4]string{"A", "B", "C", "D"}}
			updated, changed, err := applyMultiCollectionSnapshot(quest, test.goals, test.counts, test.count)
			if err != nil || !changed || updated.Data1 != test.want || updated.Status != test.status {
				t.Fatalf("multi collection = %#v changed=%t error=%v", updated, changed, err)
			}
		})
	}
}

func TestCollectionDropQueryUsesDeclarationSpecificCounters(t *testing.T) {
	for _, required := range []string{
		"CASE objective.DECLARATION_KIND",
		"WHEN 'standard' THEN ((quest.QUEST_DATA_1 >> (16*objective.MISSION_INDEX)) & 65535)",
		"WHEN 'extended' THEN CASE objective.MISSION_INDEX WHEN 0 THEN quest.QUEST_DATA_1 WHEN 1 THEN quest.QUEST_DATA_2",
	} {
		if !strings.Contains(collectionDropsForKillQuery, required) {
			t.Fatalf("collection-drop query is missing %q", required)
		}
	}
	if maxCollectionPaymentObjectives != 4 {
		t.Fatalf("collection payment objective limit = %d; want 4", maxCollectionPaymentObjectives)
	}
}

func TestUniqueDeadGrantQueryPinsExactRetainedHandlers(t *testing.T) {
	for _, required := range []string{
		"reference.CODE_NAME IN ('QNO_SD_MA_029','QNO_SD_MA_030','QNO_SD_MA_031','QNO_SD_MA_032','QNO_SD_MA_033','QNO_SD_MA_034')",
		"source.SOURCE_PARAMETER=0", "source.MONSTER_REF_OBJECT_ID=?", "precursor.CODE_NAME=CONCAT('ITEM_',reference.CODE_NAME,'_1')",
		"WHEN 'QNO_SD_MA_029' THEN -32758", "WHEN 'QNO_SD_MA_034' THEN -32753", "objective.RUNTIME_ENABLED=1", "FOR UPDATE",
	} {
		if !strings.Contains(uniqueDeadGrantQuery, required) {
			t.Fatalf("UNIQUE_DEAD grant query is missing %q", required)
		}
	}
}

func TestListDeliveriesRequiresAggregatedAuthoredInventory(t *testing.T) {
	for _, required := range []string{
		"objective.RUNTIME_ENABLED=1",
		"objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0",
		"objective.TARGET_NPC_REF_OBJECT_ID=?",
		"GROUP BY entry_item.ITEM_REF_OBJECT_ID",
		"SUM(item.STACK_COUNT)",
		"SUM(entry_item.QUANTITY)",
	} {
		if !strings.Contains(listDeliveriesAtNPCQuery, required) {
			t.Fatalf("delivery eligibility query is missing %q", required)
		}
	}
}

func TestQuestInventorySlotUsesPersistedCapacity(t *testing.T) {
	items := map[byte]questInventoryItem{13: {id: 1}, 14: {id: 2}}
	slot, ok := freeQuestInventorySlot(items, 16)
	if !ok || slot != 15 {
		t.Fatalf("free slot = %d, present=%t; want 15", slot, ok)
	}
	items[15] = questInventoryItem{id: 3}
	if slot, ok := freeQuestInventorySlot(items, 16); ok {
		t.Fatalf("full inventory returned slot %d", slot)
	}
}

func TestNextQuestAchievementCountHonorsAuthoredLimit(t *testing.T) {
	if next, err := nextQuestAchievementCount(0, sql.NullInt64{Int64: 1, Valid: true}); err != nil || next != 1 {
		t.Fatalf("first achievement = %d, %v", next, err)
	}
	if _, err := nextQuestAchievementCount(1, sql.NullInt64{Int64: 1, Valid: true}); !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("achievement beyond limit error = %v", err)
	}
	if next, err := nextQuestAchievementCount(7, sql.NullInt64{}); err != nil || next != 8 {
		t.Fatalf("unlimited achievement = %d, %v", next, err)
	}
}

func TestFindQuestItemUsesReferenceIdentity(t *testing.T) {
	const weaponListItemID = uint32(2200)
	items := map[byte]questInventoryItem{
		13: {id: 1, refID: 999, stack: 1},
		20: {id: 2, refID: weaponListItemID, stack: 1},
	}
	slot, item, ok := findQuestItem(items, weaponListItemID)
	if !ok || slot != 20 || item.id != 2 {
		t.Fatalf("quest item = slot %d item %#v present=%t", slot, item, ok)
	}
}

func TestValidateQuestRewardReferenceAcceptsRecoveredRewards(t *testing.T) {
	const chulsanRewardItemID = uint32(9228)
	weaponList := questRewardReference{IsBasicReward: true, Gold: 205, Experience: 270}
	if err := validateQuestRewardReference(QuestWeaponListID, weaponList); err != nil {
		t.Fatalf("weapon-list reward: %v", err)
	}
	chulsan := questRewardReference{IsBasicReward: true, IsItemReward: true, Experience: 68860, SkillExperience: 10000, ItemRefID: chulsanRewardItemID, ItemQuantity: 1}
	if err := validateQuestRewardReference(QuestChulsanAngerID, chulsan); err != nil {
		t.Fatalf("Chulsan reward: %v", err)
	}
	generic := questRewardReference{IsBasicReward: true, Gold: 100, Experience: 200}
	if err := validateQuestRewardReference(101, generic); err != nil {
		t.Fatalf("generic reward: %v", err)
	}
	fixedSkillPoints := questRewardReference{IsBasicReward: true, SkillPoints: 25}
	if err := validateQuestRewardReference(102, fixedSkillPoints); err != nil {
		t.Fatalf("fixed skill-point reward: %v", err)
	}
	inventoryExpansion := questRewardReference{IsBasicReward: true, InventorySlots: 3}
	if err := validateQuestRewardReference(103, inventoryExpansion); err != nil {
		t.Fatalf("inventory-expansion reward: %v", err)
	}
	hwanLevel := questRewardReference{IsBasicReward: true, Hwan: 5}
	if err := validateQuestRewardReference(104, hwanLevel); err != nil {
		t.Fatalf("Hwan-level reward: %v", err)
	}
}

func TestValidateQuestRewardReferenceRejectsUnimplementedMechanisms(t *testing.T) {
	const chulsanRewardItemID = uint32(9228)
	tests := []questRewardReference{
		{IsBasicReward: true, IsCheckClass: true},
		{IsBasicReward: true, SelectionCount: 1},
		{IsBasicReward: true, IsItemReward: true, ItemRefID: chulsanRewardItemID, ItemQuantity: 0},
	}
	for index, reward := range tests {
		questID := QuestWeaponListID
		if index == 2 {
			questID = QuestChulsanAngerID
		}
		if err := validateQuestRewardReference(questID, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
			t.Fatalf("case %d error = %v, want ErrQuestReferenceUnsupported", index, err)
		}
	}
}

func TestValidateQuestRewardReferenceAcceptsMultipleStackedItems(t *testing.T) {
	reward := questRewardReference{IsBasicReward: true, IsItemReward: true, Items: []questRewardItemReference{
		{RefID: 1001, Quantity: 250},
		{RefID: 1002, Quantity: 2},
	}}
	if err := validateQuestRewardReference(105, reward); err != nil {
		t.Fatalf("multiple stacked item rewards: %v", err)
	}
	if got := ordinaryQuestRewardItems(reward); len(got) != 2 || got[0].Quantity != 250 {
		t.Fatalf("ordinary reward items = %#v", got)
	}
}

func TestValidateQuestRewardReferenceRejectsEveryUnconfirmedRewardField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*questRewardReference)
	}{
		{name: "academy points", mutate: func(r *questRewardReference) { r.AcademyPoints = 1 }},
		{name: "conditional", mutate: func(r *questRewardReference) { r.IsCheckCondition = true }},
		{name: "country", mutate: func(r *questRewardReference) { r.IsCheckCountry = true }},
		{name: "class", mutate: func(r *questRewardReference) { r.IsCheckClass = true }},
		{name: "gender", mutate: func(r *questRewardReference) { r.IsCheckGender = true }},
		{name: "parameter 1", mutate: func(r *questRewardReference) { r.Param1 = 1 }},
		{name: "parameter 2", mutate: func(r *questRewardReference) { r.Param2 = 1 }},
		{name: "parameter 3", mutate: func(r *questRewardReference) { r.Param3 = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reward := questRewardReference{IsBasicReward: true}
			tc.mutate(&reward)
			if err := validateQuestRewardReference(QuestWeaponListID, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
				t.Fatalf("error = %v, want ErrQuestReferenceUnsupported", err)
			}
		})
	}
}

func TestApplyFixedQuestSkillPointsUsesUint32WireBound(t *testing.T) {
	progress := ProgressionResult{SkillPoints: 10}
	if err := applyFixedQuestSkillPoints(&progress, 25); err != nil || progress.SkillPoints != 35 {
		t.Fatalf("fixed skill points = %d, error = %v", progress.SkillPoints, err)
	}
	progress.SkillPoints = int(^uint32(0))
	if err := applyFixedQuestSkillPoints(&progress, 1); !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("overflow error = %v, want ErrQuestStateConflict", err)
	}
}

func TestNamedPointRewardValidationRejectsUnknownChannels(t *testing.T) {
	for _, reward := range []questRewardReference{
		{NamedPointKey: "UNKNOWN", NamedPointAmount: 1},
		{NamedPointKey: "TEMPLE_ANUBIS"},
	} {
		if err := applyQuestNamedPoint(context.Background(), nil, 1, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
			t.Fatalf("applyQuestNamedPoint(%#v) error = %v", reward, err)
		}
	}
}

func TestValidateQuestRewardReferenceAcceptsMatchingTempleAcademyDuplicate(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward: true, NamedPointKey: "TEMPLE_ANUBIS", NamedPointAmount: 500, AcademyPoints: 500,
	}
	if err := validateQuestRewardReference(611, reward); err != nil {
		t.Fatalf("matching SQL/Lua Temple point evidence: %v", err)
	}
	reward.AcademyPoints = 499
	if err := validateQuestRewardReference(611, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
		t.Fatalf("mismatched SQL/Lua Temple point error = %v", err)
	}
	reward = questRewardReference{IsBasicReward: true, AcademyPoints: 500}
	if err := validateQuestRewardReference(611, reward); !errors.Is(err, ErrQuestReferenceUnsupported) {
		t.Fatalf("standalone Academy scalar error = %v", err)
	}
}

func TestApplyQuestInventoryExpansionUsesStockMaximum(t *testing.T) {
	size, expanded, err := applyQuestInventoryExpansion(109, 3)
	if err != nil || !expanded || size != 112 {
		t.Fatalf("inventory expansion = (%d, %t, %v), want (112, true, nil)", size, expanded, err)
	}
	if _, _, err = applyQuestInventoryExpansion(112, 1); !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("overflow error = %v, want ErrQuestStateConflict", err)
	}
	if size, expanded, err = applyQuestInventoryExpansion(0, 0); err != nil || expanded || size != BaseInventorySize {
		t.Fatalf("default capacity = (%d, %t, %v)", size, expanded, err)
	}
}

func TestQuestCompletionResultTracksCollectionSlotMutations(t *testing.T) {
	result := QuestCompletionResult{ChangedSlots: []byte{13, 14}}
	if len(result.ChangedSlots) != 2 || result.ChangedSlots[0] != 13 || result.ChangedSlots[1] != 14 {
		t.Fatalf("changed collection slots = %v", result.ChangedSlots)
	}
}

func TestSelectQuestRewardItemsUsesReferenceItemIdentity(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward:  true,
		IsItemReward:   true,
		SelectionCount: 2,
		Items: []questRewardItemReference{
			{RefID: 1001, Quantity: 1},
			{RefID: 1002, Quantity: 50},
			{RefID: 1003, Quantity: 1},
		},
	}
	selected, err := selectQuestRewardItems(88, reward, []uint32{1003, 1001}, 0)
	if err != nil {
		t.Fatalf("select reward items: %v", err)
	}
	if len(selected) != 2 || selected[0].RefID != 1003 || selected[1].RefID != 1001 {
		t.Fatalf("selected items = %#v", selected)
	}
}

func TestSelectQuestRewardItemsRejectsCountDuplicateAndForeignIdentity(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward:  true,
		IsItemReward:   true,
		SelectionCount: 1,
		Items: []questRewardItemReference{
			{RefID: 1001, Quantity: 1},
			{RefID: 1002, Quantity: 1},
		},
	}
	tests := [][]uint32{{}, {1001, 1002}, {9999}}
	for _, entries := range tests {
		if _, err := selectQuestRewardItems(88, reward, entries, 0); !errors.Is(err, ErrQuestStateConflict) {
			t.Fatalf("entries %v error = %v, want ErrQuestStateConflict", entries, err)
		}
	}
	reward.SelectionCount = 2
	if _, err := selectQuestRewardItems(88, reward, []uint32{1001, 1001}, 0); !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("duplicate selection error = %v, want ErrQuestStateConflict", err)
	}
}

func TestSelectQuestRewardItemsRejectsAmbiguousReferenceIdentity(t *testing.T) {
	reward := questRewardReference{
		IsBasicReward:  true,
		IsItemReward:   true,
		SelectionCount: 1,
		Items: []questRewardItemReference{
			{RefID: 1001, Quantity: 1},
			{RefID: 1001, Quantity: 2},
		},
	}
	if _, err := selectQuestRewardItems(88, reward, []uint32{1001}, 0); !errors.Is(err, ErrQuestReferenceUnsupported) {
		t.Fatalf("ambiguous selection error = %v, want ErrQuestReferenceUnsupported", err)
	}
}

func TestSelectQuestRewardItemsEnforcesCountryFilteredCatalog(t *testing.T) {
	previous := RefItems
	RefItems = map[uint32]RefItem{
		1001: {RefObject: RefObject{Country: 0}},
		1002: {RefObject: RefObject{Country: 1}},
		1003: {RefObject: RefObject{Country: 3}},
	}
	t.Cleanup(func() { RefItems = previous })
	reward := questRewardReference{IsBasicReward: true, IsItemReward: true, IsCheckCondition: true, IsCheckCountry: true, SelectionCount: 1, Items: []questRewardItemReference{
		{RefID: 1001, Quantity: 1}, {RefID: 1002, Quantity: 1}, {RefID: 1003, Quantity: 1},
	}}
	if _, err := selectQuestRewardItems(9, reward, []uint32{1002}, 0); !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("foreign-country selection error = %v, want ErrQuestStateConflict", err)
	}
	selected, err := selectQuestRewardItems(9, reward, []uint32{1003}, 0)
	if err != nil || len(selected) != 1 || selected[0].RefID != 1003 {
		t.Fatalf("universal-country selection = %#v, error = %v", selected, err)
	}
}

func TestConditionalTypeOneRewardUsesSingleEffectiveSelection(t *testing.T) {
	reward := questRewardReference{IsBasicReward: true, IsItemReward: true, IsCheckCondition: true, ItemRewardType: 1, Items: []questRewardItemReference{
		{RefID: 10420, Quantity: 1}, {RefID: 10421, Quantity: 1},
	}}
	if got := questRewardSelectionCount(reward); got != 1 {
		t.Fatalf("effective selection count = %d, want 1", got)
	}
	selected, err := selectQuestRewardItems(167, reward, []uint32{10421}, 1)
	if err != nil || len(selected) != 1 || selected[0].RefID != 10421 {
		t.Fatalf("conditional selection = %#v, error = %v", selected, err)
	}
}

func TestSelectQuestRewardDoesNotCoerceEventRewardIDIntoQuestID(t *testing.T) {
	repository := &SQLQuestRuntimeRepository{}
	_, err := repository.SelectQuestReward(context.Background(), 42, 10000, []uint32{7})
	if !errors.Is(err, ErrQuestStateConflict) {
		t.Fatalf("event reward selection error = %v, want ErrQuestStateConflict", err)
	}
}

func TestAdvanceQuestObjectiveSaturatesAndUsesConfirmedTransitions(t *testing.T) {
	quest := CharacterQuest{QuestID: 10, Status: QuestActive, Data1: 4}
	progressed, changed, err := advanceQuestObjective(quest, 10, 3)
	if err != nil || !changed || progressed.Data1 != 7 || progressed.Status != QuestProgressed {
		t.Fatalf("progress = (%+v, %v, %v)", progressed, changed, err)
	}
	ready, changed, err := advanceQuestObjective(progressed, 10, ^uint64(0))
	if err != nil || !changed || ready.Data1 != 10 || ready.Status != QuestRewardReady {
		t.Fatalf("completion = (%+v, %v, %v)", ready, changed, err)
	}
	if unchanged, changed, err := advanceQuestObjective(ready, 10, 1); err != nil || changed || unchanged != ready {
		t.Fatalf("completed objective changed = (%+v, %v, %v)", unchanged, changed, err)
	}
}

func TestAdvanceQuestObjectiveRejectsInvalidSourceState(t *testing.T) {
	quest := CharacterQuest{QuestID: 10, Status: QuestInactive}
	if _, changed, err := advanceQuestObjective(quest, 1, 1); err == nil || changed {
		t.Fatalf("inactive advance = (changed=%v, err=%v)", changed, err)
	}
}

func TestQuestInventoryContainsAggregatesStacks(t *testing.T) {
	items := map[byte]questInventoryItem{
		13: {refID: 100, stack: 2},
		14: {refID: 100, stack: 3},
		15: {refID: 200, stack: 1},
	}
	if !questInventoryContains(items, map[uint32]uint64{100: 5, 200: 1}) {
		t.Fatal("complete delivery inventory was rejected")
	}
	if questInventoryContains(items, map[uint32]uint64{100: 6}) {
		t.Fatal("insufficient delivery inventory was accepted")
	}
}
