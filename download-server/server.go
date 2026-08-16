package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Harvi8/AegisCore/download-server/catalog"
	downloadconfig "github.com/Harvi8/AegisCore/download-server/config"
	"github.com/Harvi8/AegisCore/download-server/handler"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/spf13/viper"
)

type DownloadServer struct {
	Server       *server.Server
	healthServer *http.Server
	ready        atomic.Bool
}

func NewDownloadServer() (*DownloadServer, error) {
	if err := downloadconfig.Validate(); err != nil {
		return nil, fmt.Errorf("validate DownloadServer configuration: %w", err)
	}
	files, err := catalog.Load(
		viper.GetString(downloadconfig.ManifestPath),
		viper.GetString(downloadconfig.StorageRoot),
	)
	if err != nil {
		return nil, fmt.Errorf("load DownloadServer catalog: %w", err)
	}
	return newDownloadServer(files)
}

func newDownloadServer(files handler.PreparedFileCatalog) (*DownloadServer, error) {
	limiter, err := handler.NewTransferLimiter(viper.GetInt(downloadconfig.MaxTransfers), viper.GetInt(downloadconfig.MaxTransfersPerIP))
	if err != nil {
		return nil, err
	}
	engine := server.NewEngine(
		viper.GetString(downloadconfig.Host),
		viper.GetInt(downloadconfig.Port),
		network.EncodingOptions{
			Encryption:  true,
			EDC:         true,
			KeyExchange: true,
		},
	)
	engine.ModuleID = viper.GetString(downloadconfig.ModuleID)
	engine.SetTrafficFilter(server.TrafficFilterOptions{
		Window:      time.Duration(viper.GetInt(downloadconfig.TrafficWindowMS)) * time.Millisecond,
		PacketLimit: uint32(viper.GetUint(downloadconfig.TrafficPacketLimit)),
	})
	engine.SetAdmissionPolicy(server.AllowListedAdmissionPolicy(opcode.DownloadFileRequest))
	engine.SetModuleIdentifiedHandler(sendReady)
	requestHandler := &handler.FileRequestHandler{
		Catalog:   files,
		ChunkSize: viper.GetInt(downloadconfig.ChunkSize),
		Limiter:   limiter,
		Audit:     handler.LogTransferAudit,
	}
	engine.RegisterHandler(opcode.DownloadFileRequest, requestHandler.Handle)
	download := &DownloadServer{Server: &engine}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		if !download.ready.Load() {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ready\n"))
	})
	download.healthServer = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return download, nil
}

func sendReady(_ context.Context, session *server.Session) error {
	idleTimeout := time.Duration(viper.GetInt(downloadconfig.SessionIdleSeconds)) * time.Second
	session.SetIdleTimeout(idleTimeout)
	if session.Conn != nil {
		if err := session.Conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return fmt.Errorf("extend DownloadServer session deadline: %w", err)
		}
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.DownloadReady
	return session.Send(packet)
}

func (d *DownloadServer) Start() error {
	if d == nil || d.Server == nil {
		return fmt.Errorf("DownloadServer is not configured")
	}
	protocolListener, err := d.Server.Listen()
	if err != nil {
		return err
	}
	healthAddress := net.JoinHostPort(viper.GetString(downloadconfig.HealthHost), fmt.Sprintf("%d", viper.GetInt(downloadconfig.HealthPort)))
	healthListener, err := net.Listen("tcp4", healthAddress)
	if err != nil {
		_ = d.Server.CloseListener()
		return fmt.Errorf("listen on DownloadServer health endpoint: %w", err)
	}
	d.ready.Store(true)
	healthErrors := make(chan error, 1)
	go func() {
		err := d.healthServer.Serve(healthListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		healthErrors <- err
	}()
	protocolErrors := make(chan error, 1)
	go func() { protocolErrors <- d.Server.Serve(protocolListener) }()
	select {
	case err := <-healthErrors:
		d.ready.Store(false)
		_ = d.Server.StopAccepting()
		return err
	case err := <-protocolErrors:
		d.ready.Store(false)
		_ = d.healthServer.Close()
		return err
	}
}

func (d *DownloadServer) Shutdown(ctx context.Context) error {
	if d == nil || d.Server == nil {
		return nil
	}
	d.ready.Store(false)
	var healthErr error
	if d.healthServer != nil {
		healthErr = d.healthServer.Shutdown(ctx)
	}
	return errors.Join(d.Server.Shutdown(ctx), healthErr)
}
