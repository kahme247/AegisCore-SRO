package service

import (
	"math"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

const (
	nativeExceptionRoc        = "MOB_RM_ROC"
	nativeExceptionWhiteSnake = "MOB_TQ_WHITESNAKE"
	nativeExceptionBoneLord   = "MOB_TK_BONELORD"
	nativeExceptionReturn     = "ITEM_ETC_SCROLL_RETURN_02"
)

var nativeExceptionRecipes = [...]string{
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B",
	"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B",
}

type NativeDropExceptionContext struct {
	MonsterCode    string
	MonsterTypeID4 int
	ItemsByCode    map[string]uint32
}

// NativeDropExceptionPlan is the count contract recovered from
// SR_GameServer 0x00726020. The two equipment passes use the native generated
// equipment selector; recipes and return scrolls resolve fixed code names.
type NativeDropExceptionPlan struct {
	EnhancedEquipment int
	Equipment         int
	ReinforceRecipes  int
	ReturnScrolls     int
}

func rollNativeDropExceptionPlan(monsterCode string, monsterTypeID4 int, random DropRandom) NativeDropExceptionPlan {
	return NativeDropExceptionPlan{
		EnhancedEquipment: nativeEnhancedEquipmentCount(monsterCode, monsterTypeID4, random),
		Equipment:         nativeExceptionEquipmentCount(monsterCode, monsterTypeID4, random),
		ReinforceRecipes:  nativeExceptionRecipeCount(monsterCode, monsterTypeID4, random),
		ReturnScrolls:     nativeExceptionReturnCount(monsterCode, monsterTypeID4, random),
	}
}

func nativeEnhancedEquipmentCount(monsterCode string, monsterTypeID4 int, random DropRandom) int {
	count := random.Intn(4) + 5
	if monsterTypeID4 == 8 {
		count = random.Intn(2) + 1
	} else if monsterCode == nativeExceptionRoc {
		count = random.Intn(31) + 50
	}
	return count
}

func nativeExceptionEquipmentCount(monsterCode string, monsterTypeID4 int, random DropRandom) int {
	count := random.Intn(3)
	switch monsterCode {
	case nativeExceptionRoc:
		count = random.Intn(11) + 10
	case nativeExceptionWhiteSnake:
		count = random.Intn(3) + 1
	case nativeExceptionBoneLord:
		count = random.Intn(2)
	default:
		if monsterTypeID4 == 8 {
			count = 0
		}
	}
	return count
}

func nativeExceptionRecipeCount(monsterCode string, monsterTypeID4 int, random DropRandom) int {
	count := random.Intn(3)
	switch monsterCode {
	case nativeExceptionRoc:
		count = random.Intn(51) + 50
	case nativeExceptionWhiteSnake:
		count = random.Intn(3) + 1
	default:
		if monsterTypeID4 == 8 {
			count = random.Intn(2) + 1
		}
	}
	return count
}

func nativeExceptionReturnCount(monsterCode string, monsterTypeID4 int, random DropRandom) int {
	count := random.Intn(11) + 10
	if monsterTypeID4 == 8 {
		count = 0
	} else if monsterCode == nativeExceptionRoc {
		count = random.Intn(51) + 100
	}
	return count
}

func appendNativeDropExceptions(result *[]model.GroundItemSpec, context NativeDropExceptionContext, profiles *DropProfileCatalog, monsterLevel, country int, factor float64, random DropRandom) {
	for attempt, count := 0, nativeEnhancedEquipmentCount(context.MonsterCode, context.MonsterTypeID4, random); attempt < count; attempt++ {
		before := len(*result)
		appendProfileDrop(result, math.MaxInt, profiles, DropProfileEquip, monsterLevel, country, factor, 1, random)
		if len(*result) == before {
			continue
		}
		spec := &(*result)[len(*result)-1]
		maxMagicOptions := 12
		if ref, ok := model.RefItems[spec.ReferenceItemID]; ok {
			maxMagicOptions = ref.MaxMagicOptCount
		}
		if len(spec.MagicOptions) < maxMagicOptions {
			spec.MagicOptions = append(spec.MagicOptions, model.MagicOption{Type: 65, Value: 6, ParamType: 1852990832})
			spec.Data += uint32(float64(spec.Data) * 0.06)
		}
	}
	for attempt, count := 0, nativeExceptionEquipmentCount(context.MonsterCode, context.MonsterTypeID4, random); attempt < count; attempt++ {
		appendProfileDrop(result, math.MaxInt, profiles, DropProfileEquip, monsterLevel, country, factor, 1, random)
	}
	for attempt, count := 0, nativeExceptionRecipeCount(context.MonsterCode, context.MonsterTypeID4, random); attempt < count; attempt++ {
		code := nativeExceptionRecipes[random.Intn(len(nativeExceptionRecipes))]
		if refID := context.ItemsByCode[code]; refID != 0 {
			*result = append(*result, model.GroundItemSpec{ReferenceItemID: refID, StackCount: 1})
		}
	}
	if refID := context.ItemsByCode[nativeExceptionReturn]; refID != 0 {
		for emitted, count := 0, nativeExceptionReturnCount(context.MonsterCode, context.MonsterTypeID4, random); emitted < count; emitted++ {
			*result = append(*result, model.GroundItemSpec{ReferenceItemID: refID, StackCount: 1})
		}
	} else {
		_ = nativeExceptionReturnCount(context.MonsterCode, context.MonsterTypeID4, random)
	}
}
