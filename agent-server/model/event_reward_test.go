package model

import (
	"errors"
	"testing"
)

func TestValidateEventRewardSelectionUsesResolvedItemIdentity(t *testing.T) {
	reward := EventRewardReference{EventID: 10001, IsBasicReward: true, IsItemReward: true, SelectionCount: 2, Items: []EventRewardItemReference{{RefID: 7001, PayCount: 1}, {RefID: 7002, PayCount: 2}}}
	selected, err := ValidateEventRewardSelection(reward, []uint32{7002, 7001})
	if err != nil || len(selected) != 2 || selected[0].RefID != 7002 || selected[1].RefID != 7001 {
		t.Fatalf("selection = %#v, err = %v", selected, err)
	}
}

func TestValidateEventRewardSelectionRejectsCountDuplicateAndForeignID(t *testing.T) {
	reward := EventRewardReference{EventID: 10001, IsBasicReward: true, IsItemReward: true, SelectionCount: 1, Items: []EventRewardItemReference{{RefID: 7001, PayCount: 1}}}
	for _, entries := range [][]uint32{{}, {7001, 7001}, {7002}} {
		if _, err := ValidateEventRewardSelection(reward, entries); !errors.Is(err, ErrQuestStateConflict) {
			t.Errorf("entries %#v: expected state conflict, got %v", entries, err)
		}
	}
}

func TestValidateEventRewardSelectionKeepsEventCatalogSeparateFromQuestRewards(t *testing.T) {
	reward := EventRewardReference{EventID: 10001, IsBasicReward: true, IsItemReward: true, SelectionCount: 1, Param1: 1, Items: []EventRewardItemReference{{RefID: 7001, PayCount: 1}}}
	if _, err := ValidateEventRewardSelection(reward, []uint32{7001}); !errors.Is(err, ErrQuestReferenceUnsupported) {
		t.Fatalf("expected unsupported event mechanism, got %v", err)
	}
}

func TestValidateEventRewardSelectionRejectsUnconfirmedLegacyMechanisms(t *testing.T) {
	base := EventRewardReference{
		EventID: 10001, IsBasicReward: true, IsItemReward: true, SelectionCount: 1,
		Items: []EventRewardItemReference{{RefID: 7001, PayCount: 1}},
	}
	cases := []struct {
		name   string
		mutate func(*EventRewardReference)
	}{
		{name: "skill points", mutate: func(r *EventRewardReference) { r.SkillPoints = 1 }},
		{name: "hwan", mutate: func(r *EventRewardReference) { r.Hwan = 1 }},
		{name: "conditional", mutate: func(r *EventRewardReference) { r.IsCheckCondition = true }},
		{name: "country", mutate: func(r *EventRewardReference) { r.IsCheckCountry = true }},
		{name: "class", mutate: func(r *EventRewardReference) { r.IsCheckClass = true }},
		{name: "gender", mutate: func(r *EventRewardReference) { r.IsCheckGender = true }},
		{name: "parameter 1", mutate: func(r *EventRewardReference) { r.Param1 = 1 }},
		{name: "parameter 2", mutate: func(r *EventRewardReference) { r.Param2 = 1 }},
		{name: "parameter 3", mutate: func(r *EventRewardReference) { r.Param3 = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reward := base
			tc.mutate(&reward)
			if _, err := ValidateEventRewardSelection(reward, []uint32{7001}); !errors.Is(err, ErrQuestReferenceUnsupported) {
				t.Fatalf("expected unsupported mechanism, got %v", err)
			}
		})
	}
}
