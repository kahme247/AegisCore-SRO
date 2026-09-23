package service

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/utils"
)

type itemUseServiceRepository struct {
	characterID       int64
	slot              byte
	itemType          uint16
	vitals            model.ItemUseVitals
	result            model.ItemUseResult
	recoveryCurrentHP int
	recoveryCurrentMP int
	recoveryHealedHP  int
	recoveryHealedMP  int
}

func (r *itemUseServiceRepository) UseItem(_ context.Context, characterID int64, slot byte, itemType uint16, vitals model.ItemUseVitals) (model.ItemUseResult, error) {
	r.characterID, r.slot, r.itemType, r.vitals = characterID, slot, itemType, vitals
	return r.result, nil
}

func (r *itemUseServiceRepository) UseResurrectionItem(context.Context, int64, model.ItemUseVitals) (model.ItemUseResult, error) {
	return model.ItemUseResult{}, model.ErrItemUseUnsupported
}

func (r *itemUseServiceRepository) PersistItemBuffRecovery(_ context.Context, _ int64, currentHP, currentMP, healedHP, healedMP int) (bool, error) {
	r.recoveryCurrentHP, r.recoveryCurrentMP = currentHP, currentMP
	r.recoveryHealedHP, r.recoveryHealedMP = healedHP, healedMP
	return true, nil
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

func TestItemUseServicePassesZombieIntoAtomicPotionTransaction(t *testing.T) {
	now := time.Unix(100, 0)
	repository := &itemUseServiceRepository{result: model.ItemUseResult{
		Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 0, MP: 60,
	}}
	player := &model.Player{
		ID: 77, Level: 1, LifeState: model.Alive,
		BaseStats: model.BaseStats{HP: 30, MP: 20, Str: 20, Int: 20},
	}
	player.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusZombie, Token: 1, ExpiresAt: now.Add(time.Minute),
	}, now)
	service := NewItemUseService(repository)
	service.ConfigureItemCureRuntime(func() time.Time { return now }, nil)

	if _, err := service.UseItem(t.Context(), player, 14, 0x08EC, 0, 0); err != nil {
		t.Fatal(err)
	}
	if !repository.vitals.InvertHPRecovery {
		t.Fatal("ordinary potion transaction did not receive active Zombie state")
	}
	if hp, mp := player.CurrentVitals(); hp != 0 || mp != 60 {
		t.Fatalf("committed Zombie potion vitals = %d/%d, want 0/60", hp, mp)
	}
	if player.GetLifeState() != model.Dead {
		t.Fatalf("lethal Zombie potion life state = %v, want dead", player.GetLifeState())
	}
}

func TestItemBuffRecoveryPersistsBeforeRuntimeCommit(t *testing.T) {
	repository := &itemUseServiceRepository{}
	player := &model.Player{ID: 77, Level: 10, LifeState: model.Alive, BaseStats: model.BaseStats{HP: 10, MP: 20, Str: 20, Int: 20}}
	changed, err := NewItemUseService(repository).ApplyItemBuffRecovery(t.Context(), player, 50, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || repository.recoveryCurrentHP != 10 || repository.recoveryCurrentMP != 20 ||
		repository.recoveryHealedHP != 60 || repository.recoveryHealedMP != 60 {
		t.Fatalf("persisted recovery = %d/%d -> %d/%d changed=%t", repository.recoveryCurrentHP, repository.recoveryCurrentMP, repository.recoveryHealedHP, repository.recoveryHealedMP, changed)
	}
	if hp, mp := player.CurrentVitals(); hp != 60 || mp != 60 {
		t.Fatalf("runtime recovery = %d/%d, want 60/60", hp, mp)
	}
}

func TestItemBuffRecoveryZombieInvertsOnlyHP(t *testing.T) {
	now := time.Unix(100, 0)
	repository := &itemUseServiceRepository{}
	player := &model.Player{ID: 77, Level: 10, LifeState: model.Alive, BaseStats: model.BaseStats{HP: 60, MP: 20, Str: 20, Int: 20}}
	player.ApplyCombatStatusAt(model.CombatStatus{
		Kind: model.CombatStatusZombie, Token: 1, ExpiresAt: now.Add(time.Minute),
	}, now)
	service := NewItemUseService(repository)
	service.ConfigureItemCureRuntime(func() time.Time { return now }, nil)
	changed, err := service.ApplyItemBuffRecovery(t.Context(), player, 50, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || repository.recoveryHealedHP != 10 || repository.recoveryHealedMP != 60 {
		t.Fatalf("persisted zombie recovery = %d/%d changed=%t, want 10/60", repository.recoveryHealedHP, repository.recoveryHealedMP, changed)
	}
	if hp, mp := player.CurrentVitals(); hp != 10 || mp != 60 {
		t.Fatalf("runtime zombie recovery = %d/%d, want 10/60", hp, mp)
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
