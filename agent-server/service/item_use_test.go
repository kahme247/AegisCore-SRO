package service

import (
	"context"
	"testing"

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

	result, err := NewItemUseService(repository).UseItem(context.Background(), player, 14, 0x08EC)
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
