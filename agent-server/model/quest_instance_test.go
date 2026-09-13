package model

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func questInstanceTestObject(uid uint32) *SRObject {
	object := &SRObject{UniqueID: uid}
	object.KnownObjectList = NewKnownObjectList(object)
	return object
}

func TestQuestInstanceRegistryTransitionsScopeWithoutLosingState(t *testing.T) {
	from, _ := NewGameWorldScope(25, 3)
	to, _ := NewGameWorldScope(26, 3)
	registry := NewQuestInstanceRegistry()
	owner := questInstanceTestObject(100)
	member := questInstanceTestObject(200)
	_, _ = registry.Create(from, 311, owner)
	_, _ = registry.Join(from, member)
	_, _ = registry.ApplyVariables(from, QuestInstanceVariableMutation{Set: map[string]int64{"stage": 1}})
	instance, err := registry.TransitionScope(from, to, map[uint32]ISRObject{100: owner, 200: member})
	if err != nil {
		t.Fatal(err)
	}
	if instance.Scope != to || !reflect.DeepEqual(instance.Members, []uint32{100, 200}) || owner.GetGameWorldID() != uint32(to) || member.GetGameWorldID() != uint32(to) {
		t.Fatalf("instance=%+v scopes=%X/%X", instance, owner.GetGameWorldID(), member.GetGameWorldID())
	}
	if _, err = registry.Instance(from); !errors.Is(err, ErrQuestInstanceMissing) {
		t.Fatalf("old scope err=%v", err)
	}
	variables, err := registry.Variables(to)
	if err != nil || variables["stage"] != 1 {
		t.Fatalf("variables=%#v err=%v", variables, err)
	}
}

func TestQuestInstanceRegistryExplicitLifecycle(t *testing.T) {
	scope, err := NewGameWorldScope(41, 3)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewQuestInstanceRegistry()
	owner := questInstanceTestObject(10)
	member := questInstanceTestObject(20)
	created, err := registry.Create(scope, 311, owner)
	if err != nil || created.OwnerUID != 10 || owner.GetGameWorldID() != uint32(scope) {
		t.Fatalf("create = %+v owner scope=%08X error=%v", created, owner.GetGameWorldID(), err)
	}
	joined, err := registry.Join(scope, member)
	if err != nil || len(joined.Members) != 2 || joined.Members[0] != 10 || joined.Members[1] != 20 {
		t.Fatalf("join = %+v error=%v", joined, err)
	}
	left, err := registry.Leave(scope, member)
	if err != nil || len(left.Members) != 1 || member.GetGameWorldID() != 0 {
		t.Fatalf("leave = %+v member scope=%08X error=%v", left, member.GetGameWorldID(), err)
	}
	if _, err = registry.Leave(scope, member); !errors.Is(err, ErrQuestInstanceNotMember) {
		t.Fatalf("duplicate leave error = %v", err)
	}
	evacuated, err := registry.Destroy(scope, map[uint32]ISRObject{10: owner})
	if err != nil || len(evacuated) != 1 || evacuated[0] != 10 || owner.GetGameWorldID() != 0 {
		t.Fatalf("destroy = %v owner scope=%08X error=%v", evacuated, owner.GetGameWorldID(), err)
	}
}

func TestQuestInstanceRegistryAdmitsMemberToOnlyOneConcurrentInstance(t *testing.T) {
	first, _ := NewGameWorldScope(41, 1)
	second, _ := NewGameWorldScope(42, 1)
	registry := NewQuestInstanceRegistry()
	if _, err := registry.Create(first, 141, questInstanceTestObject(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(second, 208, questInstanceTestObject(2)); err != nil {
		t.Fatal(err)
	}
	member := questInstanceTestObject(3)
	var successes int
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for _, scope := range []GameWorldScope{first, second} {
		wait.Add(1)
		go func(scope GameWorldScope) {
			defer wait.Done()
			if _, err := registry.Join(scope, member); err == nil {
				mutex.Lock()
				successes++
				mutex.Unlock()
			}
		}(scope)
	}
	wait.Wait()
	if successes != 1 {
		t.Fatalf("concurrent successful joins = %d, want 1", successes)
	}
}

func TestGameWorldScopeWireProjection(t *testing.T) {
	if GameWorldScope(0).WorldID() != 1 || GameWorldScope(0).LayerID() != 1 {
		t.Fatal("public scope does not project to stock 1/1")
	}
	scope, err := NewGameWorldScope(0x1234, 0x5678)
	if err != nil || scope.WorldID() != 0x1234 || scope.LayerID() != 0x5678 {
		t.Fatalf("scope = %08X world/layer=%04X/%04X error=%v", scope, scope.WorldID(), scope.LayerID(), err)
	}
	for _, pair := range [][2]uint16{{0, 1}, {1, 0}, {0xffff, 1}, {1, 0xffff}} {
		if _, err = NewGameWorldScope(pair[0], pair[1]); err == nil {
			t.Fatalf("accepted invalid scope %d/%d", pair[0], pair[1])
		}
	}
}

func TestQuestInstanceVariablesAreAtomicAndInstanceLocal(t *testing.T) {
	first, _ := NewGameWorldScope(41, 1)
	second, _ := NewGameWorldScope(42, 1)
	registry := NewQuestInstanceRegistry()
	if _, err := registry.Create(first, 311, questInstanceTestObject(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(second, 311, questInstanceTestObject(2)); err != nil {
		t.Fatal(err)
	}
	const increments = 100
	var wait sync.WaitGroup
	for index := 0; index < increments; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := registry.ApplyVariables(first, QuestInstanceVariableMutation{
				Increment: []string{"QNO_TQ_BAEKRYOUNG_17_DungeonStage"},
			}); err != nil {
				t.Errorf("increment: %v", err)
			}
		}()
	}
	wait.Wait()
	variables, err := registry.ApplyVariables(first, QuestInstanceVariableMutation{
		Set: map[string]int64{
			"QNO_TQ_BAEKRYOUNG_17_MobKillTime": 0,
			"QNO_TQ_BAEKRYOUNG_17_EVENT_TIMER": 60000,
		},
	})
	if err != nil || variables["QNO_TQ_BAEKRYOUNG_17_DungeonStage"] != increments {
		t.Fatalf("variables = %#v error=%v", variables, err)
	}
	other, err := registry.Variables(second)
	if err != nil || len(other) != 0 {
		t.Fatalf("second instance variables = %#v error=%v", other, err)
	}
	variables["QNO_TQ_BAEKRYOUNG_17_DungeonStage"] = -1
	fresh, _ := registry.Variables(first)
	if fresh["QNO_TQ_BAEKRYOUNG_17_DungeonStage"] != increments {
		t.Fatal("caller mutated authoritative instance variables")
	}
}
