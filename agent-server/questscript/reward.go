package questscript

import (
	"fmt"
	"math"
)

type RewardItem struct {
	RewardType uint32
	ItemCode   string
	Quantity   uint32
}

type RewardDeclaration struct {
	QuestCodeName   string
	Variant         int
	Experience      uint64
	SkillExperience uint64
	Gold            uint64
	Items           []RewardItem
	Parameters      []Value
}

func RewardDeclarations(definitions []Definition) ([]RewardDeclaration, error) {
	rewards := make([]RewardDeclaration, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetAchievedItem" {
				continue
			}
			if len(call.Args) < 4 {
				return nil, fmt.Errorf("%w: %s variant %d has short reward declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			itemCount, ok0 := unsignedInteger(call.Args[0])
			experience, ok1 := unsignedInteger64(call.Args[1])
			skillExperience, ok2 := unsignedInteger64(call.Args[2])
			gold, ok3 := unsignedInteger64(call.Args[3])
			if !ok0 || !ok1 || !ok2 || !ok3 || (itemCount > 0 && len(call.Args) != 4+int(itemCount)*3) {
				return nil, fmt.Errorf("%w: %s variant %d has malformed reward declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			reward := RewardDeclaration{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, Experience: experience,
				SkillExperience: skillExperience, Gold: gold, Parameters: append([]Value(nil), call.Args...),
				Items: make([]RewardItem, 0, itemCount),
			}
			for index := uint32(0); index < itemCount; index++ {
				offset := 4 + int(index)*3
				rewardType, typeOK := unsignedInteger(call.Args[offset])
				quantity, quantityOK := unsignedInteger(call.Args[offset+2])
				if !typeOK || call.Args[offset+1].Kind != ValueString || call.Args[offset+1].String == "" || !quantityOK || quantity == 0 {
					return nil, fmt.Errorf("%w: %s variant %d has malformed reward item %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				reward.Items = append(reward.Items, RewardItem{RewardType: rewardType, ItemCode: call.Args[offset+1].String, Quantity: quantity})
			}
			rewards = append(rewards, reward)
		}
	}
	return rewards, nil
}

func unsignedInteger64(value Value) (uint64, bool) {
	if value.Kind != ValueNumber || value.Number < 0 || value.Number > math.MaxInt64 || math.Trunc(value.Number) != value.Number {
		return 0, false
	}
	return uint64(value.Number), true
}
