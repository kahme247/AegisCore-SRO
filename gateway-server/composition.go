package main

import (
	"context"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/handler"
)

// GatewayDependencies is the process-composition boundary for infrastructure
// already validated and created by the default configuration path.
type GatewayDependencies struct {
	Server       *server.Server
	LoginHandler *handler.LoginRequestHandler
	Pools        *frameworkdb.Pools
}

// NewGatewayServerWithDependencies constructs the Gateway lifecycle facade
// without reading configuration, loading TLS, or opening database pools.
func NewGatewayServerWithDependencies(dependencies GatewayDependencies) (GatewayServer, error) {
	if dependencies.Server == nil {
		return GatewayServer{}, fmt.Errorf("Gateway public server is required")
	}
	packetContext, cancelPacketHandling := context.WithCancel(context.Background())
	return GatewayServer{
		Server:               dependencies.Server,
		LoginHandler:         dependencies.LoginHandler,
		Pools:                dependencies.Pools,
		packetContext:        packetContext,
		cancelPacketHandling: cancelPacketHandling,
	}, nil
}
