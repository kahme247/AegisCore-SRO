package database

import (
	"database/sql"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

// GameplayRepositories is the Agent's SQL-backed persistence composition.
// Domain services and handlers consume its narrow interfaces; only this
// database package knows which concrete SQL implementation satisfies each one.
type GameplayRepositories struct {
	ReferenceData           model.ReferenceDataRepository
	Characters              model.CharacterRepository
	Inventory               model.InventoryRepository
	InventoryRelocator      model.InventoryRelocator
	Economy                 model.EconomyRepository
	ItemMall                model.ItemMallRepository
	GuildStorage            model.GuildStorageRepository
	Exchange                model.ExchangeRepository
	ItemUse                 model.ItemUseRepository
	SpeedEffects            model.SpeedEffectRepository
	ItemServices            model.ItemServiceRepository
	Alchemy                 model.AlchemyRepository
	Teleport                model.TeleportRepository
	CharacterCreator        model.CharacterCreator
	CharacterCreationPolicy model.CharacterCreationPolicyRepository
	CharacterProgress       model.CharacterProgressRepository
	CharacterAttributes     model.CharacterAttributeRepository
	Masteries               model.MasteryRepository
	Skills                  model.CharacterSkillRepository
	SkillLearning           model.SkillLearningRepository
	QuickSlots              model.CharacterQuickSlotRepository
	CharacterQuests         model.CharacterQuestRepository
	Regions                 model.GameServerRegionRepository
	Spawns                  model.ContinentSpawnRepository
	TeleportGates           model.TeleportGateRepository
	Positions               model.CharacterPositionRepository
	MovementSettings        model.MovementSettingsRepository
	GameplayRates           model.GameplayRateRepository
	Vitals                  model.CharacterVitalsRepository
}

func NewGameplayRepositories(shard, account *sql.DB) (GameplayRepositories, error) {
	var repositories GameplayRepositories
	var err error
	if repositories.ReferenceData, err = model.NewReferenceDataRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent reference-data repository: %w", err)
	}
	if repositories.Characters, err = model.NewCharacterRepository(shard, account); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character repository: %w", err)
	}
	if repositories.Inventory, err = model.NewInventoryRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent inventory repository: %w", err)
	}
	if repositories.InventoryRelocator, err = model.NewInventoryRelocationRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent inventory relocation repository: %w", err)
	}
	if repositories.Economy, err = model.NewEconomyRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent economy repository: %w", err)
	}
	if repositories.ItemMall, err = model.NewItemMallRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent item mall repository: %w", err)
	}
	if repositories.GuildStorage, err = model.NewGuildStorageRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent guild-storage repository: %w", err)
	}
	if repositories.Exchange, err = model.NewExchangeRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent exchange repository: %w", err)
	}
	itemUse, err := model.NewItemUseRepository(shard)
	if err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent item-use repository: %w", err)
	}
	repositories.ItemUse = itemUse
	repositories.SpeedEffects = itemUse
	repositories.ItemServices = itemUse
	if repositories.Alchemy, err = model.NewAlchemyRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent alchemy repository: %w", err)
	}
	if repositories.Teleport, err = model.NewTeleportRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent teleport repository: %w", err)
	}
	if repositories.CharacterCreator, err = model.NewCharacterCreationRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character creation repository: %w", err)
	}
	if repositories.CharacterCreationPolicy, err = model.NewCharacterCreationPolicyRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character creation policy: %w", err)
	}
	if repositories.CharacterProgress, err = model.NewCharacterProgressRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character-progress repository: %w", err)
	}
	if repositories.CharacterAttributes, err = model.NewCharacterAttributeRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character-attribute repository: %w", err)
	}
	if repositories.Masteries, err = model.NewMasteryRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent mastery repository: %w", err)
	}
	skills, err := model.NewCharacterSkillRepository(shard)
	if err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character-skill repository: %w", err)
	}
	repositories.Skills = skills
	repositories.SkillLearning = skills
	if repositories.QuickSlots, err = model.NewCharacterQuickSlotRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character quick-slot repository: %w", err)
	}
	if repositories.CharacterQuests, err = model.NewCharacterQuestRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character-quest repository: %w", err)
	}
	if repositories.Regions, err = model.NewGameServerRegionRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent game-server region catalog: %w", err)
	}
	if repositories.Spawns, err = model.NewContinentSpawnRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent continent spawn catalog: %w", err)
	}
	repositories.TeleportGates = model.NewTeleportGateRepository()
	if repositories.Positions, err = model.NewCharacterPositionRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character position repository: %w", err)
	}
	if repositories.MovementSettings, err = model.NewMovementSettingsRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent movement settings repository: %w", err)
	}
	if repositories.GameplayRates, err = model.NewGameplayRateRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent gameplay rates repository: %w", err)
	}
	if repositories.Vitals, err = model.NewCharacterVitalsRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character vitals repository: %w", err)
	}
	return repositories, nil
}
