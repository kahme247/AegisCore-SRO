package model

import (
	"reflect"
	"testing"
)

func TestNativeQuestInstanceDeathRulesProduceAuthoredPlans(t *testing.T) {
	tests := []struct {
		questID int
		mob     string
		talk    string
		wantSet map[string]int64
		wantInc []string
	}{
		{141, "MOB_QT_01_TAHOMET", "SN_TALK_QNO_RM_OLDWOMAN_6_12", map[string]int64{"QNO_RM_OLDWOMAN_6_Step": 3, "QNO_RM_OLDWOMAN_6_EVENT_TIMER": 72345}, nil},
		{208, "MOB_QT_01_IVY", "SN_TALK_QNO_EU_IVY_3_21", map[string]int64{"QNO_EU_IVY_3_Step": 3, "QNO_EU_IVY_3_ElapsedTime": 0, "QNO_EU_IVY_3_EVENT_TIMER": 60000}, nil},
		{251, "MOB_QT_01_HUNLANCER", "SN_TALK_QNO_CA_GORIA_7_15", map[string]int64{"QNO_CA_GORIA_7_Step": 3, "QNO_CA_GORIA_7_ElapsedTime": 0, "QNO_CA_GORIA_7_EVENT_TIMER": 60000}, nil},
		{287, "MOB_QT_TQ_SNAKEGENERAL4", "SN_TALK_QNO_TQ_ORNAMENT_4_16", map[string]int64{"QNO_TQ_ORNAMENT_4_Step": 3, "QNO_TQ_ORNAMENT_4_ElapsedTime": 0, "QNO_TQ_ORNAMENT_4_MobKillTime": 0, "QNO_TQ_ORNAMENT_4_EVENT_TIMER": 60000}, nil},
		{276, "MOB_QT_TQ_TOMBGENERAL4", "SN_TALK_QNO_TQ_STONEBOARD_4_18", map[string]int64{"QNO_TQ_STONEBOARD_4_Step": 3, "QNO_TQ_STONEBOARD_4_ElapsedTime": 0, "QNO_TQ_STONEBOARD_4_MobKillTime": 0, "QNO_TQ_STONEBOARD_4_EVENT_TIMER": 60000}, nil},
		{311, "MOB_QT_TQ_WHITESNAKE", "SN_TALK_QNO_TQ_BAEKRYEONG_17_14", map[string]int64{"QNO_TQ_BAEKRYOUNG_17_Step": 3, "QNO_TQ_BAEKRYOUNG_17_MobKillTime": 0, "QNO_TQ_BAEKRYOUNG_17_EVENT_TIMER": 72345}, []string{"QNO_TQ_BAEKRYOUNG_17_DungeonStage"}},
	}
	for _, test := range tests {
		t.Run(test.mob, func(t *testing.T) {
			scope, _ := NewGameWorldScope(uint16(test.questID), 1)
			registry := NewQuestInstanceRegistry()
			owner := questInstanceTestObject(20)
			member := questInstanceTestObject(10)
			if _, err := registry.Create(scope, test.questID, owner); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Join(scope, member); err != nil {
				t.Fatal(err)
			}
			plan, matched, err := registry.PrepareNativeQuestInstanceDeath(scope, test.questID, test.mob, 12345)
			if err != nil || !matched {
				t.Fatalf("matched=%t error=%v", matched, err)
			}
			if plan.Step != 3 || plan.TalkToken != test.talk || !reflect.DeepEqual(plan.MemberUIDs, []uint32{10, 20}) || !reflect.DeepEqual(plan.Variables.Set, test.wantSet) || !reflect.DeepEqual(plan.Variables.Increment, test.wantInc) {
				t.Fatalf("plan = %#v", plan)
			}
			if variables, _ := registry.Variables(scope); len(variables) != 0 {
				t.Fatal("prepare mutated variables before teleport submission")
			}
			variables, err := registry.CommitNativeQuestInstanceDeath(plan)
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range test.wantSet {
				if variables[key] != value {
					t.Fatalf("%s=%d want %d", key, variables[key], value)
				}
			}
			for _, key := range test.wantInc {
				if variables[key] != 1 {
					t.Fatalf("%s=%d want 1", key, variables[key])
				}
			}
		})
	}
}

func TestNativeQuestInstanceDeathRejectsWrongMobAndQuest(t *testing.T) {
	scope, _ := NewGameWorldScope(41, 1)
	registry := NewQuestInstanceRegistry()
	_, _ = registry.Create(scope, 141, questInstanceTestObject(1))
	for _, candidate := range []struct {
		quest int
		mob   string
	}{{141, "MOB_QT_01_IVY"}, {208, "MOB_QT_01_IVY"}, {999, "MOB_QT_01_TAHOMET"}} {
		if _, matched, err := registry.PrepareNativeQuestInstanceDeath(scope, candidate.quest, candidate.mob, 0); err != nil || matched {
			t.Fatalf("candidate=%+v matched=%t error=%v", candidate, matched, err)
		}
	}
}

func TestNativeQuestInstanceRelocationUsesIndependentLegacyRolls(t *testing.T) {
	rolls := []int{0, 32767, 16384}
	index := 0
	got, err := BuildNativeQuestInstanceRelocation(25000, 10, 20, 30, func() int { value := rolls[index]; index++; return value })
	if err != nil {
		t.Fatal(err)
	}
	if got.RegionID != 25000 || got.X != 30 || got.Y != 20 || got.Z != 130 || got.Heading < 180 || got.Heading > 180.01 {
		t.Fatalf("relocation = %+v", got)
	}
}

func TestBaekryeongIntermediateDeathsAdvanceAuthoredStageState(t *testing.T) {
	scope, _ := NewGameWorldScope(25, 4)
	registry := NewQuestInstanceRegistry()
	owner := questInstanceTestObject(20)
	member := questInstanceTestObject(10)
	_, _ = registry.Create(scope, 311, owner)
	_, _ = registry.Join(scope, member)
	for index, test := range []struct{ mob, talk string }{
		{"MOB_QT_TQ_FLAMEMASTER", "SN_TALK_QNO_TQ_BAEKRYEONG_17_08"},
		{"MOB_QT_TQ_BLACKSNAKE", "SN_TALK_QNO_TQ_BAEKRYEONG_17_11"},
	} {
		elapsed := int64(1000 + index*500)
		plan, matched, err := registry.PrepareNativeBaekryeongStageDeath(scope, test.mob, elapsed)
		if err != nil || !matched {
			t.Fatalf("stage %d matched=%t err=%v", index+1, matched, err)
		}
		if plan.Stage != int64(index+1) || plan.TalkToken != test.talk || !reflect.DeepEqual(plan.MemberUIDs, []uint32{10, 20}) {
			t.Fatalf("stage %d plan=%+v", index+1, plan)
		}
		variables, err := registry.CommitNativeBaekryeongStageDeath(plan)
		if err != nil {
			t.Fatal(err)
		}
		if variables[questInstanceVariableName(311, "Step")] != 1 || variables[questInstanceVariableName(311, "MobKillTime")] != elapsed || variables[questInstanceVariableName(311, "CurrentUnique")] != int64(index+1) || variables[questInstanceVariableName(311, "DungeonStage")] != int64(index+1) {
			t.Fatalf("stage %d variables=%#v", index+1, variables)
		}
	}
	if _, matched, err := registry.PrepareNativeBaekryeongStageDeath(scope, "MOB_QT_TQ_BLACKSNAKE", 2000); err != nil || matched {
		t.Fatalf("duplicate stage matched=%t err=%v", matched, err)
	}
}
