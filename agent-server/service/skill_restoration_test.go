package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type skillRestorationRepositoryFake struct {
	skillCommand   model.SkillWithdrawCommand
	masteryCommand model.MasteryWithdrawCommand
	result         model.SkillRestorationResult
	err            error
}

func (f *skillRestorationRepositoryFake) WithdrawSkill(_ context.Context, _ int, command model.SkillWithdrawCommand) (model.SkillRestorationResult, error) {
	f.skillCommand = command
	return f.result, f.err
}

func (f *skillRestorationRepositoryFake) WithdrawMastery(_ context.Context, _ int, command model.MasteryWithdrawCommand) (model.SkillRestorationResult, error) {
	f.masteryCommand = command
	return f.result, f.err
}

func TestPlanSkillWithdrawalCalculatesFullRefundAndReplacement(t *testing.T) {
	catalog := restorationTestCatalog()
	item, _ := catalog.Item(900)
	command, err := planSkillWithdrawal(catalog, []model.CharacterSkill{{ID: 102, Enabled: 1}}, item, 102, 1)
	if err != nil {
		t.Fatalf("planSkillWithdrawal() error = %v", err)
	}
	if command.CurrentSkillID != 102 || command.ResultingSkillID != 100 || command.RequiredItemQuantity != 2 || command.RefundSkillPoints != 50 {
		t.Fatalf("command = %+v", command)
	}
}

func TestPlanSkillWithdrawalRejectsDependentSkill(t *testing.T) {
	catalog := restorationTestCatalog()
	item, _ := catalog.Item(900)
	_, err := planSkillWithdrawal(catalog, []model.CharacterSkill{{ID: 102, Enabled: 1}, {ID: 200, Enabled: 1}}, item, 102, 1)
	if !errors.Is(err, model.ErrSkillRestorationDependency) {
		t.Fatalf("planSkillWithdrawal() error = %v", err)
	}
}

func TestPlanMasteryWithdrawalCalculatesFullRefundAndDependencies(t *testing.T) {
	catalog := restorationTestCatalog()
	item, _ := catalog.Item(900)
	levels, err := model.LoadLevelTable(strings.NewReader("1 1 10 0 0 0\n2 1 20 0 0 0\n3 1 30 0 0 0\n"))
	if err != nil {
		t.Fatalf("LoadLevelTable() error = %v", err)
	}
	command, err := planMasteryWithdrawal(catalog, levels, nil, item, 257, 3, 1)
	if err != nil {
		t.Fatalf("planMasteryWithdrawal() error = %v", err)
	}
	if command.RequiredItemQuantity != 2 || command.RefundSkillPoints != 50 || command.ExpectedLevel != 3 || command.TargetLevel != 1 {
		t.Fatalf("command = %+v", command)
	}
	_, err = planMasteryWithdrawal(catalog, levels, []model.CharacterSkill{{ID: 200, Enabled: 1}}, item, 257, 3, 1)
	if !errors.Is(err, model.ErrSkillRestorationDependency) {
		t.Fatalf("dependent mastery withdrawal error = %v", err)
	}
}

func TestRestorationPlannerCalculatesPaidRefundPolicy(t *testing.T) {
	catalog := restorationTestCatalog()
	item, _ := catalog.Item(901)
	command, err := planSkillWithdrawal(catalog, []model.CharacterSkill{{ID: 102, Enabled: 1}}, item, 102, 1)
	if err != nil || !command.Paid || command.RefundSkillPoints != 40 {
		t.Fatalf("paid restoration command/error = %+v / %v", command, err)
	}
}

func TestRestorationGoldCostMatchesPinnedGameServerFormula(t *testing.T) {
	cost, err := restorationGoldCostForMinimum(28, []byte{10, 20})
	if err != nil {
		t.Fatalf("restorationGoldCostForMinimum() error = %v", err)
	}
	// Per level: GoldMin / 0.8 * 80 * (1 + masteryLevel / 10),
	// rounded by adding 0.5 before the binary's integer conversion.
	if cost != 5600+8400 {
		t.Fatalf("cost = %d, want 14000", cost)
	}
}

func TestSkillRestorationServiceCommitsOnlyRepositoryResult(t *testing.T) {
	catalog := restorationTestCatalog()
	repository := &skillRestorationRepositoryFake{result: model.SkillRestorationResult{ResultingSkillID: 100, SkillPoints: 55, ItemSlot: 13, ItemRemaining: 1}}
	player := &model.Player{
		ID: 7, SkillPoints: 5, Skills: map[uint32]byte{102: 1},
		Inventory: model.Inventory{Items: map[byte]model.Item{13: {StackSize: 3}}},
	}

	result, err := NewSkillRestorationService(repository, catalog, nil).WithdrawSkill(context.Background(), player, 900, 102, 1)
	if err != nil {
		t.Fatalf("WithdrawSkill() error = %v", err)
	}
	if result.ResultingSkillID != 100 || repository.skillCommand.RequiredItemQuantity != 2 || repository.skillCommand.RefundSkillPoints != 50 {
		t.Fatalf("result/command = %+v / %+v", result, repository.skillCommand)
	}
	if _, exists := player.Skills[102]; exists || player.Skills[100] != 1 || player.SkillPoints != 55 || player.Inventory.Items[13].StackSize != 1 {
		t.Fatalf("player state = skills:%v SP:%d inventory:%v", player.Skills, player.SkillPoints, player.Inventory.Items)
	}
}

func TestSkillRestorationServiceDoesNotMutateOnRepositoryFailure(t *testing.T) {
	catalog := restorationTestCatalog()
	repository := &skillRestorationRepositoryFake{err: model.ErrSkillRestorationConflict}
	player := &model.Player{ID: 7, SkillPoints: 5, Skills: map[uint32]byte{102: 1}}

	_, err := NewSkillRestorationService(repository, catalog, nil).WithdrawSkill(context.Background(), player, 900, 102, 1)
	if !errors.Is(err, model.ErrSkillRestorationConflict) || player.Skills[102] != 1 || player.SkillPoints != 5 {
		t.Fatalf("error/player = %v / skills:%v SP:%d", err, player.Skills, player.SkillPoints)
	}
}

func restorationTestCatalog() *model.ReferenceDataCatalog {
	return model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Items: map[uint32]model.RefItem{
			900: {RefObject: model.RefObject{ID: 900, CodeName: "ITEM_MALL_SKILL_RESTORATION_POTION"}},
			901: {RefObject: model.RefObject{ID: 901, CodeName: "ITEM_QSP_ALL_POTION_1_01"}},
		},
		Skills: map[uint32]model.RefSkill{
			100: {ID: 100, BasicOriginal: 100, BasicGroup: "LINE", BasicLevel: 1, RequiredSP: 10},
			101: {ID: 101, BasicOriginal: 100, BasicGroup: "LINE", BasicLevel: 2, RequiredSP: 20},
			102: {ID: 102, BasicOriginal: 100, BasicGroup: "LINE", BasicLevel: 3, RequiredSP: 30},
			200: {ID: 200, BasicOriginal: 200, BasicGroup: "DEPENDENT", BasicLevel: 1, RequiredSkill1: 101, RequiredLevel1: 2, Mastery1: 257, MasteryLevel1: 2},
		},
	})
}

func TestSkillWithdrawalDependenciesResolveGroupPrerequisiteBeforeCollidingID(t *testing.T) {
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		638:   {ID: 638, GroupID: 189, BasicGroup: "UNRELATED", BasicLevel: 18},
		9975:  {ID: 9975, GroupID: 638, BasicGroup: "WIZARD_SPIRIT", BasicLevel: 1},
		9978:  {ID: 9978, GroupID: 638, BasicGroup: "WIZARD_SPIRIT", BasicLevel: 4},
		10033: {ID: 10033, GroupID: 640, BasicGroup: "WIZARD_SKILL", BasicLevel: 1, RequiredSkill1: 638, RequiredLevel1: 4},
	}})

	dependencies := skillWithdrawalDependencies(catalog, model.RefSkill{ID: 9978, GroupID: 638, BasicGroup: "WIZARD_SPIRIT", BasicLevel: 4}, 3, 9978)
	if len(dependencies) != 1 || dependencies[0] != 10033 {
		t.Fatalf("dependencies = %v, want [10033]", dependencies)
	}
}
