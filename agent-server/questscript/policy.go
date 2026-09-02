package questscript

import "fmt"

type CompletionPolicy struct {
	QuestCodeName      string
	Variant            int
	MissionCompleteNum uint32
	AchievementLimit   *uint32
	DivideType         *uint32
	RealTime           bool
	DayLimit           *uint32
	DayOrNight         *uint32
	LimitTime          *uint32
	HwanLevel          *uint32
	Parameters         []Call
}

type JobStartCondition struct {
	QuestCodeName  string
	Variant        int
	ConditionIndex uint32
	Parameter0     uint32
	JobStateKey    string
	Parameter2     uint32
	Parameters     []Value
}

type RaceStartCondition struct {
	QuestCodeName string
	Variant       int
	ConditionKey  string
	CountryKey    string
	Parameters    []Value
}

const (
	PointRewardSkill = "skill_point"
	PointRewardNamed = "named_point"
)

type PointReward struct {
	QuestCodeName string
	Variant       int
	RewardIndex   uint32
	Kind          string
	Amount        uint32
	PointKey      string
	Parameters    []Value
}

func CompletionPolicies(definitions []Definition) ([]CompletionPolicy, error) {
	policies := make([]CompletionPolicy, 0)
	for _, definition := range definitions {
		var policy CompletionPolicy
		policy.QuestCodeName = definition.CodeName
		policy.Variant = definition.Variant
		seen := make(map[string]bool)
		for _, call := range definition.Declarations {
			if !isCompletionPolicyCall(call.Function) {
				continue
			}
			if seen[call.Function] {
				return nil, fmt.Errorf("%w: %s variant %d repeats %s", ErrInvalidChunk, definition.CodeName, definition.Variant, call.Function)
			}
			seen[call.Function] = true
			policy.Parameters = append(policy.Parameters, call)
			switch call.Function {
			case "LuaSetRealTime":
				if len(call.Args) != 0 {
					return nil, malformedPolicy(definition, call.Function)
				}
				policy.RealTime = true
			default:
				if len(call.Args) != 1 {
					return nil, malformedPolicy(definition, call.Function)
				}
				value, ok := unsignedInteger(call.Args[0])
				if !ok {
					return nil, malformedPolicy(definition, call.Function)
				}
				switch call.Function {
				case "LuaSetMissionCompleteNum":
					policy.MissionCompleteNum = value
				case "LuaSetAchievementLimit":
					policy.AchievementLimit = uint32Pointer(value)
				case "LuaSetDivideType":
					policy.DivideType = uint32Pointer(value)
				case "LuaStartDayLimit":
					policy.DayLimit = uint32Pointer(value)
				case "LuaSetDayOrNight":
					policy.DayOrNight = uint32Pointer(value)
				case "LuaSetLimitTimeStartCondition":
					policy.LimitTime = uint32Pointer(value)
				case "LuaSetHwanLevel":
					policy.HwanLevel = uint32Pointer(value)
				}
			}
		}
		if !seen["LuaSetMissionCompleteNum"] {
			continue
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func JobStartConditions(definitions []Definition) ([]JobStartCondition, error) {
	conditions := make([]JobStartCondition, 0)
	for _, definition := range definitions {
		index := uint32(0)
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetJobTypeStartCondition" {
				continue
			}
			if len(call.Args) != 3 || call.Args[1].Kind != ValueGlobal || call.Args[1].String == "" {
				return nil, malformedPolicy(definition, call.Function)
			}
			parameter0, ok0 := unsignedInteger(call.Args[0])
			parameter2, ok2 := unsignedInteger(call.Args[2])
			if !ok0 || !ok2 {
				return nil, malformedPolicy(definition, call.Function)
			}
			conditions = append(conditions, JobStartCondition{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, ConditionIndex: index,
				Parameter0: parameter0, JobStateKey: call.Args[1].String, Parameter2: parameter2,
				Parameters: append([]Value(nil), call.Args...),
			})
			index++
		}
	}
	return conditions, nil
}

func RaceStartConditions(definitions []Definition) ([]RaceStartCondition, error) {
	conditions := make([]RaceStartCondition, 0)
	for _, definition := range definitions {
		seen := false
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetRaceStartCodition" {
				continue
			}
			if seen || len(call.Args) != 2 || !globalValue(call.Args[0], "QSC_RACE") ||
				call.Args[1].Kind != ValueGlobal || call.Args[1].String == "" {
				return nil, malformedPolicy(definition, call.Function)
			}
			seen = true
			conditions = append(conditions, RaceStartCondition{
				QuestCodeName: definition.CodeName, Variant: definition.Variant,
				ConditionKey: call.Args[0].String, CountryKey: call.Args[1].String,
				Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return conditions, nil
}

func PointRewards(definitions []Definition) ([]PointReward, error) {
	rewards := make([]PointReward, 0)
	for _, definition := range definitions {
		index := uint32(0)
		for _, call := range definition.Declarations {
			reward := PointReward{QuestCodeName: definition.CodeName, Variant: definition.Variant, RewardIndex: index}
			switch call.Function {
			case "LuaSetAchievedSkillPont":
				if len(call.Args) != 1 {
					return nil, malformedPolicy(definition, call.Function)
				}
				reward.Kind = PointRewardSkill
			case "LuaSetAchievedPoint":
				if len(call.Args) != 2 || call.Args[1].Kind != ValueString || call.Args[1].String == "" {
					return nil, malformedPolicy(definition, call.Function)
				}
				reward.Kind = PointRewardNamed
				reward.PointKey = call.Args[1].String
			default:
				continue
			}
			amount, ok := unsignedInteger(call.Args[0])
			if !ok {
				return nil, malformedPolicy(definition, call.Function)
			}
			reward.Amount = amount
			reward.Parameters = append([]Value(nil), call.Args...)
			rewards = append(rewards, reward)
			index++
		}
	}
	return rewards, nil
}

func isCompletionPolicyCall(name string) bool {
	switch name {
	case "LuaSetMissionCompleteNum", "LuaSetAchievementLimit", "LuaSetDivideType", "LuaSetRealTime",
		"LuaStartDayLimit", "LuaSetDayOrNight", "LuaSetLimitTimeStartCondition", "LuaSetHwanLevel":
		return true
	default:
		return false
	}
}

func malformedPolicy(definition Definition, function string) error {
	return fmt.Errorf("%w: %s variant %d has malformed %s declaration", ErrInvalidChunk, definition.CodeName, definition.Variant, function)
}

func uint32Pointer(value uint32) *uint32 {
	return &value
}
