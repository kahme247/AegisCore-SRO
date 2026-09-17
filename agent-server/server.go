package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/config"
	"github.com/Harvi8/AegisCore/agent-server/database"
	"github.com/Harvi8/AegisCore/agent-server/engine/environment"
	"github.com/Harvi8/AegisCore/agent-server/feature"
	"github.com/Harvi8/AegisCore/agent-server/handler/character"
	"github.com/Harvi8/AegisCore/agent-server/handler/chat"
	"github.com/Harvi8/AegisCore/agent-server/handler/cos"
	"github.com/Harvi8/AegisCore/agent-server/handler/exchange"
	"github.com/Harvi8/AegisCore/agent-server/handler/inventory"
	"github.com/Harvi8/AegisCore/agent-server/handler/lobby"
	"github.com/Harvi8/AegisCore/agent-server/handler/logout"
	"github.com/Harvi8/AegisCore/agent-server/handler/market"
	"github.com/Harvi8/AegisCore/agent-server/handler/party"
	"github.com/Harvi8/AegisCore/agent-server/handler/ping"
	"github.com/Harvi8/AegisCore/agent-server/handler/social"
	"github.com/Harvi8/AegisCore/agent-server/handler/stall"
	"github.com/Harvi8/AegisCore/agent-server/management"
	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	agentruntime "github.com/Harvi8/AegisCore/agent-server/runtime"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/aggregate"
	"github.com/Harvi8/AegisCore/framework/boot"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/logging"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/scheduler"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/security/mtls"
	"github.com/Harvi8/AegisCore/framework/server"
	gwHandlers "github.com/Harvi8/AegisCore/gateway-server/handler"
	protocolregistry "github.com/Harvi8/AegisCore/protocol/registry"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type AgentServer struct {
	*server.Server
	AdmissionTickets       *auth.AdmissionTicketStore
	AdmissionTicketServer  *auth.AdmissionTicketInstallServer
	ManagementServer       *management.Server
	UnhandledPacketsLogger *log.Logger
	packetContext          context.Context
	cancelPacketHandling   context.CancelFunc
	Lifecycle              *agentruntime.Lifecycle
	Features               feature.Set
	// Pools owns the long-lived account and shard connections used by the live
	// character-selection, reference-data bootstrap, world bootstrap, and
	// checkpoint paths. It is closed
	// only after Agent has stopped accepting and drained all sessions and queued
	// world persistence that can use those repositories.
	Pools *frameworkdb.Pools
	// ReferenceData loads the complete startup item/character snapshot through
	// Pools.Shard. It is configured before Boot so gamedata never falls back to
	// the legacy per-call sql.Open helpers.
	ReferenceData model.ReferenceDataRepository
	// World is the configured singleton used by the live Agent. It is retained
	// here so shutdown can drain its position queue before Pools is closed.
	World *service.WorldService
	// GroundItems applies party ownership and emits the binary-confirmed
	// distribution notification before a materialized item enters the world.
	GroundItems         *service.GroundItemService
	GroundItemLifecycle *service.GroundItemLifecycle
	// MonsterDrops is optional until a hydrated reference-drop catalog is
	// installed; when present, Combat invokes it after a confirmed mob death.
	MonsterDrops *service.MonsterDropService
	// MonsterDropCatalog is the startup SQL boundary used to hydrate
	// MonsterDrops after the ordinary reference snapshot is available.
	MonsterDropCatalog service.MonsterDropCatalogLoader
	// LevelTable is loaded from SRO_SHARD.REF_LEVEL_DATA before any character
	// can enter the world.
	LevelTable *model.LevelTable
	// Exchange owns the in-memory two-party exchange state so disconnect
	// teardown can release the peer immediately.
	Exchange *exchange.Manager
	// GuildStorage owns the database-backed exclusive guild chest lease and is
	// retained so disconnect teardown can release it immediately.
	GuildStorage           model.GuildStorageRepository
	Combat                 *character.CombatHandler
	QuestInstances         *model.QuestInstanceRegistry
	QuestInstanceLifecycle *model.QuestInstanceLifecycle
	DimensionHoles         *service.DimensionHoleRuntime
	Hwan                   *character.HwanHandler
	Skill                  *character.SkillHandler
	SkillRestoration       *character.SkillRestorationHandler
	Inventory              *inventory.InventoryHandler
	CharacterSelection     *lobby.CharacterSelectionHandler
	Vitals                 model.CharacterVitalsRepository
	GameplayRates          *manager.GameplayRateManager
	COSPolicy              *manager.COSPolicyManager
	COS                    *service.COSService
	DisconnectSaver        *service.DisconnectSaver
	ChatFilter             model.ChatFilterRepository
	Scheduler              *scheduler.Runtime
	MarketExpiry           *service.OpenMarketExpiryService
	DependencyMonitor      *dependencyMonitor
	ServerEvents           *service.ServerEventEngine
	ServerEventRewards     *service.ServerEventRewardRuntime
	Competitive            *service.CompetitiveRuntime
	Social                 *social.Handler
	dependencyDrainMu      *sync.Mutex
	//SpawnEngine             *spawn.SpawnEngine
}

type agentDatabaseHealthSource struct {
	pools       *frameworkdb.Pools
	engine      *server.Server
	competitive *service.CompetitiveRuntime
}

func (source agentDatabaseHealthSource) CompetitiveStatus() service.CompetitiveStatus {
	return source.competitive.Status()
}

func (s agentDatabaseHealthSource) DatabaseQueryCount() int {
	if s.pools == nil {
		return 0
	}
	total := 0
	if account, err := s.pools.Account(); err == nil {
		total += account.Stats().InUse
	}
	if shard, err := s.pools.Shard(); err == nil {
		total += shard.Stats().InUse
	}
	return total
}

func (s agentDatabaseHealthSource) MessageDispatch() server.DispatchMetrics {
	if s.engine == nil {
		return server.DispatchMetrics{}
	}
	return s.engine.DispatchMetrics()
}

// NewAgentServer constructs both Agent listeners but does not expose either
// until Start succeeds. TLS material is mandatory: there is no public SRO
// 0xE000 backend-authentication fallback or shared-secret configuration.
func NewAgentServer() (AgentServer, error) {
	features, err := config.ResolveFeatures()
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent legacy features: %w", err)
	}
	tlsConfig, err := auth.LoadAdmissionTicketServerTLSConfig(auth.MutualTLSFiles{
		CertFile: viper.GetString(config.AgentAdmissionTLSCertFile),
		KeyFile:  viper.GetString(config.AgentAdmissionTLSKeyFile),
		CAFile:   viper.GetString(config.AgentAdmissionTLSClientCAFile),
	})
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Gateway admission mTLS server: %w", err)
	}

	pools, err := frameworkdb.NewConfiguredPools(context.Background())
	if err != nil {
		return AgentServer{}, fmt.Errorf("open Agent database pools: %w", err)
	}
	keepPools := false
	defer func() {
		if !keepPools {
			_ = pools.Close()
		}
	}()
	accountPool, err := pools.Account()
	if err != nil {
		return AgentServer{}, fmt.Errorf("get Agent account pool: %w", err)
	}
	shardPool, err := pools.Shard()
	if err != nil {
		return AgentServer{}, fmt.Errorf("get Agent shard pool: %w", err)
	}
	var schedulerStore *scheduler.SQLStore
	if viper.GetBool(config.AgentSchedulerEnabled) {
		var schedulerErr error
		schedulerStore, schedulerErr = scheduler.NewSQLStore(shardPool)
		if schedulerErr != nil {
			return AgentServer{}, fmt.Errorf("configure Agent scheduler store: %w", schedulerErr)
		}
	}
	levelRepository, err := model.NewLevelTableRepository(shardPool)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent level-data repository: %w", err)
	}
	levelContext, cancelLevelLoad := context.WithTimeout(context.Background(), 5*time.Second)
	levelTable, err := levelRepository.Load(levelContext)
	cancelLevelLoad()
	if err != nil {
		return AgentServer{}, fmt.Errorf("load Agent level data from shard database: %w", err)
	}
	aggregateCoordinator, err := aggregate.NewSQLCoordinator(shardPool)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent aggregate coordinator: %w", err)
	}
	serverEvents, err := service.NewServerEventEngine(aggregateCoordinator)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure server event engine: %w", err)
	}
	competitiveRewards, err := service.NewCompetitiveRewardRepository(aggregateCoordinator)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure competitive rewards: %w", err)
	}
	competitiveRuntime, err := service.NewCompetitiveRuntime(service.CompetitiveConfig{
		MinimumPlayers: viper.GetInt(config.AgentCompetitiveMinimumPlayers),
		MaximumPlayers: viper.GetInt(config.AgentCompetitiveMaximumPlayers),
		MatchDuration:  time.Duration(viper.GetInt(config.AgentCompetitiveMatchSeconds)) * time.Second,
		WinnerGold:     viper.GetUint64(config.AgentCompetitiveWinnerGold),
		LoserGold:      viper.GetUint64(config.AgentCompetitiveLoserGold),
	}, competitiveRewards)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure competitive runtime: %w", err)
	}
	disconnectSaver, err := service.NewDisconnectSaver(aggregateCoordinator, service.DefaultDisconnectSaveTimeout)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent disconnect saver: %w", err)
	}
	repositories, err := database.NewGameplayRepositories(shardPool, accountPool)
	if err != nil {
		return AgentServer{}, err
	}
	questRuntime, err := model.NewQuestRuntimeRepository(shardPool, levelTable)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent quest runtime: %w", err)
	}
	gameplayRates, err := manager.NewGameplayRateManager(repositories.GameplayRates, manager.GameplayRateRefreshInterval)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure gameplay rate cache: %w", err)
	}
	cosPolicy, err := manager.NewCOSPolicyManager(repositories.COSPolicy, manager.COSPolicyRefreshInterval)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure COS policy cache: %w", err)
	}
	cosPolicyContext, cancelCOSPolicyLoad := context.WithTimeout(context.Background(), 5*time.Second)
	err = cosPolicy.Refresh(cosPolicyContext)
	cancelCOSPolicyLoad()
	if err != nil {
		log.Errorf("initial COS policy load failed; using safe companion defaults: %v", err)
	}
	rateContext, cancelRateLoad := context.WithTimeout(context.Background(), 5*time.Second)
	err = gameplayRates.Refresh(rateContext)
	cancelRateLoad()
	if err != nil {
		log.Errorf("initial gameplay rate load failed; using hardcoded 100%% EXP, skill EXP, mob count, and mob respawn: %v", err)
	}
	chatFilterContext, cancelChatFilterLoad := context.WithTimeout(context.Background(), 5*time.Second)
	err = repositories.ChatFilter.Refresh(chatFilterContext)
	cancelChatFilterLoad()
	if err != nil {
		return AgentServer{}, fmt.Errorf("load database-backed chat abuse filter: %w", err)
	}

	serv := server.NewEngine(
		viper.GetString(config.AgentHost),
		viper.GetInt(config.AgentPort),
		network.EncodingOptions{
			None:       false,
			Disabled:   false,
			Encryption: true,
			// The stock client expects the same sequence/CRC negotiation on a
			// fresh Agent connection that it receives from Gateway. Omitting this
			// bit leaves the client waiting after the initial 0x5000 challenge and
			// it never sends the Agent authentication request.
			EDC:          true,
			KeyExchange:  true,
			KeyChallenge: false,
		},
	)
	serv.ModuleID = viper.GetString(config.AgentModuleId)
	lifecycle := agentruntime.NewLifecycle()
	serv.SetTrafficFilter(server.TrafficFilterOptions{
		Window:      time.Duration(viper.GetInt(config.AgentTrafficWindowMS)) * time.Millisecond,
		PacketLimit: uint32(viper.GetUint(config.AgentTrafficPacketLimit)),
	})
	// Before an atomically redeemed ticket marks the session authenticated,
	// public Agent accepts exactly the stock 0x6103 AuthRequest. Afterwards
	// it exposes lobby packets, then only loading handshake packets while a
	// character bootstrap is pending, before admitting normal in-world traffic. In particular,
	// legacy 0xE000 backend authentication is rejected at the listener edge.
	registry := vsro188.ProtocolRegistry()
	authenticationOpcodes := registry.Opcodes(protocolregistry.ServiceAgent, protocolregistry.ClientToServer, protocolregistry.AgentAuthentication)
	if len(authenticationOpcodes) != 1 {
		return AgentServer{}, fmt.Errorf("configure Agent protocol admission: expected one authentication opcode, got %d", len(authenticationOpcodes))
	}
	serv.SetAdmissionPolicy(server.AvailabilityAdmissionPolicy(lifecycle.IsReady, server.AgentLifecycleAdmissionPolicy(
		authenticationOpcodes[0],
		registry.Opcodes(protocolregistry.ServiceAgent, protocolregistry.ClientToServer, protocolregistry.AgentLobby),
		registry.Opcodes(protocolregistry.ServiceAgent, protocolregistry.ClientToServer, protocolregistry.AgentCharacterLoading),
	)))

	tickets := auth.NewAdmissionTicketStore()
	admissionServer, err := auth.NewAdmissionTicketInstallServerWithReadiness(
		net.JoinHostPort(
			viper.GetString(config.AgentAdmissionHost),
			fmt.Sprintf("%d", viper.GetInt(config.AgentAdmissionPort)),
		),
		tlsConfig,
		tickets,
		lifecycle.IsReady,
	)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Gateway admission ticket listener: %w", err)
	}

	agentServer, err := NewAgentServerWithDependencies(AgentDependencies{
		Server:                 &serv,
		AdmissionTickets:       tickets,
		AdmissionTicketServer:  admissionServer,
		UnhandledPacketsLogger: logging.UnhandledPacketLogger(),
		Pools:                  pools,
		ReferenceData:          repositories.ReferenceData,
		MonsterDropCatalog:     repositories.MonsterDropCatalog,
		World:                  service.GetWorldServiceInstance(),
		LevelTable:             levelTable,
		GameplayRates:          gameplayRates,
		COSPolicy:              cosPolicy,
		Lifecycle:              lifecycle,
		Features:               features,
		DisconnectSaver:        disconnectSaver,
	})
	if err != nil {
		return AgentServer{}, fmt.Errorf("compose Agent runtime: %w", err)
	}
	agentServer.Competitive = competitiveRuntime
	agentServer.ServerEvents = serverEvents
	agentServer.DependencyMonitor, err = newDependencyMonitor(accountPool, shardPool, agentServer.DrainForDependencyLoss)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent dependency monitor: %w", err)
	}
	var groundItemLifecycle *service.GroundItemLifecycle
	if agentServer.GroundItems != nil {
		agentServer.GroundItems.ConfigureMaterializer(repositories.GroundItemMaterializer)
		// 0x0048F9B0 compares the ownership accumulator with 30.0. The
		// lifecycle scheduler expresses that recovered boundary as a duration;
		// final despawn remains reference-driven and is deliberately disabled
		// until the corresponding runtime decay source is wired.
		groundItemLifecycle = service.NewGroundItemLifecycle(service.GetWorldServiceInstance())
		agentServer.GroundItemLifecycle = groundItemLifecycle
		agentServer.GroundItems.ConfigureLifecycle(groundItemLifecycle, 30*time.Second, 0)
		if agentServer.Inventory != nil {
			agentServer.Inventory.ConfigureGroundItemLifecycle(groundItemLifecycle)
		}
	}
	if err := lifecycle.Transition(agentruntime.Loading, ""); err != nil {
		return AgentServer{}, fmt.Errorf("enter Agent loading lifecycle: %w", err)
	}

	lobby.RegisterAuthRequestHandler(&serv, agentServer.AdmissionTickets)
	gwHandlers.RegisterPatchRequestHandler(&serv)
	gwHandlers.RegisterShardlistRequestHandler(&serv)
	characterSelection, err := lobby.RegisterCharacterSelectionHandlersWithRepositoriesAndCreator(&serv, repositories.Characters, repositories.Inventory, repositories.CharacterCreator)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character-selection handlers: %w", err)
	}
	if err := characterSelection.ConfigureCharacterCreationPolicy(repositories.CharacterCreationPolicy); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character creation policy: %w", err)
	}
	if err := characterSelection.ConfigureAvatarInventoryRepository(repositories.AvatarInventory); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent avatar inventory bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureQuestRepository(repositories.CharacterQuests); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character quest bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureMasteryRepository(repositories.Masteries); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent mastery bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureSkillRepository(repositories.Skills); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character skill bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureQuickSlotRepository(repositories.QuickSlots); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character quick-slot bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureAutoPotionSettingsRepository(repositories.AutoPotionSettings); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent auto-potion settings bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureSpeedEffectRepository(repositories.SpeedEffects); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character speed-effect bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureItemServiceRepository(repositories.ItemServices); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character item-service bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureItemBuffRepository(repositories.ItemBuffs); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character item-buff bootstrap: %w", err)
	}
	if err := characterSelection.ConfigureHwanRepository(repositories.Hwan); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character HWAN bootstrap: %w", err)
	}
	characterSelection.ConfigureTimedBuffDuration(viper.GetBool(config.AgentTimedBuffDuration))
	if err := characterSelection.ConfigureMovementSettingsRepository(repositories.MovementSettings); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent movement settings: %w", err)
	}
	agentServer.CharacterSelection = characterSelection
	agentServer.ChatFilter = repositories.ChatFilter
	lobby.RegisterCharSelectionRenameRequestHandler(&serv, repositories.CharacterRename)
	gameReadyHandler := character.RegisterGameReadyHandler(&serv, repositories.ItemMall)
	gameReadyHandler.ConfigurePKRepository(repositories.CharacterPK)
	gameReadyHandler.ConfigurePremiumDecoration(repositories.PremiumDecoration)
	questCountdowns := character.NewQuestCountdownService(repositories.CharacterQuests)
	gameReadyHandler.ConfigureQuestCountdowns(questCountdowns)
	character.RegisterGuideHandler(&serv)
	agentServer.Hwan, err = character.RegisterHwanHandler(&serv, repositories.Hwan)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent HWAN handler: %w", err)
	}
	character.RegisterPremiumDailyHandler(&serv, repositories.ItemServices)
	character.RegisterPremiumSilkHandler(&serv, repositories.ItemMall, repositories.PremiumDecoration)
	character.RegisterFreePVPHandler(&serv, competitiveRuntime)
	if _, err = character.RegisterGachaHandler(&serv, repositories.Gacha, service.GetWorldServiceInstance()); err != nil {
		return AgentServer{}, err
	}
	if _, err = character.RegisterTAPHandler(&serv, repositories.TAP, service.GetWorldServiceInstance()); err != nil {
		return AgentServer{}, err
	}
	if fortress, ok := repositories.Social.(model.FortressSiegeRepository); ok {
		if _, err = character.RegisterFortressSiegeHandler(&serv, fortress, service.GetWorldServiceInstance()); err != nil {
			return AgentServer{}, err
		}
	}
	if _, err = character.RegisterSeasonalEventHandler(&serv, repositories.SeasonalEvents, service.GetWorldServiceInstance(), features.Enabled(feature.ChristmasEvent2007)); err != nil {
		return AgentServer{}, err
	}
	if _, err = character.RegisterOperatorHandler(&serv, service.GetWorldServiceInstance(), repositories.OperatorPunishments); err != nil {
		return AgentServer{}, err
	}
	ping.RegisterHandler(&serv)
	movementHandler := character.RegisterMovementHandler(&serv)
	character.RegisterSelectEntityHandler(&serv)
	character.RegisterDeselectEntityHandler(&serv)
	cos.RegisterMountStateHandler(&serv)
	cosLifecycle := cos.RegisterLifecycleHandlers(&serv, repositories.COS)
	if cosLifecycle == nil || cosLifecycle.Service() == nil {
		return AgentServer{}, fmt.Errorf("configure Agent COS lifecycle: service is unavailable")
	}
	cosLifecycle.Service().ConfigurePolicy(cosPolicy)
	service.GetWorldServiceInstance().ConfigureMountedTransportResolver(cosLifecycle.Service().ResolveMountedTransport)
	tradeSafetyOption, tradeSafetyLimit := viper.GetUint(config.AgentTradeSafetyOption), viper.GetUint(config.AgentTradeSafetyNum)
	if tradeSafetyOption > 2 || tradeSafetyLimit == 0 || tradeSafetyLimit > 255 {
		return AgentServer{}, fmt.Errorf("configure Agent safe trade: option=%d limit=%d", tradeSafetyOption, tradeSafetyLimit)
	}
	npcTalkHandler, err := character.RegisterNPCTalkHandler(&serv, repositories.CharacterQuests, questRuntime, repositories.QuestOffers, repositories.SafeTrade, byte(tradeSafetyOption), byte(tradeSafetyLimit), questCountdowns)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent NPC talk handler: %w", err)
	}
	if err := character.RegisterQuestHandler(&serv, repositories.CharacterQuests); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent quest handlers: %w", err)
	}
	questEvents, err := model.NewQuestEventDispatcher(shardPool)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent quest event dispatcher: %w", err)
	}
	if err = model.RegisterOldWomanSearchEvents(questEvents); err != nil {
		return AgentServer{}, fmt.Errorf("register QNO_RM_OLDWOMAN_5 events: %w", err)
	}
	if err = model.RegisterNativeCatchEvents(questEvents); err != nil {
		return AgentServer{}, fmt.Errorf("register native catch-action events: %w", err)
	}
	if err = model.RegisterNativeInteractionEvents(questEvents); err != nil {
		return AgentServer{}, fmt.Errorf("register native quest interaction events: %w", err)
	}
	if err = character.RegisterQuestActionHandler(&serv, questEvents); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent quest action handlers: %w", err)
	}
	if _, err := character.RegisterQuestRewardSelectionHandler(&serv, questRuntime); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent quest reward selection: %w", err)
	}
	teleportHandler := character.RegisterTeleportHandler(&serv, repositories.Teleport)
	agentServer.DimensionHoles, err = service.LoadDimensionHoleRuntime(
		context.Background(), repositories.DimensionHoleWorlds, repositories.DimensionHoleRegions, repositories.DimensionHoleStarts,
		service.GetWorldServiceInstance(), service.GetWorldServiceInstance(), model.NextActionUID,
	)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Dimension Hole runtime: %w", err)
	}
	if err = agentServer.DimensionHoles.ConfigureEntry(agentServer.DimensionHoles.Destinations, teleportHandler); err != nil {
		return AgentServer{}, fmt.Errorf("configure Dimension Hole entry: %w", err)
	}
	if dungeonTeleportPath := viper.GetString(config.AgentDungeonTeleportDataPath); dungeonTeleportPath != "" {
		dungeonEventZones, dungeonEventErr := character.LoadDungeonEventZoneCatalog(dungeonTeleportPath, viper.GetString(config.AgentDataPath))
		if dungeonEventErr != nil {
			return AgentServer{}, fmt.Errorf("configure dungeon event zones: %w", dungeonEventErr)
		}
		log.WithFields(log.Fields{
			"level_2": dungeonEventZones.ZoneCount(-32762),
			"level_3": dungeonEventZones.ZoneCount(-32763),
			"level_4": dungeonEventZones.ZoneCount(-32764),
		}).Info("loaded authoritative Tomb event zones")
		dungeonEventZoneLatch := character.NewDungeonEventZoneLatch(dungeonEventZones)
		movementHandler.ConfigureDungeonEventZones(func(player *model.Player, target navmeshv2.RtNavmeshPosition) (bool, error) {
			destination, zoneName, ok := dungeonEventZoneLatch.DestinationAt(player.GetUniqueID(), target)
			if !ok {
				return false, nil
			}
			log.WithFields(log.Fields{"uid": player.GetUniqueID(), "zone": zoneName, "region": destination.Region}).Info("activated dungeon event zone")
			return true, teleportHandler.SubmitDungeonEventZoneTeleport(player, destination)
		})
	}
	agentServer.QuestInstances = model.NewQuestInstanceRegistry()
	questInstanceMembers, err := model.NewQuestInstanceMemberRepository(shardPool)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure quest instance member repository: %w", err)
	}
	questInstanceAllocator, err := model.NewQuestInstanceScopeAllocator(shardPool)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure quest instance scope allocator: %w", err)
	}
	agentServer.QuestInstanceLifecycle, err = model.NewQuestInstanceLifecycle(agentServer.QuestInstances, questInstanceMembers, questInstanceAllocator)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure quest instance lifecycle: %w", err)
	}
	teleportHandler.ConfigureQuestInstanceExit(agentServer.QuestInstanceLifecycle)
	instanceEntries := service.NewQuestInstanceEntryService(agentServer.QuestInstanceLifecycle, service.GetPartyServiceInstance(), model.NextActionUID)
	npcTalkHandler.ConfigureQuestInstanceEntry(instanceEntries, teleportHandler)
	character.RegisterResurrectionHandler(&serv, repositories.Vitals, teleportHandler, repositories.ItemUse)
	manager.GetMobAIManagerInstance().ConfigureVitals(repositories.Vitals)
	manager.GetRespawnManagerInstance().ConfigureGameplayRates(gameplayRates)
	manager.GetRespawnManagerInstance().ConfigureGiantSpawnRatio(viper.GetInt(config.AgentGiantMonsterSpawnRatio))
	agentServer.Combat = character.RegisterCombatHandler(&serv, repositories.CharacterProgress, levelTable, gameplayRates, questRuntime)
	agentServer.Combat.ConfigureDimensionHoleEntry(agentServer.DimensionHoles.Entries)
	agentServer.Combat.ConfigurePKPenalties(repositories.CharacterPK)
	agentServer.Combat.ConfigureNativeQuestInstanceDeaths(agentServer.QuestInstances, teleportHandler)
	agentServer.Combat.ConfigureJobProgression(repositories.Social)
	agentServer.Combat.ConfigureMobAttackInterrupter(manager.GetMobAIManagerInstance().InterruptAttack)
	agentServer.Combat.ConfigureMobActionTiming(manager.GetMobAIManagerInstance().ReconcileAttackTiming)
	manager.GetMobAIManagerInstance().ConfigurePlayerAttackInterrupter(agentServer.Combat.InterruptPlayerAction)
	if agentServer.MonsterDrops != nil {
		agentServer.Combat.ConfigureMonsterDrops(agentServer.MonsterDrops)
	}
	agentServer.Combat.ConfigureVitals(repositories.Vitals)
	agentServer.Combat.ConfigureRangedAmmo(repositories.RangedAmmo)
	agentServer.Combat.ConfigureMonsterMasks(repositories.MonsterMasks)
	agentServer.Combat.ConfigureHwan(repositories.Hwan)
	cosLifecycle.Service().ConfigureCombat(agentServer.Combat, levelTable)
	agentServer.Combat.ConfigureCompanions(cosLifecycle.Service())
	movementHandler.ConfigureCombatCancellation(agentServer.Combat.CancelPlayerAttack)
	if err := character.RegisterAttributeHandlers(&serv, repositories.CharacterAttributes); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character attribute handlers: %w", err)
	}
	if err := character.RegisterMasteryHandler(&serv, repositories.Masteries, levelTable); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent mastery handler: %w", err)
	}
	agentServer.Skill, err = character.RegisterSkillHandler(&serv, repositories.SkillLearning)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent skill handler: %w", err)
	}
	restorationRepository, ok := repositories.SkillLearning.(model.SkillRestorationRepository)
	if !ok {
		return AgentServer{}, fmt.Errorf("configure Agent skill restoration: repository does not support restoration")
	}
	agentServer.SkillRestoration, err = character.RegisterSkillRestorationHandler(&serv, restorationRepository, levelTable)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent skill restoration handler: %w", err)
	}
	if err := character.RegisterQuickSlotHandler(&serv, repositories.QuickSlots, repositories.AutoPotionSettings); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character quick-slot handler: %w", err)
	}
	socialHandler := social.RegisterHandlers(&serv, repositories.Social)
	socialHandler.ConfigureCOS(cosLifecycle.Service())
	agentServer.Social = socialHandler
	agentServer.Combat.ConfigureAcademyProgression(repositories.Social, socialHandler.PublishAcademyBuffs)
	agentServer.Combat.ConfigureGuildWarScoring(socialHandler.RecordGuildWarKill)
	agentServer.Combat.ConfigureCompetitiveScoring(competitiveRuntime.RecordPlayerKill)
	if fortress, ok := any(repositories.Social).(model.FortressSiegeRepository); ok {
		agentServer.Combat.ConfigureFortressDamage(func(ctx context.Context, uniqueID uint32, actor int, damage uint32, at time.Time) (model.FortressDamageResult, error) {
			return fortress.DamageFortressStructure(ctx, 0, uniqueID, actor, damage, at)
		})
		agentServer.Combat.ConfigureFortressKillScoring(func(ctx context.Context, killer, victim int, at time.Time) error {
			return fortress.RecordFortressBattleKill(ctx, 0, killer, victim, at)
		})
	}
	agentServer.Combat.ConfigurePVPAdmissionProvider(func(source, target *model.Player, _ model.RefSkill) bool {
		if source == nil || target == nil {
			return false
		}
		participating, allowed := competitiveRuntime.CombatAdmission(source.ID, target.ID)
		return !participating || allowed
	})
	if err := characterSelection.ConfigurePlayerPreparer(socialHandler); err != nil {
		return AgentServer{}, fmt.Errorf("configure character-selection social state: %w", err)
	}
	if err := socialHandler.ConfigureSafeTrade(repositories.SafeTrade, tradeSafetyLimit); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent safe-trade bootstrap: %w", err)
	}
	gameReadyHandler.ConfigureSocial(socialHandler)
	chat.RegisterChatHandler(&serv, repositories.Social, repositories.ChatFilter)
	logoutHandler := logout.RegisterLogoutHandler(&serv)
	logoutHandler.ConfigureCommunity(socialHandler)
	stall.RegisterStallCreateHandler(&serv)
	stall.RegisterStallDestroyHandler(&serv)
	stall.RegisterStallLeaveHandler(&serv)
	stall.RegisterStallUpdateHandler(&serv)
	world := service.GetWorldServiceInstance()
	competitiveRewards.ConfigureOnlinePlayers(world)
	world.ConfigureGameplayRates(gameplayRates)
	service.GetPartyServiceInstance().ConfigureGameplayRates(gameplayRates)
	manager.GetMobAIManagerInstance().ConfigureGameplayRates(gameplayRates)
	if features.Enabled(feature.OpenMarket) {
		if _, err := market.RegisterHandlers(&serv, repositories.OpenMarket, repositories.Inventory, world); err != nil {
			return AgentServer{}, fmt.Errorf("configure Agent open-market handlers: %w", err)
		}
		agentServer.MarketExpiry, err = service.NewOpenMarketExpiryService(repositories.OpenMarket, repositories.Inventory, world)
		if err != nil {
			return AgentServer{}, fmt.Errorf("configure Agent open-market expiry: %w", err)
		}
	}
	if features.Enabled(feature.ServerEventSystem) {
		agentServer.ServerEventRewards, err = service.NewServerEventRewardRuntime(shardPool, gameplayRates, manager.GetRespawnManagerInstance(), world)
		if err != nil {
			return AgentServer{}, fmt.Errorf("configure server event reward runtime: %w", err)
		}
	}
	exchangeManager := exchange.NewManager(world, repositories.Exchange)
	if err := exchange.RegisterHandlers(&serv, exchangeManager); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent exchange handlers: %w", err)
	}
	agentServer.Exchange = exchangeManager
	agentServer.GuildStorage = repositories.GuildStorage
	agentServer.Vitals = repositories.Vitals
	agentServer.Inventory = inventory.RegisterInventoryHandlerWithGuildStorage(&serv, repositories.InventoryRelocator, exchangeManager, repositories.Economy, repositories.GuildStorage, repositories.AvatarInventory, repositories.ItemMall)
	agentServer.Inventory.ConfigureGameplayRates(gameplayRates)
	agentServer.Inventory.ConfigureMagicCube(repositories.MagicCube)
	agentServer.Inventory.ConfigureGroundItemPickupRepository(repositories.GroundItemPickup)
	agentServer.Inventory.ConfigureGroundGoldPickupRepository(repositories.GroundGoldPickup)
	agentServer.Inventory.ConfigureGroundItemDropRepository(repositories.GroundItemDrop)
	agentServer.Inventory.ConfigureGroundItemService(agentServer.GroundItems)
	agentServer.Combat.ConfigureGroundItemPickup(agentServer.Inventory.HandleGroundItemAction)
	if groundItemLifecycle != nil {
		agentServer.Inventory.ConfigureGroundItemLifecycle(groundItemLifecycle)
	}
	agentServer.Inventory.ConfigureQuestProgressRepository(questRuntime)
	agentServer.Inventory.ConfigureOldWomanSearch(questRuntime)
	agentServer.Inventory.ConfigureNativeCatchActions(questRuntime)
	agentServer.Inventory.ConfigureJobSuitTeleport(teleportHandler)
	cosInventoryRepository, ok := repositories.COS.(model.COSInventoryTransferRepository)
	if !ok {
		return AgentServer{}, fmt.Errorf("configure Agent COS inventory transfer: repository does not support transfers")
	}
	agentServer.Inventory.ConfigureCOSRepository(cosInventoryRepository)
	agentServer.Inventory.ConfigureSafeTrade(repositories.SafeTrade, cosLifecycle.Service(), world)
	agentServer.Inventory.ConfigurePackageItems(repositories.PackageItems)
	itemUseHandler := inventory.RegisterItemUseHandler(&serv, repositories.ItemUse, teleportHandler)
	itemUseHandler.ConfigureDimensionHolePortals(agentServer.DimensionHoles.Lifecycle)
	itemUseHandler.ConfigureQuestInstanceEntry(agentServer.QuestInstanceLifecycle, teleportHandler)
	itemUseHandler.ConfigureDevilSpirit(repositories.AvatarInventory)
	itemUseHandler.ConfigureCOS(cosLifecycle.Service())
	agentServer.COS = cosLifecycle.Service()
	if agentServer.COS != nil && agentServer.GroundItems != nil {
		agentServer.COS.ConfigureCargoGroundItems(agentServer.GroundItems)
	}
	if agentServer.COS != nil && groundItemLifecycle != nil {
		// COS collection shares the ground-item timer cancellation edge with
		// player pickup; the persistence transaction has already committed
		// before the service removes the visible object.
		agentServer.COS.ConfigureGroundItemLifecycle(groundItemLifecycle)
	}
	if agentServer.COS != nil {
		agentServer.COS.ConfigureGroundGoldPickup(repositories.GroundGoldPickup)
		agentServer.COS.ConfigureQuestProgress(questRuntime)
	}
	gameReadyHandler.ConfigureCOS(cosLifecycle.Service())
	manager.GetGameTimeManagerInstance().ConfigureCOS(cosLifecycle.Service())
	itemUseHandler.ConfigureTimedBuffDuration(viper.GetBool(config.AgentTimedBuffDuration))
	alchemyHandler := inventory.RegisterAlchemyHandler(&serv, repositories.Alchemy)
	manager.GetGameTimeManagerInstance().ConfigureAlchemy(alchemyHandler)
	manager.GetGameTimeManagerInstance().ConfigureNativeQuests(
		manager.NewNativeQuestManager(questRuntime, manager.GetRespawnManagerInstance()),
	)
	party.RegisterPartyHandlers(&serv, socialHandler, teleportHandler)

	if err := world.ConfigureRepositories(service.WorldRepositories{
		RegionCatalog: repositories.Regions,
		SpawnCatalog:  repositories.Spawns,
		Positions:     repositories.Positions,
		TeleportGates: repositories.TeleportGates,
	}); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent world repositories: %w", err)
	}
	agentServer.World = world
	fortressScheduler, _ := repositories.Social.(scheduledFortressController)
	if schedulerStore != nil {
		agentServer.Scheduler, err = scheduler.NewRuntime(schedulerStore, scheduler.RuntimeOptions{
			Worker:        viper.GetString(config.AgentModuleId),
			PollInterval:  time.Duration(viper.GetInt(config.AgentSchedulerPollMS)) * time.Millisecond,
			LeaseDuration: time.Duration(viper.GetInt(config.AgentSchedulerLeaseSeconds)) * time.Second,
			BatchSize:     viper.GetInt(config.AgentSchedulerBatchSize),
			RetryPolicy: scheduler.RetryPolicy{
				MaxAttempts: 5,
				BaseDelay:   time.Second,
				MaxDelay:    time.Minute,
			},
			OnError: func(err error) { log.WithError(err).Error("Agent scheduler cycle failed") },
		}, agentSchedulerHandlersWithCompetitive(world, manager.GetRespawnManagerInstance(), gameplayRates, serverEvents, competitiveRuntime, fortressScheduler))
		if err != nil {
			return AgentServer{}, fmt.Errorf("configure Agent scheduler runtime: %w", err)
		}
	}
	serv.SetSessionClosingHandler(agentServer.playerSessionClosing)
	serv.SetSessionClosedHandler(agentServer.playerSessionClosed)
	if viper.GetBool(config.AgentManagementEnabled) {
		managementTLS, managementTLSErr := mtls.LoadServerConfig(mtls.Files{
			CertFile: viper.GetString(config.AgentManagementTLSCertFile),
			KeyFile:  viper.GetString(config.AgentManagementTLSKeyFile),
			CAFile:   viper.GetString(config.AgentManagementTLSClientCAFile),
		})
		if managementTLSErr != nil {
			return AgentServer{}, fmt.Errorf("configure SMC-to-Agent management mTLS server: %w", managementTLSErr)
		}
		var scheduleAdministration management.SchedulerSource
		if schedulerStore != nil && agentServer.Scheduler != nil {
			scheduleAdministration = agentSchedulerAdministration{store: schedulerStore, runtime: agentServer.Scheduler}
		}
		agentServer.ManagementServer, err = management.NewServerWithScheduler(
			net.JoinHostPort(viper.GetString(config.AgentManagementHost), fmt.Sprintf("%d", viper.GetInt(config.AgentManagementPort))),
			managementTLS,
			world,
			scheduleAdministration,
			agentDatabaseHealthSource{pools: pools, engine: &serv, competitive: competitiveRuntime},
		)
		if err != nil {
			return AgentServer{}, fmt.Errorf("configure Agent management listener: %w", err)
		}
	}

	keepPools = true
	return agentServer, nil
}

// Start reserves the public SRO listener before binding the private installer.
// The mTLS endpoint is exposed only after both ports are successfully bound,
// so Gateway cannot receive an ACK for an Agent whose advertised public port
// failed to start.
func (a *AgentServer) Start() error {
	if a == nil || a.Server == nil || a.AdmissionTicketServer == nil {
		return fmt.Errorf("Agent admission service is not configured")
	}
	if a.Lifecycle == nil {
		return fmt.Errorf("Agent lifecycle is not configured")
	}
	if state := a.Lifecycle.Snapshot().State; state != agentruntime.Loading {
		return fmt.Errorf("Agent cannot start from lifecycle state %s", state)
	}
	publicListener, err := a.Server.Listen()
	if err != nil {
		a.markDegraded("public listener bind failed")
		return fmt.Errorf("bind Agent public listener: %w", err)
	}
	if err := a.AdmissionTicketServer.Start(); err != nil {
		_ = a.Server.CloseListener()
		a.markDegraded("admission listener startup failed")
		return fmt.Errorf("start Gateway admission ticket listener: %w", err)
	}
	if a.ManagementServer != nil {
		if err := a.ManagementServer.Start(); err != nil {
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 5*time.Second)
			_ = a.AdmissionTicketServer.Close(rollbackCtx)
			cancelRollback()
			_ = a.Server.CloseListener()
			a.markDegraded("management listener startup failed")
			return fmt.Errorf("start Agent management listener: %w", err)
		}
	}
	if a.Scheduler != nil {
		if err := a.Scheduler.Start(a.packetContext); err != nil {
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 5*time.Second)
			if a.ManagementServer != nil {
				_ = a.ManagementServer.Close(rollbackCtx)
			}
			_ = a.AdmissionTicketServer.Close(rollbackCtx)
			cancelRollback()
			_ = a.Server.CloseListener()
			a.markDegraded("scheduler startup failed")
			return fmt.Errorf("start Agent scheduler: %w", err)
		}
	}
	if a.GameplayRates != nil {
		a.GameplayRates.Start(a.packetContext)
	}
	if a.COSPolicy != nil {
		a.COSPolicy.Start(a.packetContext)
	}
	environment.StartWeatherSystem(a.packetContext, service.GetWorldServiceInstance())
	if a.ChatFilter != nil {
		go a.refreshChatFilter(a.packetContext)
	}
	if a.MarketExpiry != nil {
		a.MarketExpiry.Start(a.packetContext)
	}
	if a.Competitive != nil {
		if err := a.Competitive.Run(a.packetContext); err != nil {
			return fmt.Errorf("start competitive runtime: %w", err)
		}
	}
	if err := a.Lifecycle.Transition(agentruntime.Ready, ""); err != nil {
		if a.cancelPacketHandling != nil {
			a.cancelPacketHandling()
		}
		rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 5*time.Second)
		if a.ManagementServer != nil {
			_ = a.ManagementServer.Close(rollbackCtx)
		}
		_ = a.AdmissionTicketServer.Close(rollbackCtx)
		if a.Scheduler != nil {
			_ = a.Scheduler.Wait(rollbackCtx)
		}
		cancelRollback()
		_ = a.Server.CloseListener()
		return fmt.Errorf("enter Agent ready lifecycle: %w", err)
	}
	if a.DependencyMonitor != nil {
		a.DependencyMonitor.Start(a.packetContext)
	}
	if a.ServerEventRewards != nil {
		a.ServerEventRewards.Start(a.packetContext)
	}
	if a.Social != nil {
		a.Social.Start(a.packetContext)
	}
	go func() {
		if err := a.Server.Serve(publicListener); err != nil {
			if a.packetContext == nil || a.packetContext.Err() == nil {
				log.Errorf("Agent public listener stopped unexpectedly: %v", err)
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if shutdownErr := a.Close(shutdownCtx); shutdownErr != nil {
				log.Errorf("Agent shutdown after listener failure did not fully drain: %v", shutdownErr)
			}
		}
	}()
	go a.handlePackets(a.packetContext)
	return nil
}

func (a *AgentServer) refreshChatFilter(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := a.ChatFilter.Refresh(refreshCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Warnf("database-backed chat filter refresh failed; retaining prior snapshot: %v", err)
			}
		}
	}
}

// Close performs the Agent-facing portion of the ordered shutdown protocol:
// first stop public accepts, then stop Gateway ticket installation, and only
// then drain sessions whose terminal hooks release world/admission state. The
// process entrypoint owns signal handling; framework.Server never terminates
// the process from inside its accept loop.
func (a *AgentServer) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	manager.StopGameTimeWatchdog()
	if ctx == nil {
		ctx = context.Background()
	}
	if a.Lifecycle != nil {
		if a.Lifecycle.Snapshot().State == agentruntime.Stopped {
			return nil
		}
		if err := a.Lifecycle.Transition(agentruntime.Draining, ""); err != nil {
			return fmt.Errorf("enter Agent draining lifecycle: %w", err)
		}
		defer func() {
			_ = a.Lifecycle.Transition(agentruntime.Stopped, "")
		}()
	}
	if a.cancelPacketHandling != nil {
		a.cancelPacketHandling()
	}

	var closeErrors []error
	if a.Server != nil {
		if err := a.Server.StopAccepting(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErrors = append(closeErrors, fmt.Errorf("stop Agent public listener: %w", err))
		}
	}
	if a.ManagementServer != nil {
		if err := a.ManagementServer.Close(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop Agent management listener: %w", err))
		}
	}
	if a.AdmissionTicketServer != nil {
		if err := a.AdmissionTicketServer.Close(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop Gateway admission ticket listener: %w", err))
		}
	}
	if a.Server != nil {
		if err := a.Server.DrainSessions(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("drain Agent sessions: %w", err))
		}
	}
	if a.World != nil {
		if err := a.World.DrainPositionPersistence(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("drain Agent world position persistence: %w", err))
		}
	}
	if a.GroundItemLifecycle != nil {
		a.GroundItemLifecycle.Close()
	}
	if a.DimensionHoles != nil && a.DimensionHoles.Lifecycle != nil {
		if err := a.DimensionHoles.Lifecycle.Close(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close Dimension Hole runtime: %w", err))
		}
	}
	if a.GameplayRates != nil {
		if err := a.GameplayRates.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop gameplay rate refresher: %w", err))
		}
	}
	if a.COSPolicy != nil {
		if err := a.COSPolicy.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop COS policy refresher: %w", err))
		}
	}
	if a.Scheduler != nil {
		if err := a.Scheduler.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop Agent scheduler: %w", err))
		}
	}
	if a.Competitive != nil {
		if err := a.Competitive.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop competitive runtime: %w", err))
		}
	}
	if a.MarketExpiry != nil {
		if err := a.MarketExpiry.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop open-market expiry: %w", err))
		}
	}
	if a.DependencyMonitor != nil {
		if err := a.DependencyMonitor.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop dependency monitor: %w", err))
		}
	}
	if a.ServerEventRewards != nil {
		if err := a.ServerEventRewards.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop server event rewards: %w", err))
		}
	}
	if err := a.closePools(); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("close Agent database pools: %w", err))
	}
	return errors.Join(closeErrors...)
}

// Degrade immediately fails closed for public application admission and the
// private Gateway readiness/install endpoint. Existing sessions remain alive
// for the caller to save or drain according to the dependency failure policy.
func (a *AgentServer) Degrade(reason string) error {
	if a == nil || a.Lifecycle == nil {
		return fmt.Errorf("Agent lifecycle is not configured")
	}
	return a.Lifecycle.Transition(agentruntime.Degraded, reason)
}

// DrainForDependencyLoss implements the binary-confirmed ShardManager-loss
// policy without terminating the consolidated Agent process. Readiness fails
// closed first, then the current session snapshot runs through the ordinary
// terminal save hooks, and finally queued position checkpoints are flushed.
// The management listener and database pools remain available for diagnosis
// and an orchestrated restart.
func (a *AgentServer) DrainForDependencyLoss(ctx context.Context, reason string) error {
	if a == nil || a.Lifecycle == nil {
		return fmt.Errorf("Agent lifecycle is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	drainMu := a.dependencyDrainMu
	if drainMu == nil {
		// Preserve zero-value AgentServer fixtures while production composition
		// always installs the mutex before the server is published.
		drainMu = &sync.Mutex{}
		a.dependencyDrainMu = drainMu
	}
	drainMu.Lock()
	defer drainMu.Unlock()

	snapshot := a.Lifecycle.Snapshot()
	if snapshot.State == agentruntime.Draining || snapshot.State == agentruntime.Stopped {
		return nil
	}
	if snapshot.State != agentruntime.Degraded {
		if err := a.Degrade(reason); err != nil {
			return fmt.Errorf("degrade Agent after dependency loss: %w", err)
		}
	}
	var drainErrors []error
	if a.Server != nil {
		if err := a.Server.DrainSessions(ctx); err != nil {
			drainErrors = append(drainErrors, fmt.Errorf("drain sessions after dependency loss: %w", err))
		}
	}
	if a.World != nil {
		if err := a.World.DrainPositionPersistence(ctx); err != nil {
			drainErrors = append(drainErrors, fmt.Errorf("flush positions after dependency loss: %w", err))
		}
	}
	return errors.Join(drainErrors...)
}

func (a *AgentServer) markDegraded(reason string) {
	if a != nil && a.Lifecycle != nil {
		_ = a.Lifecycle.Transition(agentruntime.Degraded, reason)
	}
}

func (a *AgentServer) closePools() error {
	if a == nil || a.Pools == nil {
		return nil
	}
	return a.Pools.Close()
}

func (a *AgentServer) handlePackets(ctx context.Context) {
	if a == nil || a.Server == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-a.Server.PacketChannel:
			switch data.MessageID {
			case opcode.StallTalkRequest:
				log.Debugf("Stall Talk Request not handled")

			default:
				if a.UnhandledPacketsLogger != nil {
					a.UnhandledPacketsLogger.Printf("Unhandled packet opcode=0x%04X size=%d\n", data.MessageID, data.Packet.PayloadLen())
				}
			}
		}
	}
}

// playerSessionClosing releases admission at transport shutdown. It must not
// wait for packet workers or persistence; a stalled handler cannot retain the
// account reservation indefinitely.
func (a *AgentServer) playerSessionClosing(closedSession *server.Session) {
	if a == nil || closedSession == nil || a.AdmissionTickets == nil {
		return
	}
	a.AdmissionTickets.ReleaseAccount(closedSession.AdmissionAccountID())
}

// playerSessionClosed runs through Server's drained terminal hook. It must not
// remove the session itself: Server already removed it before worker and sender
// drain began.
func (a *AgentServer) playerSessionClosed(closedSession *server.Session) {
	if closedSession == nil {
		return
	}
	if a.AdmissionTickets != nil {
		// The transport is already terminal and has been removed from Server's
		// session registry before this hook runs. Release its account reservation
		// before the potentially blocking persistence/world teardown below. If a
		// teardown dependency stalls, retaining the reservation here permanently
		// rejects otherwise valid reconnects as an already-connected login. World
		// admission independently prevents a still-retained character instance
		// from being joined twice while cleanup finishes.
		a.AdmissionTickets.ReleaseAccount(closedSession.UserContext.UserID)
	}
	if closedSession.UserContext.UniqueID == 0 {
		return
	}
	if a.Exchange != nil {
		a.Exchange.CancelPlayer(closedSession.UserContext.CharacterID)
	}
	if a.Competitive != nil {
		a.Competitive.RemoveCharacter(closedSession.UserContext.CharacterID)
	}
	character.ClearNPCInteraction(closedSession.UserContext.UniqueID)
	party.HandleDisconnect(closedSession.UserContext.UniqueID)
	log.Debugf("player %s disconnected", closedSession.UserContext.CharName)
	//a.SpawnEngine.PlayerDisconnected(closedSession.UserContext.UniqueID, closedSession.UserContext.CharName)
	world := a.World
	if world == nil {
		// Retain the compatibility path for focused lifecycle tests that
		// construct AgentServer directly instead of through NewAgentServer.
		world = service.GetWorldServiceInstance()
	}
	player, playerErr := world.GetPlayerByUniqueId(closedSession.UserContext.UniqueID)
	if playerErr != nil || player == nil {
		world.PlayerDisconnected(closedSession.UserContext.UniqueID, closedSession.UserContext.CharName)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), service.DefaultDisconnectSaveTimeout)
	defer cancel()
	atomicSaveAttempted := false
	if a.DisconnectSaver != nil {
		if err := world.PrepareDisconnectSave(ctx, closedSession.UserContext.UniqueID); err != nil {
			log.WithError(err).Warn("prepare character disconnect save")
		} else {
			var cosRecords []model.COSRecord
			if a.COS != nil {
				cosRecords = a.COS.SnapshotOwned(player)
			}
			snapshot, err := service.NewDisconnectSaveSnapshot(closedSession.ID, player, cosRecords)
			if err == nil {
				atomicSaveAttempted = true
				_, err = a.DisconnectSaver.Save(ctx, snapshot)
			}
			if err != nil {
				log.WithError(err).Warn("atomically save character on disconnect")
			}
		}
	}
	if a.GuildStorage != nil && closedSession.UserContext.CharacterID > 0 {
		if err := a.GuildStorage.CloseGuildStorage(ctx, int64(closedSession.UserContext.CharacterID)); err != nil {
			log.WithError(err).Debug("release guild storage on disconnect")
		}
	}
	if a.COS != nil {
		if atomicSaveAttempted {
			a.COS.DetachAll(player)
		} else if err := a.COS.CheckpointAndDetachAll(ctx, player); err != nil {
			log.WithError(err).Warn("save and remove character COS on disconnect")
		}
	}
	if atomicSaveAttempted {
		world.PlayerDisconnectedAfterSave(closedSession.UserContext.UniqueID, closedSession.UserContext.CharName)
		return
	}
	if a.Vitals != nil {
		hp, mp := player.CurrentVitals()
		if err := a.Vitals.SaveVitals(ctx, player.ID, hp, mp); err != nil {
			log.WithError(err).Warn("save character vitals on disconnect")
		}
	}
	world.PlayerDisconnected(closedSession.UserContext.UniqueID, closedSession.UserContext.CharName)
}

// buildReferenceObjectIndex returns a fresh runtime lookup built from the
// current item and character snapshots. Characters intentionally override
// items for a duplicate ID, matching the previous item-then-character load
// order without retaining objects that disappeared from either snapshot.
func buildReferenceObjectIndex(items map[uint32]model.RefItem, characters map[uint32]model.RefChar) map[uint32]model.RefObject {
	objects := make(map[uint32]model.RefObject, len(items)+len(characters))
	for id, item := range items {
		objects[id] = item.RefObject
	}
	for id, character := range characters {
		objects[id] = character.RefObject
	}
	return objects
}

func main() {
	config.Initialize()
	logging.Init()
	diagnostics, err := startAgentDiagnostics(viper.GetBool(config.AgentDiagnosticsEnabled), viper.GetInt(config.AgentDiagnosticsPort))
	if err != nil {
		log.Fatalf("Agent diagnostics startup failed: %v", err)
	}
	boot.SetPhases("gamedata", "services", "packethandler", "network")
	reader := bufio.NewReader(os.Stdin)
	shutdownSignal, stopSignalHandling := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignalHandling()

	// Construct the Agent before any gamedata component runs. NewAgentServer
	// wires the world catalogs and checkpoint repository to its long-lived
	// shard pool; constructing it after Boot would let setupWorld fall back to
	// the legacy per-operation database adapters during production startup.
	// This does not expose either listener yet: Start remains in the network
	// phase below.
	agentServer, err := NewAgentServer()
	if err != nil {
		log.Fatalf("Agent admission mTLS startup failed: %v", err)
	}

	loadGameData := func() {
		log.Infof("loaded database-backed level data through level %d", agentServer.LevelTable.MaxLevel())

		if agentServer.ReferenceData == nil {
			log.Fatal("Agent reference-data repository is not configured")
		}
		loadContext, cancelLoad := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelLoad()
		snapshot, err := agentServer.ReferenceData.Load(loadContext)
		if err != nil {
			log.Fatalf("Agent reference-data bootstrap failed: %v", err)
		}
		referenceCatalog := model.NewReferenceDataCatalog(snapshot)
		service.GetWorldServiceInstance().ConfigureReferenceData(referenceCatalog)
		agentServer.Combat.ConfigureReferenceData(referenceCatalog)
		agentServer.Inventory.ConfigureReferenceData(referenceCatalog)
		if err := agentServer.CharacterSelection.ConfigureReferenceData(referenceCatalog); err != nil {
			log.Fatalf("Agent character passive-skill bootstrap failed: %v", err)
		}
		if err := agentServer.Skill.ConfigureReferenceData(referenceCatalog); err != nil {
			log.Fatalf("Agent skill reference-data bootstrap failed: %v", err)
		}
		if err := agentServer.SkillRestoration.ConfigureReferenceData(referenceCatalog); err != nil {
			log.Fatalf("Agent skill-restoration reference-data bootstrap failed: %v", err)
		}
		if err := manager.GetRespawnManagerInstance().ConfigureReferenceData(referenceCatalog); err != nil {
			log.Fatalf("Agent respawn reference-data bootstrap failed: %v", err)
		}
		manager.GetMobAIManagerInstance().ConfigureReferenceData(referenceCatalog)

		// Publish only after the complete snapshot loaded successfully. In
		// particular, a later catalog failure cannot leave items paired with stale
		// characters from an earlier load.
		model.RefItems = snapshot.Items
		model.RefChars = snapshot.Characters
		model.RefSkills = snapshot.Skills
		model.RefObjects = buildReferenceObjectIndex(snapshot.Items, snapshot.Characters)

		if agentServer.MonsterDropCatalog != nil {
			dropCatalog, dropErr := agentServer.MonsterDropCatalog.Load(loadContext)
			if dropErr != nil {
				log.Fatalf("Agent monster-drop catalog bootstrap failed: %v", dropErr)
			}
			agentServer.MonsterDrops = service.NewMonsterDropService(
				dropCatalog,
				service.NewLockedDropRandom(time.Now().UnixNano()),
				agentServer.GroundItems,
				agentServer.GroundItems.Materializer(),
			)
			agentServer.MonsterDrops.ConfigureGameplayRates(agentServer.GameplayRates)
			if arenaErr := agentServer.MonsterDrops.ConfigureBattleArenaTicketDropRate(viper.GetInt(config.AgentBattleArenaTicketDropRate)); arenaErr != nil {
				log.Fatalf("Agent Battle Arena ticket bootstrap failed: %v", arenaErr)
			}
			if winterErr := agentServer.MonsterDrops.ConfigureWinterEvent(agentServer.Features.Enabled(feature.WinterEvent2009)); winterErr != nil {
				log.Fatalf("Agent Winter Event 2009 bootstrap failed: %v", winterErr)
			}
			if agentServer.Combat != nil {
				agentServer.Combat.ConfigureMonsterDrops(agentServer.MonsterDrops)
			}
		}
	}

	boot.RegisterComponent("gamedata", loadGameData, 1)

	setupWorld := func() {
		world := service.GetWorldServiceInstance()
		bootstrapContext, cancelBootstrap := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelBootstrap()
		if err := world.BootstrapGameServerRegionsContext(bootstrapContext, 1); err != nil {
			log.Fatalf("Agent world bootstrap failed: %v", err)
		}
	}

	boot.RegisterComponent("gamedata", setupWorld, 1)

	startServices := func() {
		gameTime := manager.GetGameTimeManagerInstance()
		gameTime.Start()
		gameTime.StartWatchdog()
		//manager.GetKnownListManager().Start()
		manager.GetSpawnManagerInstance().Start()
		manager.GetRespawnManagerInstance().Start()
		manager.GetMobAIManagerInstance().Start()
	}

	boot.RegisterComponent("services", startServices, 1)

	log.Println("starting agent server...")
	startAgentServer := func() {
		if err := agentServer.Start(); err != nil {
			log.Fatalf("Agent server startup failed: %v", err)
		}
	}

	boot.RegisterComponent("network", startAgentServer, 1)
	boot.Boot()

	log.Println("Press Enter to exit...")
	exitRequested := make(chan struct{})
	go func() {
		if _, err := reader.ReadString('\n'); err == nil {
			close(exitRequested)
		}
	}()
	select {
	case <-shutdownSignal.Done():
		log.Info("shutdown signal received; draining Agent server")
	case <-exitRequested:
		log.Info("interactive shutdown requested; draining Agent server")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := agentServer.Close(shutdownCtx); err != nil {
		log.Errorf("Agent shutdown did not fully drain: %v", err)
	}
	if err := diagnostics.Close(shutdownCtx); err != nil {
		log.Errorf("Agent diagnostics shutdown failed: %v", err)
	}
}
