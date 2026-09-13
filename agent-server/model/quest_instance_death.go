package model

import (
	"math"
	"sort"
	"strings"
)

type NativeQuestInstanceDeathPlan struct {
	Scope      GameWorldScope
	QuestID    int
	Step       uint16
	TalkToken  string
	MemberUIDs []uint32
	Variables  QuestInstanceVariableMutation
}

type NativeQuestInstanceRelocation struct {
	RegionID int16
	X        float32
	Y        float32
	Z        float32
	Heading  float32
}

type NativeBaekryeongStageDeathPlan struct {
	Scope      GameWorldScope
	QuestID    int
	Stage      int64
	TalkToken  string
	MemberUIDs []uint32
	Variables  QuestInstanceVariableMutation
}

var nativeBaekryeongStageDeathRules = map[string]string{
	"MOB_QT_TQ_FLAMEMASTER": "SN_TALK_QNO_TQ_BAEKRYEONG_17_08",
	"MOB_QT_TQ_BLACKSNAKE":  "SN_TALK_QNO_TQ_BAEKRYEONG_17_11",
}

// PrepareNativeBaekryeongStageDeath mirrors the first two auxiliary OnDead
// callbacks. Unlike the final White Snake callback, these mutate world state
// immediately and let the world tick perform the delayed cross-world move.
func (r *QuestInstanceRegistry) PrepareNativeBaekryeongStageDeath(scope GameWorldScope, deadMobCode string, elapsedTime int64) (NativeBaekryeongStageDeathPlan, bool, error) {
	if r == nil || !scope.ValidInstance() || elapsedTime < 0 {
		return NativeBaekryeongStageDeathPlan{}, false, ErrQuestInstanceInvalid
	}
	talkToken, authored := nativeBaekryeongStageDeathRules[deadMobCode]
	if !authored {
		return NativeBaekryeongStageDeathPlan{}, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return NativeBaekryeongStageDeathPlan{}, false, ErrQuestInstanceMissing
	}
	if state.questID != 311 {
		return NativeBaekryeongStageDeathPlan{}, false, nil
	}
	currentKey := questInstanceVariableName(311, "CurrentUnique")
	wantCurrent := int64(0)
	if deadMobCode == "MOB_QT_TQ_BLACKSNAKE" {
		wantCurrent = 1
	}
	if state.variables[currentKey] != wantCurrent {
		return NativeBaekryeongStageDeathPlan{}, false, nil
	}
	members := make([]uint32, 0, len(state.members))
	for uid := range state.members {
		members = append(members, uid)
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	mutation := QuestInstanceVariableMutation{
		Set: map[string]int64{
			questInstanceVariableName(311, "Step"):        1,
			questInstanceVariableName(311, "MobKillTime"): elapsedTime,
		},
		Increment: []string{currentKey, questInstanceVariableName(311, "DungeonStage")},
	}
	return NativeBaekryeongStageDeathPlan{Scope: scope, QuestID: 311, Stage: wantCurrent + 1, TalkToken: talkToken, MemberUIDs: members, Variables: mutation}, true, nil
}

func (r *QuestInstanceRegistry) CommitNativeBaekryeongStageDeath(plan NativeBaekryeongStageDeathPlan) (map[string]int64, error) {
	if plan.QuestID != 311 || !plan.Scope.ValidInstance() || plan.Stage < 1 || plan.Stage > 2 {
		return nil, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[plan.Scope]
	if state == nil {
		return nil, ErrQuestInstanceMissing
	}
	if state.questID != 311 {
		return nil, ErrQuestInstanceInvalid
	}
	return applyQuestInstanceVariables(state, plan.Variables)
}

type nativeQuestInstanceDeathRule struct {
	mobCode       string
	talkToken     string
	setZero       []string
	increment     []string
	timerFromTime bool
}

var nativeQuestInstanceDeathRules = map[int]nativeQuestInstanceDeathRule{
	141: {mobCode: "MOB_QT_01_TAHOMET", talkToken: "SN_TALK_QNO_RM_OLDWOMAN_6_12", timerFromTime: true},
	208: {mobCode: "MOB_QT_01_IVY", talkToken: "SN_TALK_QNO_EU_IVY_3_21", setZero: []string{"ElapsedTime"}},
	251: {mobCode: "MOB_QT_01_HUNLANCER", talkToken: "SN_TALK_QNO_CA_GORIA_7_15", setZero: []string{"ElapsedTime"}},
	287: {mobCode: "MOB_QT_TQ_SNAKEGENERAL4", talkToken: "SN_TALK_QNO_TQ_ORNAMENT_4_16", setZero: []string{"ElapsedTime", "MobKillTime"}},
	276: {mobCode: "MOB_QT_TQ_TOMBGENERAL4", talkToken: "SN_TALK_QNO_TQ_STONEBOARD_4_18", setZero: []string{"ElapsedTime", "MobKillTime"}},
	311: {mobCode: "MOB_QT_TQ_WHITESNAKE", talkToken: "SN_TALK_QNO_TQ_BAEKRYEONG_17_14", setZero: []string{"MobKillTime"}, increment: []string{"DungeonStage"}, timerFromTime: true},
}

// PrepareNativeQuestInstanceDeath resolves the six auxiliary OnDead handlers
// recovered from the pinned GameServer. It does not mutate variables: the
// caller must first submit the coordinated INS_QUEST_TELEPORT action and only
// then commit the plan, matching the native success ordering.
func (r *QuestInstanceRegistry) PrepareNativeQuestInstanceDeath(scope GameWorldScope, questID int, deadMobCode string, elapsedTime int64) (NativeQuestInstanceDeathPlan, bool, error) {
	if r == nil || !scope.ValidInstance() || elapsedTime < 0 {
		return NativeQuestInstanceDeathPlan{}, false, ErrQuestInstanceInvalid
	}
	rule, authored := nativeQuestInstanceDeathRules[questID]
	if !authored || rule.mobCode != deadMobCode {
		return NativeQuestInstanceDeathPlan{}, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[scope]
	if state == nil {
		return NativeQuestInstanceDeathPlan{}, false, ErrQuestInstanceMissing
	}
	if state.questID != questID {
		return NativeQuestInstanceDeathPlan{}, false, nil
	}
	set := make(map[string]int64, len(rule.setZero)+1)
	set[questInstanceVariableName(questID, "Step")] = 3
	for _, suffix := range rule.setZero {
		set[questInstanceVariableName(questID, suffix)] = 0
	}
	timer := int64(60000)
	if rule.timerFromTime {
		if elapsedTime > math.MaxInt64-timer {
			return NativeQuestInstanceDeathPlan{}, false, ErrQuestInstanceInvalid
		}
		timer += elapsedTime
	}
	set[questInstanceVariableName(questID, "EVENT_TIMER")] = timer
	var increments []string
	if len(rule.increment) != 0 {
		increments = make([]string, len(rule.increment))
	}
	for index, suffix := range rule.increment {
		increments[index] = questInstanceVariableName(questID, suffix)
	}
	members := make([]uint32, 0, len(state.members))
	for uid := range state.members {
		members = append(members, uid)
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	return NativeQuestInstanceDeathPlan{
		Scope: scope, QuestID: questID, Step: 3, TalkToken: rule.talkToken, MemberUIDs: members,
		Variables: QuestInstanceVariableMutation{Set: set, Increment: increments},
	}, true, nil
}

func (r *QuestInstanceRegistry) CommitNativeQuestInstanceDeath(plan NativeQuestInstanceDeathPlan) (map[string]int64, error) {
	if plan.QuestID <= 0 || !plan.Scope.ValidInstance() {
		return nil, ErrQuestInstanceInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.byScope[plan.Scope]
	if state == nil {
		return nil, ErrQuestInstanceMissing
	}
	if state.questID != plan.QuestID {
		return nil, ErrQuestInstanceInvalid
	}
	return applyQuestInstanceVariables(state, plan.Variables)
}

func BuildNativeQuestInstanceRelocation(regionID int16, x, y, z float32, roll func() int) (NativeQuestInstanceRelocation, error) {
	if regionID == 0 || roll == nil {
		return NativeQuestInstanceRelocation{}, ErrQuestInstanceInvalid
	}
	next := func() (float32, error) {
		value := roll()
		if value < 0 || value > 32767 {
			return 0, ErrQuestInstanceInvalid
		}
		return float32(value) / 32767, nil
	}
	xRoll, err := next()
	if err != nil {
		return NativeQuestInstanceRelocation{}, err
	}
	zRoll, err := next()
	if err != nil {
		return NativeQuestInstanceRelocation{}, err
	}
	headingRoll, err := next()
	if err != nil {
		return NativeQuestInstanceRelocation{}, err
	}
	return NativeQuestInstanceRelocation{RegionID: regionID, X: x + 20 + 80*xRoll, Y: y, Z: z + 20 + 80*zRoll, Heading: 360 * headingRoll}, nil
}

func questInstanceVariableName(questID int, suffix string) string {
	prefixes := map[int]string{141: "QNO_RM_OLDWOMAN_6", 208: "QNO_EU_IVY_3", 251: "QNO_CA_GORIA_7", 287: "QNO_TQ_ORNAMENT_4", 276: "QNO_TQ_STONEBOARD_4", 311: "QNO_TQ_BAEKRYOUNG_17"}
	return prefixes[questID] + "_" + suffix
}

func NativeQuestInstanceElapsedTimeKey(questID int) string {
	return questInstanceVariableName(questID, "ElapsedTime")
}

func applyQuestInstanceVariables(state *questInstanceState, mutation QuestInstanceVariableMutation) (map[string]int64, error) {
	for key := range mutation.Set {
		if strings.TrimSpace(key) == "" {
			return nil, ErrQuestInstanceInvalid
		}
	}
	for _, key := range mutation.Increment {
		value := state.variables[key]
		if setValue, exists := mutation.Set[key]; exists {
			value = setValue
		}
		if strings.TrimSpace(key) == "" || value == math.MaxInt64 {
			return nil, ErrQuestInstanceInvalid
		}
	}
	for key, value := range mutation.Set {
		state.variables[key] = value
	}
	for _, key := range mutation.Increment {
		state.variables[key]++
	}
	return snapshotQuestInstanceVariables(state), nil
}
