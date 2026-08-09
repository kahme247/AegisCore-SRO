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

	"github.com/Harvi8/AegisCore/framework/boot"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/logging"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	gatewaydb "github.com/Harvi8/AegisCore/gateway-server/db"
	"github.com/Harvi8/AegisCore/gateway-server/handler"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type GatewayServer struct {
	Server               *server.Server
	LoginHandler         *handler.LoginRequestHandler
	Pools                *frameworkdb.Pools
	packetContext        context.Context
	cancelPacketHandling context.CancelFunc
	startOnce            sync.Once
}

// NewGatewayServer validates the mTLS client configuration before the public
// Gateway listener can start. There is intentionally no legacy public backend
// channel or shared-secret fallback for Agent ticket installation.
func NewGatewayServer() (GatewayServer, error) {
	if err := config.ValidateGatewayRuntime(); err != nil {
		return GatewayServer{}, fmt.Errorf("validate Gateway runtime configuration: %w", err)
	}
	tlsConfig, err := auth.LoadAdmissionTicketClientTLSConfig(auth.MutualTLSFiles{
		CertFile:   viper.GetString(config.GatewayAdmissionTLSCertFile),
		KeyFile:    viper.GetString(config.GatewayAdmissionTLSKeyFile),
		CAFile:     viper.GetString(config.AgentAdmissionTLSCAFile),
		ServerName: viper.GetString(config.AgentAdmissionTLSServerName),
	})
	if err != nil {
		return GatewayServer{}, fmt.Errorf("configure Agent admission mTLS client: %w", err)
	}
	endpoint := "https://" + net.JoinHostPort(
		viper.GetString(config.AgentHost),
		fmt.Sprintf("%d", viper.GetInt(config.AgentAdmissionPort)),
	)
	installTimeout := time.Duration(viper.GetInt(config.AgentAdmissionTimeoutSeconds)) * time.Second
	if installTimeout <= 0 {
		installTimeout = auth.DefaultAdmissionTicketInstallTimeout
	}
	installer, err := auth.NewHTTPSAdmissionTicketInstaller(
		endpoint,
		tlsConfig,
		installTimeout,
	)
	if err != nil {
		return GatewayServer{}, fmt.Errorf("configure Agent admission installer: %w", err)
	}
	pools, err := frameworkdb.NewConfiguredPools(context.Background())
	if err != nil {
		return GatewayServer{}, fmt.Errorf("open Gateway database pools: %w", err)
	}
	keepPools := false
	defer func() {
		if !keepPools {
			_ = pools.Close()
		}
	}()
	accountPool, err := pools.Account()
	if err != nil {
		return GatewayServer{}, fmt.Errorf("get Gateway account pool: %w", err)
	}
	credentials, err := gatewaydb.NewAccountCredentialRepository(accountPool)
	if err != nil {
		return GatewayServer{}, fmt.Errorf("configure Gateway credential repository: %w", err)
	}
	notices, err := gatewaydb.NewNoticeRepository(accountPool)
	if err != nil {
		return GatewayServer{}, fmt.Errorf("configure Gateway notice repository: %w", err)
	}
	shards, err := gatewaydb.NewShardRepository(accountPool)
	if err != nil {
		return GatewayServer{}, fmt.Errorf("configure Gateway shard repository: %w", err)
	}
	admissionStore, err := handler.NewAccountPoolGatewayAdmissionStore(accountPool)
	if err != nil {
		return GatewayServer{}, fmt.Errorf("configure Gateway admission repository: %w", err)
	}

	s := server.NewEngine(
		viper.GetString(config.GatewayHost),
		viper.GetInt(config.GatewayPort),
		network.EncodingOptions{
			None:         false,
			Disabled:     false,
			Encryption:   true,
			EDC:          true,
			KeyExchange:  true,
			KeyChallenge: false,
		},
	)
	s.ModuleID = viper.GetString(config.GatewayModuleId)
	s.SetTrafficFilter(server.TrafficFilterOptions{
		Window:      time.Duration(viper.GetInt(config.GatewayTrafficWindowMS)) * time.Millisecond,
		PacketLimit: uint32(viper.GetUint(config.GatewayTrafficPacketLimit)),
	})
	s.SetAdmissionPolicy(server.AllowListedAdmissionPolicy(
		opcode.PatchRequest,
		opcode.ShardlistRequest,
		opcode.LoginRequest,
		opcode.NoticeRequest,
		opcode.ShardlistPing,
	))
	handler.RegisterPatchRequestHandler(&s)
	handler.RegisterShardlistRequestHandlerWithReadiness(&s, shards, installer)
	handler.RegisterShardlistPingHandler(&s)
	handler.RegisterNoticeRequestHandlerWithRepository(&s, notices)
	loginHandler := handler.RegisterLoginRequestHandlerWithCredentialRepository(&s, installer, credentials)
	loginHandler.AdmissionEvaluator = handler.NewGatewayAdmissionEvaluator(admissionStore)
	loginHandler.InstallTimeout = installTimeout
	loginHandler.AttemptTracker = handler.NewLoginAttemptTracker(loginAttemptTrackerOptionsFromConfig())
	keepPools = true
	return NewGatewayServerWithDependencies(GatewayDependencies{
		Server:       &s,
		LoginHandler: loginHandler,
		Pools:        pools,
	})
}

func loginAttemptTrackerOptionsFromConfig() handler.LoginAttemptTrackerOptions {
	return handler.LoginAttemptTrackerOptions{
		Window:       positiveSecondsDuration(viper.GetInt(config.LoginAttemptWindowSeconds)),
		AccountLimit: positiveUint32(viper.GetInt(config.LoginAccountAttemptLimit)),
		IPLimit:      positiveUint32(viper.GetInt(config.LoginIPAttemptLimit)),
		Capacity:     viper.GetInt(config.LoginAttemptTrackerCapacity),
	}
}

func positiveUint32(value int) uint32 {
	if value <= 0 || uint64(value) > uint64(^uint32(0)) {
		return 0
	}
	return uint32(value)
}

func positiveSecondsDuration(seconds int) time.Duration {
	if seconds <= 0 || int64(seconds) > int64(^uint64(0)>>1)/int64(time.Second) {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (g *GatewayServer) Start() {
	if g == nil || g.Server == nil {
		return
	}
	g.startOnce.Do(func() {
		go func() {
			if err := g.Server.Start(); err != nil {
				if g.packetContext == nil || g.packetContext.Err() == nil {
					log.Errorf("Gateway listener stopped unexpectedly: %v", err)
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if shutdownErr := g.Shutdown(shutdownCtx); shutdownErr != nil {
					log.Errorf("Gateway shutdown after listener failure did not fully drain: %v", shutdownErr)
				}
			}
		}()
		go g.handlePackets(g.packetContext)
	})
}

// Close retains the historical no-context API while using a bounded graceful
// drain. New lifecycle owners should call Shutdown with their own deadline.
func (g *GatewayServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return g.Shutdown(ctx)
}

// Shutdown stops packet intake and public accepts, drains the sessions that
// were already admitted, and only then closes the long-lived database pools.
// This ordering prevents an in-flight login handler from querying a closed
// pool or being cut off before its session cleanup has completed.
func (g *GatewayServer) Shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if g.cancelPacketHandling != nil {
		g.cancelPacketHandling()
	}
	var listenerErr error
	if g.Server != nil {
		listenerErr = g.Server.Shutdown(ctx)
	}
	poolErr := g.closePools()
	return errors.Join(listenerErr, poolErr)
}

func (g *GatewayServer) closePools() error {
	if g == nil || g.Pools == nil {
		return nil
	}
	return g.Pools.Close()
}

func (g *GatewayServer) handlePackets(ctx context.Context) {
	if g == nil || g.Server == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-g.Server.PacketChannel:
			switch data.MessageID {
			default:
				log.Debugf("Unhandled packet opcode=0x%04X size=%d", data.MessageID, data.Packet.PayloadLen())
			}
		}
	}
}

func main() {
	config.Initialize()
	logging.Init()
	boot.SetPhases("packethandler", "server")
	reader := bufio.NewReader(os.Stdin)
	shutdownSignal, stopSignalHandling := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignalHandling()

	gw, err := NewGatewayServer()
	if err != nil {
		log.Fatalf("Gateway admission mTLS startup failed: %v", err)
	}
	startGameServer := func() {
		gw.Start()
	}

	boot.RegisterComponent("server", startGameServer, 1)
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
		log.Info("shutdown signal received; draining Gateway server")
	case <-exitRequested:
		log.Info("interactive shutdown requested; draining Gateway server")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := gw.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Gateway shutdown did not fully drain: %v", err)
	}
}
