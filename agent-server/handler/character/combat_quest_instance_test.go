package character

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type nativeQuestInstanceTeleportFake struct {
	calls int
	plan  model.NativeQuestInstanceDeathPlan
}

func (f *nativeQuestInstanceTeleportFake) SubmitNativeQuestInstanceTeleport(registry *model.QuestInstanceRegistry, plan model.NativeQuestInstanceDeathPlan, source *model.Player) error {
	f.calls++
	f.plan = plan
	_, err := registry.CommitNativeQuestInstanceDeath(plan)
	return err
}

func TestCombatRoutesAuthoredInstanceBossDeathToCoordinatedTeleport(t *testing.T) {
	scope, _ := model.NewGameWorldScope(41, 2)
	player := &model.Player{SRObject: model.NewSRObject()}
	player.SetUniqueID(100)
	registry := model.NewQuestInstanceRegistry()
	if _, err := registry.Create(scope, 141, player); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApplyVariables(scope, model.QuestInstanceVariableMutation{Set: map[string]int64{model.NativeQuestInstanceElapsedTimeKey(141): 2500}}); err != nil {
		t.Fatal(err)
	}
	target := &model.NPC{SRObject: model.NewSRObject()}
	target.UniqueID = 200
	target.Name = "MOB_QT_01_TAHOMET"
	target.SetGameWorldID(uint32(scope))
	submitter := &nativeQuestInstanceTeleportFake{}
	handler := &CombatHandler{questInstances: registry, instanceTeleport: submitter}
	if err := handler.handleNativeQuestInstanceDeath(player, target); err != nil {
		t.Fatal(err)
	}
	if submitter.calls != 1 || submitter.plan.QuestID != 141 || submitter.plan.TalkToken != "SN_TALK_QNO_RM_OLDWOMAN_6_12" {
		t.Fatalf("calls=%d plan=%+v", submitter.calls, submitter.plan)
	}
	variables, _ := registry.Variables(scope)
	if variables["QNO_RM_OLDWOMAN_6_Step"] != 3 || variables["QNO_RM_OLDWOMAN_6_EVENT_TIMER"] != 62500 {
		t.Fatalf("variables=%#v", variables)
	}
}

func TestCombatIgnoresAuthoredBossOutsideKillerInstance(t *testing.T) {
	scope, _ := model.NewGameWorldScope(41, 2)
	player := &model.Player{SRObject: model.NewSRObject()}
	player.SetUniqueID(100)
	registry := model.NewQuestInstanceRegistry()
	_, _ = registry.Create(scope, 141, player)
	target := &model.NPC{SRObject: model.NewSRObject()}
	target.Name = "MOB_QT_01_TAHOMET"
	submitter := &nativeQuestInstanceTeleportFake{}
	handler := &CombatHandler{questInstances: registry, instanceTeleport: submitter}
	if err := handler.handleNativeQuestInstanceDeath(player, target); err != nil {
		t.Fatal(err)
	}
	if submitter.calls != 0 {
		t.Fatalf("submit calls=%d", submitter.calls)
	}
}

func TestCombatCommitsBaekryeongIntermediateDeathWithoutFinalTeleport(t *testing.T) {
	scope, _ := model.NewGameWorldScope(25, 2)
	player := &model.Player{SRObject: model.NewSRObject()}
	player.SetUniqueID(100)
	registry := model.NewQuestInstanceRegistry()
	_, _ = registry.Create(scope, 311, player)
	_, _ = registry.ApplyVariables(scope, model.QuestInstanceVariableMutation{Set: map[string]int64{model.NativeQuestInstanceElapsedTimeKey(311): 9000}})
	target := &model.NPC{SRObject: model.NewSRObject()}
	target.Name = "MOB_QT_TQ_FLAMEMASTER"
	target.SetGameWorldID(uint32(scope))
	submitter := &nativeQuestInstanceTeleportFake{}
	handler := &CombatHandler{questInstances: registry, instanceTeleport: submitter}
	if err := handler.handleNativeQuestInstanceDeath(player, target); err != nil {
		t.Fatal(err)
	}
	if submitter.calls != 0 {
		t.Fatalf("intermediate stage submitted final teleport %d times", submitter.calls)
	}
	variables, _ := registry.Variables(scope)
	if variables["QNO_TQ_BAEKRYOUNG_17_Step"] != 1 || variables["QNO_TQ_BAEKRYOUNG_17_MobKillTime"] != 9000 || variables["QNO_TQ_BAEKRYOUNG_17_CurrentUnique"] != 1 || variables["QNO_TQ_BAEKRYOUNG_17_DungeonStage"] != 1 {
		t.Fatalf("variables=%#v", variables)
	}
}
