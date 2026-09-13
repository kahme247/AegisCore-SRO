package service

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestNativeDropExceptionPlanPreservesDefaultAndOverrideDraws(t *testing.T) {
	tests := []struct {
		name, code string
		typeID4    int
		ints       []int
		want       NativeDropExceptionPlan
	}{
		{"default", "MOB_OTHER", 0, []int{0, 1, 2, 3}, NativeDropExceptionPlan{5, 1, 2, 13}},
		{"roc", nativeExceptionRoc, 0, []int{0, 30, 0, 10, 0, 50, 0, 50}, NativeDropExceptionPlan{80, 20, 100, 150}},
		{"white-snake", nativeExceptionWhiteSnake, 0, []int{0, 0, 2, 0, 2, 0}, NativeDropExceptionPlan{5, 3, 3, 10}},
		{"bone-lord", nativeExceptionBoneLord, 0, []int{0, 0, 1, 0, 0}, NativeDropExceptionPlan{5, 1, 0, 10}},
		{"type-8", "MOB_OTHER", 8, []int{0, 1, 2, 0, 1, 0}, NativeDropExceptionPlan{2, 0, 2, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := rollNativeDropExceptionPlan(test.code, test.typeID4, &scriptedDropRandom{ints: test.ints})
			if got != test.want {
				t.Fatalf("plan = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestNamedDropExceptionsFeedFixedItemsIntoCommonOutput(t *testing.T) {
	context := NativeDropExceptionContext{
		MonsterCode: nativeExceptionWhiteSnake,
		ItemsByCode: map[string]uint32{
			nativeExceptionRecipes[0]: 700,
			nativeExceptionReturn:     800,
		},
	}
	result := []model.GroundItemSpec{}
	// plan: enhanced=5, equipment=1, recipes=1, scrolls=10; the final zero
	// chooses the weapon recipe. Nil profiles make both equipment phases fail
	// without consuming additional random values.
	appendNativeDropExceptions(&result, context, nil, 100, 0, 1, &scriptedDropRandom{ints: []int{0, 0, 0, 0, 0, 0, 0}})
	if len(result) != 11 || result[0].ReferenceItemID != 700 {
		t.Fatalf("fixed exception output = %+v", result)
	}
	for _, spec := range result[1:] {
		if spec.ReferenceItemID != 800 || spec.StackCount != 1 {
			t.Fatalf("return-scroll output = %+v", result)
		}
	}
}
