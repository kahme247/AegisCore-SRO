package service

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/utils"
)

type itemUseServiceRepository struct {
	characterID int64
	slot        byte
	itemType    uint16
	vitals      model.ItemUseVitals
	result      model.ItemUseResult
}

func (r *itemUseServiceRepository) UseItem(_ context.Context, characterID int64, slot byte, itemType uint16, vitals model.ItemUseVitals) (model.ItemUseResult, error) {
	r.characterID, r.slot, r.itemType, r.vitals = characterID, slot, itemType, vitals
	return r.result, nil
}

func (r *itemUseServiceRepository) UseResurrectionItem(context.Context, int64, model.ItemUseVitals) (model.ItemUseResult, error) {
	return model.ItemUseResult{}, model.ErrItemUseUnsupported
}

func TestItemUseServicePublishesCommittedRepositorySnapshot(t *testing.T) {
	inventory := model.Inventory{Items: map[byte]model.Item{}}
	repository := &itemUseServiceRepository{result: model.ItemUseResult{
		Inventory: inventory, HP: 120, MP: 80, SourceSlot: 14, NewAmount: 3,
	}}
	player := &model.Player{ID: 77, Level: 10}
	player.BaseStats.HP, player.BaseStats.MP = 50, 40
	player.BaseStats.Str, player.BaseStats.Int = 20, 15
	player.BonusStats.HP, player.BonusStats.MP = 102, 51

	result, err := NewItemUseService(repository).UseItem(context.Background(), player, 14, 0x08EC, 0, 0)
	if err != nil {
		t.Fatalf("UseItem() error = %v", err)
	}
	if repository.characterID != 77 || repository.slot != 14 || repository.itemType != 0x08EC {
		t.Fatalf("repository request = character %d slot %d type 0x%04X", repository.characterID, repository.slot, repository.itemType)
	}
	if repository.vitals.HP != 50 || repository.vitals.MP != 40 || repository.vitals.MaxHP != utils.BaseHPOrMP(10, 20)+102 || repository.vitals.MaxMP != utils.BaseHPOrMP(10, 15)+51 {
		t.Fatalf("repository vitals = %+v", repository.vitals)
	}
	if result.NewAmount != 3 || player.BaseStats.HP != 120 || player.BaseStats.MP != 80 || player.Inventory.Items == nil {
		t.Fatalf("committed result/player = %+v / HP=%d MP=%d inventory=%+v", result, player.BaseStats.HP, player.BaseStats.MP, player.Inventory)
	}
}

type lockedItemCureRepository struct {
	entered chan struct{}
	release chan struct{}
	result  model.ItemUseResult
}

func (r *lockedItemCureRepository) UseItem(context.Context, int64, byte, uint16, model.ItemUseVitals) (model.ItemUseResult, error) {
	close(r.entered)
	<-r.release
	return r.result, nil
}

func (r *lockedItemCureRepository) UseResurrectionItem(context.Context, int64, model.ItemUseVitals) (model.ItemUseResult, error) {
	return model.ItemUseResult{}, model.ErrItemUseUnsupported
}

func TestItemUseAppliesCommittedCureBeforeLaterCombatStatus(t *testing.T) {
	now := time.Unix(6_000, 0)
	effect := model.ItemCureEffect{
		Kind: model.ItemCureRandomStatus, StatusMask: model.CombatStatusBadEffectBit(model.CombatStatusDarkness),
		StatusGrade: 9, Chance: 100, Count: 1,
	}
	repository := &lockedItemCureRepository{
		entered: make(chan struct{}), release: make(chan struct{}),
		result: model.ItemUseResult{
			Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 600, MP: 250,
			SourceSlot: 14, ItemType: 0x10EC, ItemCure: &effect,
		},
	}
	service := NewItemUseService(repository)
	service.ConfigureItemCureRuntime(func() time.Time { return now }, func(int) int { return 0 })
	player := &model.Player{
		ID: 7, LifeState: model.Alive, Level: 10,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		Inventory: model.Inventory{Items: map[byte]model.Item{14: {StackSize: 1}}},
	}
	if !player.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusDarkness, Token: 1, Level: 9, ExpiresAt: now.Add(time.Minute),
	}, now) {
		t.Fatal("apply original darkness")
	}

	resultCh := make(chan model.ItemUseResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.UseItem(context.Background(), player, 14, 0x10EC, 0, 0)
		resultCh <- result
		errCh <- err
	}()
	<-repository.entered
	laterApplied := make(chan bool, 1)
	go func() {
		laterApplied <- player.ApplyCombatStatusAt(model.CombatStatus{
			Kind: model.CombatStatusDarkness, Token: 2, Level: 9, ExpiresAt: now.Add(2 * time.Minute),
		}, now)
	}()
	close(repository.release)

	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if !<-laterApplied {
		t.Fatal("later combat status was rejected")
	}
	if !result.ItemCureApplication.Changed || result.ItemCureApplication.Removed != 1 || !result.ItemCureAppliedAt.Equal(now) {
		t.Fatalf("committed cure result = %+v at %v", result.ItemCureApplication, result.ItemCureAppliedAt)
	}
	status, active := player.CombatStatus(model.CombatStatusDarkness, now)
	if !active || status.Token != 2 {
		t.Fatalf("post-commit darkness = %+v/%t, want later token 2", status, active)
	}
}
