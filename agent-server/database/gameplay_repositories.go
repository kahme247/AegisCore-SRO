package database

import (
	"database/sql"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
)

// GameplayRepositories is the Agent's SQL-backed persistence composition.
// Domain services and handlers consume its narrow interfaces; only this
// database package knows which concrete SQL implementation satisfies each one.
type GameplayRepositories struct {
	ReferenceData           model.ReferenceDataRepository
	DimensionHoleWorlds     model.DimensionHoleWorldRepository
	DimensionHoleRegions    model.DimensionHoleWorldRegionRepository
	DimensionHoleStarts     model.DimensionHoleStartPositionRepository
	Characters              model.CharacterRepository
	CharacterRename         model.CharacterRenameRepository
	CharacterPK             model.CharacterPKRepository
	PremiumDecoration       model.PremiumDecorationRepository
	Inventory               model.InventoryRepository
	RangedAmmo              model.RangedAmmoRepository
	GroundItemPickup        model.GroundItemPickupRepository
	GroundGoldPickup        model.GroundGoldPickupRepository
	GroundItemDrop          model.GroundItemDropRepository
	GroundItemMaterializer  model.GroundItemMaterializer
	MonsterDropCatalog      service.MonsterDropCatalogLoader
	AvatarInventory         model.AvatarInventoryRepository
	MonsterMasks            model.MonsterMaskRepository
	InventoryRelocator      model.InventoryRelocator
	Economy                 model.EconomyRepository
	SafeTrade               model.SafeTradeRepository
	PackageItems            model.PackageItemCatalog
	ItemMall                model.ItemMallRepository
	GuildStorage            model.GuildStorageRepository
	Social                  model.SocialRepository
	Exchange                model.ExchangeRepository
	ItemUse                 model.ItemUseRepository
	SpeedEffects            model.SpeedEffectRepository
	ItemServices            model.ItemServiceRepository
	ItemBuffs               model.ItemBuffRepository
	Alchemy                 model.AlchemyRepository
	MagicCube               model.MagicCubeRepository
	Teleport                model.TeleportRepository
	CharacterCreator        model.CharacterCreator
	CharacterCreationPolicy model.CharacterCreationPolicyRepository
	CharacterProgress       model.CharacterProgressRepository
	CharacterAttributes     model.CharacterAttributeRepository
	Masteries               model.MasteryRepository
	Skills                  model.CharacterSkillRepository
	SkillLearning           model.SkillLearningRepository
	QuickSlots              model.CharacterQuickSlotRepository
	AutoPotionSettings      model.AutoPotionSettingsRepository
	CharacterQuests         model.CharacterQuestRepository
	QuestOffers             model.QuestOfferRepository
	Regions                 model.GameServerRegionRepository
	Spawns                  model.ContinentSpawnRepository
	TeleportGates           model.TeleportGateRepository
	Positions               model.CharacterPositionRepository
	MovementSettings        model.MovementSettingsRepository
	GameplayRates           model.GameplayRateRepository
	Vitals                  model.CharacterVitalsRepository
	Hwan                    model.HwanRepository
	COS                     model.COSRepository
	COSPolicy               model.COSPolicyRepository
	ChatFilter              model.ChatFilterRepository
	OpenMarket              model.OpenMarketRepository
	Gacha                   model.GachaRepository
	TAP                     model.TAPRepository
	SeasonalEvents          model.SeasonalEventRepository
	OperatorPunishments     model.OperatorPunishmentRepository
}

func NewGameplayRepositories(shard, account *sql.DB) (GameplayRepositories, error) {
	var repositories GameplayRepositories
	var err error
	if repositories.ReferenceData, err = model.NewReferenceDataRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent reference-data repository: %w", err)
	}
	if repositories.DimensionHoleWorlds, err = model.NewDimensionHoleWorldRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent Dimension Hole world repository: %w", err)
	}
	if repositories.DimensionHoleRegions, err = model.NewDimensionHoleWorldRegionRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent Dimension Hole world-region repository: %w", err)
	}
	if repositories.DimensionHoleStarts, err = model.NewDimensionHoleStartPositionRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent Dimension Hole start-position repository: %w", err)
	}
	if repositories.Characters, err = model.NewCharacterRepository(shard, account); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character repository: %w", err)
	}
	if repositories.CharacterRename, err = model.NewCharacterRenameRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character-rename repository: %w", err)
	}
	if repositories.CharacterPK, err = model.NewCharacterPKRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character PK repository: %w", err)
	}
	if repositories.PremiumDecoration, err = model.NewPremiumDecorationRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent premium decoration repository: %w", err)
	}
	if repositories.Inventory, err = model.NewInventoryRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent inventory repository: %w", err)
	}
	if repositories.RangedAmmo, err = model.NewRangedAmmoRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent ranged ammunition repository: %w", err)
	}
	groundPickup, err := model.NewGroundItemPickupRepository(shard)
	if err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent ground-item pickup repository: %w", err)
	}
	repositories.GroundItemPickup = groundPickup
	repositories.GroundGoldPickup = groundPickup
	repositories.GroundItemDrop = groundPickup
	if repositories.GroundItemMaterializer, err = model.NewGroundItemMaterializer(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent ground-item materializer: %w", err)
	}
	if repositories.MonsterDropCatalog, err = NewMonsterDropCatalogRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent monster-drop catalog: %w", err)
	}
	if repositories.AvatarInventory, err = model.NewAvatarInventoryRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent avatar inventory repository: %w", err)
	}
	if repositories.MonsterMasks, err = model.NewMonsterMaskRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent monster-mask repository: %w", err)
	}
	if repositories.InventoryRelocator, err = model.NewInventoryRelocationRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent inventory relocation repository: %w", err)
	}
	if repositories.Economy, err = model.NewEconomyRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent economy repository: %w", err)
	}
	if repositories.SafeTrade, err = model.NewSafeTradeRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent safe-trade repository: %w", err)
	}
	if repositories.PackageItems, err = model.NewPackageItemRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent package-item repository: %w", err)
	}
	if repositories.ItemMall, err = model.NewItemMallRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent item mall repository: %w", err)
	}
	if repositories.GuildStorage, err = model.NewGuildStorageRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent guild-storage repository: %w", err)
	}
	if repositories.Social, err = model.NewSocialRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent social repository: %w", err)
	}
	if repositories.OperatorPunishments, err = model.NewOperatorPunishmentRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent operator punishment repository: %w", err)
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
	repositories.ItemBuffs = itemUse
	if repositories.Alchemy, err = model.NewAlchemyRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent alchemy repository: %w", err)
	}
	if repositories.MagicCube, err = model.NewMagicCubeRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent magic-cube repository: %w", err)
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
	if repositories.AutoPotionSettings, err = model.NewAutoPotionSettingsRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent auto-potion settings repository: %w", err)
	}
	if repositories.CharacterQuests, err = model.NewCharacterQuestRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent character-quest repository: %w", err)
	}
	if repositories.QuestOffers, err = model.NewQuestOfferRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent quest-offer repository: %w", err)
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
	if repositories.Hwan, err = model.NewHwanRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent HWAN repository: %w", err)
	}
	if repositories.COS, err = model.NewCOSRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent COS repository: %w", err)
	}
	if repositories.COSPolicy, err = model.NewCOSPolicyRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent COS policy repository: %w", err)
	}
	if repositories.ChatFilter, err = model.NewChatFilterRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent chat filter: %w", err)
	}
	if repositories.OpenMarket, err = model.NewOpenMarketRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent open-market repository: %w", err)
	}
	if repositories.Gacha, err = model.NewGachaRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent Gacha repository: %w", err)
	}
	if repositories.TAP, err = model.NewTAPRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent TAP repository: %w", err)
	}
	if repositories.SeasonalEvents, err = model.NewSeasonalEventRepository(shard); err != nil {
		return GameplayRepositories{}, fmt.Errorf("configure Agent seasonal-event repository: %w", err)
	}
	return repositories, nil
}
