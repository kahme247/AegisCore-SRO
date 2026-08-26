package main

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/feature"
	"github.com/Harvi8/AegisCore/agent-server/handler/exchange"
	"github.com/Harvi8/AegisCore/agent-server/management"
	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/model"
	agentruntime "github.com/Harvi8/AegisCore/agent-server/runtime"
	"github.com/Harvi8/AegisCore/agent-server/service"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
	log "github.com/sirupsen/logrus"
)

// AgentDependencies is the process-composition boundary for already-created
// infrastructure. Repository construction and handler wiring stay outside the
// runtime lifecycle object, while AgentServer retains ownership of resources
// that must be drained in a specific order.
type AgentDependencies struct {
	Server                 *server.Server
	AdmissionTickets       *auth.AdmissionTicketStore
	AdmissionTicketServer  *auth.AdmissionTicketInstallServer
	ManagementServer       *management.Server
	UnhandledPacketsLogger *log.Logger
	Pools                  *frameworkdb.Pools
	ReferenceData          model.ReferenceDataRepository
	World                  *service.WorldService
	LevelTable             *model.LevelTable
	Exchange               *exchange.Manager
	GuildStorage           model.GuildStorageRepository
	GameplayRates          *manager.GameplayRateManager
	DisconnectSaver        *service.DisconnectSaver
	Lifecycle              *agentruntime.Lifecycle
	Features               feature.Set
}

// NewAgentServerWithDependencies constructs the lifecycle facade without
// loading configuration, opening files, binding listeners, or creating
// database pools. NewAgentServer remains the compatibility composition root.
func NewAgentServerWithDependencies(dependencies AgentDependencies) (AgentServer, error) {
	if dependencies.Server == nil {
		return AgentServer{}, fmt.Errorf("Agent public server is required")
	}
	packetContext, cancelPacketHandling := context.WithCancel(context.Background())
	lifecycle := dependencies.Lifecycle
	if lifecycle == nil {
		lifecycle = agentruntime.NewLifecycle()
	}
	agent := AgentServer{
		Server:                 dependencies.Server,
		AdmissionTickets:       dependencies.AdmissionTickets,
		AdmissionTicketServer:  dependencies.AdmissionTicketServer,
		ManagementServer:       dependencies.ManagementServer,
		UnhandledPacketsLogger: dependencies.UnhandledPacketsLogger,
		packetContext:          packetContext,
		cancelPacketHandling:   cancelPacketHandling,
		Pools:                  dependencies.Pools,
		ReferenceData:          dependencies.ReferenceData,
		World:                  dependencies.World,
		LevelTable:             dependencies.LevelTable,
		Exchange:               dependencies.Exchange,
		GuildStorage:           dependencies.GuildStorage,
		GameplayRates:          dependencies.GameplayRates,
		DisconnectSaver:        dependencies.DisconnectSaver,
		Lifecycle:              lifecycle,
		Features:               cloneFeatureSet(dependencies.Features),
	}
	dependencies.Server.SetSessionClosedHandler(agent.playerSessionClosed)
	return agent, nil
}

func cloneFeatureSet(source feature.Set) feature.Set {
	result := make(feature.Set, len(source))
	for key, enabled := range source {
		result[key] = enabled
	}
	return result
}
