package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/config"
	"github.com/Harvi8/AegisCore/agent-server/database"
	"github.com/Harvi8/AegisCore/agent-server/handler/character"
	"github.com/Harvi8/AegisCore/agent-server/handler/chat"
	"github.com/Harvi8/AegisCore/agent-server/handler/cos"
	"github.com/Harvi8/AegisCore/agent-server/handler/exchange"
	"github.com/Harvi8/AegisCore/agent-server/handler/inventory"
	"github.com/Harvi8/AegisCore/agent-server/handler/lobby"
	"github.com/Harvi8/AegisCore/agent-server/handler/logout"
	"github.com/Harvi8/AegisCore/agent-server/handler/party"
	"github.com/Harvi8/AegisCore/agent-server/handler/ping"
	"github.com/Harvi8/AegisCore/agent-server/handler/social"
	"github.com/Harvi8/AegisCore/agent-server/handler/stall"
	"github.com/Harvi8/AegisCore/agent-server/management"
	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/boot"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/logging"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/security/mtls"
	"github.com/Harvi8/AegisCore/framework/server"
	gwHandlers "github.com/Harvi8/AegisCore/gateway-server/handler"
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
	// LevelTable is loaded from the client-matched LevelData.txt during
	// gamedata bootstrap before any character can enter the world.
	LevelTable *model.LevelTable
	// Exchange owns the in-memory two-party exchange state so disconnect
	// teardown can release the peer immediately.
	Exchange *exchange.Manager
	// GuildStorage owns the database-backed exclusive guild chest lease and is
	// retained so disconnect teardown can release it immediately.
	GuildStorage       model.GuildStorageRepository
	Combat             *character.CombatHandler
	Hwan               *character.HwanHandler
	Skill              *character.SkillHandler
	CharacterSelection *lobby.CharacterSelectionHandler
	Vitals             model.CharacterVitalsRepository
	GameplayRates      *manager.GameplayRateManager
	COS                *service.COSService
	//SpawnEngine             *spawn.SpawnEngine
}

// NewAgentServer constructs both Agent listeners but does not expose either
// until Start succeeds. TLS material is mandatory: there is no public SRO
// 0xE000 backend-authentication fallback or shared-secret configuration.
func NewAgentServer() (AgentServer, error) {
	levelDataPath := viper.GetString(config.AgentLevelDataFile)
	levelDataFile, err := os.Open(levelDataPath)
	if err != nil {
		return AgentServer{}, fmt.Errorf("open Agent level data %q: %w", levelDataPath, err)
	}
	levelTable, err := model.LoadLevelTable(levelDataFile)
	closeErr := levelDataFile.Close()
	if err != nil {
		return AgentServer{}, fmt.Errorf("parse Agent level data %q: %w", levelDataPath, err)
	}
	if closeErr != nil {
		return AgentServer{}, fmt.Errorf("close Agent level data %q: %w", levelDataPath, closeErr)
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
	rateContext, cancelRateLoad := context.WithTimeout(context.Background(), 5*time.Second)
	err = gameplayRates.Refresh(rateContext)
	cancelRateLoad()
	if err != nil {
		log.Errorf("initial gameplay rate load failed; using hardcoded 100%% EXP, skill EXP, mob count, and mob respawn: %v", err)
	}

	serv := server.NewEngine(
		viper.GetString(config.AgentHost),
		viper.GetInt(config.AgentPort),
		network.EncodingOptions{
			None:       false,
			Disabled:   false,
			Encryption: true,
			// The stock client starts a fresh Agent connection after an in-game
			// restart but does not reliably reset its sequence/CRC state. Keeping
			// EDC enabled makes that otherwise valid reconnect fail before it can
			// redeem its Gateway-issued admission ticket. Encryption and the key
			// exchange remain enabled.
			EDC:          false,
			KeyExchange:  true,
			KeyChallenge: false,
		},
	)
	serv.ModuleID = viper.GetString(config.AgentModuleId)
	serv.SetTrafficFilter(server.TrafficFilterOptions{
		Window:      time.Duration(viper.GetInt(config.AgentTrafficWindowMS)) * time.Millisecond,
		PacketLimit: uint32(viper.GetUint(config.AgentTrafficPacketLimit)),
	})
	// Before an atomically redeemed ticket marks the session authenticated,
	// public Agent accepts exactly the stock 0x6103 AuthRequest. Afterwards
	// it exposes lobby packets, then only loading handshake packets while a
	// character bootstrap is pending, before admitting normal in-world traffic. In particular,
	// legacy 0xE000 backend authentication is rejected at the listener edge.
	serv.SetAdmissionPolicy(server.AgentLifecycleAdmissionPolicy(
		opcode.AuthRequest,
		[]uint16{
			opcode.JoinLobbyRequest,
			opcode.LobbyActionRequest,
			opcode.LobbyRenameRequest,
			opcode.AgentPingRequest,
		},
		[]uint16{opcode.TeleportReadyResponse, opcode.GameReadyRequest, opcode.AgentPingRequest},
	))

	tickets := auth.NewAdmissionTicketStore()
	admissionServer, err := auth.NewAdmissionTicketInstallServer(
		net.JoinHostPort(
			viper.GetString(config.AgentAdmissionHost),
			fmt.Sprintf("%d", viper.GetInt(config.AgentAdmissionPort)),
		),
		tlsConfig,
		tickets,
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
		LevelTable:             levelTable,
		GameplayRates:          gameplayRates,
	})
	if err != nil {
		return AgentServer{}, fmt.Errorf("compose Agent runtime: %w", err)
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
	lobby.RegisterCharSelectionRenameRequestHandler(&serv, repositories.CharacterRename)
	gameReadyHandler := character.RegisterGameReadyHandler(&serv, repositories.ItemMall)
	character.RegisterGuideHandler(&serv)
	agentServer.Hwan, err = character.RegisterHwanHandler(&serv, repositories.Hwan)
	if err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent HWAN handler: %w", err)
	}
	character.RegisterPremiumDailyHandler(&serv, repositories.ItemServices)
	ping.RegisterHandler(&serv)
	movementHandler := character.RegisterMovementHandler(&serv)
	character.RegisterSelectEntityHandler(&serv)
	character.RegisterDeselectEntityHandler(&serv)
	cos.RegisterMountStateHandler(&serv)
	cosLifecycle := cos.RegisterLifecycleHandlers(&serv, repositories.COS)
	if err := character.RegisterNPCTalkHandler(&serv, repositories.CharacterQuests, questRuntime); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent NPC talk handler: %w", err)
	}
	if err := character.RegisterQuestHandler(&serv, repositories.CharacterQuests); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent quest handlers: %w", err)
	}
	teleportHandler := character.RegisterTeleportHandler(&serv, repositories.Teleport)
	character.RegisterResurrectionHandler(&serv, repositories.Vitals, repositories.ItemUse)
	manager.GetMobAIManagerInstance().ConfigureVitals(repositories.Vitals)
	manager.GetRespawnManagerInstance().ConfigureGameplayRates(gameplayRates)
	manager.GetRespawnManagerInstance().ConfigureGiantSpawnRatio(viper.GetInt(config.AgentGiantMonsterSpawnRatio))
	agentServer.Combat = character.RegisterCombatHandler(&serv, repositories.CharacterProgress, levelTable, gameplayRates, questRuntime)
	agentServer.Combat.ConfigureVitals(repositories.Vitals)
	agentServer.Combat.ConfigureMonsterMasks(repositories.MonsterMasks)
	agentServer.Combat.ConfigureHwan(repositories.Hwan)
	cosLifecycle.Service().ConfigureCombat(agentServer.Combat, levelTable)
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
	if err := character.RegisterQuickSlotHandler(&serv, repositories.QuickSlots); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent character quick-slot handler: %w", err)
	}
	socialHandler := social.RegisterHandlers(&serv, repositories.Social)
	gameReadyHandler.ConfigureSocial(socialHandler)
	chat.RegisterChatHandler(&serv, repositories.Social)
	logout.RegisterLogoutHandler(&serv)
	stall.RegisterStallCreateHandler(&serv)
	stall.RegisterStallDestroyHandler(&serv)
	stall.RegisterStallLeaveHandler(&serv)
	stall.RegisterStallUpdateHandler(&serv)
	world := service.GetWorldServiceInstance()
	world.ConfigureGameplayRates(gameplayRates)
	exchangeManager := exchange.NewManager(world, repositories.Exchange)
	if err := exchange.RegisterHandlers(&serv, exchangeManager); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent exchange handlers: %w", err)
	}
	agentServer.Exchange = exchangeManager
	agentServer.GuildStorage = repositories.GuildStorage
	agentServer.Vitals = repositories.Vitals
	inventory.RegisterInventoryHandlerWithGuildStorage(&serv, repositories.InventoryRelocator, exchangeManager, repositories.Economy, repositories.GuildStorage, repositories.AvatarInventory, repositories.ItemMall)
	itemUseHandler := inventory.RegisterItemUseHandler(&serv, repositories.ItemUse, teleportHandler)
	itemUseHandler.ConfigureDevilSpirit(repositories.AvatarInventory)
	itemUseHandler.ConfigureCOS(cosLifecycle.Service())
	agentServer.COS = cosLifecycle.Service()
	gameReadyHandler.ConfigureCOS(cosLifecycle.Service())
	manager.GetGameTimeManagerInstance().ConfigureCOS(cosLifecycle.Service())
	itemUseHandler.ConfigureTimedBuffDuration(viper.GetBool(config.AgentTimedBuffDuration))
	inventory.RegisterAlchemyHandler(&serv, repositories.Alchemy)
	party.RegisterPartyHandlers(&serv, socialHandler)

	if err := world.ConfigureRepositories(service.WorldRepositories{
		RegionCatalog: repositories.Regions,
		SpawnCatalog:  repositories.Spawns,
		Positions:     repositories.Positions,
		TeleportGates: repositories.TeleportGates,
	}); err != nil {
		return AgentServer{}, fmt.Errorf("configure Agent world repositories: %w", err)
	}
	agentServer.World = world
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
		agentServer.ManagementServer, err = management.NewServer(
			net.JoinHostPort(viper.GetString(config.AgentManagementHost), fmt.Sprintf("%d", viper.GetInt(config.AgentManagementPort))),
			managementTLS,
			world,
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
	publicListener, err := a.Server.Listen()
	if err != nil {
		return fmt.Errorf("bind Agent public listener: %w", err)
	}
	if err := a.AdmissionTicketServer.Start(); err != nil {
		_ = a.Server.CloseListener()
		return fmt.Errorf("start Gateway admission ticket listener: %w", err)
	}
	if a.ManagementServer != nil {
		if err := a.ManagementServer.Start(); err != nil {
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 5*time.Second)
			_ = a.AdmissionTicketServer.Close(rollbackCtx)
			cancelRollback()
			_ = a.Server.CloseListener()
			return fmt.Errorf("start Agent management listener: %w", err)
		}
	}
	if a.GameplayRates != nil {
		a.GameplayRates.Start(a.packetContext)
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

// Close performs the Agent-facing portion of the ordered shutdown protocol:
// first stop public accepts, then stop Gateway ticket installation, and only
// then drain sessions whose terminal hooks release world/admission state. The
// process entrypoint owns signal handling; framework.Server never terminates
// the process from inside its accept loop.
func (a *AgentServer) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
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
	if a.GameplayRates != nil {
		if err := a.GameplayRates.Wait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("stop gameplay rate refresher: %w", err))
		}
	}
	if err := a.closePools(); err != nil {
		closeErrors = append(closeErrors, fmt.Errorf("close Agent database pools: %w", err))
	}
	return errors.Join(closeErrors...)
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

// playerSessionClosed runs through Server's drained terminal hook. It must not
// remove the session itself: Server already removed it before worker and sender
// drain began.
func (a *AgentServer) playerSessionClosed(closedSession *server.Session) {
	if closedSession == nil {
		return
	}
	if a.AdmissionTickets != nil {
		// Keep a fully joined character reserved until its world teardown has
		// completed, while still releasing a successfully authenticated session
		// that disconnected before it acquired a character UID.
		defer a.AdmissionTickets.ReleaseAccount(closedSession.UserContext.UserID)
	}
	if closedSession.UserContext.UniqueID == 0 {
		return
	}
	if a.Exchange != nil {
		a.Exchange.CancelPlayer(closedSession.UserContext.CharacterID)
	}
	if a.GuildStorage != nil && closedSession.UserContext.CharacterID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := a.GuildStorage.CloseGuildStorage(ctx, int64(closedSession.UserContext.CharacterID)); err != nil {
			log.WithError(err).Debug("release guild storage on disconnect")
		}
		cancel()
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
	if a.Vitals != nil {
		if player, err := world.GetPlayerByUniqueId(closedSession.UserContext.UniqueID); err == nil && player != nil {
			hp, mp := player.CurrentVitals()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := a.Vitals.SaveVitals(ctx, player.ID, hp, mp); err != nil {
				log.WithError(err).Warn("save character vitals on disconnect")
			}
			cancel()
		}
	}
	if a.COS != nil {
		if player, err := world.GetPlayerByUniqueId(closedSession.UserContext.UniqueID); err == nil && player != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := a.COS.UnsummonAll(ctx, player); err != nil {
				log.WithError(err).Warn("save and remove character COS on disconnect")
			}
			cancel()
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
		log.Infof("loaded client-matched level data through level %d", agentServer.LevelTable.MaxLevel())

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
		agentServer.Combat.ConfigureReferenceData(referenceCatalog)
		if err := agentServer.CharacterSelection.ConfigureReferenceData(referenceCatalog); err != nil {
			log.Fatalf("Agent character passive-skill bootstrap failed: %v", err)
		}
		if err := agentServer.Skill.ConfigureReferenceData(referenceCatalog); err != nil {
			log.Fatalf("Agent skill reference-data bootstrap failed: %v", err)
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
		manager.GetGameTimeManagerInstance().Start()
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
