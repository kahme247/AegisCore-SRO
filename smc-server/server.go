package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/logging"
	smcconfig "github.com/Harvi8/AegisCore/smc-server/config"
	log "github.com/sirupsen/logrus"
)

type SMCServer struct {
	httpServer *http.Server
	listener   net.Listener
	pools      *frameworkdb.Pools
	audit      *JSONLAuditRecorder
}

func NewSMCServer(settings smcconfig.Settings) (*SMCServer, error) {
	startupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pools, err := frameworkdb.NewConfiguredPools(startupContext)
	if err != nil {
		return nil, fmt.Errorf("open SMC database pools: %w", err)
	}
	keepPools := false
	defer func() {
		if !keepPools {
			_ = pools.Close()
		}
	}()
	accountPool, err := pools.Account()
	if err != nil {
		return nil, fmt.Errorf("get SMC account pool: %w", err)
	}
	shardPool, err := pools.Shard()
	if err != nil {
		return nil, fmt.Errorf("get SMC shard pool: %w", err)
	}
	dashboard, err := NewDashboardRepository(accountPool, shardPool, settings.GatewayAddress, settings.AgentAddress)
	if err != nil {
		return nil, err
	}
	directory, err := NewDirectoryRepository(accountPool, shardPool)
	if err != nil {
		return nil, err
	}
	var livePlayers LivePlayerSource
	var disconnects PlayerDisconnectSource
	var broadcasts BroadcastSource
	var schedules ScheduleSource
	if settings.AgentManagementEnabled {
		managementClient, clientErr := NewAgentManagementClient(settings)
		err = clientErr
		if err != nil {
			return nil, err
		}
		livePlayers = managementClient
		disconnects = managementClient
		broadcasts = managementClient
		schedules = managementClient
	}
	audit, err := OpenJSONLAuditRecorder(settings.AuditPath)
	if err != nil {
		return nil, err
	}
	keepAudit := false
	defer func() {
		if !keepAudit {
			_ = audit.Close()
		}
	}()
	app, err := NewApp(settings, dashboard, directory, livePlayers, disconnects, broadcasts, audit)
	if err != nil {
		return nil, err
	}
	app.schedules = schedules
	listener, err := net.Listen("tcp", net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port)))
	if err != nil {
		return nil, fmt.Errorf("bind SMC Server listener: %w", err)
	}
	keepPools = true
	keepAudit = true
	return &SMCServer{
		listener: listener, pools: pools, audit: audit,
		httpServer: &http.Server{
			Handler:           app,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    16 << 10,
		},
	}, nil
}

func (s *SMCServer) Serve() error {
	if s == nil || s.httpServer == nil || s.listener == nil {
		return fmt.Errorf("SMC Server is not configured")
	}
	log.Infof("SMC Server listening on http://%s", s.listener.Addr())
	err := s.httpServer.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *SMCServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var serverErr error
	if s.httpServer != nil {
		serverErr = s.httpServer.Shutdown(ctx)
	}
	var poolErr error
	if s.pools != nil {
		poolErr = s.pools.Close()
	}
	var auditErr error
	if s.audit != nil {
		auditErr = s.audit.Close()
	}
	return errors.Join(serverErr, poolErr, auditErr)
}

func main() {
	smcconfig.Initialize()
	logging.Init()
	settings, err := smcconfig.Load()
	if err != nil {
		log.Fatal(err)
	}
	server, err := NewSMCServer(settings)
	settings.AdminPassword = ""
	if err != nil {
		log.Fatalf("SMC Server startup failed: %v", err)
	}
	shutdownSignal, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve() }()
	select {
	case <-shutdownSignal.Done():
		log.Info("shutdown signal received; stopping SMC Server")
	case err := <-serveResult:
		if err != nil {
			log.Errorf("SMC Server listener stopped unexpectedly: %v", err)
		}
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Errorf("SMC Server shutdown did not complete cleanly: %v", err)
	}
}
