package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
)

type skillLearningRepositoryFake struct {
	command model.SkillLearnCommand
	result  model.SkillLearnResult
	err     error
	calls   int
}

func (f *skillLearningRepositoryFake) LearnSkill(_ context.Context, _ int, command model.SkillLearnCommand) (model.SkillLearnResult, error) {
	f.calls++
	f.command = command
	return f.result, f.err
}

func TestSkillServiceUpgradesOnlyImmediateLevelAfterRequirements(t *testing.T) {
	const original = uint32(100)
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		100: {ID: 100, GroupID: 1, BasicGroup: "FIRE", BasicOriginal: original, BasicLevel: 1},
		101: {ID: 101, GroupID: 1, BasicGroup: "FIRE", BasicOriginal: original, BasicLevel: 2, Mastery1: 257, MasteryLevel1: 2, RequiredSkill1: 200, RequiredLevel1: 1, RequiredSP: 3, RequiredRace: 0},
		200: {ID: 200, GroupID: 2, BasicGroup: "PASSIVE", BasicOriginal: 200, BasicLevel: 1},
	}})
	repository := &skillLearningRepositoryFake{result: model.SkillLearnResult{Skill: model.CharacterSkill{ID: 101, Enabled: 1}, ReplacedSkillID: 100, SkillPoints: 7}}
	player := &model.Player{SRObject: model.SRObject{RefObjectID: 1907}, ID: 9, BaseStats: model.BaseStats{Str: 20, Int: 20}, SkillPoints: 10, Masteries: map[uint32]byte{257: 2}, Skills: map[uint32]byte{100: 1, 200: 1}}

	result, err := NewSkillService(repository, catalog).LearnSkill(context.Background(), player, 101)
	if err != nil {
		t.Fatalf("LearnSkill: %v", err)
	}
	if repository.calls != 1 || repository.command.ReplacedSkillID != 100 || repository.command.RequiredOwnedSkillIDs[0] != 200 {
		t.Fatalf("repository command = %+v", repository.command)
	}
	if result.Skill.ID != 101 || player.SkillPoints != 7 || player.Skills[101] != 1 {
		t.Fatalf("result/player = %+v / %+v", result, player)
	}
	if _, exists := player.Skills[100]; exists {
		t.Fatal("previous skill level remains in runtime state")
	}
}

func TestSkillServiceRejectsSkippedLevelWithoutMutation(t *testing.T) {
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		100: {ID: 100, BasicGroup: "FIRE", BasicOriginal: 100, BasicLevel: 1},
		102: {ID: 102, BasicGroup: "FIRE", BasicOriginal: 100, BasicLevel: 3, RequiredRace: 0},
	}})
	repository := &skillLearningRepositoryFake{}
	player := &model.Player{SRObject: model.SRObject{RefObjectID: 1907}, ID: 9, Skills: map[uint32]byte{100: 1}, Masteries: map[uint32]byte{}}

	_, err := NewSkillService(repository, catalog).LearnSkill(context.Background(), player, 102)
	if !errors.Is(err, model.ErrSkillPrerequisiteRequired) || repository.calls != 0 {
		t.Fatalf("LearnSkill error/calls = %v/%d", err, repository.calls)
	}
}

func TestSkillServiceEnforcesMasteryAttributesRaceAndSP(t *testing.T) {
	skill := model.RefSkill{ID: 100, BasicOriginal: 100, BasicLevel: 1, Mastery1: 257, MasteryLevel1: 5, RequiredStr: 20, RequiredInt: 20, RequiredSP: 3, RequiredRace: 0}
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{100: skill}})
	tests := []struct {
		name   string
		player *model.Player
		want   error
	}{
		{"race", &model.Player{SRObject: model.SRObject{RefObjectID: 14875}, ID: 1}, model.ErrSkillInvalid},
		{"mastery", &model.Player{SRObject: model.SRObject{RefObjectID: 1907}, ID: 1, Masteries: map[uint32]byte{257: 4}}, model.ErrSkillMasteryRequired},
		{"strength", &model.Player{SRObject: model.SRObject{RefObjectID: 1907}, ID: 1, Masteries: map[uint32]byte{257: 5}, BaseStats: model.BaseStats{Str: 19, Int: 20}}, model.ErrSkillStrengthRequired},
		{"intellect", &model.Player{SRObject: model.SRObject{RefObjectID: 1907}, ID: 1, Masteries: map[uint32]byte{257: 5}, BaseStats: model.BaseStats{Str: 20, Int: 19}}, model.ErrSkillIntellectRequired},
		{"SP", &model.Player{SRObject: model.SRObject{RefObjectID: 1907}, ID: 1, Masteries: map[uint32]byte{257: 5}, BaseStats: model.BaseStats{Str: 20, Int: 20}, SkillPoints: 2}, model.ErrSkillInsufficientSP},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSkillService(&skillLearningRepositoryFake{}, catalog).LearnSkill(context.Background(), test.player, 100)
			if !errors.Is(err, test.want) {
				t.Fatalf("LearnSkill error = %v, want %v", err, test.want)
			}
		})
	}
}
