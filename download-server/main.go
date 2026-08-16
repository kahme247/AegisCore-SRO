package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	downloadconfig "github.com/Harvi8/AegisCore/download-server/config"
	"github.com/Harvi8/AegisCore/framework/logging"
	log "github.com/sirupsen/logrus"
)

func main() {
	downloadconfig.Initialize()
	logging.Init()
	download, err := NewDownloadServer()
	if err != nil {
		log.Fatalf("DownloadServer startup failed: %v", err)
	}

	listenerErr := make(chan error, 1)
	go func() { listenerErr <- download.Start() }()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signalContext.Done():
		log.Info("shutdown signal received; draining DownloadServer")
	case err := <-listenerErr:
		if err != nil {
			log.Errorf("DownloadServer listener stopped unexpectedly: %v", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := download.Shutdown(shutdownContext); err != nil {
		log.Errorf("DownloadServer shutdown did not fully drain: %v", err)
	}
}
